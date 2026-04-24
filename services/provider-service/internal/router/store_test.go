package router_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/BuzzLyutic/payment-gateway-microservices/services/provider-service/internal/router"
)

// Инфраструктура

func startRedis(t *testing.T) (addr string, cleanup func()) {
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
		t.Fatalf("start redis: %v", err)
	}

	host, err := container.Host(ctx)
	if err != nil {
		t.Fatalf("container host: %v", err)
	}
	port, err := container.MappedPort(ctx, "6379")
	if err != nil {
		t.Fatalf("container port: %v", err)
	}

	addr = fmt.Sprintf("%s:%s", host, port.Port())
	cleanup = func() { _ = container.Terminate(ctx) }
	return addr, cleanup
}

// newTestStore создаёт Store подключённый к тестовому Redis.
func newTestStore(t *testing.T) (*router.Store, func()) {
	t.Helper()
	addr, cleanup := startRedis(t)
	store := router.NewStore(addr, "", 0)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := store.Ping(ctx); err != nil {
		cleanup()
		t.Fatalf("ping redis: %v", err)
	}

	return store, cleanup
}

// Тесты Ping / Close

func TestStore_Ping(t *testing.T) {
	store, cleanup := newTestStore(t)
	defer cleanup()
	defer store.Close()

	if err := store.Ping(context.Background()); err != nil {
		t.Errorf("expected ping to succeed: %v", err)
	}
}

func TestStore_Close(t *testing.T) {
	store, cleanup := newTestStore(t)
	defer cleanup()

	if err := store.Close(); err != nil {
		t.Errorf("expected close to succeed: %v", err)
	}
}

// Тесты Save / Load

// TestStore_SaveAndLoad — сохраняем и загружаем статистику.
func TestStore_SaveAndLoad(t *testing.T) {
	store, cleanup := newTestStore(t)
	defer cleanup()
	defer store.Close()

	ctx := context.Background()

	alpha := 15.0
	beta := 3.0
	latencies := []float64{120.5, 95.0, 200.0, 150.0}

	if err := store.Save(ctx, "mock_provider_a", alpha, beta, latencies); err != nil {
		t.Fatalf("Save: %v", err)
	}

	snapshot, err := store.Load(ctx, "mock_provider_a")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if snapshot == nil {
		t.Fatal("expected non-nil snapshot")
	}

	if snapshot.Alpha != alpha {
		t.Errorf("alpha: got %v, want %v", snapshot.Alpha, alpha)
	}
	if snapshot.Beta != beta {
		t.Errorf("beta: got %v, want %v", snapshot.Beta, beta)
	}
	if len(snapshot.Latencies) != len(latencies) {
		t.Errorf("latencies len: got %d, want %d",
			len(snapshot.Latencies), len(latencies))
	}
	for i, l := range latencies {
		if snapshot.Latencies[i] != l {
			t.Errorf("latency[%d]: got %v, want %v", i, snapshot.Latencies[i], l)
		}
	}
}

// TestStore_Load_NotFound — загрузка несуществующего ключа возвращает nil, nil.
func TestStore_Load_NotFound(t *testing.T) {
	store, cleanup := newTestStore(t)
	defer cleanup()
	defer store.Close()

	snapshot, err := store.Load(context.Background(), "nonexistent_provider")
	if err != nil {
		t.Fatalf("Load nonexistent: %v", err)
	}
	if snapshot != nil {
		t.Errorf("expected nil for unknown provider, got %+v", snapshot)
	}
}

// TestStore_Save_Overwrite — повторное сохранение перезаписывает данные.
func TestStore_Save_Overwrite(t *testing.T) {
	store, cleanup := newTestStore(t)
	defer cleanup()
	defer store.Close()

	ctx := context.Background()
	name := "mock_provider_b"

	// Первое сохранение.
	if err := store.Save(ctx, name, 1.0, 1.0, []float64{100}); err != nil {
		t.Fatalf("first Save: %v", err)
	}

	// Перезапись.
	if err := store.Save(ctx, name, 20.0, 5.0, []float64{50, 75}); err != nil {
		t.Fatalf("second Save: %v", err)
	}

	snapshot, err := store.Load(ctx, name)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if snapshot.Alpha != 20.0 {
		t.Errorf("expected alpha=20.0 after overwrite, got %v", snapshot.Alpha)
	}
	if len(snapshot.Latencies) != 2 {
		t.Errorf("expected 2 latencies after overwrite, got %d", len(snapshot.Latencies))
	}
}

// TestStore_Save_EmptyLatencies — пустой слайс латентностей.
func TestStore_Save_EmptyLatencies(t *testing.T) {
	store, cleanup := newTestStore(t)
	defer cleanup()
	defer store.Close()

	ctx := context.Background()

	if err := store.Save(ctx, "mock_provider_c", 1.0, 1.0, []float64{}); err != nil {
		t.Fatalf("Save empty latencies: %v", err)
	}

	snapshot, err := store.Load(ctx, "mock_provider_c")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if snapshot == nil {
		t.Fatal("expected non-nil snapshot")
	}
	if len(snapshot.Latencies) != 0 {
		t.Errorf("expected empty latencies, got %d", len(snapshot.Latencies))
	}
}

// Тесты LoadAll

// TestStore_LoadAll_MixedProviders — часть провайдеров есть в Redis, часть нет.
func TestStore_LoadAll_MixedProviders(t *testing.T) {
	store, cleanup := newTestStore(t)
	defer cleanup()
	defer store.Close()

	ctx := context.Background()

	// Сохраняем только двух из трёх.
	_ = store.Save(ctx, "provider_a", 10.0, 2.0, []float64{100})
	_ = store.Save(ctx, "provider_b", 5.0, 5.0, []float64{200})

	result, err := store.LoadAll(ctx, []string{"provider_a", "provider_b", "provider_c"})
	if err != nil {
		t.Fatalf("LoadAll: %v", err)
	}

	if _, ok := result["provider_a"]; !ok {
		t.Error("provider_a must be in result")
	}
	if _, ok := result["provider_b"]; !ok {
		t.Error("provider_b must be in result")
	}
	// provider_c не сохранён — его не должно быть в map (nil пропускается).
	if _, ok := result["provider_c"]; ok {
		t.Error("provider_c must not be in result (no saved stats)")
	}
}

// TestStore_LoadAll_Empty — пустой список провайдеров.
func TestStore_LoadAll_Empty(t *testing.T) {
	store, cleanup := newTestStore(t)
	defer cleanup()
	defer store.Close()

	result, err := store.LoadAll(context.Background(), []string{})
	if err != nil {
		t.Fatalf("LoadAll empty: %v", err)
	}
	if len(result) != 0 {
		t.Errorf("expected empty map, got %d entries", len(result))
	}
}

// TestStore_LoadAll_AllExist — все провайдеры присутствуют.
func TestStore_LoadAll_AllExist(t *testing.T) {
	store, cleanup := newTestStore(t)
	defer cleanup()
	defer store.Close()

	ctx := context.Background()
	providers := []string{"prov_x", "prov_y"}

	for _, name := range providers {
		_ = store.Save(ctx, name, 3.0, 7.0, []float64{50})
	}

	result, err := store.LoadAll(ctx, providers)
	if err != nil {
		t.Fatalf("LoadAll: %v", err)
	}

	if len(result) != len(providers) {
		t.Errorf("expected %d results, got %d", len(providers), len(result))
	}
}
