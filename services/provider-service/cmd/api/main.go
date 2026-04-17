package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	natsgo "github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/BuzzLyutic/payment-gateway-microservices/services/provider-service/internal/adapter"
	"github.com/BuzzLyutic/payment-gateway-microservices/services/provider-service/internal/circuitbreaker"
	"github.com/BuzzLyutic/payment-gateway-microservices/services/provider-service/internal/config"
	"github.com/BuzzLyutic/payment-gateway-microservices/services/provider-service/internal/consumer"
	"github.com/BuzzLyutic/payment-gateway-microservices/services/provider-service/internal/events"
	"github.com/BuzzLyutic/payment-gateway-microservices/services/provider-service/internal/handler"
	"github.com/BuzzLyutic/payment-gateway-microservices/services/provider-service/internal/middleware"
	"github.com/BuzzLyutic/payment-gateway-microservices/services/provider-service/internal/publisher"
	"github.com/BuzzLyutic/payment-gateway-microservices/services/provider-service/internal/repository"
	"github.com/BuzzLyutic/payment-gateway-microservices/services/provider-service/internal/router"
	"github.com/BuzzLyutic/payment-gateway-microservices/services/provider-service/internal/service"
)

func main() {
	cfg := config.Load()

	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: cfg.LogLevel,
	}))
	slog.SetDefault(logger)

	slog.Info("starting provider service",
		"port", cfg.Server.Port,
		"log_level", cfg.LogLevel.String(),
	)

	ctx := context.Background()

	// Redis для Thompson Sampling
	redisStore := router.NewStore(cfg.Redis.Addr, cfg.Redis.Password, cfg.Redis.DB)
	if err := redisStore.Ping(ctx); err != nil {
		// Не фатально — работаем без персистентности
		slog.Warn("redis unavailable, thompson sampling will not persist stats",
			"addr", cfg.Redis.Addr,
			"error", err,
		)
		redisStore = nil
	} else {
		slog.Info("connected to redis for thompson sampling persistence")
		defer redisStore.Close()
	}

	// Подключение к БД
	repo, err := repository.New(ctx, cfg.Database.DSN())
	if err != nil {
		slog.Error("failed to connect to database", "error", err)
		os.Exit(1)
	}
	defer repo.Close()

	slog.Info("connected to database")

	// Загружаем всех активных провайдеров для регистрации адаптеров.
	// Используем пустые строки - получаем всех без фильтра по валюте/методу.
	providers, err := repo.FindAll(ctx)
	if err != nil {
		slog.Error("failed to load providers", "error", err)
		os.Exit(1)
	}

	// Registry - регистрируем адаптер для каждого провайдера
	registry := adapter.NewRegistry()
	providerNames := make([]string, 0, len(providers))
	for _, p := range providers {
		providerNames = append(providerNames, p.Name)
		switch p.Type {
		case "mock":
			registry.Register(p.Name, adapter.NewMockAdapter(p.Config))
			slog.Info("registered mock adapter", "provider", p.Name)
		default:
			slog.Warn("unknown provider type, skipping", "provider", p.Name, "type", p.Type)
		}
	}

	// NATS JetStream
	js, nc, err := setupJetStream(ctx, cfg.NATS.URL)
	if err != nil {
		slog.Error("failed to setup NATS", "error", err)
		os.Exit(1)
	}
	defer nc.Close()
	slog.Info("connected to NATS")


	// Thompson Sampling Router с персистентностью
	var thompsonRouter *router.Router
	if redisStore != nil {
		thompsonRouter = router.NewRouterWithStore(redisStore)
	} else {
		thompsonRouter = router.NewRouter()
	}

	// Загружаем сохранённую статистику при старте
	if err := thompsonRouter.LoadFromStore(ctx, providerNames); err != nil {
		// Не фатально — начинаем с априорного Beta(1,1)
		slog.Warn("failed to load thompson sampling stats, starting fresh",
			"error", err,
		)
	}

	cbManager := circuitbreaker.NewManager(
    	circuitbreaker.DefaultConfig(),
    	thompsonRouter.OnHalfOpen, // колбэк: CB → Thompson Sampling
	)

	// Инициализируем метрики CB для всех провайдеров сразу при старте
	// Без этого gauge появляется только после первого перехода состояния
	for _, p := range providers {
    	cbManager.InitMetrics(p.Name)
	}

	svc := service.New(repo, registry, thompsonRouter, cbManager)
	pub := publisher.New(js)

	// Роутер
	mux := http.NewServeMux()
	handler.NewHealthHandler(repo).Register(mux)
	handler.NewProcessHandler(svc).Register(mux)
	mux.Handle("GET /metrics", promhttp.Handler())

	// Middleware chain: Recover → RequestID → Logging → Handler
	var h http.Handler = mux
	h = middleware.Metrics(h)
	h = middleware.Logging(h)
	h = middleware.RequestID(h)
	h = middleware.Recover(h)

	srv := &http.Server{
		Addr:         ":" + cfg.Server.Port,
		Handler:      h,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	// Контекст для фоновых горутин
	bgCtx, bgCancel := context.WithCancel(ctx)
	defer bgCancel()

	// Consumer — слушает payment.created
	paymentConsumer := consumer.New(svc, pub)
	go func() {
		if err := paymentConsumer.Start(bgCtx, js); err != nil {
			slog.Error("consumer error", "error", err)
		}
	}()

	// HTTP сервер
	go func() {
		slog.Info("server started", "addr", srv.Addr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("server failed", "error", err)
			os.Exit(1)
		}
	}()

	// Graceful shutdown
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	sig := <-quit

	slog.Info("received shutdown signal", "signal", sig.String())

	bgCancel()
	
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		slog.Error("server forced to shutdown", "error", err)
		os.Exit(1)
	}

	slog.Info("server stopped gracefully")
}

// setupJetStream подключается к NATS и создаёт Stream PAYMENTS.
func setupJetStream(ctx context.Context, natsURL string) (jetstream.JetStream, *natsgo.Conn, error) {
	nc, err := natsgo.Connect(natsURL)
	if err != nil {
		return nil, nil, fmt.Errorf("nats connect: %w", err)
	}

	js, err := jetstream.New(nc)
	if err != nil {
		nc.Close()
		return nil, nil, fmt.Errorf("jetstream new: %w", err)
	}

	_, err = js.CreateOrUpdateStream(ctx, jetstream.StreamConfig{
		Name: events.StreamName,
		Subjects: []string{
			events.SubjectPaymentCreated,
			events.SubjectPaymentCompleted,
			events.SubjectPaymentRiskApproved,
			events.SubjectPaymentRiskBlocked,
		},
		Storage:   jetstream.FileStorage,
		Retention: jetstream.WorkQueuePolicy,
		MaxAge:    72 * time.Hour,
	})
	if err != nil {
		nc.Close()
		return nil, nil, fmt.Errorf("create stream: %w", err)
	}

	return js, nc, nil
}
