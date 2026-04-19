package router_test

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"testing"

	"github.com/BuzzLyutic/payment-gateway-microservices/services/provider-service/internal/domain"
	"github.com/BuzzLyutic/payment-gateway-microservices/services/provider-service/internal/router"
)

func TestMain(m *testing.M) {
	// Отключаем slog во время тестов
	slog.SetDefault(slog.New(slog.NewTextHandler(io.Discard, nil)))
	os.Exit(m.Run())
}

// Хелперы

func newProvider(name string, commission float64) *domain.Provider {
	return &domain.Provider{
		Name:           name,
		Type:           "mock",
		Status:         domain.ProviderStatusActive,
		Currencies:     []string{"RUB"},
		PaymentMethods: []string{"card"},
		CommissionPct:  commission,
	}
}

// warmup прогоняет N транзакций с заданным результатом для провайдера.
func warmup(r *router.Router, providerName string, successes, failures int, latencyMs int64) {
	for i := 0; i < successes; i++ {
		r.RecordResult(providerName, true, latencyMs)
	}
	for i := 0; i < failures; i++ {
		r.RecordResult(providerName, false, latencyMs)
	}
}

// Тесты Select

func TestRouter_Select_EmptyList_ReturnsError(t *testing.T) {
	r := router.NewRouter()

	_, err := r.Select(context.Background(), []*domain.Provider{})
	if err == nil {
		t.Error("Select() with empty list should return error")
	}
}

func TestRouter_Select_SingleProvider_AlwaysReturnsIt(t *testing.T) {
	r := router.NewRouter()
	p := newProvider("only_provider", 1.0)

	for i := 0; i < 20; i++ {
		selected, err := r.Select(context.Background(), []*domain.Provider{p})
		if err != nil {
			t.Fatalf("call %d: unexpected error: %v", i, err)
		}
		if selected.Name != "only_provider" {
			t.Errorf("call %d: selected = %v, want only_provider", i, selected.Name)
		}
	}
}

func TestRouter_Select_ReturnsValidProvider(t *testing.T) {
	r := router.NewRouter()
	providers := []*domain.Provider{
		newProvider("provider_a", 1.0),
		newProvider("provider_b", 2.0),
		newProvider("provider_c", 1.5),
	}

	names := map[string]bool{
		"provider_a": true,
		"provider_b": true,
		"provider_c": true,
	}

	for i := 0; i < 50; i++ {
		selected, err := r.Select(context.Background(), providers)
		if err != nil {
			t.Fatalf("call %d: unexpected error: %v", i, err)
		}
		if !names[selected.Name] {
			t.Errorf("call %d: selected unknown provider %q", i, selected.Name)
		}
	}
}

// Тесты RecordResult и накопления статистики

func TestRouter_RecordResult_PrefersSuccessfulProvider(t *testing.T) {
	r := router.NewRouter()

	providerGood := newProvider("provider_good", 1.0)
	providerBad := newProvider("provider_bad", 1.0)
	providers := []*domain.Provider{providerGood, providerBad}

	// Прогреваем: provider_good — 40 успехов, provider_bad — 40 неудач
	warmup(r, "provider_good", 40, 0, 100)
	warmup(r, "provider_bad", 0, 40, 100)

	// Считаем выборы на 200 итерациях
	goodCount := 0
	total := 200

	for i := 0; i < total; i++ {
		selected, err := r.Select(context.Background(), providers)
		if err != nil {
			t.Fatalf("call %d: unexpected error: %v", i, err)
		}
		if selected.Name == "provider_good" {
			goodCount++
		}
	}

	goodPct := float64(goodCount) / float64(total) * 100
	t.Logf("provider_good selected: %d/%d (%.1f%%)", goodCount, total, goodPct)

	// provider_good должен выбираться >> 50% после прогрева
	if goodPct < 75 {
		t.Errorf("provider_good selected %.1f%%, want >= 75%%", goodPct)
	}
}

func TestRouter_RecordResult_EqualProviders_ApproximatelyEqual(t *testing.T) {
	r := router.NewRouter()

	providerA := newProvider("provider_a", 1.0)
	providerB := newProvider("provider_b", 1.0)
	providers := []*domain.Provider{providerA, providerB}

	// Оба провайдера одинаковые — статистика не накапливается
	aCount := 0
	total := 300

	for i := 0; i < total; i++ {
		selected, err := r.Select(context.Background(), providers)
		if err != nil {
			t.Fatalf("call %d: unexpected error: %v", i, err)
		}
		if selected.Name == "provider_a" {
			aCount++
		}
		// Не записываем результаты — оба остаются с априорным Beta(1,1)
	}

	pct := float64(aCount) / float64(total) * 100
	t.Logf("provider_a selected: %d/%d (%.1f%%)", aCount, total, pct)

	// Без накопленной статистики — примерно 50/50, допуск ±20%
	if pct < 30 || pct > 70 {
		t.Errorf("provider_a selected %.1f%%, want 30-70%% (equal providers)", pct)
	}
}

// Тесты OnHalfOpen (сброс параметров)

