package repository_test

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/BuzzLyutic/payment-gateway-microservices/services/transaction-service/internal/domain"
	"github.com/BuzzLyutic/payment-gateway-microservices/services/transaction-service/internal/repository"
)

// setup

func setupPostgres(t *testing.T) *repository.TransactionRepository {
	t.Helper()
	ctx := context.Background()

	container, err := postgres.Run(ctx,
		"postgres:16-alpine",
		postgres.WithDatabase("testdb"),
		postgres.WithUsername("test"),
		postgres.WithPassword("test"),
		postgres.WithOrderedInitScripts(
		"../../migrations/001_create_transactions.sql", 
		"../../migrations/002_add_payment_method.sql", 
		"../../migrations/003_add_fraud_fields.sql"),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).
				WithStartupTimeout(30*time.Second),
		),
	)
	if err != nil {
		t.Fatalf("start postgres container: %v", err)
	}

	t.Cleanup(func() {
		if err := container.Terminate(ctx); err != nil {
			t.Logf("terminate postgres: %v", err)
		}
	})

	dsn, err := container.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatalf("get connection string: %v", err)
	}

	repo, err := repository.New(ctx, dsn)
	if err != nil {
		t.Fatalf("create repository: %v", err)
	}

	t.Cleanup(func() { repo.Close() })

	return repo
}

func newTestTransaction(idempotencyKey string) *domain.Transaction {
	return &domain.Transaction{
		IdempotencyKey: idempotencyKey,
		MerchantID:     "merchant-test-001",
		Amount:         10000,
		Currency:       "RUB",
		PaymentMethod:  "card",
		Status:         domain.StatusPending,
	}
}

// Create

func TestRepository_Create_Success(t *testing.T) {
	repo := setupPostgres(t)
	ctx := context.Background()

	tx := newTestTransaction("idem-key-001")
	err := repo.Create(ctx, tx)

	if err != nil {
		t.Fatalf("Create() error: %v", err)
	}
	if tx.ID == "" {
		t.Error("Create() should populate tx.ID via RETURNING")
	}
	if tx.CreatedAt.IsZero() {
		t.Error("Create() should populate tx.CreatedAt via RETURNING")
	}
	if tx.UpdatedAt.IsZero() {
		t.Error("Create() should populate tx.UpdatedAt via RETURNING")
	}
	if tx.Status != domain.StatusPending {
		t.Errorf("Status = %q, want pending", tx.Status)
	}
}

func TestRepository_Create_DuplicateIdempotencyKey_Fails(t *testing.T) {
	// UNIQUE constraint на idempotency_key — защита от двойного создания.
	// Критично для финансовой системы.
	repo := setupPostgres(t)
	ctx := context.Background()

	tx1 := newTestTransaction("duplicate-key")
	if err := repo.Create(ctx, tx1); err != nil {
		t.Fatalf("first Create() error: %v", err)
	}

	tx2 := newTestTransaction("duplicate-key") // тот же ключ
	err := repo.Create(ctx, tx2)

	if err == nil {
		t.Fatal("CRITICAL: duplicate idempotency_key accepted — double payment possible!")
	}
}

func TestRepository_Create_WithOptionalFields(t *testing.T) {
	repo := setupPostgres(t)
	ctx := context.Background()

	desc := "Test payment for order #123"
	cardHash := "abc123hash"
	customerIP := "192.168.1.1"
	email := "user@example.com"

	tx := &domain.Transaction{
		IdempotencyKey: "idem-with-fields",
		MerchantID:     "merchant-001",
		Amount:         50000,
		Currency:       "USD",
		PaymentMethod:  "card",
		Status:         domain.StatusPending,
		Description:    &desc,
		CardHash:       &cardHash,
		CustomerIP:     &customerIP,
		CustomerEmail:  &email,
		Metadata:       map[string]string{"order_id": "123", "product": "subscription"},
	}

	if err := repo.Create(ctx, tx); err != nil {
		t.Fatalf("Create() error: %v", err)
	}

	// Читаем обратно и проверяем все поля.
	got, err := repo.GetByID(ctx, tx.ID)
	if err != nil {
		t.Fatalf("GetByID() error: %v", err)
	}

	if got.Description == nil || *got.Description != desc {
		t.Errorf("Description = %v, want %q", got.Description, desc)
	}
	if got.CardHash == nil || *got.CardHash != cardHash {
		t.Errorf("CardHash = %v, want %q", got.CardHash, cardHash)
	}
	if got.CustomerIP == nil || *got.CustomerIP != customerIP {
		t.Errorf("CustomerIP = %v, want %q", got.CustomerIP, customerIP)
	}
	if got.CustomerEmail == nil || *got.CustomerEmail != email {
		t.Errorf("CustomerEmail = %v, want %q", got.CustomerEmail, email)
	}
	if got.Metadata["order_id"] != "123" {
		t.Errorf("Metadata[order_id] = %q, want %q", got.Metadata["order_id"], "123")
	}
}

