package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/BuzzLyutic/payment-gateway-microservices/services/provider-service/internal/adapter"
	"github.com/BuzzLyutic/payment-gateway-microservices/services/provider-service/internal/config"
	"github.com/BuzzLyutic/payment-gateway-microservices/services/provider-service/internal/handler"
	"github.com/BuzzLyutic/payment-gateway-microservices/services/provider-service/internal/middleware"
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

	svc := service.New(repo, registry)

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

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		slog.Error("server forced to shutdown", "error", err)
		os.Exit(1)
	}

	slog.Info("server stopped gracefully")
}
