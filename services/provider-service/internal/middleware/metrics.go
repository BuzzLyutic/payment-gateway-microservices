package middleware

import (
	"fmt"
	"net/http"
	"time"

	"github.com/BuzzLyutic/payment-gateway-microservices/services/provider-service/internal/metrics"
)

func newResponseWriter(w http.ResponseWriter) *responseWriter {
	return &responseWriter{w, http.StatusOK}
}

// Metrics собирает HTTP-метрики для каждого запроса.
func Metrics(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rw := newResponseWriter(w)

		next.ServeHTTP(rw, r)

		duration := time.Since(start).Seconds()
		status := fmt.Sprintf("%d", rw.statusCode)

		metrics.HTTPRequestsTotal.WithLabelValues(r.Method, r.URL.Path, status).Inc()
		metrics.HTTPRequestDuration.WithLabelValues(r.Method, r.URL.Path).Observe(duration)
	})
}
