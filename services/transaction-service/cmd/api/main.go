package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/BuzzLyutic/payment-gateway-microservices/services/transaction-service/internal/config"
	"github.com/BuzzLyutic/payment-gateway-microservices/services/transaction-service/internal/handler"
	"github.com/BuzzLyutic/payment-gateway-microservices/services/transaction-service/internal/idempotency"
	"github.com/BuzzLyutic/payment-gateway-microservices/services/transaction-service/internal/middleware"
	"github.com/BuzzLyutic/payment-gateway-microservices/services/transaction-service/internal/provider"
	"github.com/BuzzLyutic/payment-gateway-microservices/services/transaction-service/internal/repository"
	"github.com/BuzzLyutic/payment-gateway-microservices/services/transaction-service/internal/service"
	"github.com/BuzzLyutic/payment-gateway-microservices/services/transaction-service/internal/worker"
	"github.com/redis/go-redis/v9"
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

	idempotencyStore := idempotency.NewStore(rdb)
	defer idempotencyStore.Close()

	// Provider + Service
	mockProvider := provider.NewMockProvider()
	txService := service.New(repo, mockProvider)

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

	// Worker - отдельный контекст с cancel для graceful shutdown
	workerCtx, workerCancel := context.WithCancel(ctx)
	defer workerCancel()

	w := worker.New(txService, cfg.Worker.Interval, cfg.Worker.BatchSize)
	go w.Run(workerCtx)

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

	// 1 - Останавливаем воркер - не берёт новые задачи
	workerCancel()

	// 2 - Останавливаем HTTP-сервер - дожидаемся текущих запросов
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		slog.Error("server forced to shutdown", "error", err)
		os.Exit(1)
	}

	slog.Info("server stopped gracefully")
}
