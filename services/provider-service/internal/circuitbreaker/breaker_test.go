package circuitbreaker_test

import (
	"context"
	"io"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/BuzzLyutic/payment-gateway-microservices/services/provider-service/internal/circuitbreaker"
)

func TestMain(m *testing.M) {
	// Отключаем slog во время тестов
	slog.SetDefault(slog.New(slog.NewTextHandler(io.Discard, nil)))
	os.Exit(m.Run())
}

// Хелперы

// newFastConfig создаёт конфиг с минимальными порогами для быстрых тестов.
func newFastConfig() circuitbreaker.Config {
	return circuitbreaker.Config{
		FailureThreshold:  3,
		OpenTimeout:       100 * time.Millisecond,
		HalfOpenSuccesses: 2,
	}
}

// openBreaker доводит CB до состояния Open через N failures.
func openBreaker(b *circuitbreaker.Breaker, n int) {
	for i := 0; i < n; i++ {
		b.RecordFailure()
	}
}

// Тесты начального состояния

func TestBreaker_InitialState_IsClosed(t *testing.T) {
	b := circuitbreaker.New("test_provider", newFastConfig(), nil)

	if b.CurrentState() != circuitbreaker.StateClosed {
		t.Errorf("initial state = %v, want Closed", b.CurrentState())
	}
}

func TestBreaker_InitialState_AllowsRequests(t *testing.T) {
	b := circuitbreaker.New("test_provider", newFastConfig(), nil)

	if err := b.Allow(); err != nil {
		t.Errorf("expected Allow() = nil, got %v", err)
	}
}

func TestBreaker_IsOpen_FalseInitially(t *testing.T) {
	b := circuitbreaker.New("test_provider", newFastConfig(), nil)

	if b.IsOpen() {
		t.Error("IsOpen() = true, want false initially")
	}
}

// Тесты перехода Closed → Open

func TestBreaker_OpensAfterFailureThreshold(t *testing.T) {
	cfg := newFastConfig() // FailureThreshold = 3
	b := circuitbreaker.New("test_provider", cfg, nil)

	// 2 failures — ещё Closed
	b.RecordFailure()
	b.RecordFailure()

	if b.CurrentState() != circuitbreaker.StateClosed {
		t.Errorf("state after 2 failures = %v, want Closed", b.CurrentState())
	}

	// 3-й failure — переходит в Open
	b.RecordFailure()

	if b.CurrentState() != circuitbreaker.StateOpen {
		t.Errorf("state after 3 failures = %v, want Open", b.CurrentState())
	}
}

func TestBreaker_Open_RejectsRequests(t *testing.T) {
	b := circuitbreaker.New("test_provider", newFastConfig(), nil)
	openBreaker(b, 3)

	if err := b.Allow(); err == nil {
		t.Error("Allow() = nil on Open CB, want ErrOpen")
	}
}

func TestBreaker_Open_IsOpenReturnsTrue(t *testing.T) {
	b := circuitbreaker.New("test_provider", newFastConfig(), nil)
	openBreaker(b, 3)

	if !b.IsOpen() {
		t.Error("IsOpen() = false on Open CB, want true")
	}
}

func TestBreaker_SuccessResetsFailureCounter(t *testing.T) {
	cfg := newFastConfig() // FailureThreshold = 3
	b := circuitbreaker.New("test_provider", cfg, nil)

	// 2 failures, потом success — счётчик сбрасывается
	b.RecordFailure()
	b.RecordFailure()
	b.RecordSuccess()

	// Ещё 2 failures — не должен открыться (счётчик был сброшен)
	b.RecordFailure()
	b.RecordFailure()

	if b.CurrentState() != circuitbreaker.StateClosed {
		t.Errorf("state = %v, want Closed (counter was reset by success)", b.CurrentState())
	}
}

// Тесты перехода Open → HalfOpen

