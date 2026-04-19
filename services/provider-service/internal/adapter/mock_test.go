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

func TestGetIntOrDefault_IntType(t *testing.T) {
	// Ветка case int: — не покрыта (pgx даёт float64, но int тоже должен работать)
	cfg := map[string]any{
		"success_rate":   int(100), // явный int, не float64
		"min_latency_ms": int(0),
		"max_latency_ms": int(0),
	}

	a := adapter.NewMockAdapter(cfg)
	if a == nil {
		t.Fatal("expected non-nil adapter with int config values")
	}

	req := &domain.ProcessRequest{
		TransactionID: "tx_int_type",
		MerchantID:    "merchant_1",
		Amount:        1000,
		Currency:      "RUB",
		PaymentMethod: "card",
	}

	// success_rate=100 (int) → всегда captured
	result, err := a.ProcessPayment(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Status != domain.ResultCaptured {
		t.Errorf("expected captured with int success_rate=100, got %v", result.Status)
	}
}

func TestGetIntOrDefault_StringType(t *testing.T) {
	// Ветка case string: — парсинг строки в int
	cfg := map[string]any{
		"success_rate":   "100", // строка
		"min_latency_ms": "0",
		"max_latency_ms": "0",
	}

	a := adapter.NewMockAdapter(cfg)
	if a == nil {
		t.Fatal("expected non-nil adapter with string config values")
	}

	req := &domain.ProcessRequest{
		TransactionID: "tx_string_type",
		MerchantID:    "merchant_1",
		Amount:        1000,
		Currency:      "RUB",
		PaymentMethod: "card",
	}

	result, err := a.ProcessPayment(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Status != domain.ResultCaptured {
		t.Errorf("expected captured with string success_rate=100, got %v", result.Status)
	}
}

func TestGetIntOrDefault_InvalidStringFallback(t *testing.T) {
	// Ветка case string: с невалидным значением → fallback
	cfg := map[string]any{
		"success_rate":   "not-a-number", // невалидная строка → fallback=80
		"min_latency_ms": "0",
		"max_latency_ms": "0",
	}

	// Не паникует, создаёт адаптер с дефолтным success_rate=80
	a := adapter.NewMockAdapter(cfg)
	if a == nil {
		t.Fatal("expected non-nil adapter even with invalid string config")
	}
}

func TestGetIntOrDefault_UnknownTypeFallback(t *testing.T) {
	// Ветка default: — неизвестный тип → fallback
	cfg := map[string]any{
		"success_rate":   []int{1, 2, 3}, // неподдерживаемый тип
		"min_latency_ms": "0",
		"max_latency_ms": "0",
	}

	// Не паникует, использует fallback
	a := adapter.NewMockAdapter(cfg)
	if a == nil {
		t.Fatal("expected non-nil adapter with unknown type config")
	}
}

func TestGetIntOrDefault_MissingKey_UsesFallback(t *testing.T) {
	// Ключ отсутствует → fallback
	cfg := map[string]any{} // пустой конфиг

	a := adapter.NewMockAdapter(cfg)
	if a == nil {
		t.Fatal("expected non-nil adapter with empty config")
	}

	// Дефолты: success_rate=80, min=50ms, max=200ms
	// Просто проверяем что не паникует при вызове
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	req := &domain.ProcessRequest{
		TransactionID: "tx_fallback",
		MerchantID:    "merchant_1",
		Amount:        1000,
		Currency:      "RUB",
		PaymentMethod: "card",
	}

	// Может вернуть любой результат — главное не паникует
	_, _ = a.ProcessPayment(ctx, req)
}
