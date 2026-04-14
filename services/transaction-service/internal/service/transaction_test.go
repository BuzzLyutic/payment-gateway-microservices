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
