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

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"

	"github.com/BuzzLyutic/payment-gateway-microservices/services/provider-service/internal/adapter"
	"github.com/BuzzLyutic/payment-gateway-microservices/services/provider-service/internal/config"
	"github.com/BuzzLyutic/payment-gateway-microservices/services/provider-service/internal/consumer"
	"github.com/BuzzLyutic/payment-gateway-microservices/services/provider-service/internal/events"
	"github.com/BuzzLyutic/payment-gateway-microservices/services/provider-service/internal/handler"
	"github.com/BuzzLyutic/payment-gateway-microservices/services/provider-service/internal/middleware"
	"github.com/BuzzLyutic/payment-gateway-microservices/services/provider-service/internal/publisher"
	"github.com/BuzzLyutic/payment-gateway-microservices/services/provider-service/internal/repository"
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
	for _, p := range providers {
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

	svc := service.New(repo, registry)
	pub := publisher.New(js)

	// Роутер
	mux := http.NewServeMux()
	handler.NewHealthHandler(repo).Register(mux)
	handler.NewProcessHandler(svc).Register(mux)

	// Middleware chain: Recover → RequestID → Logging → Handler
	var h http.Handler = mux
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
func setupJetStream(ctx context.Context, natsURL string) (jetstream.JetStream, *nats.Conn, error) {
	nc, err := nats.Connect(natsURL)
	if err != nil {
		return nil, nil, fmt.Errorf("nats connect: %w", err)
	}

	js, err := jetstream.New(nc)
	if err != nil {
		nc.Close()
		return nil, nil, fmt.Errorf("jetstream new: %w", err)
	}

	_, err = js.CreateOrUpdateStream(ctx, jetstream.StreamConfig{
		Name:      events.StreamName,
		Subjects:  []string{events.SubjectPaymentCreated, events.SubjectPaymentCompleted},
		Storage:   jetstream.FileStorage,
		Retention: jetstream.WorkQueuePolicy,
	})
	if err != nil {
		nc.Close()
		return nil, nil, fmt.Errorf("create stream: %w", err)
	}

	return js, nc, nil
}
