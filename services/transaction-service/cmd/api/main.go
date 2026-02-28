package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
	"github.com/BuzzLyutic/payment-gateway-microservices/services/transaction-service/internal/handler"
	"github.com/BuzzLyutic/payment-gateway-microservices/services/transaction-service/internal/config"
	"github.com/BuzzLyutic/payment-gateway-microservices/services/transaction-service/internal/repository"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	}))
	slog.SetDefault(logger)

	// Загрузка конфигурации
	cfg := config.Load()

	// Подключение к БД
	ctx := context.Background()
	repo, err := repository.New(ctx, cfg.Database.DSN())
	if err != nil {
		slog.Error("failed to connect to database", "error", err)
		os.Exit(1)
	}
	defer repo.Close()

	// Роутер
	mux := http.NewServeMux()

	// Объявление и регистрация хэндлера
	healthHandler := handler.NewHealthHandler(repo)
	healthHandler.Register(mux)

	srv := &http.Server{
		Addr: ":" + cfg.Server.Port,
		Handler: mux,
		ReadTimeout: 10 * time.Second,
		WriteTimeout: 10 * time.Second,
		IdleTimeout: 60 * time.Second,
	}

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

	ctx, cancel := context.WithTimeout(context.Background(), 5 * time.Second)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		slog.Error("server forced to shutdown", "error", err)
		os.Exit(1)
	}
	slog.Info("server stopped gracefully")
}
