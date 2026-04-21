package service_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/BuzzLyutic/payment-gateway-microservices/services/provider-service/internal/adapter"
	"github.com/BuzzLyutic/payment-gateway-microservices/services/provider-service/internal/circuitbreaker"
	"github.com/BuzzLyutic/payment-gateway-microservices/services/provider-service/internal/domain"
	"github.com/BuzzLyutic/payment-gateway-microservices/services/provider-service/internal/router"
	"github.com/BuzzLyutic/payment-gateway-microservices/services/provider-service/internal/service"
)

func TestMain(m *testing.M) {
	// Отключаем slog во время тестов
	slog.SetDefault(slog.New(slog.NewTextHandler(io.Discard, nil)))
	os.Exit(m.Run())
}

// Моки

type mockRepo struct {
	providers []*domain.Provider
	err       error
}

func (m *mockRepo) FindActive(_ context.Context, _, _ string) ([]*domain.Provider, error) {
	return m.providers, m.err
}

// mockAdapter с счётчиком вызовов — нужен для проверки retry.
type mockAdapter struct {
	result *adapter.AdapterResult
	err    error
	calls  int
	// callResults позволяет задать разные ответы на каждый вызов.
	// Если nil — всегда возвращает result/err.
	callResults []callResult
}

type callResult struct {
	result *adapter.AdapterResult
	err    error
}

func (m *mockAdapter) ProcessPayment(_ context.Context, _ *domain.ProcessRequest) (*adapter.AdapterResult, error) {
	m.calls++

	if m.callResults != nil && m.calls <= len(m.callResults) {
		r := m.callResults[m.calls-1]
		return r.result, r.err
	}

	return m.result, m.err
}

// Хелперы

func newTestRegistry(providerName string, a adapter.PaymentAdapter) *adapter.Registry {
	r := adapter.NewRegistry()
	r.Register(providerName, a)
	return r
}

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

func newTestRequest() *domain.ProcessRequest {
	return &domain.ProcessRequest{
		TransactionID: "tx_test_001",
		MerchantID:    "merchant_1",
		Amount:        10000,
		Currency:      "RUB",
		PaymentMethod: "card",
	}
}

// newTestService создаёт сервис с реальными Router и CB для тестов.
// Это позволяет тестировать интеграцию компонентов без моков роутера.
func newTestService(repo service.Repository, registry *adapter.Registry) *service.Service {
	r := router.NewRouter()
	cb := circuitbreaker.NewManager(circuitbreaker.DefaultConfig(), r.OnHalfOpen)
	return service.New(repo, registry, r, cb)
}

// newTestServiceWithCB создаёт сервис с кастомным конфигом CB.
// Используется для тестов где нужно быстро открыть CB.
func newTestServiceWithCB(repo service.Repository, registry *adapter.Registry, cbCfg circuitbreaker.Config) *service.Service {
	r := router.NewRouter()
	cb := circuitbreaker.NewManager(cbCfg, r.OnHalfOpen)
	return service.New(repo, registry, r, cb)
}

// Базовые тесты

func TestService_ProcessPayment_Success(t *testing.T) {
	provider := newTestProvider("mock_provider_a")
	repo := &mockRepo{providers: []*domain.Provider{provider}}
	a := &mockAdapter{result: &adapter.AdapterResult{
		ProviderTxID: "mock_tx_abc",
		Status:       domain.ResultCaptured,
	}}

	svc := newTestService(repo, newTestRegistry("mock_provider_a", a))

	result, err := svc.ProcessPayment(context.Background(), newTestRequest())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.Status != domain.ResultCaptured {
		t.Errorf("status = %v, want captured", result.Status)
	}
	if result.Provider != "mock_provider_a" {
		t.Errorf("provider = %v, want mock_provider_a", result.Provider)
	}
	if result.ProviderTxID != "mock_tx_abc" {
		t.Errorf("provider_tx_id = %v, want mock_tx_abc", result.ProviderTxID)
	}
	if result.TransactionID != "tx_test_001" {
		t.Errorf("transaction_id = %v, want tx_test_001", result.TransactionID)
	}
}

