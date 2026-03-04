package service

import (
	"context"
	"errors"
	"testing"

	"github.com/BuzzLyutic/payment-gateway-microservices/services/transaction-service/internal/domain"
	"github.com/BuzzLyutic/payment-gateway-microservices/services/transaction-service/internal/provider"
)

// --- Mock Repository ---

type mockRepo struct {
	createFn      func(ctx context.Context, tx *domain.Transaction) error
	getByIDFn     func(ctx context.Context, id string) (*domain.Transaction, error)
	fetchPendFn   func(ctx context.Context, limit int) ([]*domain.Transaction, error)
	updateStatFn  func(ctx context.Context, id string, status domain.Status, prov, txID, errMsg *string) error
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

// --- Mock Provider ---

type mockProvider struct {
	processFn func(ctx context.Context, tx *domain.Transaction) (*provider.Result, error)
}

func (m *mockProvider) ProcessPayment(ctx context.Context, tx *domain.Transaction) (*provider.Result, error) {
	if m.processFn != nil {
		return m.processFn(ctx, tx)
	}
	return &provider.Result{
		ProviderTxID: "mock_tx_abc",
		Status:       domain.StatusCaptured,
	}, nil
}

// --- Tests ---

func TestTransactionService_CreatePayment(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		repo := &mockRepo{}
		prov := &mockProvider{}
		svc := New(repo, prov)

		tx, err := svc.CreatePayment(context.Background(), CreatePaymentRequest{
			IdempotencyKey: "key-1",
			MerchantID:     "m_123",
			Amount:         10000,
			Currency:       "RUB",
			Description:    "test payment",
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
		if tx.Amount != 10000 {
			t.Errorf("amount = %d, want 10000", tx.Amount)
		}
	})

	t.Run("repository error", func(t *testing.T) {
		repo := &mockRepo{
			createFn: func(_ context.Context, _ *domain.Transaction) error {
				return errors.New("db connection lost")
			},
		}
		prov := &mockProvider{}
		svc := New(repo, prov)

		_, err := svc.CreatePayment(context.Background(), CreatePaymentRequest{
			MerchantID: "m_123",
			Amount:     10000,
			Currency:   "RUB",
		})

		if err == nil {
			t.Fatal("expected error, got nil")
		}
	})
}

func TestTransactionService_GetPayment(t *testing.T) {
	t.Run("found", func(t *testing.T) {
		repo := &mockRepo{
			getByIDFn: func(_ context.Context, id string) (*domain.Transaction, error) {
				return &domain.Transaction{
					ID:     id,
					Status: domain.StatusCaptured,
					Amount: 5000,
				}, nil
			},
		}
		prov := &mockProvider{}
		svc := New(repo, prov)

		tx, err := svc.GetPayment(context.Background(), "abc-123")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if tx.Status != domain.StatusCaptured {
			t.Errorf("status = %s, want captured", tx.Status)
		}
	})

	t.Run("not found", func(t *testing.T) {
		repo := &mockRepo{}
		prov := &mockProvider{}
		svc := New(repo, prov)

		_, err := svc.GetPayment(context.Background(), "nonexistent")
		if !errors.Is(err, domain.ErrNotFound) {
			t.Errorf("error = %v, want ErrNotFound", err)
		}
	})
}

func TestTransactionService_ProcessPending_Captured(t *testing.T) {
	var updatedStatus domain.Status

	repo := &mockRepo{
		fetchPendFn: func(_ context.Context, _ int) ([]*domain.Transaction, error) {
			return []*domain.Transaction{
				{ID: "tx-1", Amount: 10000, Status: domain.StatusProcessing},
			}, nil
		},
		updateStatFn: func(_ context.Context, _ string, status domain.Status, _, _, _ *string) error {
			updatedStatus = status
			return nil
		},
	}

	prov := &mockProvider{
		processFn: func(_ context.Context, _ *domain.Transaction) (*provider.Result, error) {
			return &provider.Result{
				ProviderTxID: "prov_tx_1",
				Status:       domain.StatusCaptured,
			}, nil
		},
	}

	svc := New(repo, prov)

	count, err := svc.ProcessPendingPayments(context.Background(), 10)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if count != 1 {
		t.Errorf("count = %d, want 1", count)
	}
	if updatedStatus != domain.StatusCaptured {
		t.Errorf("status = %s, want captured", updatedStatus)
	}
}

func TestTransactionService_ProcessPending_ProviderDecline(t *testing.T) {
	var updatedStatus domain.Status

	repo := &mockRepo{
		fetchPendFn: func(_ context.Context, _ int) ([]*domain.Transaction, error) {
			return []*domain.Transaction{
				{ID: "tx-2", Amount: 5000, Status: domain.StatusProcessing},
			}, nil
		},
		updateStatFn: func(_ context.Context, _ string, status domain.Status, _, _, _ *string) error {
			updatedStatus = status
			return nil
		},
	}

	prov := &mockProvider{
		processFn: func(_ context.Context, _ *domain.Transaction) (*provider.Result, error) {
			return &provider.Result{
				Status:       domain.StatusDeclined,
				ErrorMessage: "insufficient funds",
			}, nil
		},
	}

	svc := New(repo, prov)
	svc.ProcessPendingPayments(context.Background(), 10)

	if updatedStatus != domain.StatusDeclined {
		t.Errorf("status = %s, want declined", updatedStatus)
	}
}

func TestTransactionService_ProcessPending_RetryExhausted(t *testing.T) {
	var updatedStatus domain.Status
	callCount := 0

	repo := &mockRepo{
		fetchPendFn: func(_ context.Context, _ int) ([]*domain.Transaction, error) {
			return []*domain.Transaction{
				{ID: "tx-3", Amount: 3000, Status: domain.StatusProcessing},
			}, nil
		},
		updateStatFn: func(_ context.Context, _ string, status domain.Status, _, _, _ *string) error {
			updatedStatus = status
			return nil
		},
	}

	prov := &mockProvider{
		processFn: func(_ context.Context, _ *domain.Transaction) (*provider.Result, error) {
			callCount++
			return nil, provider.ErrTransient
		},
	}

	svc := New(repo, prov)
	svc.ProcessPendingPayments(context.Background(), 10)

	if updatedStatus != domain.StatusFailed {
		t.Errorf("status = %s, want failed", updatedStatus)
	}
	// 1 initial + 3 retries = 4
	if callCount != 4 {
		t.Errorf("provider called %d times, want 4", callCount)
	}
}
