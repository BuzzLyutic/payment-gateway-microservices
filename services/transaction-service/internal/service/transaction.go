package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/BuzzLyutic/payment-gateway-microservices/services/transaction-service/internal/domain"
	"github.com/BuzzLyutic/payment-gateway-microservices/services/transaction-service/internal/events"
	"github.com/BuzzLyutic/payment-gateway-microservices/services/transaction-service/internal/provider"
)

// Repository - интерфейс для слоя данных.
type Repository interface {
	Create(ctx context.Context, tx *domain.Transaction) error
	GetByID(ctx context.Context, id string) (*domain.Transaction, error)
	FetchPending(ctx context.Context, limit int) ([]*domain.Transaction, error)
	UpdateStatus(ctx context.Context, id string, status domain.Status, provider *string, providerTxID *string, errorMessage *string) error
}

type TransactionService struct {
	repo Repository
	provider provider.Provider
	publisher Publisher
	maxRetries int
}

// Publisher — интерфейс для публикации событий.
type Publisher interface {
	PublishPaymentCreated(ctx context.Context, event events.PaymentCreated) error
}

func New(repo Repository, prov provider.Provider, pub Publisher) *TransactionService {
	return &TransactionService{
		repo:       repo,
		provider:   prov,
		publisher:  pub,
		maxRetries: 3,
	}
}

// CreatePayment создаёт новую транзакцию со статусом pending.
func (s *TransactionService) CreatePayment(ctx context.Context, req CreatePaymentRequest) (*domain.Transaction, error) {
	tx := &domain.Transaction{
		IdempotencyKey: req.IdempotencyKey,
		MerchantID:     req.MerchantID,
		Amount:         req.Amount,
		Currency:       req.Currency,
		PaymentMethod:  req.PaymentMethod,
		Status:         domain.StatusPending,
		Description:    strPtr(req.Description),
		CardHash:       strPtr(req.CardHash),
		CustomerIP:     strPtr(req.CustomerIP),
		CustomerEmail:  strPtr(req.CustomerEmail),
		Metadata:       req.Metadata,
	}

	if err := s.repo.Create(ctx, tx); err != nil {
		slog.Error("failed to create transaction",
			"error", err,
			"merchant_id", req.MerchantID,
		)
		return nil, err
	}

	slog.Info("transaction created",
		"id", tx.ID,
		"merchant_id", tx.MerchantID,
		"amount", tx.Amount,
		"currency", tx.Currency,
	)

	return tx, nil
}

// GetPayment возвращает транзакцию по ID.
func (s *TransactionService) GetPayment(ctx context.Context, id string) (*domain.Transaction, error) {
	return s.repo.GetByID(ctx, id)
}

// CreatePaymentRequest - входные данные для создания платежа.
// Отделён от domain.Transaction - handler формирует request, сервис создаёт сущность.
type CreatePaymentRequest struct {
	IdempotencyKey string
	MerchantID     string
	Amount         int64
	Currency       string
	PaymentMethod  string
	Description    string
	CardHash       string
	CustomerIP     string
	CustomerEmail  string
	Metadata       map[string]string
}


// ProcessPendingPayments забирает pending-транзакции и обрабатывает каждую.
// Вызывается воркером по таймеру.
func (s *TransactionService) ProcessPendingPayments(ctx context.Context, limit int) (int, error) {
	txns, err := s.repo.FetchPending(ctx, limit)
	if err != nil {
		return 0, fmt.Errorf("fetch pending: %w", err)
	}

	for _, tx := range txns {
		s.processOne(ctx, tx)
	}

	return len(txns), nil
}

// processOne публикует событие payment.created для каждой pending-транзакции.
// Обработку результата берёт на себя consumer (payment.completed).
func (s *TransactionService) processOne(ctx context.Context, tx *domain.Transaction) {
	slog.Info("publishing payment.created",
		"id", tx.ID,
		"amount", tx.Amount,
		"currency", tx.Currency,
	)

	event := events.PaymentCreated{
		TransactionID: tx.ID,
		MerchantID:    tx.MerchantID,
		Amount:        tx.Amount,
		Currency:      tx.Currency,
		PaymentMethod: tx.PaymentMethod,
		CardHash:      derefStr(tx.CardHash),
		CustomerIP:    derefStr(tx.CustomerIP),
		CustomerEmail: derefStr(tx.CustomerEmail),
		CreatedAt:     tx.CreatedAt,
	}

	if err := s.publisher.PublishPaymentCreated(ctx, event); err != nil {
		slog.Error("failed to publish payment.created",
			"id", tx.ID,
			"error", err,
		)
		return
	}

	slog.Info("payment.created published", "id", tx.ID)
}

// callProviderWithRetry вызывает провайдера с exponential backoff.
// Retry только при transient-ошибках. Decline не ретраится.
func (s *TransactionService) callProviderWithRetry(ctx context.Context, tx *domain.Transaction) (*provider.Result, error) {
	var lastErr error

	for attempt := 0; attempt <= s.maxRetries; attempt++ {
		if attempt > 0 {
			// Exponential backoff: 100ms, 200ms, 400ms
			backoff := time.Duration(1<<uint(attempt-1)) * 100 * time.Millisecond

			slog.Warn("retrying provider call",
				"tx_id", tx.ID,
				"attempt", attempt,
				"backoff", backoff,
			)

			select {
			case <-time.After(backoff):
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}

		result, err := s.provider.ProcessPayment(ctx, tx)
		if err == nil {
			return result, nil // success или decline - оба через Result
		}

		// Только transient-ошибки ретраим
		if !errors.Is(err, provider.ErrTransient) {
			return nil, err
		}

		lastErr = err
	}

	return nil, fmt.Errorf("max retries (%d) exceeded: %w", s.maxRetries, lastErr)
}


// strPtr - хелпер для конвертации string - *string.
// Пустая строка - nil (= NULL в БД).
func strPtr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func derefStr(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