func TestRouter_OnHalfOpen_IncreasesUncertainty(t *testing.T) {
	r := router.NewRouter()

	// Накапливаем большую статистику успехов
	warmup(r, "provider_a", 100, 0, 100)

	providerA := newProvider("provider_a", 1.0)
	providerB := newProvider("provider_b", 1.0)
	providers := []*domain.Provider{providerA, providerB}

	// До сброса provider_a должен выбираться очень часто
	aCountBefore := 0
	for i := 0; i < 100; i++ {
		selected, _ := r.Select(context.Background(), providers)
		if selected.Name == "provider_a" {
			aCountBefore++
		}
	}

	// Сбрасываем параметры (имитируем переход CB в HalfOpen)
	r.OnHalfOpen("provider_a")

	// После сброса неопределённость выросла — provider_b получает больше шансов
	aCountAfter := 0
	for i := 0; i < 100; i++ {
		selected, _ := r.Select(context.Background(), providers)
		if selected.Name == "provider_a" {
			aCountAfter++
		}
	}

	t.Logf("provider_a before reset: %d/100, after reset: %d/100",
		aCountBefore, aCountAfter)

	// После сброса provider_a должен выбираться реже чем до
	if aCountAfter >= aCountBefore {
		t.Errorf("after OnHalfOpen provider_a count %d >= before %d, want less (more uncertainty)",
			aCountAfter, aCountBefore)
	}
}

func TestRouter_OnHalfOpen_PreservesSuccessProbabilityDirection(t *testing.T) {
	// Математическое свойство: E[θ] = a/(a+b) сохраняется после умножения
	// обоих параметров на одинаковый коэффициент.
	// Проверяем косвенно: после сброса provider_good всё ещё лучше provider_bad.
	r := router.NewRouter()

	// provider_good: высокий success rate
	warmup(r, "provider_good", 80, 20, 100)
	// provider_bad: низкий success rate
	warmup(r, "provider_bad", 20, 80, 100)

	// Сбрасываем оба
	r.OnHalfOpen("provider_good")
	r.OnHalfOpen("provider_bad")

	providerGood := newProvider("provider_good", 1.0)
	providerBad := newProvider("provider_bad", 1.0)
	providers := []*domain.Provider{providerGood, providerBad}

	goodCount := 0
	for i := 0; i < 200; i++ {
		selected, _ := r.Select(context.Background(), providers)
		if selected.Name == "provider_good" {
			goodCount++
		}
	}

	goodPct := float64(goodCount) / 200 * 100
	t.Logf("provider_good after reset: %.1f%%", goodPct)

	// Даже после сброса provider_good должен выбираться чаще
	// (репутация сохранена, только выросла неопределённость)
	if goodPct < 55 {
		t.Errorf("provider_good selected %.1f%% after reset, want >= 55%% (reputation preserved)", goodPct)
	}
}

// Тест влияния комиссии

func TestRouter_Select_PrefersLowerCommission_WhenSuccessEqual(t *testing.T) {
	r := router.NewRouter()

	// Оба провайдера одинаковые по success rate
	warmup(r, "provider_cheap", 50, 50, 100)
	warmup(r, "provider_expensive", 50, 50, 100)

	providerCheap := newProvider("provider_cheap", 0.5)         // низкая комиссия
	providerExpensive := newProvider("provider_expensive", 2.9) // высокая комиссия
	providers := []*domain.Provider{providerCheap, providerExpensive}

	cheapCount := 0
	total := 200

	for i := 0; i < total; i++ {
		selected, err := r.Select(context.Background(), providers)
		if err != nil {
			t.Fatalf("call %d: unexpected error: %v", i, err)
		}
		if selected.Name == "provider_cheap" {
			cheapCount++
		}
	}

	cheapPct := float64(cheapCount) / float64(total) * 100
	t.Logf("provider_cheap selected: %d/%d (%.1f%%)", cheapCount, total, cheapPct)

	// При одинаковом success rate дешёвый провайдер должен выбираться чаще
	if cheapPct < 55 {
		t.Errorf("provider_cheap selected %.1f%%, want >= 55%% (lower commission)", cheapPct)
	}
}

// Тест влияния латентности

func TestRouter_Select_PrefersLowerLatency_WhenSuccessEqual(t *testing.T) {
	r := router.NewRouter()

	// provider_fast: 50ms, provider_slow: 1800ms (близко к SLA)
	warmup(r, "provider_fast", 50, 50, 50)
	warmup(r, "provider_slow", 50, 50, 1800)

	providerFast := newProvider("provider_fast", 1.0)
	providerSlow := newProvider("provider_slow", 1.0)
	providers := []*domain.Provider{providerFast, providerSlow}

	fastCount := 0
	total := 200

	for i := 0; i < total; i++ {
		selected, err := r.Select(context.Background(), providers)
		if err != nil {
			t.Fatalf("call %d: unexpected error: %v", i, err)
		}
		if selected.Name == "provider_fast" {
			fastCount++
		}
	}

	fastPct := float64(fastCount) / float64(total) * 100
	t.Logf("provider_fast selected: %d/%d (%.1f%%)", fastCount, total, fastPct)

	if fastPct < 55 {
		t.Errorf("provider_fast selected %.1f%%, want >= 55%% (lower latency)", fastPct)
	}
}

