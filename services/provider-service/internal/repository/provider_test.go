package repository_test

import (
	"context"
	"testing"
	"time"

	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
	"github.com/testcontainers/testcontainers-go/modules/postgres"

	"github.com/BuzzLyutic/payment-gateway-microservices/services/provider-service/internal/domain"
	"github.com/BuzzLyutic/payment-gateway-microservices/services/provider-service/internal/repository"
)

// Инфраструктура

const (
	testDBName = "providers_test"
	testDBUser = "test"
	testDBPass = "test"
)

// startPostgres поднимает PostgreSQL в Docker с миграциями.
func startPostgres(t *testing.T) (dsn string, cleanup func()) {
	t.Helper()
	ctx := context.Background()

	container, err := postgres.RunContainer(ctx,
		testcontainers.WithImage("postgres:16-alpine"),
		postgres.WithDatabase(testDBName),
		postgres.WithUsername(testDBUser),
		postgres.WithPassword(testDBPass),
		postgres.WithInitScripts(
			"../../migrations/001_create_providers.sql",
			"../../migrations/002_seed_providers.sql",
		),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).
				WithStartupTimeout(30*time.Second),
		),
	)
	if err != nil {
		t.Fatalf("start postgres: %v", err)
	}

	connStr, err := container.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatalf("connection string: %v", err)
	}

	cleanup = func() { _ = container.Terminate(ctx) }
	return connStr, cleanup
}

// newTestRepo создаёт репозиторий подключённый к тестовой БД.
func newTestRepo(t *testing.T) (*repository.ProviderRepository, func()) {
	t.Helper()
	dsn, dbCleanup := startPostgres(t)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	repo, err := repository.New(ctx, dsn)
	if err != nil {
		dbCleanup()
		t.Fatalf("create repository: %v", err)
	}

	cleanup := func() {
		repo.Close()
		dbCleanup()
	}
	return repo, cleanup
}

// Тесты Ping

func TestRepository_Ping(t *testing.T) {
	repo, cleanup := newTestRepo(t)
	defer cleanup()

	if err := repo.Ping(context.Background()); err != nil {
		t.Errorf("Ping failed: %v", err)
	}
}

// Тесты FindAll

// TestRepository_FindAll_ReturnsSeedData — seed содержит 3 провайдера.
func TestRepository_FindAll_ReturnsSeedData(t *testing.T) {
	repo, cleanup := newTestRepo(t)
	defer cleanup()

	providers, err := repo.FindAll(context.Background())
	if err != nil {
		t.Fatalf("FindAll: %v", err)
	}

	// 002_seed_providers.sql вставляет 3 провайдера.
	if len(providers) < 3 {
		t.Errorf("expected at least 3 providers from seed, got %d", len(providers))
	}
}

// TestRepository_FindAll_FieldsPopulated — все поля заполнены корректно.
func TestRepository_FindAll_FieldsPopulated(t *testing.T) {
	repo, cleanup := newTestRepo(t)
	defer cleanup()

	providers, err := repo.FindAll(context.Background())
	if err != nil {
		t.Fatalf("FindAll: %v", err)
	}

	for _, p := range providers {
		if p.ID == "" {
			t.Error("provider ID must not be empty")
		}
		if p.Name == "" {
			t.Error("provider Name must not be empty")
		}
		if p.Status == "" {
			t.Error("provider Status must not be empty")
		}
		if len(p.Currencies) == 0 {
			t.Errorf("provider %q: Currencies must not be empty", p.Name)
		}
		if len(p.PaymentMethods) == 0 {
			t.Errorf("provider %q: PaymentMethods must not be empty", p.Name)
		}
		if p.CreatedAt.IsZero() {
			t.Errorf("provider %q: CreatedAt must not be zero", p.Name)
		}
	}
}

// TestRepository_FindAll_OrderedByName — результат отсортирован по имени.
func TestRepository_FindAll_OrderedByName(t *testing.T) {
	repo, cleanup := newTestRepo(t)
	defer cleanup()

	providers, err := repo.FindAll(context.Background())
	if err != nil {
		t.Fatalf("FindAll: %v", err)
	}

	for i := 1; i < len(providers); i++ {
		if providers[i].Name < providers[i-1].Name {
			t.Errorf("not sorted: %q comes after %q",
				providers[i].Name, providers[i-1].Name)
		}
	}
}

// Тесты FindActive

// TestRepository_FindActive_USDCard — USD + card поддерживают mock_provider_a и mock_provider_b.
func TestRepository_FindActive_USDCard(t *testing.T) {
	repo, cleanup := newTestRepo(t)
	defer cleanup()

	providers, err := repo.FindActive(context.Background(), "USD", "card")
	if err != nil {
		t.Fatalf("FindActive: %v", err)
	}

	// Из seed: mock_provider_a (USD+card), mock_provider_b (USD+card)
	if len(providers) < 2 {
		t.Errorf("expected at least 2 providers for USD+card, got %d", len(providers))
	}

	for _, p := range providers {
		if p.Status != domain.ProviderStatusActive {
			t.Errorf("provider %q must be active", p.Name)
		}
		if !contains(p.Currencies, "USD") {
			t.Errorf("provider %q must support USD", p.Name)
		}
		if !contains(p.PaymentMethods, "card") {
			t.Errorf("provider %q must support card", p.Name)
		}
	}
}

