package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/BuzzLyutic/payment-gateway-microservices/services/transaction-service/internal/domain"
	"github.com/BuzzLyutic/payment-gateway-microservices/services/transaction-service/internal/events"
)

// mockRepo и mockPublisher — без mockProvider
type mockRepo struct {
	createFn     func(ctx context.Context, tx *domain.Transaction) error
	getByIDFn    func(ctx context.Context, id string) (*domain.Transaction, error)
	fetchPendFn  func(ctx context.Context, limit int) ([]*domain.Transaction, error)
	updateStatFn func(ctx context.Context, id string, status domain.Status, prov, txID, errMsg *string) error
	fetchStuckFn func(ctx context.Context, threshold time.Duration, limit int) ([]*domain.Transaction, error)
}

func (m *mockRepo) Create(ctx context.Context, tx *domain.Transaction) error {
	if m.createFn != nil {
		return m.createFn(ctx, tx)
	}
	tx.ID = "test-id-123"
	return nil
}

func (m *mockRepo) GetByID(ctx context.Context, id string) (*domain.Transaction, error) {
	if m.getByIDFn != nil {
		return m.getByIDFn(ctx, id)
	}
	return nil, domain.ErrNotFound
}

func (m *mockRepo) FetchPending(ctx context.Context, limit int) ([]*domain.Transaction, error) {
	if m.fetchPendFn != nil {
		return m.fetchPendFn(ctx, limit)
	}
	return nil, nil
}

func (m *mockRepo) UpdateStatus(ctx context.Context, id string, status domain.Status, prov, txID, errMsg *string) error {
	if m.updateStatFn != nil {
		return m.updateStatFn(ctx, id, status, prov, txID, errMsg)
	}
	return nil
}

func (m *mockRepo) FetchStuck(ctx context.Context, threshold time.Duration, limit int) ([]*domain.Transaction, error) {
	if m.fetchStuckFn != nil {
		return m.fetchStuckFn(ctx, threshold, limit)
	}
	return nil, nil
}

type mockPublisher struct {
	publishedEvents []events.PaymentCreated
	err             error
}

func (m *mockPublisher) PublishPaymentCreated(_ context.Context, event events.PaymentCreated) error {
	m.publishedEvents = append(m.publishedEvents, event)
	return m.err
}

// --- Tests ---

