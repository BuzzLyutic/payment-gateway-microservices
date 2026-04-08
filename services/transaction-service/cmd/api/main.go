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
	"github.com/redis/go-redis/v9"

	"github.com/BuzzLyutic/payment-gateway-microservices/services/transaction-service/internal/config"
	"github.com/BuzzLyutic/payment-gateway-microservices/services/transaction-service/internal/consumer"
	"github.com/BuzzLyutic/payment-gateway-microservices/services/transaction-service/internal/events"
	"github.com/BuzzLyutic/payment-gateway-microservices/services/transaction-service/internal/handler"
	"github.com/BuzzLyutic/payment-gateway-microservices/services/transaction-service/internal/idempotency"
	"github.com/BuzzLyutic/payment-gateway-microservices/services/transaction-service/internal/middleware"
	"github.com/BuzzLyutic/payment-gateway-microservices/services/transaction-service/internal/provider"
	"github.com/BuzzLyutic/payment-gateway-microservices/services/transaction-service/internal/publisher"
	"github.com/BuzzLyutic/payment-gateway-microservices/services/transaction-service/internal/repository"
	"github.com/BuzzLyutic/payment-gateway-microservices/services/transaction-service/internal/service"
	"github.com/BuzzLyutic/payment-gateway-microservices/services/transaction-service/internal/worker"
)

func main() {
	// Загрузка конфигурации
	cfg := config.Load()

	// Логгер
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: cfg.LogLevel,
	}))
	slog.SetDefault(logger)

	slog.Info("starting transaction service",
		"port", cfg.Server.Port,
		"log_level", cfg.LogLevel.String(),
	)

	// Подключение к БД
	ctx := context.Background()
	repo, err := repository.New(ctx, cfg.Database.DSN())
	if err != nil {
		slog.Error("failed to connect to database", "error", err)
		os.Exit(1)
	}
	defer repo.Close()

	// Redis
	rdb := redis.NewClient(&redis.Options{
		Addr: cfg.Redis.Addr,
	})

	if err := rdb.Ping(ctx).Err(); err != nil {
		slog.Error("failed to connect to redis", "error", err)
		os.Exit(1)
	}
	slog.Info("connected to Redis")

	// NATS JetStream
	js, nc, err := setupJetStream(ctx, cfg.NATS.URL)
	if err != nil {
		slog.Error("failed to setup NATS", "error", err)
		os.Exit(1)
	}
	defer nc.Close()
	slog.Info("connected to NATS")

	idempotencyStore := idempotency.NewStore(rdb)
	defer idempotencyStore.Close()

	// Provider + Service
	mockProvider := provider.NewMockProvider()
	pub := publisher.New(js)
	txService := service.New(repo, mockProvider, pub)

	// Роутер
	mux := http.NewServeMux()

	// Регистрация хэндлеров
	handler.NewHealthHandler(repo, idempotencyStore).Register(mux)
	handler.NewPaymentHandler(txService, idempotencyStore).Register(mux)

	// Middleware chain: Recover → RequestID → Logging → Handler
	var h http.Handler = mux
	h = middleware.Logging(h)
	h = middleware.RequestID(h)
	h = middleware.Recover(h)


	// HTTP-сервер
	srv := &http.Server{
		Addr: ":" + cfg.Server.Port,
		Handler: h,
		ReadTimeout: 10 * time.Second,
		WriteTimeout: 10 * time.Second,
		IdleTimeout: 60 * time.Second,
	}

	// Контекст для фоновых горутин
	bgCtx, bgCancel := context.WithCancel(ctx)
	defer bgCancel()

	// Worker — публикует payment.created
	w := worker.New(txService, cfg.Worker.Interval, cfg.Worker.BatchSize)
	go w.Run(bgCtx)

	// Consumer — слушает payment.completed
	paymentConsumer := consumer.New(repo)
	go func() {
		if err := paymentConsumer.Start(bgCtx, js); err != nil {
			slog.Error("consumer error", "error", err)
		}
	}()

	go func() {
		slog.Info("starting server", "addr", srv.Addr)
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

	// 2 - Останавливаем HTTP-сервер - дожидаемся текущих запросов
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
