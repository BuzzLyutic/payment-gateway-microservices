package circuitbreaker

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"
)

// State - состояние Circuit Breaker.
type State int

const (
	StateClosed   State = iota // нормальная работа
	StateOpen                  // провайдер недоступен, запросы отклоняются
	StateHalfOpen              // тестовый режим, пропускается один запрос
)

func (s State) String() string {
	switch s {
	case StateClosed:
		return "closed"
	case StateOpen:
		return "open"
	case StateHalfOpen:
		return "half-open"
	default:
		return "unknown"
	}
}

// Config - конфигурация Circuit Breaker.
type Config struct {
	// Сколько подряд failures переводят CB в Open
	FailureThreshold int
	// Как долго CB остаётся в Open перед переходом в HalfOpen
	OpenTimeout time.Duration
	// Сколько успехов в HalfOpen нужно для возврата в Closed
	HalfOpenSuccesses int
}

func DefaultConfig() Config {
	return Config{
		FailureThreshold:  5,
		OpenTimeout:       30 * time.Second,
		HalfOpenSuccesses: 2,
	}
}

// Breaker - Circuit Breaker для одного провайдера.
// Потокобезопасен.
type Breaker struct {
	mu sync.Mutex

	providerName string
	config       Config
	state        State

	consecutiveFailures int
	consecutiveSuccesses int
	openedAt            time.Time

	// Колбэк вызывается при переходе в HalfOpen.
	// Thompson Sampling использует это для сброса параметров.
	onHalfOpen func(providerName string)
}

func New(providerName string, cfg Config, onHalfOpen func(string)) *Breaker {
	return &Breaker{
		providerName: providerName,
		config:       cfg,
		state:        StateClosed,
		onHalfOpen:   onHalfOpen,
	}
}

// ErrOpen возвращается когда CB в состоянии Open.
var ErrOpen = fmt.Errorf("circuit breaker is open")

// Allow проверяет, можно ли пропустить запрос.
// Возвращает ErrOpen если провайдер недоступен.
func (b *Breaker) Allow() error {
	b.mu.Lock()
	defer b.mu.Unlock()

	switch b.state {
	case StateClosed:
		return nil

	case StateOpen:
		// Проверяем истёк ли таймаут
		if time.Since(b.openedAt) >= b.config.OpenTimeout {
			b.transitionTo(StateHalfOpen)
			return nil
		}
		return ErrOpen

	case StateHalfOpen:
		// В HalfOpen пропускаем запросы для теста
		return nil

	default:
		return nil
	}
}

// RecordSuccess записывает успешный результат.
func (b *Breaker) RecordSuccess() {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.consecutiveFailures = 0

	switch b.state {
	case StateHalfOpen:
		b.consecutiveSuccesses++
		if b.consecutiveSuccesses >= b.config.HalfOpenSuccesses {
			b.transitionTo(StateClosed)
		}
	case StateClosed:
		// всё нормально, ничего не делаем
	}
}

// RecordFailure записывает неудачный результат.
func (b *Breaker) RecordFailure() {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.consecutiveSuccesses = 0

	switch b.state {
	case StateClosed:
		b.consecutiveFailures++
		if b.consecutiveFailures >= b.config.FailureThreshold {
			b.transitionTo(StateOpen)
		}

	case StateHalfOpen:
		// Один failure в HalfOpen — сразу обратно в Open
		b.transitionTo(StateOpen)
	}
}

// IsOpen возвращает true если провайдер недоступен.
// Используется роутером для фильтрации.
func (b *Breaker) IsOpen() bool {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.state == StateOpen {
		// Проверяем не истёк ли таймаут (без перехода)
		if time.Since(b.openedAt) >= b.config.OpenTimeout {
			return false // скоро станет HalfOpen при следующем Allow()
		}
		return true
	}
	return false
}

// State возвращает текущее состояние (для метрик/логов).
func (b *Breaker) CurrentState() State {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.state
}

func (b *Breaker) transitionTo(next State) {
	prev := b.state
	b.state = next

	slog.Info("circuit breaker state changed",
		"provider", b.providerName,
		"from", prev.String(),
		"to", next.String(),
	)

	switch next {
	case StateOpen:
		b.openedAt = time.Now()
		b.consecutiveFailures = 0

	case StateHalfOpen:
		b.consecutiveSuccesses = 0
		// Уведомляем Thompson Sampling о восстановлении
		if b.onHalfOpen != nil {
			go b.onHalfOpen(b.providerName)
		}

	case StateClosed:
		b.consecutiveFailures = 0
		b.consecutiveSuccesses = 0
	}
}

// Manager управляет набором Circuit Breaker-ов для всех провайдеров.
type Manager struct {
	mu       sync.RWMutex
	breakers map[string]*Breaker
	config   Config
	// Колбэк передаётся в каждый новый Breaker
	onHalfOpen func(providerName string)
}

func NewManager(cfg Config, onHalfOpen func(string)) *Manager {
	return &Manager{
		breakers:   make(map[string]*Breaker),
		config:     cfg,
		onHalfOpen: onHalfOpen,
	}
}

// Get возвращает или создаёт Breaker для провайдера.
func (m *Manager) Get(providerName string) *Breaker {
	m.mu.RLock()
	b, ok := m.breakers[providerName]
	m.mu.RUnlock()
	if ok {
		return b
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	// Double-check после получения write lock
	if b, ok = m.breakers[providerName]; ok {
		return b
	}

	b = New(providerName, m.config, m.onHalfOpen)
	m.breakers[providerName] = b
	return b
}

// Allow - shortcut для проверки конкретного провайдера.
func (m *Manager) Allow(providerName string) error {
	return m.Get(providerName).Allow()
}

func (m *Manager) RecordSuccess(providerName string) {
	m.Get(providerName).RecordSuccess()
}

func (m *Manager) RecordFailure(providerName string) {
	m.Get(providerName).RecordFailure()
}

func (m *Manager) IsOpen(providerName string) bool {
	return m.Get(providerName).IsOpen()
}

// States возвращает состояние всех CB (для health check / метрик).
func (m *Manager) States(ctx context.Context) map[string]string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	result := make(map[string]string, len(m.breakers))
	for name, b := range m.breakers {
		result[name] = b.CurrentState().String()
	}
	return result
}
