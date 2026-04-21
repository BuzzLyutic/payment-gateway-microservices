package idempotency_test

import (
	"context"
	"sync"
	"testing"

	goredis "github.com/redis/go-redis/v9"
	tcredis "github.com/testcontainers/testcontainers-go/modules/redis"

	"github.com/BuzzLyutic/payment-gateway-microservices/services/transaction-service/internal/idempotency"
)

// setup

func setupRedis(t *testing.T) *idempotency.Store {
	t.Helper()
	ctx := context.Background()

	container, err := tcredis.Run(ctx, "redis:7-alpine")
	if err != nil {
		t.Fatalf("start redis container: %v", err)
	}

	t.Cleanup(func() {
		if err := container.Terminate(ctx); err != nil {
			t.Logf("terminate redis: %v", err)
		}
	})

	addr, err := container.Endpoint(ctx, "")
	if err != nil {
		t.Fatalf("get redis endpoint: %v", err)
	}

	client := goredis.NewClient(&goredis.Options{Addr: addr})
	t.Cleanup(func() { client.Close() })

	return idempotency.NewStore(client)
}

// Lock

func TestIdempotency_Lock_FirstTime_ReturnsTrue(t *testing.T) {
	store := setupRedis(t)
	ctx := context.Background()

	locked, err := store.Lock(ctx, "new-key-001")
	if err != nil {
		t.Fatalf("Lock() error: %v", err)
	}
	if !locked {
		t.Error("first Lock() should return true (key is new)")
	}
}

func TestIdempotency_Lock_SecondTime_ReturnsFalse(t *testing.T) {
	// Повторный Lock того же ключа → false.
	// Это основа idempotency: второй запрос с тем же ключом должен
	// получить ответ первого, а не создать новую транзакцию.
	store := setupRedis(t)
	ctx := context.Background()

	_, err := store.Lock(ctx, "duplicate-key")
	if err != nil {
		t.Fatalf("first Lock() error: %v", err)
	}

	locked, err := store.Lock(ctx, "duplicate-key")
	if err != nil {
		t.Fatalf("second Lock() error: %v", err)
	}
	if locked {
		t.Error("CRITICAL: second Lock() returned true — duplicate payment possible!")
	}
}

func TestIdempotency_Lock_DifferentKeys_Independent(t *testing.T) {
	// Разные ключи независимы — не мешают друг другу.
	store := setupRedis(t)
	ctx := context.Background()

	locked1, err := store.Lock(ctx, "key-a")
	if err != nil || !locked1 {
		t.Fatalf("Lock(key-a): locked=%v err=%v", locked1, err)
	}

	locked2, err := store.Lock(ctx, "key-b")
	if err != nil || !locked2 {
		t.Fatalf("Lock(key-b): locked=%v err=%v", locked2, err)
	}
}

func TestIdempotency_Lock_AfterUnlock_ReturnsTrue(t *testing.T) {
	// Lock → Unlock → Lock: после освобождения ключ снова свободен.
	// Сценарий: первый запрос упал при создании транзакции.
	store := setupRedis(t)
	ctx := context.Background()

	_, err := store.Lock(ctx, "unlock-test")
	if err != nil {
		t.Fatalf("first Lock(): %v", err)
	}

	if err := store.Unlock(ctx, "unlock-test"); err != nil {
		t.Fatalf("Unlock(): %v", err)
	}

	locked, err := store.Lock(ctx, "unlock-test")
	if err != nil {
		t.Fatalf("second Lock(): %v", err)
	}
	if !locked {
		t.Error("Lock after Unlock should return true")
	}
}

func TestIdempotency_Lock_Concurrent_OnlyOneWins(t *testing.T) {
	// Гонка: два запроса одновременно пытаются захватить один ключ.
	// Только один должен победить. Критично для защиты от double spending.
	store := setupRedis(t)
	ctx := context.Background()

	const goroutines = 10
	results := make([]bool, goroutines)
	var wg sync.WaitGroup
	var mu sync.Mutex
	var successCount int

	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			locked, err := store.Lock(ctx, "race-key")
			if err != nil {
				t.Errorf("goroutine %d Lock() error: %v", idx, err)
				return
			}
			mu.Lock()
			results[idx] = locked
			if locked {
				successCount++
			}
			mu.Unlock()
		}(i)
	}

	wg.Wait()

	if successCount != 1 {
		t.Errorf("CRITICAL: %d goroutines acquired lock, want exactly 1 — race condition!", successCount)
	}
}

// SetTransactionID / GetTransactionID