// Тест затухания (decay)

func TestRouter_Decay_AdaptsToChangingProvider(t *testing.T) {
	// Проверяем что алгоритм адаптируется к изменению качества провайдера.
	// Сначала provider_a хороший, потом становится плохим.
	r := router.NewRouter()

	providerA := newProvider("provider_a", 1.0)
	providerB := newProvider("provider_b", 1.0)
	providers := []*domain.Provider{providerA, providerB}

	// Фаза 1: provider_a хорошый (100 успехов), provider_b плохой (100 неудач)
	warmup(r, "provider_a", 100, 0, 100)
	warmup(r, "provider_b", 0, 100, 100)

	// Фаза 2: ситуация меняется на противоположную
	// provider_a начинает давать сбои, provider_b восстанавливается
	for i := 0; i < 200; i++ {
		r.RecordResult("provider_a", false, 100) // provider_a деградирует
		r.RecordResult("provider_b", true, 100)  // provider_b восстанавливается
	}

	// После адаптации provider_b должен выбираться чаще
	bCount := 0
	total := 100

	for i := 0; i < total; i++ {
		selected, err := r.Select(context.Background(), providers)
		if err != nil {
			t.Fatalf("call %d: unexpected error: %v", i, err)
		}
		if selected.Name == "provider_b" {
			bCount++
		}
	}

	bPct := float64(bCount) / float64(total) * 100
	t.Logf("provider_b (recovered) selected: %d/%d (%.1f%%)", bCount, total, bPct)

	if bPct < 60 {
		t.Errorf("provider_b selected %.1f%% after recovery, want >= 60%% (decay working)", bPct)
	}
}

// Тест детерминированности при фиксированных данных

func TestRouter_Select_NeverPanics(t *testing.T) {
	// Stress-тест: много провайдеров, много вызовов, разные комиссии.
	// Просто убеждаемся что не паникует.
	r := router.NewRouter()

	providers := make([]*domain.Provider, 10)
	for i := range providers {
		name := fmt.Sprintf("provider_%d", i)
		providers[i] = newProvider(name, float64(i)*0.3)
		warmup(r, name, i*5, (10-i)*5, int64(i*50+50))
	}

	defer func() {
		if rec := recover(); rec != nil {
			t.Errorf("Select() panicked: %v", rec)
		}
	}()

	for i := 0; i < 1000; i++ {
		_, err := r.Select(context.Background(), providers)
		if err != nil {
			t.Fatalf("call %d: unexpected error: %v", i, err)
		}
	}
}

func TestRouter_GetParams_ReturnsAlphaBeta(t *testing.T) {
	r := router.NewRouter()

	// До любых записей — априорные значения (alpha=1, beta=1)
	alpha, beta := r.GetParams("new_provider")

	if alpha <= 0 {
		t.Errorf("alpha = %v, want > 0", alpha)
	}
	if beta <= 0 {
		t.Errorf("beta = %v, want > 0", beta)
	}
}

func TestRouter_GetParams_ChangesAfterRecordResult(t *testing.T) {
	r := router.NewRouter()

	alphaBefore, betaBefore := r.GetParams("provider_a")

	// Записываем успехи — alpha должна вырасти
	for i := 0; i < 10; i++ {
		r.RecordResult("provider_a", true, 100)
	}

	alphaAfter, _ := r.GetParams("provider_a")

	if alphaAfter <= alphaBefore {
		t.Errorf("alpha after successes (%v) should be > alpha before (%v)",
			alphaAfter, alphaBefore)
	}

	_ = betaBefore
}

func TestRouter_GetParams_BetaIncreasesOnFailure(t *testing.T) {
	r := router.NewRouter()

	_, betaBefore := r.GetParams("provider_b")

	// Записываем неудачи — beta должна вырасти
	for i := 0; i < 10; i++ {
		r.RecordResult("provider_b", false, 100)
	}

	_, betaAfter := r.GetParams("provider_b")

	if betaAfter <= betaBefore {
		t.Errorf("beta after failures (%v) should be > beta before (%v)",
			betaAfter, betaBefore)
	}
}

func TestNewRouterWithStore_NotNil(t *testing.T) {
	// NewRouterWithStore(nil) — store=nil допустим (работает без персистентности)
	r := router.NewRouterWithStore(nil)
	if r == nil {
		t.Fatal("NewRouterWithStore(nil) returned nil")
	}
}

func TestNewRouterWithStore_WorksLikeNewRouter(t *testing.T) {
	// С nil store поведение идентично NewRouter()
	r := router.NewRouterWithStore(nil)

	p := newProvider("provider_x", 1.0)

	selected, err := r.Select(context.Background(), []*domain.Provider{p})
	if err != nil {
		t.Fatalf("Select() error: %v", err)
	}
	if selected.Name != "provider_x" {
		t.Errorf("selected = %q, want %q", selected.Name, "provider_x")
	}
}
