package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/BuzzLyutic/payment-gateway-microservices/services/api-gateway/internal/auth"
	"github.com/BuzzLyutic/payment-gateway-microservices/services/api-gateway/internal/config"
	"github.com/BuzzLyutic/payment-gateway-microservices/services/api-gateway/internal/health"
	"github.com/BuzzLyutic/payment-gateway-microservices/services/api-gateway/internal/middleware"
	"github.com/BuzzLyutic/payment-gateway-microservices/services/api-gateway/internal/proxy"
	"github.com/redis/go-redis/v9"
)

func main() {
	// Logger
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	}))
	slog.SetDefault(logger)

	logger.Info("starting api-gateway")

	// Config
	cfg, err := config.Load()
	if err != nil {
		logger.Error("failed to load config", slog.String("error", err.Error()))
		os.Exit(1)
	}

	// Redis
	rdb, err := connectRedis(cfg.RedisURL, logger)
	if err != nil {
		logger.Error("failed to connect to redis", slog.String("error", err.Error()))
		os.Exit(1)
	}
	defer rdb.Close()

	// Wiring
	authStore := auth.NewStore(rdb, cfg.DefaultRateLimit)

	txProxy, err := proxy.New(cfg.TransactionServiceURL, logger)
	if err != nil {
		logger.Error("failed to create proxy",
			slog.String("url", cfg.TransactionServiceURL),
			slog.String("error", err.Error()),
		)
		os.Exit(1)
	}

	// Router
	mux := http.NewServeMux()

	// Health — без аутентификации, без rate limiting.
	mux.Handle("GET /health", health.NewHandler(rdb, logger))

	// Защищённые маршруты — полная цепочка middleware.
	// Порядок: Recovery → RequestID → Logging → Auth → RateLimit → Proxy
	protected := chain(
		middleware.Recovery(logger),
		middleware.RequestID,
		middleware.Logging(logger),
		middleware.Auth(authStore, logger),
		middleware.RateLimit(rdb, logger),
	)

	mux.Handle("POST /api/v1/payments", protected(txProxy))
	mux.Handle("GET /api/v1/payments/{id}", protected(txProxy))

	// HTTP Server
	server := &http.Server{
		Addr:    ":" + cfg.Port,
		Handler: mux,
		// ReadTimeout — максимальное время чтения запроса от клиента.
		ReadTimeout: 5 * time.Second,
		// WriteTimeout — максимальное время записи ответа клиенту.
		// Больше proxyTimeout (25s) чтобы успеть вернуть 502/504.
		WriteTimeout: 30 * time.Second,
		// IdleTimeout — время keep-alive соединения без запросов.
		IdleTimeout: 60 * time.Second,
	}

	// Graceful Shutdown
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	go func() {
		logger.Info("api-gateway listening", slog.String("addr", server.Addr))
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("server error", slog.String("error", err.Error()))
			os.Exit(1)
		}
	}()

	<-ctx.Done()
	logger.Info("shutting down api-gateway")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := server.Shutdown(shutdownCtx); err != nil {
		logger.Error("shutdown error", slog.String("error", err.Error()))
	}

	logger.Info("api-gateway stopped")
}

// chain применяет middleware справа налево —
// первый в списке оборачивает снаружи, последний вызывается перед handler.
func chain(middlewares ...func(http.Handler) http.Handler) func(http.Handler) http.Handler {
	return func(final http.Handler) http.Handler {
		for i := len(middlewares) - 1; i >= 0; i-- {
			final = middlewares[i](final)
		}
		return final
	}
}

// connectRedis подключается к Redis и проверяет доступность через Ping.
func connectRedis(redisURL string, logger *slog.Logger) (*redis.Client, error) {
	opt, err := redis.ParseURL(redisURL)
	if err != nil {
		return nil, err
	}

	rdb := redis.NewClient(opt)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := rdb.Ping(ctx).Err(); err != nil {
		return nil, err
	}

	logger.Info("redis connected", slog.String("addr", opt.Addr))
	return rdb, nil
}
