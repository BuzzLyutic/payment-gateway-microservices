package service

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/BuzzLyutic/payment-gateway-microservices/services/transaction-service/internal/domain"
	"github.com/BuzzLyutic/payment-gateway-microservices/services/transaction-service/internal/events"
)

// Repository - интерфейс для слоя данных.
type Repository interface {
	Create(ctx context.Context, tx *domain.Transaction) error
	GetByID(ctx context.Context, id string) (*domain.Transaction, error)
	FetchPending(ctx context.Context, limit int) ([]*domain.Transaction, error)
	UpdateStatus(ctx context.Context, id string, status domain.Status, provider *string, providerTxID *string, errorMessage *string) error
	FetchStuck(ctx context.Context, threshold time.Duration, limit int) ([]*domain.Transaction, error)
}

type TransactionService struct {
	repo      Repository
	publisher Publisher
}

// Publisher — интерфейс для публикации событий.
type Publisher interface {
	PublishPaymentCreated(ctx context.Context, event events.PaymentCreated) error
}

func New(repo Repository, pub Publisher) *TransactionService {
	return &TransactionService{
		repo:      repo,
		publisher: pub,
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

// ResolveStuckPayments находит и переводит застрявшие транзакции в failed.
func (s *TransactionService) ResolveStuckPayments(ctx context.Context, threshold time.Duration, limit int) (int, error) {
	txns, err := s.repo.FetchStuck(ctx, threshold, limit)
	if err != nil {
		return 0, fmt.Errorf("resolve stuck: %w", err)
	}

	for _, tx := range txns {
		slog.Warn("resolved stuck transaction",
			"id", tx.ID,
			"merchant_id", tx.MerchantID,
			"amount", tx.Amount,
			"currency", tx.Currency,
		)
	}

	return len(txns), nil
}