func TestTransactionService_CreatePayment_Success(t *testing.T) {
	repo := &mockRepo{}
	pub := &mockPublisher{}
	svc := New(repo, pub)

	tx, err := svc.CreatePayment(context.Background(), CreatePaymentRequest{
		IdempotencyKey: "key-1",
		MerchantID:     "m_123",
		Amount:         10000,
		Currency:       "RUB",
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tx.ID != "test-id-123" {
		t.Errorf("id = %s, want test-id-123", tx.ID)
	}
	if tx.Status != domain.StatusPending {
		t.Errorf("status = %s, want pending", tx.Status)
	}
}

func TestTransactionService_CreatePayment_RepoError(t *testing.T) {
	repo := &mockRepo{
		createFn: func(_ context.Context, _ *domain.Transaction) error {
			return errors.New("db connection lost")
		},
	}
	pub := &mockPublisher{}
	svc := New(repo, pub)

	_, err := svc.CreatePayment(context.Background(), CreatePaymentRequest{
		MerchantID: "m_123",
		Amount:     10000,
		Currency:   "RUB",
	})

	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestTransactionService_GetPayment_Found(t *testing.T) {
	repo := &mockRepo{
		getByIDFn: func(_ context.Context, id string) (*domain.Transaction, error) {
			return &domain.Transaction{
				ID:     id,
				Status: domain.StatusCaptured,
				Amount: 5000,
			}, nil
		},
	}
	pub := &mockPublisher{}
	svc := New(repo, pub)

	tx, err := svc.GetPayment(context.Background(), "abc-123")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tx.Status != domain.StatusCaptured {
		t.Errorf("status = %s, want captured", tx.Status)
	}
}

func TestTransactionService_GetPayment_NotFound(t *testing.T) {
	repo := &mockRepo{}
	pub := &mockPublisher{}
	svc := New(repo, pub)

	_, err := svc.GetPayment(context.Background(), "nonexistent")
	if !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("error = %v, want ErrNotFound", err)
	}
}

func TestTransactionService_ProcessPending_PublishesEvent(t *testing.T) {
	repo := &mockRepo{
		fetchPendFn: func(_ context.Context, _ int) ([]*domain.Transaction, error) {
			return []*domain.Transaction{
				{
					ID:            "tx-1",
					Amount:        10000,
					Currency:      "RUB",
					MerchantID:    "merchant-001",
					PaymentMethod: "card",
					Status:        domain.StatusPending,
					CreatedAt:     time.Now(),
				},
			}, nil
		},
	}

	pub := &mockPublisher{}
	svc := New(repo, pub)

	count, err := svc.ProcessPendingPayments(context.Background(), 10)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if count != 1 {
		t.Errorf("count = %d, want 1", count)
	}
	if len(pub.publishedEvents) != 1 {
		t.Fatalf("expected 1 published event, got %d", len(pub.publishedEvents))
	}
	if pub.publishedEvents[0].TransactionID != "tx-1" {
		t.Errorf("expected transaction_id tx-1, got %s",
			pub.publishedEvents[0].TransactionID)
	}
}

func TestTransactionService_ProcessPending_PublishError_ContinuesBatch(t *testing.T) {
	// Ошибка публикации одной транзакции не должна останавливать batch.
	repo := &mockRepo{
		fetchPendFn: func(_ context.Context, _ int) ([]*domain.Transaction, error) {
			return []*domain.Transaction{
				{ID: "tx-1", Amount: 10000, Status: domain.StatusPending, CreatedAt: time.Now()},
				{ID: "tx-2", Amount: 20000, Status: domain.StatusPending, CreatedAt: time.Now()},
			}, nil
		},
	}

	pub := &mockPublisher{err: errors.New("nats unavailable")}
	svc := New(repo, pub)

	count, err := svc.ProcessPendingPayments(context.Background(), 10)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Обе транзакции обработаны (пусть и с ошибкой публикации)
	if count != 2 {
		t.Errorf("count = %d, want 2", count)
	}
}

func TestStrPtr_EmptyString_ReturnsNil(t *testing.T) {
	// strPtr("") должен вернуть nil (= NULL в БД).
	// Тестируем через CreatePayment с пустыми опциональными полями.
	repo := &mockRepo{}
	pub := &mockPublisher{}
	svc := New(repo, pub)

	tx, err := svc.CreatePayment(context.Background(), CreatePaymentRequest{
		IdempotencyKey: "key-1",
		MerchantID:     "m_123",
		Amount:         10000,
		Currency:       "RUB",
		Description:    "",    // пустой → nil
		CardHash:       "",    // пустой → nil
		CustomerIP:     "",    // пустой → nil
		CustomerEmail:  "",    // пустой → nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Пустые поля должны быть nil в транзакции.
	if tx.Description != nil {
		t.Errorf("Description: expected nil for empty string, got %q", *tx.Description)
	}
	if tx.CardHash != nil {
		t.Errorf("CardHash: expected nil for empty string, got %q", *tx.CardHash)
	}
	if tx.CustomerIP != nil {
		t.Errorf("CustomerIP: expected nil for empty string, got %q", *tx.CustomerIP)
	}
	if tx.CustomerEmail != nil {
		t.Errorf("CustomerEmail: expected nil for empty string, got %q", *tx.CustomerEmail)
	}
}

func TestStrPtr_NonEmptyString_ReturnsPointer(t *testing.T) {
	// strPtr("value") должен вернуть *string.
	repo := &mockRepo{}
	pub := &mockPublisher{}
	svc := New(repo, pub)

	tx, err := svc.CreatePayment(context.Background(), CreatePaymentRequest{
		IdempotencyKey: "key-2",
		MerchantID:     "m_123",
		Amount:         10000,
		Currency:       "RUB",
		Description:    "Test payment",
		CardHash:       "abc123hash",
		CustomerIP:     "192.168.1.1",
		CustomerEmail:  "user@example.com",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if tx.Description == nil || *tx.Description != "Test payment" {
		t.Errorf("Description: expected %q, got %v", "Test payment", tx.Description)
	}
	if tx.CardHash == nil || *tx.CardHash != "abc123hash" {
		t.Errorf("CardHash: expected %q, got %v", "abc123hash", tx.CardHash)
	}
	if tx.CustomerIP == nil || *tx.CustomerIP != "192.168.1.1" {
		t.Errorf("CustomerIP: expected %q, got %v", "192.168.1.1", tx.CustomerIP)
	}
	if tx.CustomerEmail == nil || *tx.CustomerEmail != "user@example.com" {
		t.Errorf("CustomerEmail: expected %q, got %v", "user@example.com", tx.CustomerEmail)
	}
}

func TestDerefStr_NilPointer_ReturnsEmpty(t *testing.T) {
	// derefStr(nil) → "" — тестируем через processOne с nil полями.
	repo := &mockRepo{
		fetchPendFn: func(_ context.Context, _ int) ([]*domain.Transaction, error) {
			return []*domain.Transaction{
				{
					ID:            "tx-nil-fields",
					Amount:        5000,
					Currency:      "USD",
					MerchantID:    "merchant-001",
					PaymentMethod: "card",
					Status:        domain.StatusPending,
					CardHash:      nil, // nil → derefStr → ""
					CustomerIP:    nil,
					CustomerEmail: nil,
					CreatedAt:     time.Now(),
				},
			}, nil
		},
	}

	pub := &mockPublisher{}
	svc := New(repo, pub)

	count, err := svc.ProcessPendingPayments(context.Background(), 10)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if count != 1 {
		t.Errorf("count = %d, want 1", count)
	}

	// Проверяем что nil поля стали пустыми строками в событии.
	if len(pub.publishedEvents) != 1 {
		t.Fatalf("expected 1 event, got %d", len(pub.publishedEvents))
	}
	e := pub.publishedEvents[0]
	if e.CardHash != "" {
		t.Errorf("CardHash: expected empty string for nil, got %q", e.CardHash)
	}
	if e.CustomerIP != "" {
		t.Errorf("CustomerIP: expected empty string for nil, got %q", e.CustomerIP)
	}
	if e.CustomerEmail != "" {
		t.Errorf("CustomerEmail: expected empty string for nil, got %q", e.CustomerEmail)
	}
}

func TestDerefStr_NonNilPointer_ReturnsValue(t *testing.T) {
	// derefStr(&"value") → "value"
	cardHash := "hash_abc123"
	customerIP := "10.0.0.1"
	customerEmail := "test@example.com"

	repo := &mockRepo{
		fetchPendFn: func(_ context.Context, _ int) ([]*domain.Transaction, error) {
			return []*domain.Transaction{
				{
					ID:            "tx-with-fields",
					Amount:        5000,
					Currency:      "USD",
					MerchantID:    "merchant-001",
					PaymentMethod: "card",
					Status:        domain.StatusPending,
					CardHash:      &cardHash,
					CustomerIP:    &customerIP,
					CustomerEmail: &customerEmail,
					CreatedAt:     time.Now(),
				},
			}, nil
		},
	}

	pub := &mockPublisher{}
	svc := New(repo, pub)

	_, err := svc.ProcessPendingPayments(context.Background(), 10)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(pub.publishedEvents) != 1 {
		t.Fatalf("expected 1 event, got %d", len(pub.publishedEvents))
	}
	e := pub.publishedEvents[0]
	if e.CardHash != cardHash {
		t.Errorf("CardHash: got %q, want %q", e.CardHash, cardHash)
	}
	if e.CustomerIP != customerIP {
		t.Errorf("CustomerIP: got %q, want %q", e.CustomerIP, customerIP)
	}
	if e.CustomerEmail != customerEmail {
		t.Errorf("CustomerEmail: got %q, want %q", e.CustomerEmail, customerEmail)
	}
}

func TestProcessPendingPayments_FetchError(t *testing.T) {
	repo := &mockRepo{
		fetchPendFn: func(_ context.Context, _ int) ([]*domain.Transaction, error) {
			return nil, errors.New("database connection lost")
		},
	}
	pub := &mockPublisher{}
	svc := New(repo, pub)

	_, err := svc.ProcessPendingPayments(context.Background(), 10)
	if err == nil {
		t.Fatal("expected error for fetch failure, got nil")
	}
}

func TestProcessPendingPayments_EmptyBatch(t *testing.T) {
	repo := &mockRepo{
		fetchPendFn: func(_ context.Context, _ int) ([]*domain.Transaction, error) {
			return []*domain.Transaction{}, nil
		},
	}
	pub := &mockPublisher{}
	svc := New(repo, pub)

	count, err := svc.ProcessPendingPayments(context.Background(), 10)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if count != 0 {
		t.Errorf("count = %d, want 0 for empty batch", count)
	}
}

// Тесты ResolveStuckPayments

func TestResolveStuckPayments_Empty(t *testing.T) {
	repo := &mockRepo{
		fetchStuckFn: func(_ context.Context, _ time.Duration, _ int) ([]*domain.Transaction, error) {
			return nil, nil
		},
	}
	svc := New(repo, &mockPublisher{})

	count, err := svc.ResolveStuckPayments(context.Background(), 10*time.Minute, 10)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if count != 0 {
		t.Errorf("expected 0 resolved, got %d", count)
	}
}

func TestResolveStuckPayments_Found(t *testing.T) {
	stuckTxns := []*domain.Transaction{
		{ID: "tx-stuck-1", MerchantID: "merch-1", Amount: 1000, Currency: "USD"},
		{ID: "tx-stuck-2", MerchantID: "merch-1", Amount: 2000, Currency: "RUB"},
	}
	repo := &mockRepo{
		fetchStuckFn: func(_ context.Context, _ time.Duration, _ int) ([]*domain.Transaction, error) {
			return stuckTxns, nil
		},
	}
	svc := New(repo, &mockPublisher{})

	count, err := svc.ResolveStuckPayments(context.Background(), 10*time.Minute, 10)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if count != 2 {
		t.Errorf("expected 2 resolved, got %d", count)
	}
}

func TestResolveStuckPayments_RepoError(t *testing.T) {
	repo := &mockRepo{
		fetchStuckFn: func(_ context.Context, _ time.Duration, _ int) ([]*domain.Transaction, error) {
			return nil, errors.New("db unavailable")
		},
	}
	svc := New(repo, &mockPublisher{})

	_, err := svc.ResolveStuckPayments(context.Background(), 10*time.Minute, 10)
	if err == nil {
		t.Fatal("expected error from repo")
	}
}

func TestResolveStuckPayments_ThresholdAndLimitPassed(t *testing.T) {
	var capturedThreshold time.Duration
	var capturedLimit int

	repo := &mockRepo{
		fetchStuckFn: func(_ context.Context, threshold time.Duration, limit int) ([]*domain.Transaction, error) {
			capturedThreshold = threshold
			capturedLimit = limit
			return nil, nil
		},
	}
	svc := New(repo, &mockPublisher{})

	wantThreshold := 15 * time.Minute
	wantLimit := 7

	_, _ = svc.ResolveStuckPayments(context.Background(), wantThreshold, wantLimit)

	if capturedThreshold != wantThreshold {
		t.Errorf("threshold: got %v, want %v", capturedThreshold, wantThreshold)
	}
	if capturedLimit != wantLimit {
		t.Errorf("limit: got %d, want %d", capturedLimit, wantLimit)
	}
}
