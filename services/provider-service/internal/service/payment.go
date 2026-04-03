package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math/rand"
	"time"

	"github.com/BuzzLyutic/payment-gateway-microservices/services/provider-service/internal/adapter"
	"github.com/BuzzLyutic/payment-gateway-microservices/services/provider-service/internal/domain"
)

// Repository - интерфейс для доступа к данным провайдеров.
type Repository interface {
	FindActive(ctx context.Context, currency, paymentMethod string) ([]*domain.Provider, error)
}

// Service - бизнес-логика обработки платежей.
type Service struct {
	repo       Repository
	registry   *adapter.Registry
	maxRetries int
	baseDelay  time.Duration
}

func New(repo Repository, registry *adapter.Registry) *Service {
	return &Service{
		repo:       repo,
		registry:   registry,
		maxRetries: 3,
		baseDelay:  100 * time.Millisecond,
	}
}

// ProcessPayment обрабатывает платёж: фильтрация - routing - вызов адаптера с retry.
func (s *Service) ProcessPayment(ctx context.Context, req *domain.ProcessRequest) (*domain.ProcessResult, error) {
	// 1. Фильтрация
	providers, err := s.repo.FindActive(ctx, req.Currency, req.PaymentMethod)
	if err != nil {
		return nil, fmt.Errorf("find providers: %w", err)
	}

	if len(providers) == 0 {
		slog.Warn("no providers available",
			"currency", req.Currency,
			"payment_method", req.PaymentMethod,
			"transaction_id", req.TransactionID,
		)
		return &domain.ProcessResult{
			TransactionID: req.TransactionID,
			Status:        domain.ResultFailed,
			ErrorMessage:  domain.ErrNoProviderAvailable.Error(),
		}, nil
	}

	// 2. Routing - MVP: первый из списка
	selected := providers[0]

	slog.Info("provider selected",
		"provider", selected.Name,
		"transaction_id", req.TransactionID,
		"candidates", len(providers),
	)

	// 3. Получаем адаптер
	pa, err := s.registry.Get(selected.Name)
	if err != nil {
		return nil, fmt.Errorf("get adapter: %w", err)
	}

	// 4. Вызов с retry и замером латентности
	start := time.Now()
	result, err := s.callWithRetry(ctx, pa, req, selected.Name)
	latency := time.Since(start).Milliseconds()

	if err != nil {
		slog.Error("provider call failed after retries",
			"provider", selected.Name,
			"transaction_id", req.TransactionID,
			"error", err,
		)
		return &domain.ProcessResult{
			TransactionID: req.TransactionID,
			Provider:      selected.Name,
			Status:        domain.ResultFailed,
			ErrorMessage:  err.Error(),
			LatencyMs:     latency,
		}, nil
	}

	slog.Info("payment processed",
		"provider", selected.Name,
		"transaction_id", req.TransactionID,
		"status", result.Status,
		"latency_ms", latency,
	)

	return &domain.ProcessResult{
		TransactionID: req.TransactionID,
		Provider:      selected.Name,
		ProviderTxID:  result.ProviderTxID,
		Status:        result.Status,
		ErrorMessage:  result.ErrorMessage,
		LatencyMs:     latency,
	}, nil
}

// callWithRetry вызывает адаптер с exponential backoff + jitter.
// Retry только при transient-ошибках. Decline не ретраится.
func (s *Service) callWithRetry(ctx context.Context, pa adapter.PaymentAdapter, req *domain.ProcessRequest, providerName string) (*adapter.AdapterResult, error) {
	var lastErr error

	for attempt := 0; attempt <= s.maxRetries; attempt++ {
		if attempt > 0 {
			delay := s.backoffWithJitter(attempt)

			slog.Warn("retrying provider call",
				"provider", providerName,
				"transaction_id", req.TransactionID,
				"attempt", attempt,
				"delay", delay,
			)

			select {
			case <-time.After(delay):
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}

		result, err := pa.ProcessPayment(ctx, req)
		if err == nil {
			return result, nil
		}

		if !errors.Is(err, adapter.ErrTransient) {
			return nil, err
		}

		lastErr = err
	}

	return nil, fmt.Errorf("max retries (%d) exceeded: %w", s.maxRetries, lastErr)
}

// backoffWithJitter возвращает задержку: base * 2^(attempt-1) ± 25%.
// attempt 1 - ~100ms, attempt 2 - ~200ms, attempt 3 - ~400ms.
func (s *Service) backoffWithJitter(attempt int) time.Duration {
	backoff := s.baseDelay * (1 << uint(attempt-1))

	// Jitter: ±25%
	jitter := time.Duration(rand.Int63n(int64(backoff)/2)) - time.Duration(int64(backoff)/4)

	return backoff + jitter
}
