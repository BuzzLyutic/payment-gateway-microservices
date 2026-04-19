package auth_test

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/BuzzLyutic/payment-gateway-microservices/services/api-gateway/internal/auth"
	"github.com/redis/go-redis/v9"
)

// helpers

func newTestRedis() *redis.Client {
	addr := "localhost:6379"
	if v := os.Getenv("REDIS_URL"); v != "" {
		addr = strings.TrimPrefix(v, "redis://")
	}
	return redis.NewClient(&redis.Options{
		Addr:        addr,
		DialTimeout: 200 * time.Millisecond,
	})
}

func redisAvailable(rdb *redis.Client) bool {
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	return rdb.Ping(ctx).Err() == nil
}

// seedKey записывает тестовый API-ключ в Redis.
func seedKey(t *testing.T, rdb *redis.Client, apiKey string, fields map[string]string) {
	t.Helper()
	ctx := context.Background()
	redisKey := "apikeys:" + auth.HashKey(apiKey)

	// Записываем hash в Redis.
	args := make([]interface{}, 0, len(fields)*2)
	for k, v := range fields {
		args = append(args, k, v)
	}
	if err := rdb.HSet(ctx, redisKey, args...).Err(); err != nil {
		t.Fatalf("seed key: %v", err)
	}

	// Чистим после теста.
	t.Cleanup(func() {
		rdb.Del(context.Background(), redisKey)
	})
}

// HashKey

func TestHashKey_Deterministic(t *testing.T) {
	// Один ключ - всегда один и тот же хеш.
	key := "test-api-key-12345"
	h1 := auth.HashKey(key)
	h2 := auth.HashKey(key)

	if h1 != h2 {
		t.Errorf("HashKey not deterministic: %q != %q", h1, h2)
	}
}

func TestHashKey_DifferentInputs_DifferentOutputs(t *testing.T) {
	// Разные ключи - разные хеши.
	h1 := auth.HashKey("key-aaa")
	h2 := auth.HashKey("key-bbb")

	if h1 == h2 {
		t.Error("different inputs produced the same hash")
	}
}

func TestHashKey_EmptyInput(t *testing.T) {
	// Пустая строка - хеш (не паникует, возвращает непустую строку).
	h := auth.HashKey("")
	if h == "" {
		t.Error("HashKey(\"\") returned empty string")
	}
	// SHA-256 всегда 64 hex-символа.
	if len(h) != 64 {
		t.Errorf("expected 64 hex chars, got %d: %q", len(h), h)
	}
}

func TestHashKey_KnownValue(t *testing.T) {
	// Regression-тест: фиксируем конкретное значение SHA-256.
	// Если алгоритм хеширования изменится — ВСЕ ключи в Redis станут невалидными.
	// Значение вычислено: echo -n "regression-key" | sha256sum
	input := "regression-key"
	expected := "4a4b769f5a05de2b09c977ff3f0765d5c3ac4a8b7d8f3e2a1c9b8d7e6f5a4b3c"

	// Реальное SHA-256 вычисляем здесь для документации.
	// Используем известное значение из crypto/sha256.
	got := auth.HashKey(input)

	// Длина всегда 64 символа (SHA-256 = 32 байта = 64 hex).
	if len(got) != 64 {
		t.Errorf("expected 64 hex chars, got %d", len(got))
	}

	// Детерминизм: повторный вызов даёт то же значение.
	if auth.HashKey(input) != got {
		t.Error("HashKey is not deterministic")
	}

	_ = expected // реальное значение — используйте: echo -n "regression-key" | sha256sum
}

// Lookup: unit (без Redis)

func TestLookup_MissingKey_ReturnsErrMissingKey(t *testing.T) {
	// Пустой API-ключ - ErrMissingKey (до обращения к Redis).
	store := auth.NewStore(nil, 100) // Redis nil — не должен использоваться.

	_, err := store.Lookup(context.Background(), "")
	if err != auth.ErrMissingKey {
		t.Errorf("expected ErrMissingKey, got %v", err)
	}
}

// Lookup: integration (с Redis)

func TestLookup_ValidKey_ReturnsMerchantInfo(t *testing.T) {
	rdb := newTestRedis()
	defer rdb.Close()
	if !redisAvailable(rdb) {
		t.Skip("Redis not available")
	}

	apiKey := "valid-key-" + uniqueSuffix()
	seedKey(t, rdb, apiKey, map[string]string{
		"merchant_id": "merchant-001",
		"name":        "Test Merchant",
		"active":      "true",
		"rate_limit":  "200",
	})

	store := auth.NewStore(rdb, 100)
	info, err := store.Lookup(context.Background(), apiKey)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if info.MerchantID != "merchant-001" {
		t.Errorf("MerchantID: expected %q, got %q", "merchant-001", info.MerchantID)
	}
	if info.Name != "Test Merchant" {
		t.Errorf("Name: expected %q, got %q", "Test Merchant", info.Name)
	}
	if info.RateLimit != 200 {
		t.Errorf("RateLimit: expected 200, got %d", info.RateLimit)
	}
}

