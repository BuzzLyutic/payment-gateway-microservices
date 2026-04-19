package adapter_test

import (
	"context"
	"errors"
	"testing"

	"github.com/BuzzLyutic/payment-gateway-microservices/services/provider-service/internal/adapter"
	"github.com/BuzzLyutic/payment-gateway-microservices/services/provider-service/internal/domain"
)

// stubAdapter — минимальная реализация PaymentAdapter для тестов Registry.
type stubAdapter struct {
	result *adapter.AdapterResult
	err    error
}

func (s *stubAdapter) ProcessPayment(
	_ context.Context,
	_ *domain.ProcessRequest,
) (*adapter.AdapterResult, error) {
	return s.result, s.err
}

// NewRegistry

func TestNewRegistry_NotNil(t *testing.T) {
	r := adapter.NewRegistry()
	if r == nil {
		t.Error("NewRegistry() returned nil")
	}
}

// Register + Get

func TestRegistry_Register_And_Get(t *testing.T) {
	r := adapter.NewRegistry()
	a := &stubAdapter{result: &adapter.AdapterResult{
		Status: domain.ResultCaptured,
	}}

	r.Register("provider_a", a)

	got, err := r.Get("provider_a")
	if err != nil {
		t.Fatalf("Get() unexpected error: %v", err)
	}
	if got == nil {
		t.Fatal("Get() returned nil adapter")
	}

	// Проверяем что возвращается именно зарегистрированный адаптер.
	result, err := got.ProcessPayment(context.Background(), &domain.ProcessRequest{})
	if err != nil {
		t.Fatalf("ProcessPayment() error: %v", err)
	}
	if result.Status != domain.ResultCaptured {
		t.Errorf("status = %v, want captured", result.Status)
	}
}

func TestRegistry_Get_NotFound(t *testing.T) {
	r := adapter.NewRegistry()

	_, err := r.Get("nonexistent_provider")
	if err == nil {
		t.Fatal("expected error for unknown provider, got nil")
	}

	// Ошибка должна содержать имя провайдера для диагностики.
	if !errors.Is(err, err) { // всегда true — проверяем текст
		t.Error("error should be non-nil")
	}
	expected := "adapter not found: nonexistent_provider"
	if err.Error() != expected {
		t.Errorf("error = %q, want %q", err.Error(), expected)
	}
}

func TestRegistry_Register_MultipleProviders(t *testing.T) {
	r := adapter.NewRegistry()

	adapterA := &stubAdapter{result: &adapter.AdapterResult{Status: domain.ResultCaptured}}
	adapterB := &stubAdapter{result: &adapter.AdapterResult{Status: domain.ResultDeclined}}

	r.Register("provider_a", adapterA)
	r.Register("provider_b", adapterB)

	gotA, err := r.Get("provider_a")
	if err != nil {
		t.Fatalf("Get(provider_a) error: %v", err)
	}
	gotB, err := r.Get("provider_b")
	if err != nil {
		t.Fatalf("Get(provider_b) error: %v", err)
	}

	// Каждый адаптер возвращает свой результат.
	resA, _ := gotA.ProcessPayment(context.Background(), &domain.ProcessRequest{})
	resB, _ := gotB.ProcessPayment(context.Background(), &domain.ProcessRequest{})

	if resA.Status != domain.ResultCaptured {
		t.Errorf("provider_a: status = %v, want captured", resA.Status)
	}
	if resB.Status != domain.ResultDeclined {
		t.Errorf("provider_b: status = %v, want declined", resB.Status)
	}
}

func TestRegistry_Register_Overwrites(t *testing.T) {
	// Повторная регистрация с тем же именем — перезаписывает адаптер.
	r := adapter.NewRegistry()

	old := &stubAdapter{result: &adapter.AdapterResult{Status: domain.ResultDeclined}}
	new := &stubAdapter{result: &adapter.AdapterResult{Status: domain.ResultCaptured}}

	r.Register("provider_a", old)
	r.Register("provider_a", new) // перезапись

	got, err := r.Get("provider_a")
	if err != nil {
		t.Fatalf("Get() error: %v", err)
	}

	result, _ := got.ProcessPayment(context.Background(), &domain.ProcessRequest{})
	if result.Status != domain.ResultCaptured {
		t.Errorf("expected new adapter (captured), got %v", result.Status)
	}
}

func TestRegistry_Get_EmptyRegistry(t *testing.T) {
	r := adapter.NewRegistry()

	_, err := r.Get("any_provider")
	if err == nil {
		t.Error("expected error from empty registry, got nil")
	}
}