// GetByID

func TestRepository_GetByID_NotFound(t *testing.T) {
	repo := setupPostgres(t)
	ctx := context.Background()

	_, err := repo.GetByID(ctx, "00000000-0000-0000-0000-000000000000")

	if !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("GetByID() error = %v, want ErrNotFound", err)
	}
}

func TestRepository_GetByID_ReturnsCorrectData(t *testing.T) {
	repo := setupPostgres(t)
	ctx := context.Background()

	created := newTestTransaction("idem-get-test")
	created.Amount = 99999
	created.Currency = "EUR"
	created.MerchantID = "merchant-getbyid"

	if err := repo.Create(ctx, created); err != nil {
		t.Fatalf("Create() error: %v", err)
	}

	got, err := repo.GetByID(ctx, created.ID)
	if err != nil {
		t.Fatalf("GetByID() error: %v", err)
	}

	if got.ID != created.ID {
		t.Errorf("ID = %q, want %q", got.ID, created.ID)
	}
	if got.Amount != 99999 {
		t.Errorf("Amount = %d, want 99999", got.Amount)
	}
	if got.Currency != "EUR" {
		t.Errorf("Currency = %q, want EUR", got.Currency)
	}
	if got.MerchantID != "merchant-getbyid" {
		t.Errorf("MerchantID = %q, want merchant-getbyid", got.MerchantID)
	}
	if got.Status != domain.StatusPending {
		t.Errorf("Status = %q, want pending", got.Status)
	}
}

// FetchPending

func TestRepository_FetchPending_ReturnsOnlyPending(t *testing.T) {
	// Критично: FetchPending не должен трогать транзакции в других статусах.
	repo := setupPostgres(t)
	ctx := context.Background()

	// Создаём pending транзакции.
	for i := 0; i < 3; i++ {
		tx := newTestTransaction(fmt.Sprintf("fetch-pending-%d", i))
		if err := repo.Create(ctx, tx); err != nil {
			t.Fatalf("Create pending %d: %v", i, err)
		}
	}

	// Создаём одну captured транзакцию (через UpdateStatus).
	captured := newTestTransaction("fetch-captured")
	if err := repo.Create(ctx, captured); err != nil {
		t.Fatalf("Create captured: %v", err)
	}
	// FetchPending переведёт её в processing — потом обновляем в captured.
	fetched, _ := repo.FetchPending(ctx, 10)
	for _, tx := range fetched {
		if tx.ID == captured.ID {
			repo.UpdateStatus(ctx, tx.ID, domain.StatusCaptured, nil, nil, nil)
		}
	}

	// Создаём ещё pending после.
	pending := newTestTransaction("fetch-pending-after")
	if err := repo.Create(ctx, pending); err != nil {
		t.Fatalf("Create pending after: %v", err)
	}

	result, err := repo.FetchPending(ctx, 10)
	if err != nil {
		t.Fatalf("FetchPending() error: %v", err)
	}

	for _, tx := range result {
		// FetchPending переводит в processing атомарно.
		if tx.Status != domain.StatusProcessing {
			t.Errorf("FetchPending returned tx with status %q, want processing", tx.Status)
		}
	}

	// Только pending транзакции были выбраны (captured — нет).
	for _, tx := range result {
		if tx.ID == captured.ID {
			t.Error("FetchPending returned captured transaction — should only return pending")
		}
	}
}

func TestRepository_FetchPending_RespectsLimit(t *testing.T) {
	repo := setupPostgres(t)
	ctx := context.Background()

	// Создаём 5 pending транзакций.
	for i := 0; i < 5; i++ {
		tx := newTestTransaction(fmt.Sprintf("limit-test-%d", i))
		if err := repo.Create(ctx, tx); err != nil {
			t.Fatalf("Create %d: %v", i, err)
		}
	}

	const limit = 3
	result, err := repo.FetchPending(ctx, limit)
	if err != nil {
		t.Fatalf("FetchPending() error: %v", err)
	}

	if len(result) > limit {
		t.Errorf("FetchPending(limit=%d) returned %d transactions, want <= %d",
			limit, len(result), limit)
	}
}

