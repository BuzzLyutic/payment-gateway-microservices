package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math/rand"
	"time"

	"github.com/BuzzLyutic/payment-gateway-microservices/services/provider-service/internal/adapter"
	"github.com/BuzzLyutic/payment-gateway-microservices/services/provider-service/internal/circuitbreaker"
	"github.com/BuzzLyutic/payment-gateway-microservices/services/provider-service/internal/domain"
	"github.com/BuzzLyutic/payment-gateway-microservices/services/provider-service/internal/metrics"
	"github.com/BuzzLyutic/payment-gateway-microservices/services/provider-service/internal/router"
	"github.com/prometheus/client_golang/prometheus"
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
	allProviders, err := s.repo.FindActive(ctx, req.Currency, req.PaymentMethod)
	if err != nil {
		return nil, fmt.Errorf("find providers: %w", err)
	}

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
		// Счётчик: нет доступных провайдеров — специальный label "none"
		metrics.PaymentsTotal.WithLabelValues("none", "failed").Inc()

		slog.Error("no providers available after CB filtering",
			"transaction_id", req.TransactionID,
		)
		return &domain.ProcessResult{
			TransactionID: req.TransactionID,
			Status:        domain.ResultFailed,
			ErrorMessage:  domain.ErrNoProviderAvailable.Error(),
		}, nil
	}

	selected, err := s.router.Select(ctx, candidates)
	if err != nil {
		return nil, fmt.Errorf("router select: %w", err)
	}

	slog.Info("provider selected by thompson sampling",
		"provider", selected.Name,
		"transaction_id", req.TransactionID,
		"candidates", len(candidates),
	)

	pa, err := s.registry.Get(selected.Name)
	if err != nil {
		return nil, fmt.Errorf("get adapter: %w", err)
	}

	if err := s.cb.Allow(selected.Name); err != nil {
		slog.Warn("circuit breaker rejected request",
			"provider", selected.Name,
			"transaction_id", req.TransactionID,
		)
		metrics.PaymentsTotal.WithLabelValues(selected.Name, "failed").Inc()
		return &domain.ProcessResult{
			TransactionID: req.TransactionID,
			Status:        domain.ResultFailed,
			ErrorMessage:  "provider unavailable",
		}, nil
	}

	// Замер латентности через timer — автоматически запишет в гистограмму
	timer := prometheus.NewTimer(
		metrics.PaymentDuration.WithLabelValues(selected.Name),
	)
	start := time.Now()
	result, err := s.callWithRetry(ctx, pa, req, selected.Name)
	latencyMs := time.Since(start).Milliseconds()
	timer.ObserveDuration()

	if err != nil {
		s.cb.RecordFailure(selected.Name)
		s.router.RecordResult(selected.Name, false, latencyMs)

		metrics.PaymentsTotal.WithLabelValues(selected.Name, "failed").Inc()

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

	success := result.Status == domain.ResultCaptured
	if success {
		s.cb.RecordSuccess(selected.Name)
	} else {
		s.cb.RecordSuccess(selected.Name)
	}
	s.router.RecordResult(selected.Name, success, latencyMs)

	// Счётчик по фактическому статусу
	metrics.PaymentsTotal.WithLabelValues(selected.Name, string(result.Status)).Inc()

	// Обновляем gauges Thompson Sampling после каждой транзакции
	alpha, beta := s.router.GetParams(selected.Name)
	metrics.ThompsonAlpha.WithLabelValues(selected.Name).Set(alpha)
	metrics.ThompsonBeta.WithLabelValues(selected.Name).Set(beta)
	metrics.ThompsonSuccessProbability.WithLabelValues(selected.Name).Set(alpha / (alpha + beta))

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

func (s *Service) callWithRetry(ctx context.Context, pa adapter.PaymentAdapter, req *domain.ProcessRequest, providerName string) (*adapter.AdapterResult, error) {
	var lastErr error

	for attempt := 0; attempt <= s.maxRetries; attempt++ {
		if attempt > 0 {
			delay := s.backoffWithJitter(attempt)

			// Счётчик retry
			metrics.PaymentRetries.WithLabelValues(providerName).Inc()

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
