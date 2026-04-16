package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// Все метрики сервиса в одном месте.
// promauto регистрирует их в DefaultRegisterer автоматически.

var (
	// Бизнес-метрики

	// PaymentsTotal — счётчик обработанных платежей по провайдеру и статусу.
	// Labels: provider (название провайдера), status (captured/declined/failed)
	PaymentsTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "provider",
			Name:      "payments_total",
			Help:      "Total number of payments processed, by provider and status.",
		},
		[]string{"provider", "status"},
	)

	// PaymentDuration — гистограмма латентности вызова провайдера.
	// Buckets покрывают диапазон от 50ms до 5s — типичный для платёжных API.
	PaymentDuration = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Namespace: "provider",
			Name:      "payment_duration_seconds",
			Help:      "Payment processing duration in seconds.",
			Buckets:   []float64{0.05, 0.1, 0.2, 0.5, 1.0, 2.0, 5.0},
		},
		[]string{"provider"},
	)

	// PaymentRetries — счётчик retry-попыток по провайдеру.
	PaymentRetries = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "provider",
			Name:      "payment_retries_total",
			Help:      "Total number of retry attempts by provider.",
		},
		[]string{"provider"},
	)

	// Circuit Breaker

	// CircuitBreakerState — текущее состояние CB по провайдеру.
	// Gauge: 0 = closed, 1 = open, 2 = half-open.
	// Позволяет строить алерты: если state == 1 дольше N минут — PagerDuty.
	CircuitBreakerState = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: "provider",
			Name:      "circuit_breaker_state",
			Help:      "Circuit breaker state: 0=closed, 1=open, 2=half-open.",
		},
		[]string{"provider"},
	)

	// CircuitBreakerTransitions — счётчик переходов CB по провайдеру и направлению.
	CircuitBreakerTransitions = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "provider",
			Name:      "circuit_breaker_transitions_total",
			Help:      "Total circuit breaker state transitions.",
		},
		[]string{"provider", "from", "to"},
	)

	// Thompson Sampling

	// ThompsonAlpha — текущее значение alpha (успехи) по провайдеру.
	// Gauge — значение меняется, не накапливается.
	ThompsonAlpha = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: "provider",
			Name:      "thompson_alpha",
			Help:      "Thompson Sampling alpha parameter (successes) by provider.",
		},
		[]string{"provider"},
	)

	// ThompsonBeta — текущее значение beta (неудачи) по провайдеру.
	ThompsonBeta = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: "provider",
			Name:      "thompson_beta",
			Help:      "Thompson Sampling beta parameter (failures) by provider.",
		},
		[]string{"provider"},
	)

	// ThompsonSuccessProbability — E[θ] = alpha/(alpha+beta).
	// Это главная метрика для дашборда: показывает оценку качества провайдера.
	ThompsonSuccessProbability = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: "provider",
			Name:      "thompson_success_probability",
			Help:      "Thompson Sampling estimated success probability (alpha/(alpha+beta)).",
		},
		[]string{"provider"},
	)

	// HTTP-метрики

	// HTTPRequestsTotal — счётчик HTTP-запросов по методу, пути и статус-коду.
	HTTPRequestsTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "provider",
			Name:      "http_requests_total",
			Help:      "Total HTTP requests by method, path and status code.",
		},
		[]string{"method", "path", "status"},
	)

	// HTTPRequestDuration — латентность HTTP-запросов.
	HTTPRequestDuration = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Namespace: "provider",
			Name:      "http_request_duration_seconds",
			Help:      "HTTP request duration in seconds.",
			Buckets:   prometheus.DefBuckets,
		},
		[]string{"method", "path"},
	)

	// NATS-метрики

	// NATSMessagesProcessed — обработанные сообщения по subject и результату.
	NATSMessagesProcessed = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "provider",
			Name:      "nats_messages_processed_total",
			Help:      "Total NATS messages processed by subject and result.",
		},
		[]string{"subject", "result"}, // result: ack, nak, term
	)
)
