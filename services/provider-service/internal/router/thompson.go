package router

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"math/rand"
	"sync"
	"time"

	"github.com/BuzzLyutic/payment-gateway-microservices/services/provider-service/internal/domain"
)

// Профили весовых коэффициентов из алгоритма (формула 7).
const (
	w1Default = 0.60 // вес вероятности успеха (θ)
	w2Default = 0.25 // вес латентности
	w3Default = 0.15 // вес комиссии

	// SLA и лимит комиссии — можно вынести в конфиг
	latencySLAMs  = 2000.0 // 2 секунды
	maxCommission = 3.0    // 3%

	// Коэффициент затухания γ (формула 5, 6)
	gamma = 0.99

	// Коэффициент сброса ρ при переходе CB в HalfOpen (формула 8)
	rhoReset = 0.1
)

// ProviderStats хранит параметры бета-распределения и статистику латентности
// для одного провайдера.
type ProviderStats struct {
	mu sync.Mutex

	// Параметры бета-распределения Beta(alpha, beta)
	alpha float64 // успехи + 1 (априорное = 1)
	beta  float64 // неудачи + 1 (априорное = 1)

	// Накопленная латентность для p95
	// Используем простое скользящее окно последних N измерений
	latencies []float64
	maxWindow int

	// Комиссия берётся из domain.Provider.CommissionPct
}

func newProviderStats() *ProviderStats {
	return &ProviderStats{
		alpha:     1.0, // априорное Beta(1,1)
		beta:      1.0,
		latencies: make([]float64, 0, 100),
		maxWindow: 100,
	}
}

// sample генерирует θᵢ ~ Beta(alpha, beta) через метод Johnk.
// Стандартная библиотека Go не имеет бета-распределения,
// используем гамма-сэмплирование: Beta(a,b) = Gamma(a,1) / (Gamma(a,1) + Gamma(b,1))
func (s *ProviderStats) sample() float64 {
	s.mu.Lock()
	defer s.mu.Unlock()

	x := sampleGamma(s.alpha)
	y := sampleGamma(s.beta)
	if x+y == 0 {
		return 0.5
	}
	return x / (x + y)
}

// p95Latency вычисляет 95-й процентиль из окна наблюдений.
// Если наблюдений нет — возвращает 0 (не штрафуем нового провайдера).
func (s *ProviderStats) p95Latency() float64 {
	s.mu.Lock()
	defer s.mu.Unlock()

	n := len(s.latencies)
	if n == 0 {
		return 0
	}

	// Копируем чтобы не сортировать оригинал
	sorted := make([]float64, n)
	copy(sorted, s.latencies)
	sortFloat64(sorted)

	idx := int(float64(n) * 0.95)
	if idx >= n {
		idx = n - 1
	}
	return sorted[idx]
}

// recordResult обновляет параметры по формулам (5) и (6).
func (s *ProviderStats) recordResult(success bool, latencyMs float64) {
	s.mu.Lock()
	defer s.mu.Unlock()

	r := 0.0
	if success {
		r = 1.0
	}

	// Формула (5): aᵢ ← max(1, γ·aᵢ + r)
	s.alpha = math.Max(1.0, gamma*s.alpha+r)
	// Формула (6): bᵢ ← max(1, γ·bᵢ + (1−r))
	s.beta = math.Max(1.0, gamma*s.beta+(1.0-r))

	// Обновляем скользящее окно латентности
	if latencyMs > 0 {
		if len(s.latencies) >= s.maxWindow {
			// Сдвигаем окно: убираем самое старое
			s.latencies = s.latencies[1:]
		}
		s.latencies = append(s.latencies, latencyMs)
	}
}

// resetForHalfOpen применяет формулу (8): αᵢ ← max(1, αᵢ·ρ), βᵢ ← max(1, βᵢ·ρ).
// Сохраняет E[θ] но увеличивает дисперсию — провайдер "помнит" репутацию
// но алгоритм становится менее уверен в его текущих характеристиках.
func (s *ProviderStats) resetForHalfOpen() {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.alpha = math.Max(1.0, s.alpha*rhoReset)
	s.beta = math.Max(1.0, s.beta*rhoReset)

	slog.Info("thompson sampling: beta params reset for half-open",
		"alpha", s.alpha,
		"beta", s.beta,
	)
}