// TestRepository_FindActive_RUBSBP — RUB + sbp: mock_provider_a и mock_provider_c.
func TestRepository_FindActive_RUBSBP(t *testing.T) {
	repo, cleanup := newTestRepo(t)
	defer cleanup()

	providers, err := repo.FindActive(context.Background(), "RUB", "sbp")
	if err != nil {
		t.Fatalf("FindActive: %v", err)
	}

	if len(providers) < 2 {
		t.Errorf("expected at least 2 providers for RUB+sbp, got %d", len(providers))
	}
}

// TestRepository_FindActive_NoMatch — несуществующая комбинация возвращает пустой срез.
func TestRepository_FindActive_NoMatch(t *testing.T) {
	repo, cleanup := newTestRepo(t)
	defer cleanup()

	providers, err := repo.FindActive(context.Background(), "JPY", "crypto")
	if err != nil {
		t.Fatalf("FindActive: %v", err)
	}
	if len(providers) != 0 {
		t.Errorf("expected 0 providers for JPY+crypto, got %d", len(providers))
	}
}

// Тесты GetByName

// TestRepository_GetByName_Exists — получение существующего провайдера.
func TestRepository_GetByName_Exists(t *testing.T) {
	repo, cleanup := newTestRepo(t)
	defer cleanup()

	p, err := repo.GetByName(context.Background(), "mock_provider_a")
	if err != nil {
		t.Fatalf("GetByName: %v", err)
	}

	if p.Name != "mock_provider_a" {
		t.Errorf("wrong name: %q", p.Name)
	}
	if p.Type != "mock" {
		t.Errorf("wrong type: %q", p.Type)
	}
	if p.Status != domain.ProviderStatusActive {
		t.Errorf("wrong status: %q", p.Status)
	}
	// Из seed: commission_pct = 2.500
	if p.CommissionPct != 2.5 {
		t.Errorf("wrong commission_pct: %v", p.CommissionPct)
	}
}

// TestRepository_GetByName_NotFound — несуществующий провайдер возвращает ошибку.
func TestRepository_GetByName_NotFound(t *testing.T) {
	repo, cleanup := newTestRepo(t)
	defer cleanup()

	_, err := repo.GetByName(context.Background(), "nonexistent_provider")
	if err == nil {
		t.Fatal("expected error for nonexistent provider")
	}
}

// TestRepository_GetByName_Config — JSONB config парсится корректно.
func TestRepository_GetByName_Config(t *testing.T) {
	repo, cleanup := newTestRepo(t)
	defer cleanup()

	p, err := repo.GetByName(context.Background(), "mock_provider_a")
	if err != nil {
		t.Fatalf("GetByName: %v", err)
	}

	// Из seed: '{"success_rate": 95, "min_latency_ms": 150, "max_latency_ms": 250}'
	if p.Config == nil {
		t.Fatal("config must not be nil")
	}

	successRate, ok := p.Config["success_rate"]
	if !ok {
		t.Error("config must contain success_rate")
	}
	// JSON числа десериализуются как float64.
	if successRate.(float64) != 95 {
		t.Errorf("wrong success_rate: %v", successRate)
	}
}

// TestRepository_GetByName_Arrays — массивы currencies и payment_methods корректны.
func TestRepository_GetByName_Arrays(t *testing.T) {
	repo, cleanup := newTestRepo(t)
	defer cleanup()

	p, err := repo.GetByName(context.Background(), "mock_provider_b")
	if err != nil {
		t.Fatalf("GetByName: %v", err)
	}

	// mock_provider_b: currencies=[RUB, USD], payment_methods=[card]
	if !contains(p.Currencies, "RUB") || !contains(p.Currencies, "USD") {
		t.Errorf("mock_provider_b currencies: got %v, want [RUB USD]", p.Currencies)
	}
	if !contains(p.PaymentMethods, "card") {
		t.Errorf("mock_provider_b payment_methods: got %v, want [card]", p.PaymentMethods)
	}
}

// TestRepository_Close — Close не паникует и не возвращает ошибку.
func TestRepository_Close(t *testing.T) {
	dsn, dbCleanup := startPostgres(t)
	defer dbCleanup()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	repo, err := repository.New(ctx, dsn)
	if err != nil {
		t.Fatalf("create repository: %v", err)
	}

	// Close не должен паниковать.
	repo.Close()

	// Повторный Close тоже не должен паниковать (pgxpool.Close идемпотентен).
	repo.Close()
}

// Хелпер

func contains(slice []string, val string) bool {
	for _, s := range slice {
		if s == val {
			return true
		}
	}
	return false
}