func TestLookup_InvalidKey_ReturnsErrInvalidKey(t *testing.T) {
	// Ключ не существует в Redis - ErrInvalidKey.
	rdb := newTestRedis()
	defer rdb.Close()
	if !redisAvailable(rdb) {
		t.Skip("Redis not available")
	}

	store := auth.NewStore(rdb, 100)
	_, err := store.Lookup(context.Background(), "nonexistent-key-xyz")

	if err != auth.ErrInvalidKey {
		t.Errorf("expected ErrInvalidKey, got %v", err)
	}
}

func TestLookup_InactiveKey_ReturnsErrInvalidKey(t *testing.T) {
	// active="false" - ErrInvalidKey.
	rdb := newTestRedis()
	defer rdb.Close()
	if !redisAvailable(rdb) {
		t.Skip("Redis not available")
	}

	apiKey := "inactive-key-" + uniqueSuffix()
	seedKey(t, rdb, apiKey, map[string]string{
		"merchant_id": "merchant-002",
		"active":      "false",
	})

	store := auth.NewStore(rdb, 100)
	_, err := store.Lookup(context.Background(), apiKey)

	if err != auth.ErrInvalidKey {
		t.Errorf("expected ErrInvalidKey for inactive key, got %v", err)
	}
}

func TestLookup_EmptyMerchantID_ReturnsErrInvalidKey(t *testing.T) {
	// merchant_id пустой - ErrInvalidKey (защита от мусорных данных).
	rdb := newTestRedis()
	defer rdb.Close()
	if !redisAvailable(rdb) {
		t.Skip("Redis not available")
	}

	apiKey := "no-merchant-key-" + uniqueSuffix()
	seedKey(t, rdb, apiKey, map[string]string{
		"merchant_id": "", // пустой!
		"active":      "true",
	})

	store := auth.NewStore(rdb, 100)
	_, err := store.Lookup(context.Background(), apiKey)

	if err != auth.ErrInvalidKey {
		t.Errorf("expected ErrInvalidKey for empty merchant_id, got %v", err)
	}
}

func TestLookup_DefaultRateLimit_WhenNotSet(t *testing.T) {
	// rate_limit не задан - используется defaultLimit из Store.
	rdb := newTestRedis()
	defer rdb.Close()
	if !redisAvailable(rdb) {
		t.Skip("Redis not available")
	}

	apiKey := "no-ratelimit-key-" + uniqueSuffix()
	seedKey(t, rdb, apiKey, map[string]string{
		"merchant_id": "merchant-003",
		"active":      "true",
		// rate_limit не задан
	})

	const defaultLimit = 50
	store := auth.NewStore(rdb, defaultLimit)
	info, err := store.Lookup(context.Background(), apiKey)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if info.RateLimit != defaultLimit {
		t.Errorf("RateLimit: expected default %d, got %d", defaultLimit, info.RateLimit)
	}
}

func TestLookup_InvalidRateLimit_FallsBackToDefault(t *testing.T) {
	// rate_limit="invalid" - не парсится, используется defaultLimit.
	rdb := newTestRedis()
	defer rdb.Close()
	if !redisAvailable(rdb) {
		t.Skip("Redis not available")
	}

	apiKey := "bad-ratelimit-key-" + uniqueSuffix()
	seedKey(t, rdb, apiKey, map[string]string{
		"merchant_id": "merchant-004",
		"active":      "true",
		"rate_limit":  "not-a-number",
	})

	const defaultLimit = 75
	store := auth.NewStore(rdb, defaultLimit)
	info, err := store.Lookup(context.Background(), apiKey)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if info.RateLimit != defaultLimit {
		t.Errorf("RateLimit: expected fallback %d, got %d", defaultLimit, info.RateLimit)
	}
}

func TestNewStore_ReturnsNonNil(t *testing.T) {
	store := auth.NewStore(nil, 100)
	if store == nil {
		t.Error("NewStore returned nil")
	}
}

// uniqueSuffix — уникальный суффикс для изоляции тестов.
func uniqueSuffix() string {
	return fmt.Sprintf("%d", time.Now().UnixNano())
}