// successProbability возвращает E[θ] = a/(a+b) для логирования.
func (s *ProviderStats) successProbability() float64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.alpha / (s.alpha + s.beta)
}

// Router реализует маршрутизацию на основе Thompson Sampling.
type Router struct {
	mu    sync.RWMutex
	stats map[string]*ProviderStats // ключ: provider.Name
	// store — персистентность в Redis.
	// nil если Redis недоступен — работаем без персистентности.
	store *Store

	// txCounts — счётчик транзакций по провайдеру для периодического flush.
	txCounts map[string]int
}

func NewRouter() *Router {
	return &Router{
		stats:    make(map[string]*ProviderStats),
		txCounts: make(map[string]int),
	}
}

// NewRouterWithStore создаёт Router с персистентностью в Redis.
func NewRouterWithStore(store *Store) *Router {
	return &Router{
		stats:    make(map[string]*ProviderStats),
		store:    store,
		txCounts: make(map[string]int),
	}
}

// getOrCreate возвращает или инициализирует статистику для провайдера.
func (r *Router) getOrCreate(providerName string) *ProviderStats {
	r.mu.RLock()
	s, ok := r.stats[providerName]
	r.mu.RUnlock()
	if ok {
		return s
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	if s, ok = r.stats[providerName]; ok {
		return s
	}
	s = newProviderStats()
	r.stats[providerName] = s
	return s
}

// Select выбирает оптимального провайдера по формуле (7).
// providers — уже отфильтрованный список (валюта, метод, CB не Open).
// Возвращает ошибку если список пуст.
func (r *Router) Select(ctx context.Context, providers []*domain.Provider) (*domain.Provider, error) {
	if len(providers) == 0 {
		return nil, fmt.Errorf("no providers to select from")
	}

	if len(providers) == 1 {
		return providers[0], nil
	}

	bestScore := -1.0
	var selected *domain.Provider

	for _, p := range providers {
		stats := r.getOrCreate(p.Name)

		// θᵢ ~ Beta(aᵢ, bᵢ) — стохастический сэмпл
		theta := stats.sample()

		// Компонент латентности: max(0, 1 - Lp95/LSLA)
		p95 := stats.p95Latency()
		latencyScore := 0.0
		if p95 == 0 {
			// Новый провайдер — не штрафуем, даём нейтральный score
			latencyScore = 0.5
		} else {
			latencyScore = math.Max(0, 1.0-p95/latencySLAMs)
		}

		// Компонент комиссии: max(0, 1 - Cᵢ/Cconfig)
		commissionScore := math.Max(0, 1.0-p.CommissionPct/maxCommission)

		// Формула (7): score = w1·θ + w2·latency + w3·commission
		score := w1Default*theta + w2Default*latencyScore + w3Default*commissionScore

		slog.Debug("thompson sampling score",
			"provider", p.Name,
			"theta", fmt.Sprintf("%.4f", theta),
			"success_prob", fmt.Sprintf("%.4f", stats.successProbability()),
			"p95_ms", fmt.Sprintf("%.1f", p95),
			"latency_score", fmt.Sprintf("%.4f", latencyScore),
			"commission_pct", p.CommissionPct,
			"commission_score", fmt.Sprintf("%.4f", commissionScore),
			"total_score", fmt.Sprintf("%.4f", score),
		)

		if selected == nil || score > bestScore {
			bestScore = score
			selected = p
		}
	}

	return selected, nil
}

// RecordResult обновляет статистику после обработки транзакции.
// success=true если статус captured, false для declined/failed.
func (r *Router) RecordResult(providerName string, success bool, latencyMs int64) {
	stats := r.getOrCreate(providerName)
	stats.recordResult(success, float64(latencyMs))

	slog.Info("thompson sampling: result recorded",
		"provider", providerName,
		"success", success,
		"latency_ms", latencyMs,
		"success_prob", fmt.Sprintf("%.4f", stats.successProbability()),
	)

	// Периодический flush в Redis каждые persistEvery транзакций
	r.mu.Lock()
	r.txCounts[providerName]++
	count := r.txCounts[providerName]
	r.mu.Unlock()

	if count%persistEvery == 0 {
		r.persistStats(providerName, stats)
	}
}

// OnHalfOpen вызывается Circuit Breaker при переходе провайдера в HalfOpen.
// Это колбэк который передаётся в circuitbreaker.Manager.
func (r *Router) OnHalfOpen(providerName string) {
	stats := r.getOrCreate(providerName)
	stats.resetForHalfOpen()

	// Сразу сохраняем сброшенные параметры —
	// при рестарте после CB события должны восстановиться корректно
	r.persistStats(providerName, stats)
}

// GetParams возвращает текущие параметры бета-распределения провайдера.
// Используется для экспорта метрик Prometheus.
func (r *Router) GetParams(providerName string) (alpha, beta float64) {
	stats := r.getOrCreate(providerName)
	stats.mu.Lock()
	defer stats.mu.Unlock()
	return stats.alpha, stats.beta
}

// LoadFromStore загружает статистику из Redis при старте.
// Вызывается один раз из main.go после создания Router.
func (r *Router) LoadFromStore(ctx context.Context, providerNames []string) error {
	if r.store == nil {
		return nil
	}

	snapshots, err := r.store.LoadAll(ctx, providerNames)
	if err != nil {
		return fmt.Errorf("load from store: %w", err)
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	for name, snap := range snapshots {
		stats := &ProviderStats{
			alpha:     snap.Alpha,
			beta:      snap.Beta,
			latencies: snap.Latencies,
			maxWindow: 100,
		}
		r.stats[name] = stats

		slog.Info("thompson sampling: loaded stats from redis",
			"provider", name,
			"alpha", fmt.Sprintf("%.4f", snap.Alpha),
			"beta", fmt.Sprintf("%.4f", snap.Beta),
			"latencies_count", len(snap.Latencies),
			"success_prob", fmt.Sprintf("%.4f", snap.Alpha/(snap.Alpha+snap.Beta)),
		)
	}

	// Провайдеры без сохранённой статистики получат априорное Beta(1,1)
	// при первом обращении через getOrCreate
	loaded := len(snapshots)
	slog.Info("thompson sampling: restore complete",
		"loaded", loaded,
		"total", len(providerNames),
		"fresh_start", len(providerNames)-loaded,
	)

	return nil
}

// persistStats сохраняет статистику провайдера в Redis.
// Вызывается внутри RecordResult каждые persistEvery транзакций.
// Не блокирует основной поток — ошибки только логируются.
func (r *Router) persistStats(providerName string, stats *ProviderStats) {
	if r.store == nil {
		slog.Warn("thompson sampling: store is nil, skipping persist",
            "provider", providerName,
        )
		return
	}

	// Снимаем снапшот под локом stats
	stats.mu.Lock()
	alpha := stats.alpha
	beta := stats.beta
	latencies := make([]float64, len(stats.latencies))
	copy(latencies, stats.latencies)
	stats.mu.Unlock()

	// Сохраняем асинхронно чтобы не блокировать обработку транзакций
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()

		if err := r.store.Save(ctx, providerName, alpha, beta, latencies); err != nil {
			slog.Warn("thompson sampling: failed to persist stats",
				"provider", providerName,
				"error", err,
			)
			return
		}

		slog.Debug("thompson sampling: stats persisted",
			"provider", providerName,
			"alpha", fmt.Sprintf("%.4f", alpha),
			"beta", fmt.Sprintf("%.4f", beta),
		)
	}()
}

// --- вспомогательные функции ---

// sampleGamma генерирует сэмпл из Gamma(shape, 1) методом Marsaglia-Tsang.
func sampleGamma(shape float64) float64 {
	if shape < 1.0 {
		// Для shape < 1: Gamma(shape) = Gamma(shape+1) * U^(1/shape)
		return sampleGamma(shape+1.0) * math.Pow(rand.Float64(), 1.0/shape)
	}

	d := shape - 1.0/3.0
	c := 1.0 / math.Sqrt(9.0*d)

	for {
		x := rand.NormFloat64()
		v := 1.0 + c*x
		if v <= 0 {
			continue
		}
		v = v * v * v
		u := rand.Float64()

		if u < 1.0-0.0331*(x*x)*(x*x) {
			return d * v
		}
		if math.Log(u) < 0.5*x*x+d*(1.0-v+math.Log(v)) {
			return d * v
		}
	}
}

// sortFloat64 — простая сортировка для небольших срезов.
func sortFloat64(a []float64) {
	// insertion sort — эффективен для n <= 100
	for i := 1; i < len(a); i++ {
		key := a[i]
		j := i - 1
		for j >= 0 && a[j] > key {
			a[j+1] = a[j]
			j--
		}
		a[j+1] = key
	}
}
