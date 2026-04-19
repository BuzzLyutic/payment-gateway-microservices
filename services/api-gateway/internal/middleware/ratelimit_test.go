package middleware_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/BuzzLyutic/payment-gateway-microservices/services/api-gateway/internal/auth"
	"github.com/BuzzLyutic/payment-gateway-microservices/services/api-gateway/internal/middleware"
	"github.com/redis/go-redis/v9"
)

// helpers

// withMerchant оборачивает handler, добавляя MerchantInfo в контекст.
// Имитирует то, что делает Auth middleware перед RateLimit.
func withMerchant(info *auth.MerchantInfo, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := middleware.WithMerchant(r.Context(), info)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// deadRedis возвращает Redis-клиент на заведомо нерабочий адрес.
func deadRedis() *redis.Client {
	return redis.NewClient(&redis.Options{
		Addr:        "localhost:1", // порт недоступен
		DialTimeout: 50 * time.Millisecond,
	})
}

// tests

func TestRateLimit_NoMerchantInContext_SkipsLimit(t *testing.T) {
	// Merchant не в контексте — RateLimit должен пропустить запрос (защитная ветка).
	// Это не должно происходить в production (Auth идёт раньше),
	// но middleware защищается от этого явно.
	rdb := deadRedis()
	defer rdb.Close()

	called := false
	handler := middleware.RateLimit(rdb, newTestLogger())(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			called = true
			w.WriteHeader(http.StatusOK)
		}),
	)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/payments", nil)
	// Контекст без MerchantInfo.
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if !called {
		t.Error("expected next handler to be called when no merchant in context")
	}
	if rr.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rr.Code)
	}
}

func TestRateLimit_RedisUnavailable_FailOpen(t *testing.T) {
	// Redis недоступен - fail-open: запрос проходит.
	// Доступность важнее строгого rate limiting при сбое Redis.
	rdb := deadRedis()
	defer rdb.Close()

	info := &auth.MerchantInfo{
		MerchantID: "merchant-fail-open",
		RateLimit:  10,
	}

	called := false
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})

	handler := withMerchant(info, middleware.RateLimit(rdb, newTestLogger())(inner))

	req := httptest.NewRequest(http.MethodGet, "/api/v1/payments", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if !called {
		t.Error("fail-open: expected next handler to be called on Redis error")
	}
	if rr.Code != http.StatusOK {
		t.Errorf("fail-open: expected 200, got %d", rr.Code)
	}
}

func TestRateLimit_WithinLimit_PassesThrough(t *testing.T) {
	// Интеграционный тест с реальным Redis (если доступен).
	// Если Redis недоступен — пропускаем через t.Skip.
	rdb := redis.NewClient(&redis.Options{
		Addr:        getRedisAddr(),
		DialTimeout: 200 * time.Millisecond,
	})
	defer rdb.Close()

	if !redisAvailable(rdb) {
		t.Skip("Redis not available, skipping integration test")
	}

	info := &auth.MerchantInfo{
		MerchantID: "merchant-within-limit-" + uniqueSuffix(),
		RateLimit:  100,
	}

	called := false
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})

	handler := withMerchant(info, middleware.RateLimit(rdb, newTestLogger())(inner))

	req := httptest.NewRequest(http.MethodGet, "/api/v1/payments", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if !called {
		t.Error("within limit: expected next handler to be called")
	}
	if rr.Code != http.StatusOK {
		t.Errorf("within limit: expected 200, got %d", rr.Code)
	}
}

