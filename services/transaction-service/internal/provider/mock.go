package provider

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"time"

	"github.com/BuzzLyutic/payment-gateway-microservices/services/transaction-service/internal/domain"
)

// ErrTransient - временная ошибка провайдера, можно повторить.
var ErrTransient = errors.New("transient provider error")

// Result - ответ от провайдера.
type Result struct {
	ProviderTxID string
	Status       domain.Status // captured или declined
	ErrorMessage string        // причина отклонения
}

// Provider - интерфейс платёжного провайдера.
// В проде будет несколько реализаций (Stripe, YooKassa и т.д.).
type Provider interface {
	ProcessPayment(ctx context.Context, tx *domain.Transaction) (*Result, error)
}

// MockProvider имитирует внешний платёжный провайдер.
// 70% - success, 20% - transient error (retriable), 10% - decline (terminal).
type MockProvider struct{}

func NewMockProvider() *MockProvider {
	return &MockProvider{}
}

func (p *MockProvider) ProcessPayment(ctx context.Context, tx *domain.Transaction) (*Result, error) {
	// Имитация задержки 100–500 мс
	delay := time.Duration(100+rand.Intn(400)) * time.Millisecond

	select {
	case <-time.After(delay):
	case <-ctx.Done():
		return nil, ctx.Err()
	}

	// Случайный исход
	roll := rand.Intn(100)

	switch {
	case roll < 70: // 70% - успех
		return &Result{
			ProviderTxID: generateTxID(),
			Status:       domain.StatusCaptured,
		}, nil

	case roll < 90: // 20% - transient error (retry)
		return nil, fmt.Errorf("%w: provider timeout", ErrTransient)

	default: // 10% - decline (не retry)
		return &Result{
			Status:       domain.StatusDeclined,
			ErrorMessage: "insufficient funds",
		}, nil
	}
}

func generateTxID() string {
	const chars = "abcdefghijklmnopqrstuvwxyz0123456789"
	b := make([]byte, 8)
	for i := range b {
		b[i] = chars[rand.Intn(len(chars))]
	}
	return "mock_tx_" + string(b)
}
