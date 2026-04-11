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

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/redis/go-redis/v9"
	"github.com/BuzzLyutic/payment-gateway-microservices/services/risk-service/internal/config"
	"github.com/BuzzLyutic/payment-gateway-microservices/services/risk-service/internal/consumer"
	"github.com/BuzzLyutic/payment-gateway-microservices/services/risk-service/internal/engine"
	"github.com/BuzzLyutic/payment-gateway-microservices/services/risk-service/internal/evaluator"
	"github.com/BuzzLyutic/payment-gateway-microservices/services/risk-service/internal/health"
	"github.com/BuzzLyutic/payment-gateway-microservices/services/risk-service/internal/loader"
	"github.com/BuzzLyutic/payment-gateway-microservices/services/risk-service/internal/publisher"
)

func main() {
	// Logger
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	}))
	slog.SetDefault(logger)

	logger.Info("starting risk-service")

	// Config
	cfg, err := config.Load()
	if err != nil {
		logger.Error("failed to load config", slog.String("error", err.Error()))
		os.Exit(1)
	}

	// Rules
	rules, err := loader.Load(cfg.RulesPath)
	if err != nil {
		logger.Error("failed to load rules",
			slog.String("path", cfg.RulesPath),
			slog.String("error", err.Error()),
		)
		os.Exit(1)
	}
	logger.Info("rules loaded", slog.Int("count", len(rules)))

	// Redis
	rdb, err := connectRedis(cfg.RedisURL, logger)
	if err != nil {
		logger.Error("failed to connect to redis", slog.String("error", err.Error()))
		os.Exit(1)
	}
	defer rdb.Close()

	// NATS
	nc, js, err := connectNATS(cfg.NatsURL, logger)
	if err != nil {
		logger.Error("failed to connect to nats", slog.String("error", err.Error()))
		os.Exit(1)
	}
	defer nc.Drain()

	// Wiring
	eng := engine.New(rules, rdb, logger)
	eval := evaluator.New(eng, logger)
	pub := publisher.New(js, logger)
	cons := consumer.New(js, eval, pub, logger)

	// HTTP (health only)
	healthHandler := health.NewHandler(rdb, nc, logger)
	httpServer := &http.Server{
		Addr:         ":" + cfg.Port,
		Handler:      http.NewServeMux(),
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 5 * time.Second,
		IdleTimeout:  30 * time.Second,
	}
	mux := http.NewServeMux()
	mux.Handle("/health", healthHandler)
	httpServer.Handler = mux

	// Graceful shutdown
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// HTTP в отдельной горутине
	go func() {
		logger.Info("http server listening", slog.String("addr", httpServer.Addr))
		if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("http server error", slog.String("error", err.Error()))
		}
	}()

	// Consumer блокирует до отмены ctx
	if err := cons.Start(ctx); err != nil && !errors.Is(err, context.Canceled) {
		logger.Error("consumer error", slog.String("error", err.Error()))
	}

	// Даём HTTP-серверу время завершить in-flight запросы
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		logger.Error("http shutdown error", slog.String("error", err.Error()))
	}

	logger.Info("risk-service stopped")
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

// connectNATS подключается к NATS и инициализирует JetStream.
func connectNATS(natsURL string, logger *slog.Logger) (*nats.Conn, jetstream.JetStream, error) {
	nc, err := nats.Connect(natsURL,
		nats.RetryOnFailedConnect(true),
		nats.MaxReconnects(10),
		nats.ReconnectWait(2*time.Second),
		nats.DisconnectErrHandler(func(nc *nats.Conn, err error) {
			if err != nil {
				logger.Warn("nats disconnected", slog.String("error", err.Error()))
			}
		}),
		nats.ReconnectHandler(func(nc *nats.Conn) {
			logger.Info("nats reconnected", slog.String("url", nc.ConnectedUrl()))
		}),
	)
	if err != nil {
		return nil, nil, err
	}

	js, err := jetstream.New(nc)
	if err != nil {
		nc.Close()
		return nil, nil, err
	}

	logger.Info("nats connected", slog.String("url", natsURL))
	return nc, js, nil
}
