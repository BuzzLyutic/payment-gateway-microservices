package adapter

import (
	"context"
	"fmt"
	"math/rand"
	"strconv"
	"time"

	"github.com/BuzzLyutic/payment-gateway-microservices/services/provider-service/internal/domain"
)

// MockAdapter имитирует внешний платёжный провайдер с настраиваемыми параметрами.
// Один тип - разные инстансы для provider_a, provider_b, provider_c.
type MockAdapter struct {
	successRate int           // процент успешных транзакций (0–100)
	minLatency  time.Duration // минимальная задержка
	maxLatency  time.Duration // максимальная задержка
}

// NewMockAdapter создаёт адаптер из конфигурации провайдера (поле config в БД).
// Ожидаемые ключи: success_rate, min_latency_ms, max_latency_ms.
func NewMockAdapter(cfg map[string]any) *MockAdapter {
	return &MockAdapter{
		successRate: getIntOrDefault(cfg, "success_rate", 80),
		minLatency:  time.Duration(getIntOrDefault(cfg, "min_latency_ms", 50)) * time.Millisecond,
		maxLatency:  time.Duration(getIntOrDefault(cfg, "max_latency_ms", 200)) * time.Millisecond,
	}
}

func (a *MockAdapter) ProcessPayment(ctx context.Context, req *domain.ProcessRequest) (*AdapterResult, error) {
	if err := a.simulateLatency(ctx); err != nil {
		return nil, err
	}

	roll := rand.Intn(100)

	switch {
	case roll < a.successRate:
		return &AdapterResult{
			ProviderTxID: generateTxID(),
			Status:       domain.ResultCaptured,
		}, nil

	case roll < a.successRate + (100-a.successRate)/2:
		return nil, fmt.Errorf("%w: provider timeout", ErrTransient)

	default:
		return &AdapterResult{
			Status:       domain.ResultDeclined,
			ErrorMessage: "insufficient funds",
		}, nil
	}
}

// simulateLatency имитирует сетевую задержку.
func (a *MockAdapter) simulateLatency(ctx context.Context) error {
	if a.maxLatency == 0 {
		return nil
	}

	spread := a.maxLatency - a.minLatency
	delay := a.minLatency + time.Duration(rand.Int63n(int64(spread+1)))

	select {
	case <-time.After(delay):
		return nil
	case <-ctx.Done():
		return ctx.Err()
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

func getIntOrDefault(cfg map[string]any, key string, fallback int) int {
    v, ok := cfg[key]
    if !ok {
        return fallback
    }
    // pgx сканирует числа из JSONB как float64
    switch val := v.(type) {
    case float64:
        return int(val)
    case int:
        return val
    case string:
        i, err := strconv.Atoi(val)
        if err != nil {
            return fallback
        }
        return i
    default:
        return fallback
    }
}
