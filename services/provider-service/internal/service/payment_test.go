package service_test

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/BuzzLyutic/payment-gateway-microservices/services/provider-service/internal/adapter"
	"github.com/BuzzLyutic/payment-gateway-microservices/services/provider-service/internal/domain"
	"github.com/BuzzLyutic/payment-gateway-microservices/services/provider-service/internal/service"
)

// ── Моки ────────────────────────────────────────────────────────────────────

type mockRepo struct {
	providers []*domain.Provider
	err       error
}

func (m *mockRepo) FindActive(_ context.Context, _, _ string) ([]*domain.Provider, error) {
	return m.providers, m.err
}

type mockAdapter struct {
	result *adapter.AdapterResult
	err    error
}

func (m *mockAdapter) ProcessPayment(_ context.Context, _ *domain.ProcessRequest) (*adapter.AdapterResult, error) {
	return m.result, m.err
}

// newTestRegistry создаёт registry с указанным адаптером для провайдера.
func newTestRegistry(providerName string, a adapter.PaymentAdapter) *adapter.Registry {
	r := adapter.NewRegistry()
	r.Register(providerName, a)
	return r
}

// newTestProvider создаёт тестового провайдера.
func newTestProvider(name string) *domain.Provider {
	return &domain.Provider{
		Name:           name,
		Type:           "mock",
		Status:         domain.ProviderStatusActive,
		Currencies:     []string{"RUB"},
		PaymentMethods: []string{"card"},
		CommissionPct:  2.5,
	}
}

// newTestRequest создаёт тестовый запрос.
func newTestRequest() *domain.ProcessRequest {
	return &domain.ProcessRequest{
		TransactionID: "tx_test_001",
		MerchantID:    "merchant_1",
		Amount:        10000,
		Currency:      "RUB",
		PaymentMethod: "card",
	}
}

// ── Тесты ───────────────────────────────────────────────────────────────────

func TestService_ProcessPayment_Success(t *testing.T) {
	provider := newTestProvider("mock_provider_a")

	repo := &mockRepo{providers: []*domain.Provider{provider}}
	a := &mockAdapter{result: &adapter.AdapterResult{
		ProviderTxID: "mock_tx_abc",
		Status:       domain.ResultCaptured,
	}}

	svc := service.New(repo, newTestRegistry("mock_provider_a", a))

	result, err := svc.ProcessPayment(context.Background(), newTestRequest())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.Status != domain.ResultCaptured {
		t.Errorf("expected captured, got: %v", result.Status)
	}
	if result.Provider != "mock_provider_a" {
		t.Errorf("expected mock_provider_a, got: %v", result.Provider)
	}
	if result.ProviderTxID != "mock_tx_abc" {
		t.Errorf("expected mock_tx_abc, got: %v", result.ProviderTxID)
	}
	if result.TransactionID != "tx_test_001" {
		t.Errorf("expected tx_test_001, got: %v", result.TransactionID)
	}
}

func TestService_ProcessPayment_NoProviders(t *testing.T) {
	repo := &mockRepo{providers: []*domain.Provider{}}
	svc := service.New(repo, adapter.NewRegistry())

	result, err := svc.ProcessPayment(context.Background(), newTestRequest())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.Status != domain.ResultFailed {
		t.Errorf("expected failed, got: %v", result.Status)
	}
	if result.ErrorMessage != domain.ErrNoProviderAvailable.Error() {
		t.Errorf("expected ErrNoProviderAvailable message, got: %v", result.ErrorMessage)
	}
}

func TestService_ProcessPayment_RepoError(t *testing.T) {
	repoErr := errors.New("database connection lost")
	repo := &mockRepo{err: repoErr}
	svc := service.New(repo, adapter.NewRegistry())

	_, err := svc.ProcessPayment(context.Background(), newTestRequest())
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, repoErr) {
		t.Errorf("expected repoErr, got: %v", err)
	}
}

func TestService_ProcessPayment_Declined(t *testing.T) {
	provider := newTestProvider("mock_provider_a")

	repo := &mockRepo{providers: []*domain.Provider{provider}}
	a := &mockAdapter{result: &adapter.AdapterResult{
		Status:       domain.ResultDeclined,
		ErrorMessage: "insufficient funds",
	}}

	svc := service.New(repo, newTestRegistry("mock_provider_a", a))

	result, err := svc.ProcessPayment(context.Background(), newTestRequest())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.Status != domain.ResultDeclined {
		t.Errorf("expected declined, got: %v", result.Status)
	}
	if result.ErrorMessage != "insufficient funds" {
		t.Errorf("expected 'insufficient funds', got: %v", result.ErrorMessage)
	}
}

func TestService_ProcessPayment_TransientRetry(t *testing.T) {
	provider := newTestProvider("mock_provider_a")
	repo := &mockRepo{providers: []*domain.Provider{provider}}

	// Адаптер всегда возвращает transient — исчерпает все retry
	a := &mockAdapter{err: fmt.Errorf("%w: timeout", adapter.ErrTransient)}

	svc := service.New(repo, newTestRegistry("mock_provider_a", a))

	result, err := svc.ProcessPayment(context.Background(), newTestRequest())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// После исчерпания retry — ResultFailed
	if result.Status != domain.ResultFailed {
		t.Errorf("expected failed after retries, got: %v", result.Status)
	}
}

func TestService_ProcessPayment_LatencyMeasured(t *testing.T) {
	provider := newTestProvider("mock_provider_a")
	repo := &mockRepo{providers: []*domain.Provider{provider}}
	a := &mockAdapter{result: &adapter.AdapterResult{
		Status: domain.ResultCaptured,
	}}

	svc := service.New(repo, newTestRegistry("mock_provider_a", a))

	result, err := svc.ProcessPayment(context.Background(), newTestRequest())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.LatencyMs < 0 {
		t.Errorf("expected non-negative latency, got: %v", result.LatencyMs)
	}
}