func TestService_ProcessPayment_NoProviders(t *testing.T) {
	repo := &mockRepo{providers: []*domain.Provider{}}
	svc := newTestService(repo, adapter.NewRegistry())

	result, err := svc.ProcessPayment(context.Background(), newTestRequest())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.Status != domain.ResultFailed {
		t.Errorf("status = %v, want failed", result.Status)
	}
	if result.ErrorMessage != domain.ErrNoProviderAvailable.Error() {
		t.Errorf("error = %v, want ErrNoProviderAvailable", result.ErrorMessage)
	}
}

func TestService_ProcessPayment_RepoError(t *testing.T) {
	repoErr := errors.New("database connection lost")
	repo := &mockRepo{err: repoErr}
	svc := newTestService(repo, adapter.NewRegistry())

	_, err := svc.ProcessPayment(context.Background(), newTestRequest())
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, repoErr) {
		t.Errorf("error = %v, want repoErr", err)
	}
}

func TestService_ProcessPayment_Declined(t *testing.T) {
	provider := newTestProvider("mock_provider_a")
	repo := &mockRepo{providers: []*domain.Provider{provider}}
	a := &mockAdapter{result: &adapter.AdapterResult{
		Status:       domain.ResultDeclined,
		ErrorMessage: "insufficient funds",
	}}

	svc := newTestService(repo, newTestRegistry("mock_provider_a", a))

	result, err := svc.ProcessPayment(context.Background(), newTestRequest())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.Status != domain.ResultDeclined {
		t.Errorf("status = %v, want declined", result.Status)
	}
	if result.ErrorMessage != "insufficient funds" {
		t.Errorf("error = %v, want 'insufficient funds'", result.ErrorMessage)
	}
}

func TestService_ProcessPayment_LatencyMeasured(t *testing.T) {
	provider := newTestProvider("mock_provider_a")
	repo := &mockRepo{providers: []*domain.Provider{provider}}
	a := &mockAdapter{result: &adapter.AdapterResult{Status: domain.ResultCaptured}}

	svc := newTestService(repo, newTestRegistry("mock_provider_a", a))

	result, err := svc.ProcessPayment(context.Background(), newTestRequest())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.LatencyMs < 0 {
		t.Errorf("latency = %v, want >= 0", result.LatencyMs)
	}
}

// Тесты Retry

func TestService_ProcessPayment_TransientRetry_ExhaustsRetries(t *testing.T) {
	provider := newTestProvider("mock_provider_a")
	repo := &mockRepo{providers: []*domain.Provider{provider}}

	// Адаптер всегда возвращает transient — исчерпает все retry
	a := &mockAdapter{err: fmt.Errorf("%w: timeout", adapter.ErrTransient)}

	// Минимальный CB threshold чтобы он не открылся раньше времени
	cbCfg := circuitbreaker.Config{
		FailureThreshold:  100,
		OpenTimeout:       30 * time.Second,
		HalfOpenSuccesses: 2,
	}
	r := router.NewRouter()
	cb := circuitbreaker.NewManager(cbCfg, r.OnHalfOpen)
	svc := service.New(repo, newTestRegistry("mock_provider_a", a), r, cb)

	result, err := svc.ProcessPayment(context.Background(), newTestRequest())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Status != domain.ResultFailed {
		t.Errorf("status = %v, want failed after retries", result.Status)
	}
	// Service делает 1 попытку + maxRetries=3 повтора = 4 вызова
	if a.calls != 4 {
		t.Errorf("adapter calls = %d, want 4 (1 + 3 retries)", a.calls)
	}
}

func TestService_ProcessPayment_TransientThenSuccess(t *testing.T) {
	provider := newTestProvider("mock_provider_a")
	repo := &mockRepo{providers: []*domain.Provider{provider}}

	// Первые 2 попытки — transient, третья — успех
	a := &mockAdapter{
		callResults: []callResult{
			{err: fmt.Errorf("%w: timeout", adapter.ErrTransient)},
			{err: fmt.Errorf("%w: timeout", adapter.ErrTransient)},
			{result: &adapter.AdapterResult{
				ProviderTxID: "tx_after_retry",
				Status:       domain.ResultCaptured,
			}},
		},
	}

	svc := newTestService(repo, newTestRegistry("mock_provider_a", a))

	result, err := svc.ProcessPayment(context.Background(), newTestRequest())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Status != domain.ResultCaptured {
		t.Errorf("status = %v, want captured", result.Status)
	}
	if a.calls != 3 {
		t.Errorf("calls = %d, want 3", a.calls)
	}
}

