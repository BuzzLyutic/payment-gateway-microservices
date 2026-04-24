package engine_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/BuzzLyutic/payment-gateway-microservices/services/risk-service/internal/engine"
)

// startRedis поднимает Redis в Docker и возвращает клиент + cleanup.
func startRedis(t *testing.T) (*redis.Client, func()) {
	t.Helper()
	ctx := context.Background()

	req := testcontainers.ContainerRequest{
		Image:        "redis:7-alpine",
		ExposedPorts: []string{"6379/tcp"},
		WaitingFor:   wait.ForLog("Ready to accept connections"),
	}

	container, err := testcontainers.GenericContainer(ctx,
		testcontainers.GenericContainerRequest{
			ContainerRequest: req,
			Started:          true,
		},
	)
	if err != nil {
		t.Fatalf("start redis container: %v", err)
	}

	host, err := container.Host(ctx)
	if err != nil {
		t.Fatalf("get host: %v", err)
	}
	port, err := container.MappedPort(ctx, "6379")
	if err != nil {
		t.Fatalf("get port: %v", err)
	}

	client := redis.NewClient(&redis.Options{
		Addr: fmt.Sprintf("%s:%s", host, port.Port()),
	})

	cleanup := func() {
		_ = client.Close()
		_ = container.Terminate(ctx)
	}

	return client, cleanup
}

// ── Тесты checkAndIncrement ──────────────────────────────────────────────────

// TestCheckAndIncrement_BelowThreshold — первый запрос не триггерит правило.
func TestCheckAndIncrement_BelowThreshold(t *testing.T) {
	rdb, cleanup := startRedis(t)
	defer cleanup()

	ctx := context.Background()
	key := "test:below:merchant-1"

	triggered, err := engine.ExportCheckAndIncrement(ctx, rdb, key, time.Minute, 5)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if triggered {
		t.Error("first request should not trigger (count=1, threshold=5)")
	}
}

// TestCheckAndIncrement_ExceedsThreshold — счётчик превышает порог.
func TestCheckAndIncrement_ExceedsThreshold(t *testing.T) {
	rdb, cleanup := startRedis(t)
	defer cleanup()

	ctx := context.Background()
	key := "test:exceed:card-abc"

	// Вызываем threshold+1 раз.
	threshold := 3
	var triggered bool
	var err error

	for i := 0; i <= threshold; i++ {
		triggered, err = engine.ExportCheckAndIncrement(ctx, rdb, key, time.Minute, threshold)
		if err != nil {
			t.Fatalf("call %d: unexpected error: %v", i+1, err)
		}
	}

	if !triggered {
		t.Errorf("expected triggered after %d calls with threshold %d", threshold+1, threshold)
	}
}

// TestCheckAndIncrement_TTLSetOnFirstCall — TTL устанавливается при count==1.
// Это покрывает ветку `if count == 1` в checkAndIncrement.
func TestCheckAndIncrement_TTLSetOnFirstCall(t *testing.T) {
	rdb, cleanup := startRedis(t)
	defer cleanup()

	ctx := context.Background()
	key := "test:ttl:merchant-2"
	window := 2 * time.Second

	_, err := engine.ExportCheckAndIncrement(ctx, rdb, key, window, 10)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Сразу после первого вызова TTL должен быть установлен.
	ttl, err := rdb.TTL(ctx, key).Result()
	if err != nil {
		t.Fatalf("TTL: %v", err)
	}
	if ttl <= 0 {
		t.Errorf("expected TTL > 0 after first call, got %v", ttl)
	}
	if ttl > window {
		t.Errorf("TTL %v exceeds window %v", ttl, window)
	}
}

// TestCheckAndIncrement_TTLNotResetOnSubsequentCalls — повторные вызовы
// не сбрасывают TTL (окно не сдвигается).
func TestCheckAndIncrement_TTLNotResetOnSubsequentCalls(t *testing.T) {
	rdb, cleanup := startRedis(t)
	defer cleanup()

	ctx := context.Background()
	key := "test:ttl-stable:ip-1"
	window := 10 * time.Second

	// Первый вызов — устанавливает TTL.
	_, err := engine.ExportCheckAndIncrement(ctx, rdb, key, window, 10)
	if err != nil {
		t.Fatalf("first call error: %v", err)
	}

	ttlAfterFirst, _ := rdb.TTL(ctx, key).Result()

	time.Sleep(100 * time.Millisecond)

	// Второй вызов — не должен сбрасывать TTL.
	_, err = engine.ExportCheckAndIncrement(ctx, rdb, key, window, 10)
	if err != nil {
		t.Fatalf("second call error: %v", err)
	}

	ttlAfterSecond, _ := rdb.TTL(ctx, key).Result()

	// TTL после второго вызова должен быть меньше или равен первому.
	if ttlAfterSecond > ttlAfterFirst {
		t.Errorf("TTL increased after second call: first=%v, second=%v",
			ttlAfterFirst, ttlAfterSecond)
	}
}

// TestCheckAndIncrement_KeyExpiresAfterWindow — ключ истекает по окончании окна.
func TestCheckAndIncrement_KeyExpiresAfterWindow(t *testing.T) {
	rdb, cleanup := startRedis(t)
	defer cleanup()

	ctx := context.Background()
	key := "test:expires:card-xyz"
	window := 1 * time.Second

	// Накручиваем счётчик выше порога.
	for i := 0; i < 5; i++ {
		_, _ = engine.ExportCheckAndIncrement(ctx, rdb, key, window, 3)
	}

	// Ждём истечения окна.
	time.Sleep(window + 200*time.Millisecond)

	// После истечения — новый запрос не должен триггерить.
	triggered, err := engine.ExportCheckAndIncrement(ctx, rdb, key, window, 3)
	if err != nil {
		t.Fatalf("unexpected error after expiry: %v", err)
	}
	if triggered {
		t.Error("after TTL expiry, first new call should not trigger")
	}
}

// TestCheckAndIncrement_AtThreshold — ровно на пороге не триггерит.
func TestCheckAndIncrement_AtThreshold(t *testing.T) {
	rdb, cleanup := startRedis(t)
	defer cleanup()

	ctx := context.Background()
	key := "test:at-threshold:merchant"

	threshold := 5
	var triggered bool

	for i := 0; i < threshold; i++ {
		var err error
		triggered, err = engine.ExportCheckAndIncrement(ctx, rdb, key, time.Minute, threshold)
		if err != nil {
			t.Fatalf("call %d: %v", i+1, err)
		}
	}

	// При count == threshold: count > threshold false.
	if triggered {
		t.Errorf("at threshold (%d) should not trigger (count > threshold)", threshold)
	}
}

// TestCheckAndIncrement_AboveThreshold — на threshold+1 триггерит.
func TestCheckAndIncrement_AboveThreshold(t *testing.T) {
	rdb, cleanup := startRedis(t)
	defer cleanup()

	ctx := context.Background()
	key := "test:above-threshold:merchant"

	threshold := 5
	var triggered bool

	for i := 0; i <= threshold; i++ {
		var err error
		triggered, err = engine.ExportCheckAndIncrement(ctx, rdb, key, time.Minute, threshold)
		if err != nil {
			t.Fatalf("call %d: %v", i+1, err)
		}
	}

	if !triggered {
		t.Errorf("above threshold (%d+1) must trigger", threshold)
	}
}