func TestBreaker_TransitionsToHalfOpenAfterTimeout(t *testing.T) {
	cfg := circuitbreaker.Config{
		FailureThreshold:  3,
		OpenTimeout:       50 * time.Millisecond, // очень короткий для теста
		HalfOpenSuccesses: 2,
	}
	b := circuitbreaker.New("test_provider", cfg, nil)
	openBreaker(b, 3)

	// Сразу после открытия — Open
	if err := b.Allow(); err == nil {
		t.Error("Allow() should return ErrOpen immediately after opening")
	}

	// Ждём истечения таймаута
	time.Sleep(60 * time.Millisecond)

	// После таймаута Allow() должен пройти (переход в HalfOpen)
	if err := b.Allow(); err != nil {
		t.Errorf("Allow() = %v after timeout, want nil (HalfOpen)", err)
	}

	if b.CurrentState() != circuitbreaker.StateHalfOpen {
		t.Errorf("state = %v after timeout, want HalfOpen", b.CurrentState())
	}
}

func TestBreaker_HalfOpen_OnHalfOpenCallbackCalled(t *testing.T) {
	cfg := circuitbreaker.Config{
		FailureThreshold:  3,
		OpenTimeout:       50 * time.Millisecond,
		HalfOpenSuccesses: 2,
	}

	callbackCalled := make(chan string, 1)
	onHalfOpen := func(name string) {
		callbackCalled <- name
	}

	b := circuitbreaker.New("test_provider", cfg, onHalfOpen)
	openBreaker(b, 3)

	time.Sleep(60 * time.Millisecond)
	b.Allow() // триггерит переход в HalfOpen

	select {
	case name := <-callbackCalled:
		if name != "test_provider" {
			t.Errorf("callback provider = %q, want test_provider", name)
		}
	case <-time.After(200 * time.Millisecond):
		t.Error("onHalfOpen callback was not called within timeout")
	}
}

func TestBreaker_IsOpen_FalseAfterTimeout(t *testing.T) {
	cfg := circuitbreaker.Config{
		FailureThreshold:  3,
		OpenTimeout:       50 * time.Millisecond,
		HalfOpenSuccesses: 2,
	}
	b := circuitbreaker.New("test_provider", cfg, nil)
	openBreaker(b, 3)

	time.Sleep(60 * time.Millisecond)

	// IsOpen() должен вернуть false после таймаута
	// (CB ещё не в HalfOpen формально, но запросы пропускать должен)
	if b.IsOpen() {
		t.Error("IsOpen() = true after timeout, want false")
	}
}

// Тесты перехода HalfOpen → Closed

func TestBreaker_HalfOpen_ClosesAfterRequiredSuccesses(t *testing.T) {
	cfg := circuitbreaker.Config{
		FailureThreshold:  3,
		OpenTimeout:       50 * time.Millisecond,
		HalfOpenSuccesses: 2, // нужно 2 успеха
	}
	b := circuitbreaker.New("test_provider", cfg, nil)
	openBreaker(b, 3)

	time.Sleep(60 * time.Millisecond)
	b.Allow() // переход в HalfOpen

	// 1 успех — ещё HalfOpen
	b.RecordSuccess()
	if b.CurrentState() != circuitbreaker.StateHalfOpen {
		t.Errorf("state after 1 success = %v, want HalfOpen", b.CurrentState())
	}

	// 2-й успех — переход в Closed
	b.RecordSuccess()
	if b.CurrentState() != circuitbreaker.StateClosed {
		t.Errorf("state after 2 successes = %v, want Closed", b.CurrentState())
	}
}

func TestBreaker_HalfOpen_ReopensOnFailure(t *testing.T) {
	cfg := circuitbreaker.Config{
		FailureThreshold:  3,
		OpenTimeout:       50 * time.Millisecond,
		HalfOpenSuccesses: 2,
	}
	b := circuitbreaker.New("test_provider", cfg, nil)
	openBreaker(b, 3)

	time.Sleep(60 * time.Millisecond)
	b.Allow() // переход в HalfOpen

	// Один failure в HalfOpen — сразу обратно в Open
	b.RecordFailure()

	if b.CurrentState() != circuitbreaker.StateOpen {
		t.Errorf("state after failure in HalfOpen = %v, want Open", b.CurrentState())
	}
	if err := b.Allow(); err == nil {
		t.Error("Allow() should return ErrOpen after reopening")
	}
}

// Тесты Manager

func TestManager_Get_CreatesNewBreaker(t *testing.T) {
	m := circuitbreaker.NewManager(newFastConfig(), nil)

	b := m.Get("provider_x")
	if b == nil {
		t.Fatal("Get() returned nil")
	}
	if b.CurrentState() != circuitbreaker.StateClosed {
		t.Errorf("new breaker state = %v, want Closed", b.CurrentState())
	}
}