// Тесты Circuit Breaker

func TestService_CircuitBreaker_OpensAfterFailures(t *testing.T) {
	provider := newTestProvider("mock_provider_a")
	repo := &mockRepo{providers: []*domain.Provider{provider}}
	a := &mockAdapter{err: fmt.Errorf("%w: timeout", adapter.ErrTransient)}

	// CB открывается после 2 failures — порог специально низкий для теста
	cbCfg := circuitbreaker.Config{
		FailureThreshold:  2,
		OpenTimeout:       30 * time.Second,
		HalfOpenSuccesses: 1,
	}
	r := router.NewRouter()
	cb := circuitbreaker.NewManager(cbCfg, r.OnHalfOpen)
	svc := service.New(repo, newTestRegistry("mock_provider_a", a), r, cb)

	req := newTestRequest()

	// Первые два вызова открывают CB (каждый вызов = 1 failure после retry)
	svc.ProcessPayment(context.Background(), req)
	svc.ProcessPayment(context.Background(), req)

	// Третий вызов — CB должен быть Open, провайдер отфильтрован
	result, err := svc.ProcessPayment(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Нет доступных провайдеров — ErrNoProviderAvailable
	if result.Status != domain.ResultFailed {
		t.Errorf("status = %v, want failed (cb open)", result.Status)
	}
	if result.ErrorMessage != domain.ErrNoProviderAvailable.Error() {
		t.Errorf("error = %q, want ErrNoProviderAvailable", result.ErrorMessage)
	}
}

func TestService_CircuitBreaker_RecordsSuccessOnCaptured(t *testing.T) {
	provider := newTestProvider("mock_provider_a")
	repo := &mockRepo{providers: []*domain.Provider{provider}}
	a := &mockAdapter{result: &adapter.AdapterResult{Status: domain.ResultCaptured}}

	cbCfg := circuitbreaker.Config{
		FailureThreshold:  2,
		OpenTimeout:       30 * time.Second,
		HalfOpenSuccesses: 1,
	}
	r := router.NewRouter()
	cb := circuitbreaker.NewManager(cbCfg, r.OnHalfOpen)
	svc := service.New(repo, newTestRegistry("mock_provider_a", a), r, cb)

	// Много успешных вызовов — CB должен остаться Closed
	for i := 0; i < 10; i++ {
		result, err := svc.ProcessPayment(context.Background(), newTestRequest())
		if err != nil {
			t.Fatalf("call %d: unexpected error: %v", i, err)
		}
		if result.Status != domain.ResultCaptured {
			t.Errorf("call %d: status = %v, want captured", i, result.Status)
		}
	}

	// CB должен быть Closed
	if cb.IsOpen("mock_provider_a") {
		t.Error("circuit breaker should be closed after successful calls")
	}
}

func TestService_CircuitBreaker_DeclinedDoesNotOpenCB(t *testing.T) {
	provider := newTestProvider("mock_provider_a")
	repo := &mockRepo{providers: []*domain.Provider{provider}}

	// Declined — это бизнес-решение провайдера, не техническая ошибка
	a := &mockAdapter{result: &adapter.AdapterResult{
		Status:       domain.ResultDeclined,
		ErrorMessage: "insufficient funds",
	}}

	cbCfg := circuitbreaker.Config{
		FailureThreshold:  3,
		OpenTimeout:       30 * time.Second,
		HalfOpenSuccesses: 1,
	}
	r := router.NewRouter()
	cb := circuitbreaker.NewManager(cbCfg, r.OnHalfOpen)
	svc := service.New(repo, newTestRegistry("mock_provider_a", a), r, cb)

	// Много declined — CB не должен открыться
	for i := 0; i < 10; i++ {
		svc.ProcessPayment(context.Background(), newTestRequest())
	}

	if cb.IsOpen("mock_provider_a") {
		t.Error("circuit breaker should stay closed for declined payments")
	}
}

// Тесты Thompson Sampling

func TestService_ThompsonSampling_PrefersSuccessfulProvider(t *testing.T) {
	// provider_good: всегда captured
	// provider_bad: всегда declined
	providerGood := newTestProvider("provider_good")
	providerBad := newTestProvider("provider_bad")

	repo := &mockRepo{providers: []*domain.Provider{providerGood, providerBad}}

	adapterGood := &mockAdapter{result: &adapter.AdapterResult{
		ProviderTxID: "tx_good",
		Status:       domain.ResultCaptured,
	}}
	adapterBad := &mockAdapter{result: &adapter.AdapterResult{
		Status:       domain.ResultDeclined,
		ErrorMessage: "insufficient funds",
	}}

	registry := adapter.NewRegistry()
	registry.Register("provider_good", adapterGood)
	registry.Register("provider_bad", adapterBad)

	r := router.NewRouter()
	cb := circuitbreaker.NewManager(circuitbreaker.DefaultConfig(), r.OnHalfOpen)
	svc := service.New(repo, registry, r, cb)

	// "Прогреваем" алгоритм: много транзакций чтобы статистика накопилась.
	// На первых итерациях Thompson Sampling исследует оба провайдера.
	warmupReq := newTestRequest()
	for i := 0; i < 50; i++ {
		svc.ProcessPayment(context.Background(), warmupReq)
	}

	// После прогрева: считаем выборы каждого провайдера
	goodCount := 0
	total := 100

	for i := 0; i < total; i++ {
		result, err := svc.ProcessPayment(context.Background(), newTestRequest())
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result.Provider == "provider_good" {
			goodCount++
		}
	}

	// provider_good должен выбираться значительно чаще.
	// Не требуем 100% — алгоритм продолжает исследование.
	// Порог 70% достаточно консервативен для стохастического алгоритма.
	goodPct := float64(goodCount) / float64(total) * 100
	t.Logf("provider_good selected: %d/%d (%.1f%%)", goodCount, total, goodPct)

	if goodPct < 70 {
		t.Errorf("provider_good selected %.1f%%, want >= 70%% after warmup", goodPct)
	}
}

func TestService_ThompsonSampling_ExploresNewProvider(t *testing.T) {
	providerA := newTestProvider("provider_a")
	providerB := newTestProvider("provider_b")

	repo := &mockRepo{providers: []*domain.Provider{providerA, providerB}}

	adapterA := &mockAdapter{result: &adapter.AdapterResult{Status: domain.ResultCaptured}}
	adapterB := &mockAdapter{result: &adapter.AdapterResult{Status: domain.ResultCaptured}}

	registry := adapter.NewRegistry()
	registry.Register("provider_a", adapterA)
	registry.Register("provider_b", adapterB)

	r := router.NewRouter()
	cb := circuitbreaker.NewManager(circuitbreaker.DefaultConfig(), r.OnHalfOpen)
	svc := service.New(repo, registry, r, cb)

	seen := map[string]int{}
	total := 200

	for i := 0; i < total; i++ {
		result, err := svc.ProcessPayment(context.Background(), newTestRequest())
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		seen[result.Provider]++
	}

	t.Logf("provider_a: %d/%d, provider_b: %d/%d",
		seen["provider_a"], total,
		seen["provider_b"], total,
	)

	// Ключевое свойство Thompson Sampling — исследование.
	// Оба провайдера должны получить хотя бы несколько вызовов.
	// Минимальный порог 5% от total — достаточно консервативен.
	minCalls := total / 20 // 5% = 10 вызовов из 200
	if seen["provider_a"] < minCalls {
		t.Errorf("provider_a got only %d calls, want >= %d (exploration)",
			seen["provider_a"], minCalls)
	}
	if seen["provider_b"] < minCalls {
		t.Errorf("provider_b got only %d calls, want >= %d (exploration)",
			seen["provider_b"], minCalls)
	}
}

func TestService_ThompsonSampling_SingleProvider_AlwaysSelected(t *testing.T) {
	provider := newTestProvider("only_provider")
	repo := &mockRepo{providers: []*domain.Provider{provider}}
	a := &mockAdapter{result: &adapter.AdapterResult{Status: domain.ResultCaptured}}

	svc := newTestService(repo, newTestRegistry("only_provider", a))

	for i := 0; i < 10; i++ {
		result, err := svc.ProcessPayment(context.Background(), newTestRequest())
		if err != nil {
			t.Fatalf("call %d: unexpected error: %v", i, err)
		}
		if result.Provider != "only_provider" {
			t.Errorf("call %d: provider = %v, want only_provider", i, result.Provider)
		}
	}
}
