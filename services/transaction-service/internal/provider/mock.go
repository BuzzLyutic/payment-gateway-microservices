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
type MockProvider struct {
	minDelay time.Duration
	maxDelay time.Duration
}

func NewMockProvider() *MockProvider {
	return &MockProvider{
		minDelay: 100 * time.Millisecond,
		maxDelay: 500 * time.Millisecond,
	}
}

// NewMockProviderWithDelay создаёт мок с настраиваемой задержкой.
// Для тестов: NewMockProviderWithDelay(0, 0)
func NewMockProviderWithDelay(min, max time.Duration) *MockProvider {
	return &MockProvider{
		minDelay: min,
		maxDelay: max,
	}
}

func (p *MockProvider) ProcessPayment(ctx context.Context, tx *domain.Transaction) (*Result, error) {
	// Задержка - пропускается если min == max == 0
	if p.maxDelay > 0 {
		spread := p.maxDelay - p.minDelay
		delay := p.minDelay + time.Duration(rand.Int63n(int64(spread+1)))

		select {
		case <-time.After(delay):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}

	roll := rand.Intn(100)

	switch {
	case roll < 70:
		return &Result{
			ProviderTxID: generateTxID(),
			Status:       domain.StatusCaptured,
		}, nil

	case roll < 90:
		return nil, fmt.Errorf("%w: provider timeout", ErrTransient)

	default:
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
