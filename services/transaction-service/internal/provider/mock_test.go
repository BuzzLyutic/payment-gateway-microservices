package provider

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/BuzzLyutic/payment-gateway-microservices/services/transaction-service/internal/domain"
)

func TestMockProvider_ReturnsResult(t *testing.T) {
	p := NewMockProviderWithDelay(0, 0) // без задержки в тестах
	tx := &domain.Transaction{
		ID:       "tx-1",
		Amount:   10000,
		Currency: "RUB",
	}

	result, err := p.ProcessPayment(context.Background(), tx)

	if err != nil {
		if !errors.Is(err, ErrTransient) {
			t.Fatalf("unexpected error type: %v", err)
		}
		return
	}

	switch result.Status {
	case domain.StatusCaptured:
		if result.ProviderTxID == "" {
			t.Error("captured but no provider_tx_id")
		}
	case domain.StatusDeclined:
		if result.ErrorMessage == "" {
			t.Error("declined but no error_message")
		}
	default:
		t.Errorf("unexpected status: %s", result.Status)
	}
}

func TestMockProvider_Distribution(t *testing.T) {
	p := NewMockProviderWithDelay(0, 0) // без задержки - 1000 итераций за миллисекунды
	tx := &domain.Transaction{ID: "tx-1", Amount: 10000, Currency: "RUB"}

	counts := map[string]int{
		"captured":  0,
		"declined":  0,
		"transient": 0,
	}

	iterations := 1000
	for i := 0; i < iterations; i++ {
		result, err := p.ProcessPayment(context.Background(), tx)
		if err != nil {
			counts["transient"]++
			continue
		}
		counts[string(result.Status)]++
	}

	capturedPct := float64(counts["captured"]) / float64(iterations) * 100
	declinedPct := float64(counts["declined"]) / float64(iterations) * 100
	transientPct := float64(counts["transient"]) / float64(iterations) * 100

	t.Logf("Distribution over %d calls: captured=%.1f%% declined=%.1f%% transient=%.1f%%",
		iterations, capturedPct, declinedPct, transientPct)

	if capturedPct < 55 || capturedPct > 85 {
		t.Errorf("captured=%.1f%%, expected ~70%%", capturedPct)
	}
	if declinedPct < 2 || declinedPct > 25 {
		t.Errorf("declined=%.1f%%, expected ~10%%", declinedPct)
	}
	if transientPct < 5 || transientPct > 35 {
		t.Errorf("transient=%.1f%%, expected ~20%%", transientPct)
	}
}

func TestMockProvider_RespectsContextCancellation(t *testing.T) {
	p := NewMockProviderWithDelay(1*time.Second, 2*time.Second) // длинная задержка
	tx := &domain.Transaction{ID: "tx-1", Amount: 10000}

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // отменяем сразу

	_, err := p.ProcessPayment(ctx, tx)
	if err == nil {
		t.Fatal("expected error on cancelled context")
	}
}

func TestMockProvider_DefaultHasDelay(t *testing.T) {
	p := NewMockProvider() // дефолтный — 100-500ms
	tx := &domain.Transaction{ID: "tx-1", Amount: 10000}

	start := time.Now()
	p.ProcessPayment(context.Background(), tx)
	elapsed := time.Since(start)

	if elapsed < 100*time.Millisecond {
		t.Errorf("default provider should have delay, elapsed=%v", elapsed)
	}
}
