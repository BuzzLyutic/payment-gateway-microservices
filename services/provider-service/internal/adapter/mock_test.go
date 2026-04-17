package adapter_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/BuzzLyutic/payment-gateway-microservices/services/provider-service/internal/adapter"
	"github.com/BuzzLyutic/payment-gateway-microservices/services/provider-service/internal/domain"
)

func TestNewMockAdapter_Defaults(t *testing.T) {
	a := adapter.NewMockAdapter(map[string]any{})

	if a == nil {
		t.Fatal("expected non-nil adapter")
	}
}

func TestNewMockAdapter_FromConfig(t *testing.T) {
	cfg := map[string]any{
		"success_rate":   float64(100),
		"min_latency_ms": float64(0),
		"max_latency_ms": float64(0),
	}

	a := adapter.NewMockAdapter(cfg)

	// success_rate=100 — всегда captured, никогда transient/declined
	req := &domain.ProcessRequest{
		TransactionID: "tx_001",
		MerchantID:    "merchant_1",
		Amount:        1000,
		Currency:      "RUB",
		PaymentMethod: "card",
	}

	for range 10 {
		result, err := a.ProcessPayment(context.Background(), req)
		if err != nil {
			t.Errorf("expected no error with success_rate=100, got: %v", err)
		}
		if result.Status != domain.ResultCaptured {
			t.Errorf("expected captured, got: %v", result.Status)
		}
	}
}

func TestMockAdapter_AlwaysDeclined(t *testing.T) {
	// success_rate=0 — никогда не captured
	cfg := map[string]any{
		"success_rate":   float64(0),
		"min_latency_ms": float64(0),
		"max_latency_ms": float64(0),
	}

	a := adapter.NewMockAdapter(cfg)

	req := &domain.ProcessRequest{
		TransactionID: "tx_002",
		MerchantID:    "merchant_1",
		Amount:        1000,
		Currency:      "RUB",
		PaymentMethod: "card",
	}

	for range 10 {
		result, err := a.ProcessPayment(context.Background(), req)
		// success_rate=0: все уходит в transient или declined ветку
		// transient возвращает err, declined возвращает result
		if err != nil {
			if !errors.Is(err, adapter.ErrTransient) {
				t.Errorf("expected ErrTransient, got: %v", err)
			}
			continue
		}
		if result.Status == domain.ResultCaptured {
			t.Error("expected not captured with success_rate=0")
		}
	}
}

func TestMockAdapter_ContextCancellation(t *testing.T) {
	cfg := map[string]any{
		"success_rate":   float64(100),
		"min_latency_ms": float64(500),
		"max_latency_ms": float64(1000),
	}

	a := adapter.NewMockAdapter(cfg)

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	req := &domain.ProcessRequest{
		TransactionID: "tx_003",
		MerchantID:    "merchant_1",
		Amount:        1000,
		Currency:      "RUB",
		PaymentMethod: "card",
	}

	_, err := a.ProcessPayment(ctx, req)
	if err == nil {
		t.Error("expected context cancellation error")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("expected DeadlineExceeded, got: %v", err)
	}
}

func TestMockAdapter_ProviderTxID(t *testing.T) {
	cfg := map[string]any{
		"success_rate":   float64(100),
		"min_latency_ms": float64(0),
		"max_latency_ms": float64(0),
	}

	a := adapter.NewMockAdapter(cfg)

	req := &domain.ProcessRequest{
		TransactionID: "tx_004",
		MerchantID:    "merchant_1",
		Amount:        1000,
		Currency:      "RUB",
		PaymentMethod: "card",
	}

	result, err := a.ProcessPayment(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.ProviderTxID == "" {
		t.Error("expected non-empty provider_tx_id on captured")
	}
}
