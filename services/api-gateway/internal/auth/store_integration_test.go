//go:build integration

package auth_test

import (
    "context"
    "errors"
    "fmt"
    "testing"
	"time"

    "github.com/redis/go-redis/v9"
    "github.com/BuzzLyutic/payment-gateway-microservices/services/api-gateway/internal/auth"
)

func newTestRedis(t *testing.T) *redis.Client {
	t.Helper()
	rdb := redis.NewClient(&redis.Options{Addr: "localhost:6379"})
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := rdb.Ping(ctx).Err(); err != nil {
		t.Skipf("redis unavailable, skipping: %v", err)
	}
	t.Cleanup(func() { rdb.Close() })
	return rdb
}

// seedAPIKey добавляет тестовый ключ в Redis и регистрирует cleanup.
func seedAPIKey(
	t *testing.T,
	rdb *redis.Client,
	apiKey, merchantID, name string,
	rateLimit int,
	active bool,
) string {
	t.Helper()

	// Используем ту же функцию что и Store — гарантируем совпадение хешей.
	redisKey := fmt.Sprintf("apikeys:%s", auth.HashKey(apiKey))

	activeStr := "false"
	if active {
		activeStr = "true"
	}

	ctx := context.Background()
	err := rdb.HSet(ctx, redisKey,
		"merchant_id", merchantID,
		"name", name,
		"rate_limit", fmt.Sprintf("%d", rateLimit),
		"active", activeStr,
	).Err()
	if err != nil {
		t.Fatalf("seed api key: %v", err)
	}

	// Cleanup — удаляем ключ после теста чтобы не засорять Redis.
	t.Cleanup(func() {
		rdb.Del(context.Background(), redisKey)
	})

	return redisKey
}

func TestLookup_ValidKey(t *testing.T) {
	rdb := newTestRedis(t)
	store := auth.NewStore(rdb, 100)

	apiKey := fmt.Sprintf("test_key_%d", time.Now().UnixNano())
	seedAPIKey(t, rdb, apiKey, "merchant_test", "Test Key", 50, true)

	info, err := store.Lookup(context.Background(), apiKey)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if info.MerchantID != "merchant_test" {
		t.Errorf("expected merchant_id %q, got %q", "merchant_test", info.MerchantID)
	}
	if info.RateLimit != 50 {
		t.Errorf("expected rate_limit 50, got %d", info.RateLimit)
	}
	if info.Name != "Test Key" {
		t.Errorf("expected name %q, got %q", "Test Key", info.Name)
	}
}

func TestLookup_InactiveKey(t *testing.T) {
	rdb := newTestRedis(t)
	store := auth.NewStore(rdb, 100)

	apiKey := fmt.Sprintf("inactive_key_%d", time.Now().UnixNano())
	seedAPIKey(t, rdb, apiKey, "merchant_test", "Inactive", 100, false)

	_, err := store.Lookup(context.Background(), apiKey)
	if !errors.Is(err, auth.ErrInvalidKey) {
		t.Errorf("expected ErrInvalidKey, got: %v", err)
	}
}

func TestLookup_NonexistentKey(t *testing.T) {
	rdb := newTestRedis(t)
	store := auth.NewStore(rdb, 100)

	_, err := store.Lookup(context.Background(), "nonexistent_key_xyz_123")
	if !errors.Is(err, auth.ErrInvalidKey) {
		t.Errorf("expected ErrInvalidKey, got: %v", err)
	}
}

func TestLookup_EmptyKey(t *testing.T) {
	rdb := newTestRedis(t)
	store := auth.NewStore(rdb, 100)

	_, err := store.Lookup(context.Background(), "")
	if !errors.Is(err, auth.ErrMissingKey) {
		t.Errorf("expected ErrMissingKey, got: %v", err)
	}
}

func TestLookup_DefaultRateLimit(t *testing.T) {
	rdb := newTestRedis(t)
	// Нестандартное значение — легче заметить если что-то пошло не так.
	store := auth.NewStore(rdb, 77)

	apiKey := fmt.Sprintf("no_limit_key_%d", time.Now().UnixNano())

	// Сидируем без rate_limit — должен примениться defaultLimit.
	redisKey := fmt.Sprintf("apikeys:%s", auth.HashKey(apiKey))
	rdb.HSet(context.Background(), redisKey,
		"merchant_id", "merchant_test",
		"name", "No Limit Key",
		"active", "true",
	)
	t.Cleanup(func() { rdb.Del(context.Background(), redisKey) })

	info, err := store.Lookup(context.Background(), apiKey)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if info.RateLimit != 77 {
		t.Errorf("expected default rate_limit 77, got %d", info.RateLimit)
	}
}

func TestLookup_RateLimitFromRedis_OverridesDefault(t *testing.T) {
	rdb := newTestRedis(t)
	store := auth.NewStore(rdb, 100)

	apiKey := fmt.Sprintf("custom_limit_key_%d", time.Now().UnixNano())
	// rate_limit в Redis = 25, default = 100. Должен применяться Redis-значение.
	seedAPIKey(t, rdb, apiKey, "merchant_test", "Custom Limit", 25, true)

	info, err := store.Lookup(context.Background(), apiKey)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if info.RateLimit != 25 {
		t.Errorf("expected rate_limit 25 from Redis, got %d", info.RateLimit)
	}
}