func TestManager_Get_ReturnsSameInstance(t *testing.T) {
	m := circuitbreaker.NewManager(newFastConfig(), nil)

	b1 := m.Get("provider_x")
	b2 := m.Get("provider_x")

	// Должны быть один и тот же объект
	if b1 != b2 {
		t.Error("Get() returned different instances for same provider")
	}
}

func TestManager_Allow_DelegatesToBreaker(t *testing.T) {
	m := circuitbreaker.NewManager(newFastConfig(), nil)

	// Закрытый CB — Allow проходит
	if err := m.Allow("provider_x"); err != nil {
		t.Errorf("Allow() = %v on closed CB, want nil", err)
	}

	// Открываем через RecordFailure
	for i := 0; i < 3; i++ {
		m.RecordFailure("provider_x")
	}

	// Открытый CB — Allow возвращает ошибку
	if err := m.Allow("provider_x"); err == nil {
		t.Error("Allow() = nil on open CB, want error")
	}
}

func TestManager_IsOpen_ReflectsState(t *testing.T) {
	m := circuitbreaker.NewManager(newFastConfig(), nil)

	if m.IsOpen("provider_x") {
		t.Error("IsOpen() = true initially, want false")
	}

	for i := 0; i < 3; i++ {
		m.RecordFailure("provider_x")
	}

	if !m.IsOpen("provider_x") {
		t.Error("IsOpen() = false after failures, want true")
	}
}

func TestManager_IsolatesBreakersBetweenProviders(t *testing.T) {
	m := circuitbreaker.NewManager(newFastConfig(), nil)

	// Открываем CB для provider_a
	for i := 0; i < 3; i++ {
		m.RecordFailure("provider_a")
	}

	// provider_b должен быть независим
	if m.IsOpen("provider_b") {
		t.Error("provider_b CB open, want closed (independent from provider_a)")
	}
	if err := m.Allow("provider_b"); err != nil {
		t.Errorf("provider_b Allow() = %v, want nil", err)
	}
}

func TestManager_States_ReturnsAllBreakers(t *testing.T) {
	m := circuitbreaker.NewManager(newFastConfig(), nil)

	// Создаём breakers для трёх провайдеров
	m.Get("provider_a")
	m.Get("provider_b")
	m.Get("provider_c")

	// Открываем один
	for i := 0; i < 3; i++ {
		m.RecordFailure("provider_a")
	}

	states := m.States(context.TODO())

	if len(states) != 3 {
		t.Errorf("States() returned %d entries, want 3", len(states))
	}
	if states["provider_a"] != "open" {
		t.Errorf("provider_a state = %q, want open", states["provider_a"])
	}
	if states["provider_b"] != "closed" {
		t.Errorf("provider_b state = %q, want closed", states["provider_b"])
	}
}

// Тест полного цикла

func TestBreaker_FullCycle_ClosedOpenHalfOpenClosed(t *testing.T) {
	cfg := circuitbreaker.Config{
		FailureThreshold:  3,
		OpenTimeout:       50 * time.Millisecond,
		HalfOpenSuccesses: 2,
	}
	b := circuitbreaker.New("test_provider", cfg, nil)

	// Closed
	if b.CurrentState() != circuitbreaker.StateClosed {
		t.Fatalf("step 1: state = %v, want Closed", b.CurrentState())
	}

	// Closed → Open
	openBreaker(b, 3)
	if b.CurrentState() != circuitbreaker.StateOpen {
		t.Fatalf("step 2: state = %v, want Open", b.CurrentState())
	}

	// Open → HalfOpen (после таймаута)
	time.Sleep(60 * time.Millisecond)
	b.Allow()
	if b.CurrentState() != circuitbreaker.StateHalfOpen {
		t.Fatalf("step 3: state = %v, want HalfOpen", b.CurrentState())
	}

	// HalfOpen → Closed (после HalfOpenSuccesses успехов)
	b.RecordSuccess()
	b.RecordSuccess()
	if b.CurrentState() != circuitbreaker.StateClosed {
		t.Fatalf("step 4: state = %v, want Closed", b.CurrentState())
	}

	// Closed — снова принимает запросы нормально
	if err := b.Allow(); err != nil {
		t.Errorf("step 5: Allow() = %v, want nil after recovery", err)
	}
}
