package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	// HTTP метрики
	HTTPRequestsTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "auth_service_http_requests_total",
			Help: "Total number of HTTP requests",
		},
		[]string{"method", "path", "status"},
	)

	HTTPRequestDuration = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "auth_service_http_request_duration_seconds",
			Help:    "HTTP request duration in seconds",
			Buckets: prometheus.DefBuckets,
		},
		[]string{"method", "path"},
	)

	// Database метрики
	DBQueriesTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "auth_service_db_queries_total",
			Help: "Total number of database queries",
		},
		[]string{"operation", "status"},
	)

	DBQueryDuration = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "auth_service_db_query_duration_seconds",
			Help:    "Database query duration in seconds",
			Buckets: prometheus.DefBuckets,
		},
		[]string{"operation"},
	)

	// Redis метрики
	RedisOperationsTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "auth_service_redis_operations_total",
			Help: "Total number of Redis operations",
		},
		[]string{"operation", "status"},
	)

	// Бизнес метрики
	MerchantsRegisteredTotal = promauto.NewCounter(
		prometheus.CounterOpts{
			Name: "auth_service_merchants_registered_total",
			Help: "Total number of registered merchants",
		},
	)

	ActiveSessionsGauge = promauto.NewGauge(
		prometheus.GaugeOpts{
			Name: "auth_service_active_sessions",
			Help: "Number of active sessions",
		},
	)
)