func TestRateLimit_ExceedsLimit_Returns429(t *testing.T) {
	// Интеграционный тест: исчерпываем лимит, проверяем 429 + заголовки.
	rdb := redis.NewClient(&redis.Options{
		Addr:        getRedisAddr(),
		DialTimeout: 200 * time.Millisecond,
	})
	defer rdb.Close()

	if !redisAvailable(rdb) {
		t.Skip("Redis not available, skipping integration test")
	}

	// Лимит 1 — первый запрос добавит count=1, второй count=2 > limit=1.
	info := &auth.MerchantInfo{
		MerchantID: "merchant-exceed-" + uniqueSuffix(),
		RateLimit:  1,
	}

	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	rateLimitMW := middleware.RateLimit(rdb, newTestLogger())
	handler := withMerchant(info, rateLimitMW(inner))

	// Первый запрос — count=1, limit=1: 1 > 1 false - проходит.
	req1 := httptest.NewRequest(http.MethodGet, "/api/v1/payments", nil)
	rr1 := httptest.NewRecorder()
	handler.ServeHTTP(rr1, req1)

	if rr1.Code != http.StatusOK {
		t.Errorf("first request: expected 200, got %d", rr1.Code)
	}

	// Второй запрос — count=2 > limit=1 - 429.
	req2 := httptest.NewRequest(http.MethodGet, "/api/v1/payments", nil)
	rr2 := httptest.NewRecorder()
	handler.ServeHTTP(rr2, req2)

	if rr2.Code != http.StatusTooManyRequests {
		t.Errorf("second request: expected 429, got %d", rr2.Code)
	}

	// Проверяем Retry-After заголовок.
	retryAfter := rr2.Header().Get("Retry-After")
	if retryAfter == "" {
		t.Error("expected Retry-After header on 429 response")
	}

	// Проверяем тело ответа.
	var body map[string]any
	if err := json.NewDecoder(rr2.Body).Decode(&body); err != nil {
		t.Fatalf("decode response body: %v", err)
	}
	if body["error"] != "rate limit exceeded" {
		t.Errorf("unexpected error message: %v", body["error"])
	}
	if _, ok := body["retry_after_seconds"]; !ok {
		t.Error("expected retry_after_seconds in response body")
	}
}

func TestRateLimit_RetryAfterHeader_ValidRange(t *testing.T) {
	// Проверяем что Retry-After содержит разумное значение (1-60 секунд).
	rdb := redis.NewClient(&redis.Options{
		Addr:        getRedisAddr(),
		DialTimeout: 200 * time.Millisecond,
	})
	defer rdb.Close()

	if !redisAvailable(rdb) {
		t.Skip("Redis not available, skipping integration test")
	}

	info := &auth.MerchantInfo{
		MerchantID: "merchant-retry-" + uniqueSuffix(),
		RateLimit:  0, // лимит 0 - любой запрос exceeds (count=1 > 0)
	}

	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	handler := withMerchant(info, middleware.RateLimit(rdb, newTestLogger())(inner))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusTooManyRequests {
		t.Fatalf("expected 429, got %d", rr.Code)
	}

	retryAfter := rr.Header().Get("Retry-After")
	if retryAfter == "" {
		t.Fatal("expected Retry-After header")
	}

	// Значение должно быть в диапазоне 1-61 секунда.
	var seconds int
	if _, err := fmt.Sscanf(retryAfter, "%d", &seconds); err != nil {
		t.Fatalf("Retry-After is not a number: %q", retryAfter)
	}
	if seconds < 1 || seconds > 61 {
		t.Errorf("Retry-After out of expected range [1,61]: %d", seconds)
	}
}

// secondsUntilNextMinute (через публичный API)

// secondsUntilNextMinute недоступна напрямую (unexported).
// Тестируем косвенно через Retry-After заголовок в ответе 429.
// Прямой тест — через internal_test пакет (см. ниже).

// helpers

func getRedisAddr() string {
	if addr := os.Getenv("REDIS_URL"); addr != "" {
		// Парсим "redis://localhost:6379" - "localhost:6379"
		addr = strings.TrimPrefix(addr, "redis://")
		return addr
	}
	return "localhost:6379"
}

func redisAvailable(rdb *redis.Client) bool {
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	return rdb.Ping(ctx).Err() == nil
}

func uniqueSuffix() string {
	return fmt.Sprintf("%d", time.Now().UnixNano())
}