func TestIdempotency_SetAndGet_TransactionID(t *testing.T) {
	store := setupRedis(t)
	ctx := context.Background()

	// Lock устанавливает "processing".
	locked, err := store.Lock(ctx, "set-get-key")
	if err != nil || !locked {
		t.Fatalf("Lock(): locked=%v err=%v", locked, err)
	}

	// Проверяем что GetTransactionID возвращает "processing".
	val, err := store.GetTransactionID(ctx, "set-get-key")
	if err != nil {
		t.Fatalf("GetTransactionID() error: %v", err)
	}
	if val != "processing" {
		t.Errorf("after Lock: GetTransactionID = %q, want %q", val, "processing")
	}

	// SetTransactionID заменяет "processing" на реальный ID.
	const realTxID = "tx-real-id-123"
	if err := store.SetTransactionID(ctx, "set-get-key", realTxID); err != nil {
		t.Fatalf("SetTransactionID() error: %v", err)
	}

	// Проверяем что теперь возвращается реальный ID.
	val, err = store.GetTransactionID(ctx, "set-get-key")
	if err != nil {
		t.Fatalf("GetTransactionID() after set error: %v", err)
	}
	if val != realTxID {
		t.Errorf("after SetTransactionID: got %q, want %q", val, realTxID)
	}
}

func TestIdempotency_GetTransactionID_NonExistentKey(t *testing.T) {
	store := setupRedis(t)
	ctx := context.Background()

	val, err := store.GetTransactionID(ctx, "nonexistent-key")
	if err != nil {
		t.Fatalf("GetTransactionID() error: %v", err)
	}
	if val != "" {
		t.Errorf("GetTransactionID for nonexistent key = %q, want empty string", val)
	}
}

func TestIdempotency_SetTransactionID_WithoutLock_Noop(t *testing.T) {
	// SetTransactionID с Mode XX — обновляет только существующий ключ.
	// Если Lock не был вызван — SetTransactionID безопасно игнорируется.
	store := setupRedis(t)
	ctx := context.Background()

	// Без предварительного Lock.
	err := store.SetTransactionID(ctx, "no-lock-key", "tx-123")
	if err != nil {
		t.Fatalf("SetTransactionID without Lock error: %v", err)
	}

	// Ключ не должен существовать.
	val, err := store.GetTransactionID(ctx, "no-lock-key")
	if err != nil {
		t.Fatalf("GetTransactionID: %v", err)
	}
	if val != "" {
		t.Errorf("SetTransactionID without Lock created key with value %q, want empty", val)
	}
}

// Idempotency сквозной сценарий

func TestIdempotency_FullFlow_DuplicateRequest(t *testing.T) {
	// Полный сценарий: первый запрос создаёт транзакцию,
	// второй запрос с тем же ключом получает тот же результат.
	store := setupRedis(t)
	ctx := context.Background()

	key := "full-flow-key"

	// Запрос 1: Lock → создать транзакцию → SetTransactionID.
	locked, err := store.Lock(ctx, key)
	if err != nil || !locked {
		t.Fatalf("first Lock(): locked=%v err=%v", locked, err)
	}

	const txID = "tx-created-001"
	if err := store.SetTransactionID(ctx, key, txID); err != nil {
		t.Fatalf("SetTransactionID: %v", err)
	}

	// Запрос 2: Lock вернёт false → GetTransactionID → вернуть тот же ответ.
	locked2, err := store.Lock(ctx, key)
	if err != nil {
		t.Fatalf("second Lock() error: %v", err)
	}
	if locked2 {
		t.Fatal("second request should not acquire lock")
	}

	val, err := store.GetTransactionID(ctx, key)
	if err != nil {
		t.Fatalf("GetTransactionID: %v", err)
	}
	if val != txID {
		t.Errorf("second request got txID=%q, want %q", val, txID)
	}
}

// Ping

func TestIdempotency_Ping(t *testing.T) {
	store := setupRedis(t)
	ctx := context.Background()

	if err := store.Ping(ctx); err != nil {
		t.Errorf("Ping() error: %v", err)
	}
}

// redisKey

func TestIdempotency_RedisKey_Prefix(t *testing.T) {
	// Проверяем что разные ключи не конфликтуют с другими данными в Redis.
	// Косвенно: Lock("key") и Lock("key") → второй возвращает false.
	// Прямая проверка prefix через поведение.
	store := setupRedis(t)
	ctx := context.Background()

	store.Lock(ctx, "test-key")

	// Тот же "голый" ключ — должен конфликтовать (один namespace).
	locked, _ := store.Lock(ctx, "test-key")
	if locked {
		t.Error("same key should conflict")
	}

	// Другой ключ — свободен.
	locked, _ = store.Lock(ctx, "other-key")
	if !locked {
		t.Error("different key should be free")
	}
}
