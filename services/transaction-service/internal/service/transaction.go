package service

import (
	"context"
	"log/slog"

	"github.com/BuzzLyutic/payment-gateway-microservices/services/transaction-service/internal/domain"
)

// Repository - интерфейс для слоя данных.
type Repository interface {
	Create(ctx context.Context, tx *domain.Transaction) error
	GetByID(ctx context.Context, id string) (*domain.Transaction, error)
}

type TransactionService struct {
	repo Repository
}

func New(repo Repository) *TransactionService {
	return &TransactionService{repo: repo}
}

// CreatePayment создаёт новую транзакцию со статусом pending.
func (s *TransactionService) CreatePayment(ctx context.Context, req CreatePaymentRequest) (*domain.Transaction, error) {
	tx := &domain.Transaction{
		IdempotencyKey: req.IdempotencyKey,
		MerchantID:     req.MerchantID,
		Amount:         req.Amount,
		Currency:       req.Currency,
		Status:         domain.StatusPending,
		Description:    strPtr(req.Description),
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
	Description    string
	Metadata       map[string]string
}

// strPtr - хелпер для конвертации string - *string.
// Пустая строка - nil (= NULL в БД).
func strPtr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