func TestRepository_FetchPending_AtomicTransition(t *testing.T) {
	// FetchPending атомарно переводит pending → processing.
	// Повторный FetchPending не должен вернуть те же транзакции.
	repo := setupPostgres(t)
	ctx := context.Background()

	tx := newTestTransaction("atomic-transition")
	if err := repo.Create(ctx, tx); err != nil {
		t.Fatalf("Create: %v", err)
	}

	// Первый FetchPending.
	result1, err := repo.FetchPending(ctx, 10)
	if err != nil {
		t.Fatalf("FetchPending 1: %v", err)
	}
	if len(result1) == 0 {
		t.Fatal("expected at least 1 transaction")
	}

	// Второй FetchPending не должен вернуть те же транзакции.
	result2, err := repo.FetchPending(ctx, 10)
	if err != nil {
		t.Fatalf("FetchPending 2: %v", err)
	}

	ids1 := make(map[string]bool)
	for _, tx := range result1 {
		ids1[tx.ID] = true
	}

	for _, tx := range result2 {
		if ids1[tx.ID] {
			t.Errorf("CRITICAL: transaction %q returned twice by FetchPending — double processing possible!", tx.ID)
		}
	}
}

func TestRepository_FetchPending_ConcurrentWorkers_NoDoubleProcessing(t *testing.T) {
	// Два воркера одновременно вызывают FetchPending.
	// FOR UPDATE SKIP LOCKED гарантирует что каждая транзакция
	// обрабатывается только одним воркером.
	// Критично для корректности финансовой системы.
	repo := setupPostgres(t)
	ctx := context.Background()

	const txCount = 10
	for i := 0; i < txCount; i++ {
		tx := newTestTransaction(fmt.Sprintf("concurrent-%d", i))
		if err := repo.Create(ctx, tx); err != nil {
			t.Fatalf("Create %d: %v", i, err)
		}
	}

	var mu sync.Mutex
	seen := make(map[string]int) // id → сколько раз получен

	var wg sync.WaitGroup
	const workers = 3

	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			result, err := repo.FetchPending(ctx, txCount)
			if err != nil {
				t.Errorf("FetchPending error: %v", err)
				return
			}
			mu.Lock()
			for _, tx := range result {
				seen[tx.ID]++
			}
			mu.Unlock()
		}()
	}

	wg.Wait()

	for id, count := range seen {
		if count > 1 {
			t.Errorf("CRITICAL: transaction %q processed %d times — double payment!", id, count)
		}
	}
}

// UpdateStatus

func TestRepository_UpdateStatus_Success(t *testing.T) {
	repo := setupPostgres(t)
	ctx := context.Background()

	tx := newTestTransaction("update-status-test")
	if err := repo.Create(ctx, tx); err != nil {
		t.Fatalf("Create: %v", err)
	}

	provider := "mock_provider"
	providerTxID := "prov_tx_123"

	err := repo.UpdateStatus(ctx, tx.ID, domain.StatusCaptured, &provider, &providerTxID, nil)
	if err != nil {
		t.Fatalf("UpdateStatus() error: %v", err)
	}

	// Проверяем что обновление сохранилось.
	got, err := repo.GetByID(ctx, tx.ID)
	if err != nil {
		t.Fatalf("GetByID after update: %v", err)
	}

	if got.Status != domain.StatusCaptured {
		t.Errorf("Status = %q, want captured", got.Status)
	}
	if got.Provider == nil || *got.Provider != provider {
		t.Errorf("Provider = %v, want %q", got.Provider, provider)
	}
	if got.ProviderTxID == nil || *got.ProviderTxID != providerTxID {
		t.Errorf("ProviderTxID = %v, want %q", got.ProviderTxID, providerTxID)
	}
}

func TestRepository_UpdateStatus_NotFound(t *testing.T) {
	repo := setupPostgres(t)
	ctx := context.Background()

	err := repo.UpdateStatus(ctx,
		"00000000-0000-0000-0000-000000000000",
		domain.StatusCaptured, nil, nil, nil,
	)

	if !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("UpdateStatus() error = %v, want ErrNotFound", err)
	}
}

func TestRepository_UpdateStatus_WithErrorMessage(t *testing.T) {
	repo := setupPostgres(t)
	ctx := context.Background()

	tx := newTestTransaction("update-with-error")
	if err := repo.Create(ctx, tx); err != nil {
		t.Fatalf("Create: %v", err)
	}

	errMsg := "insufficient funds"
	provider := "mock_provider"

	err := repo.UpdateStatus(ctx, tx.ID, domain.StatusDeclined, &provider, nil, &errMsg)
	if err != nil {
		t.Fatalf("UpdateStatus() error: %v", err)
	}

	got, err := repo.GetByID(ctx, tx.ID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}

	if got.Status != domain.StatusDeclined {
		t.Errorf("Status = %q, want declined", got.Status)
	}
	if got.ErrorMessage == nil || *got.ErrorMessage != errMsg {
		t.Errorf("ErrorMessage = %v, want %q", got.ErrorMessage, errMsg)
	}
}

// Ping / Close

func TestRepository_Ping(t *testing.T) {
	repo := setupPostgres(t)
	ctx := context.Background()

	if err := repo.Ping(ctx); err != nil {
		t.Errorf("Ping() error: %v", err)
	}
}
