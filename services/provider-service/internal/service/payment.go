package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"
	"math/rand"

	"github.com/BuzzLyutic/payment-gateway-microservices/services/provider-service/internal/adapter"
	"github.com/BuzzLyutic/payment-gateway-microservices/services/provider-service/internal/circuitbreaker"
	"github.com/BuzzLyutic/payment-gateway-microservices/services/provider-service/internal/domain"
	"github.com/BuzzLyutic/payment-gateway-microservices/services/provider-service/internal/router"
)

type Repository interface {
	FindActive(ctx context.Context, currency, paymentMethod string) ([]*domain.Provider, error)
}

type Service struct {
	repo       Repository
	registry   *adapter.Registry
	router     *router.Router
	cb         *circuitbreaker.Manager
	maxRetries int
	baseDelay  time.Duration
}

func New(
	repo Repository,
	registry *adapter.Registry,
	router *router.Router,
	cb *circuitbreaker.Manager,
) *Service {
	return &Service{
		repo:       repo,
		registry:   registry,
		router:     router,
		cb:         cb,
		maxRetries: 3,
		baseDelay:  100 * time.Millisecond,
	}
}

func (s *Service) ProcessPayment(ctx context.Context, req *domain.ProcessRequest) (*domain.ProcessResult, error) {
	// 1. Получаем всех активных провайдеров для валюты и метода
	allProviders, err := s.repo.FindActive(ctx, req.Currency, req.PaymentMethod)
	if err != nil {
		return nil, fmt.Errorf("find providers: %w", err)
	}

	// 2. Фильтрация: исключаем провайдеров с открытым Circuit Breaker
	candidates := make([]*domain.Provider, 0, len(allProviders))
	for _, p := range allProviders {
		if s.cb.IsOpen(p.Name) {
			slog.Warn("provider filtered: circuit breaker open",
				"provider", p.Name,
				"transaction_id", req.TransactionID,
			)
			continue
		}
		candidates = append(candidates, p)
	}

	if len(candidates) == 0 {
		slog.Error("no providers available after CB filtering",
			"transaction_id", req.TransactionID,
			"currency", req.Currency,
			"payment_method", req.PaymentMethod,
			"total_providers", len(allProviders),
		)
		return &domain.ProcessResult{
			TransactionID: req.TransactionID,
			Status:        domain.ResultFailed,
			ErrorMessage:  domain.ErrNoProviderAvailable.Error(),
		}, nil
	}

	// 3. Thompson Sampling выбирает провайдера
	selected, err := s.router.Select(ctx, candidates)
	if err != nil {
		return nil, fmt.Errorf("router select: %w", err)
	}

	slog.Info("provider selected by thompson sampling",
		"provider", selected.Name,
		"transaction_id", req.TransactionID,
		"candidates", len(candidates),
	)

	// 4. Получаем адаптер
	pa, err := s.registry.Get(selected.Name)
	if err != nil {
		return nil, fmt.Errorf("get adapter: %w", err)
	}

	// 5. Проверяем Circuit Breaker перед вызовом
	if err := s.cb.Allow(selected.Name); err != nil {
		// CB открылся между фильтрацией и вызовом — редкий race condition
		slog.Warn("circuit breaker rejected request",
			"provider", selected.Name,
			"transaction_id", req.TransactionID,
		)
		return &domain.ProcessResult{
			TransactionID: req.TransactionID,
			Status:        domain.ResultFailed,
			ErrorMessage:  "provider unavailable",
		}, nil
	}

	// 6. Вызов с retry и замером латентности
	start := time.Now()
	result, err := s.callWithRetry(ctx, pa, req, selected.Name)
	latencyMs := time.Since(start).Milliseconds()

	if err != nil {
		// Transient-ошибки и исчерпание retry — failure для CB и Thompson
		s.cb.RecordFailure(selected.Name)
		s.router.RecordResult(selected.Name, false, latencyMs)

		slog.Error("provider call failed",
			"provider", selected.Name,
			"transaction_id", req.TransactionID,
			"error", err,
			"latency_ms", latencyMs,
		)
		return &domain.ProcessResult{
			TransactionID: req.TransactionID,
			Provider:      selected.Name,
			Status:        domain.ResultFailed,
			ErrorMessage:  err.Error(),
			LatencyMs:     latencyMs,
		}, nil
	}

	// 7. Обновляем статистику по результату
	success := result.Status == domain.ResultCaptured
	if success {
		s.cb.RecordSuccess(selected.Name)
	} else {
		// Declined — не failure для CB (это бизнес-решение провайдера),
		// но для Thompson Sampling это неуспех
		s.cb.RecordSuccess(selected.Name) // провайдер ответил корректно
	}
	s.router.RecordResult(selected.Name, success, latencyMs)

	slog.Info("payment processed",
		"provider", selected.Name,
		"transaction_id", req.TransactionID,
		"status", result.Status,
		"latency_ms", latencyMs,
	)

	return &domain.ProcessResult{
		TransactionID: req.TransactionID,
		Provider:      selected.Name,
		ProviderTxID:  result.ProviderTxID,
		Status:        result.Status,
		ErrorMessage:  result.ErrorMessage,
		LatencyMs:     latencyMs,
	}, nil
}

// callWithRetry — без изменений, оставляем как было
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

func (s *Service) backoffWithJitter(attempt int) time.Duration {
	backoff := s.baseDelay * (1 << uint(attempt-1))
	jitter := time.Duration(rand.Int63n(int64(backoff)/2)) - time.Duration(int64(backoff)/4)
	return backoff + jitter
}
