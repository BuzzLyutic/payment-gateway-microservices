package middleware_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/redis/go-redis/v9"
	"github.com/BuzzLyutic/payment-gateway-microservices/services/api-gateway/internal/auth"
	"github.com/BuzzLyutic/payment-gateway-microservices/services/api-gateway/internal/middleware"
)

func TestRateLimit_RedisUnavailable_FailOpen(t *testing.T) {
	// Redis недоступен — запрос должен пройти (fail-open).
	rdb := redis.NewClient(&redis.Options{Addr: "localhost:1"})
	defer rdb.Close()

	handler := applyRateLimitWithMerchant(rdb, &auth.MerchantInfo{
		MerchantID: "merchant-test",
		RateLimit:  10,
	})(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		}),
	)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected fail-open (200), got %d", rr.Code)
	}
}

// applyRateLimitWithMerchant — вспомогательная функция:
// добавляет MerchantInfo в контекст и применяет RateLimit middleware.
func applyRateLimitWithMerchant(
	rdb *redis.Client,
	info *auth.MerchantInfo,
) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := middleware.WithMerchant(r.Context(), info)
			middleware.RateLimit(rdb, newTestLogger())(next).
				ServeHTTP(w, r.WithContext(ctx))
		})
	}
}
