package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/BuzzLyutic/payment-gateway-microservices/services/transaction-service/internal/domain"
	"github.com/BuzzLyutic/payment-gateway-microservices/services/transaction-service/internal/service"
)

// --- Mocks ---

type mockPaymentService struct {
	createFn func(ctx context.Context, req service.CreatePaymentRequest) (*domain.Transaction, error)
	getFn    func(ctx context.Context, id string) (*domain.Transaction, error)
}

func (m *mockPaymentService) CreatePayment(ctx context.Context, req service.CreatePaymentRequest) (*domain.Transaction, error) {
	return m.createFn(ctx, req)
}

func (m *mockPaymentService) GetPayment(ctx context.Context, id string) (*domain.Transaction, error) {
	return m.getFn(ctx, id)
}

type mockIdempotency struct {
	lockFn   func(ctx context.Context, key string) (bool, error)
	setFn    func(ctx context.Context, key, txID string) error
	getFn    func(ctx context.Context, key string) (string, error)
	unlockFn func(ctx context.Context, key string) error
}

func (m *mockIdempotency) Lock(ctx context.Context, key string) (bool, error) {
	if m.lockFn != nil {
		return m.lockFn(ctx, key)
	}
	return true, nil
}

func (m *mockIdempotency) SetTransactionID(ctx context.Context, key, txID string) error {
	if m.setFn != nil {
		return m.setFn(ctx, key, txID)
	}
	return nil
}

func (m *mockIdempotency) GetTransactionID(ctx context.Context, key string) (string, error) {
	if m.getFn != nil {
		return m.getFn(ctx, key)
	}
	return "", nil
}

func (m *mockIdempotency) Unlock(ctx context.Context, key string) error {
	if m.unlockFn != nil {
		return m.unlockFn(ctx, key)
	}
	return nil
}

// --- Helpers ---

func newTestHandler(svc *mockPaymentService, idem *mockIdempotency) (*PaymentHandler, *http.ServeMux) {
	h := NewPaymentHandler(svc, idem)
	mux := http.NewServeMux()
	h.Register(mux)
	return h, mux
}

func makeCreateRequest(body string, idempotencyKey string) *http.Request {
	req := httptest.NewRequest(http.MethodPost, "/api/v1/payments", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	if idempotencyKey != "" {
		req.Header.Set("X-Idempotency-Key", idempotencyKey)
	}
	return req
}

func testTransaction() *domain.Transaction {
	return &domain.Transaction{
		ID:        "tx-test-123",
		Status:    domain.StatusPending,
		Amount:    10000,
		Currency:  "RUB",
		CreatedAt: time.Date(2025, 1, 15, 10, 30, 0, 0, time.UTC),
		UpdatedAt: time.Date(2025, 1, 15, 10, 30, 0, 0, time.UTC),
	}
}

// --- CreatePayment Tests ---

func TestCreatePayment_Success(t *testing.T) {
	svc := &mockPaymentService{
		createFn: func(_ context.Context, req service.CreatePaymentRequest) (*domain.Transaction, error) {
			return testTransaction(), nil
		},
	}
	idem := &mockIdempotency{}
	_, mux := newTestHandler(svc, idem)

	body := `{
		"amount": 10000,
		"currency": "RUB",
		"merchant_id": "m_123",
		"payment_method": {"type": "card"}
	}`
	req := makeCreateRequest(body, "key-001")
	rr := httptest.NewRecorder()

	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusCreated {
		t.Errorf("status = %d, want %d", rr.Code, http.StatusCreated)
	}

	var resp PaymentResponse
	json.NewDecoder(rr.Body).Decode(&resp)

	if resp.ID != "tx-test-123" {
		t.Errorf("id = %s, want tx-test-123", resp.ID)
	}
	if resp.Status != "pending" {
		t.Errorf("status = %s, want pending", resp.Status)
	}
}

func TestCreatePayment_MissingIdempotencyKey(t *testing.T) {
	svc := &mockPaymentService{}
	idem := &mockIdempotency{}
	_, mux := newTestHandler(svc, idem)

	body := `{"amount":10000,"currency":"RUB","merchant_id":"m_123"}`
	req := makeCreateRequest(body, "") // без ключа
	rr := httptest.NewRecorder()

	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", rr.Code, http.StatusBadRequest)
	}
}

func TestCreatePayment_ValidationFailed(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{
			"missing amount",
			`{"currency":"RUB","merchant_id":"m_123","payment_method":{"type":"card"}}`,
		},
		{
			"negative amount",
			`{"amount":-100,"currency":"RUB","merchant_id":"m_123","payment_method":{"type":"card"}}`,
		},
		{
			"missing currency",
			`{"amount":10000,"merchant_id":"m_123","payment_method":{"type":"card"}}`,
		},
		{
			"missing merchant_id",
			`{"amount":10000,"currency":"RUB","payment_method":{"type":"card"}}`,
		},
		{
			"missing payment_method type",
			`{"amount":10000,"currency":"RUB","merchant_id":"m_123"}`,
		},
		{
			"all empty",
			`{}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := &mockPaymentService{}
			idem := &mockIdempotency{}
			_, mux := newTestHandler(svc, idem)

			req := makeCreateRequest(tt.body, "key-001")
			rr := httptest.NewRecorder()

			mux.ServeHTTP(rr, req)

			if rr.Code != http.StatusBadRequest {
				t.Errorf("status = %d, want %d", rr.Code, http.StatusBadRequest)
			}

			var resp ErrorResponse
			json.NewDecoder(rr.Body).Decode(&resp)
			if resp.Error != "validation failed" {
				t.Errorf("error = %s, want 'validation failed'", resp.Error)
			}
		})
	}
}

func TestCreatePayment_InvalidJSON(t *testing.T) {
	svc := &mockPaymentService{}
	idem := &mockIdempotency{}
	_, mux := newTestHandler(svc, idem)

	req := makeCreateRequest("not json at all", "key-001")
	rr := httptest.NewRecorder()

	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", rr.Code, http.StatusBadRequest)
	}
}

func TestCreatePayment_IdempotentDuplicate(t *testing.T) {
	capturedTx := testTransaction()
	capturedTx.Status = domain.StatusCaptured

	svc := &mockPaymentService{
		getFn: func(_ context.Context, id string) (*domain.Transaction, error) {
			return capturedTx, nil
		},
	}

	idem := &mockIdempotency{
		lockFn: func(_ context.Context, _ string) (bool, error) {
			return false, nil // ключ уже существует
		},
		getFn: func(_ context.Context, _ string) (string, error) {
			return "tx-test-123", nil // возвращаем ID
		},
	}

	_, mux := newTestHandler(svc, idem)

	body := `{"amount":99999,"currency":"USD","merchant_id":"other"}`
	req := makeCreateRequest(body, "key-001")
	rr := httptest.NewRecorder()

	mux.ServeHTTP(rr, req)

	// 200, не 201 — повторный запрос
	if rr.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", rr.Code, http.StatusOK)
	}

	var resp PaymentResponse
	json.NewDecoder(rr.Body).Decode(&resp)

	// Данные от первого запроса, не от второго
	if resp.Status != "captured" {
		t.Errorf("status = %s, want captured", resp.Status)
	}
}

func TestCreatePayment_IdempotentProcessing(t *testing.T) {
	svc := &mockPaymentService{}

	idem := &mockIdempotency{
		lockFn: func(_ context.Context, _ string) (bool, error) {
			return false, nil
		},
		getFn: func(_ context.Context, _ string) (string, error) {
			return "processing", nil // ещё обрабатывается
		},
	}

	_, mux := newTestHandler(svc, idem)

	req := makeCreateRequest(`{}`, "key-001")
	rr := httptest.NewRecorder()

	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusConflict {
		t.Errorf("status = %d, want %d", rr.Code, http.StatusConflict)
	}
}

// --- GetPayment Tests ---

func TestGetPayment_Success(t *testing.T) {
	svc := &mockPaymentService{
		getFn: func(_ context.Context, id string) (*domain.Transaction, error) {
			tx := testTransaction()
			tx.ID = id
			return tx, nil
		},
	}
	idem := &mockIdempotency{}
	_, mux := newTestHandler(svc, idem)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/payments/tx-123", nil)
	rr := httptest.NewRecorder()

	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", rr.Code, http.StatusOK)
	}

	var resp PaymentResponse
	json.NewDecoder(rr.Body).Decode(&resp)

	if resp.ID != "tx-123" {
		t.Errorf("id = %s, want tx-123", resp.ID)
	}
}

func TestGetPayment_NotFound(t *testing.T) {
	svc := &mockPaymentService{
		getFn: func(_ context.Context, _ string) (*domain.Transaction, error) {
			return nil, domain.ErrNotFound
		},
	}
	idem := &mockIdempotency{}
	_, mux := newTestHandler(svc, idem)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/payments/nonexistent", nil)
	rr := httptest.NewRecorder()

	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d", rr.Code, http.StatusNotFound)
	}
}

func TestHashCard_EmptyString_ReturnsEmpty(t *testing.T) {
	// hashCard("") → "" — карта не передана, хешировать нечего.
	// Тестируем косвенно: запрос без card_number → CardHash пустой.
	svc := &mockPaymentService{
		createFn: func(_ context.Context, req service.CreatePaymentRequest) (*domain.Transaction, error) {
			// Проверяем что CardHash пустой когда card_number не передан.
			if req.CardHash != "" {
				return nil, fmt.Errorf("expected empty CardHash, got %q", req.CardHash)
			}
			return testTransaction(), nil
		},
	}
	idem := &mockIdempotency{}
	_, mux := newTestHandler(svc, idem)

	// Запрос без card_number.
	body := `{
		"amount": 10000,
		"currency": "RUB",
		"merchant_id": "m_123",
		"payment_method": {"type": "card"}
	}`
	req := makeCreateRequest(body, "key-hash-empty")
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusCreated {
		t.Errorf("status = %d, want 201; body: %s", rr.Code, rr.Body.String())
	}
}

func TestHashCard_NonEmptyString_ProducesHash(t *testing.T) {
	// hashCard("4111111111111111") → 64-символьный hex SHA-256.
	var capturedHash string

	svc := &mockPaymentService{
		createFn: func(_ context.Context, req service.CreatePaymentRequest) (*domain.Transaction, error) {
			capturedHash = req.CardHash
			return testTransaction(), nil
		},
	}
	idem := &mockIdempotency{}
	_, mux := newTestHandler(svc, idem)

	body := `{
		"amount": 10000,
		"currency": "RUB",
		"merchant_id": "m_123",
		"payment_method": {
			"type": "card",
			"card_number": "4111111111111111"
		}
	}`
	req := makeCreateRequest(body, "key-hash-nonempty")
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusCreated {
		t.Errorf("status = %d, want 201", rr.Code)
	}

	// SHA-256 всегда 64 hex символа.
	if len(capturedHash) != 64 {
		t.Errorf("CardHash length = %d, want 64 (SHA-256 hex)", len(capturedHash))
	}
}

func TestHashCard_SpacesStripped(t *testing.T) {
	// "4111 1111 1111 1111" и "4111111111111111" должны давать одинаковый хеш.
	var hashWithSpaces, hashWithoutSpaces string

	makeSvc := func(capture *string) *mockPaymentService {
		return &mockPaymentService{
			createFn: func(_ context.Context, req service.CreatePaymentRequest) (*domain.Transaction, error) {
				*capture = req.CardHash
				return testTransaction(), nil
			},
		}
	}

	idem := &mockIdempotency{}

	// Запрос с пробелами.
	_, mux1 := newTestHandler(makeSvc(&hashWithSpaces), idem)
	body1 := `{"amount":10000,"currency":"RUB","merchant_id":"m_123",
		"payment_method":{"type":"card","card_number":"4111 1111 1111 1111"}}`
	req1 := makeCreateRequest(body1, "key-spaces")
	rr1 := httptest.NewRecorder()
	mux1.ServeHTTP(rr1, req1)

	// Запрос без пробелов.
	_, mux2 := newTestHandler(makeSvc(&hashWithoutSpaces), idem)
	body2 := `{"amount":10000,"currency":"RUB","merchant_id":"m_123",
		"payment_method":{"type":"card","card_number":"4111111111111111"}}`
	req2 := makeCreateRequest(body2, "key-nospaces")
	rr2 := httptest.NewRecorder()
	mux2.ServeHTTP(rr2, req2)

	if rr1.Code != http.StatusCreated || rr2.Code != http.StatusCreated {
		t.Fatalf("unexpected status: %d, %d", rr1.Code, rr2.Code)
	}

	if hashWithSpaces != hashWithoutSpaces {
		t.Errorf("hash with spaces %q != hash without %q (spaces not stripped)",
			hashWithSpaces, hashWithoutSpaces)
	}
}

func TestToPaymentResponse_WithProvider(t *testing.T) {
	// toPaymentResponse когда tx.Provider != nil — ветка не покрыта.
	provider := "mock_provider_a"
	svc := &mockPaymentService{
		getFn: func(_ context.Context, id string) (*domain.Transaction, error) {
			tx := testTransaction()
			tx.ID = id
			tx.Provider = &provider // ← не nil
			tx.Status = domain.StatusCaptured
			return tx, nil
		},
	}
	idem := &mockIdempotency{}
	_, mux := newTestHandler(svc, idem)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/payments/tx-with-provider", nil)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rr.Code)
	}

	var resp PaymentResponse
	json.NewDecoder(rr.Body).Decode(&resp)

	if resp.Provider != "mock_provider_a" {
		t.Errorf("Provider = %q, want %q", resp.Provider, "mock_provider_a")
	}
	if resp.Status != "captured" {
		t.Errorf("Status = %q, want %q", resp.Status, "captured")
	}
}

func TestGetPayment_ServiceError(t *testing.T) {
	// GetPayment: ошибка не ErrNotFound → 500.
	// Покрываем ветку writeJSON со статусом 500.
	svc := &mockPaymentService{
		getFn: func(_ context.Context, _ string) (*domain.Transaction, error) {
			return nil, errors.New("database connection lost")
		},
	}
	idem := &mockIdempotency{}
	_, mux := newTestHandler(svc, idem)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/payments/tx-error", nil)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", rr.Code)
	}
}

func TestCreatePayment_ServiceError(t *testing.T) {
	// CreatePayment: сервис вернул ошибку → 500.
	svc := &mockPaymentService{
		createFn: func(_ context.Context, _ service.CreatePaymentRequest) (*domain.Transaction, error) {
			return nil, errors.New("db write failed")
		},
	}
	idem := &mockIdempotency{}
	_, mux := newTestHandler(svc, idem)

	body := `{
		"amount": 10000,
		"currency": "RUB",
		"merchant_id": "m_123",
		"payment_method": {"type": "card"}
	}`
	req := makeCreateRequest(body, "key-svc-error")
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", rr.Code)
	}
}

func TestCreatePayment_IdempotentGetError(t *testing.T) {
	// Повторный запрос, GetTransactionID вернул ошибку → 500.
	svc := &mockPaymentService{}
	idem := &mockIdempotency{
		lockFn: func(_ context.Context, _ string) (bool, error) {
			return false, nil // уже заблокирован
		},
		getFn: func(_ context.Context, _ string) (string, error) {
			return "", errors.New("redis unavailable")
		},
	}
	_, mux := newTestHandler(svc, idem)

	req := makeCreateRequest(`{}`, "key-idem-get-error")
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", rr.Code)
	}
}

func TestCreatePayment_IdempotentFetchTxError(t *testing.T) {
	// Повторный запрос, GetTransactionID вернул ID, но GetPayment упал → 500.
	svc := &mockPaymentService{
		getFn: func(_ context.Context, _ string) (*domain.Transaction, error) {
			return nil, errors.New("db unavailable")
		},
	}
	idem := &mockIdempotency{
		lockFn: func(_ context.Context, _ string) (bool, error) {
			return false, nil
		},
		getFn: func(_ context.Context, _ string) (string, error) {
			return "tx-existing-id", nil
		},
	}
	_, mux := newTestHandler(svc, idem)

	req := makeCreateRequest(`{}`, "key-fetch-error")
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", rr.Code)
	}
}

func TestCreatePayment_LockError_ContinuesProcessing(t *testing.T) {
	// Lock вернул ошибку (Redis недоступен) — продолжаем обработку.
	// locked=false, err!=nil → пропускаем idempotency, декодируем запрос.
	svc := &mockPaymentService{
		createFn: func(_ context.Context, _ service.CreatePaymentRequest) (*domain.Transaction, error) {
			return testTransaction(), nil
		},
	}
	idem := &mockIdempotency{
		lockFn: func(_ context.Context, _ string) (bool, error) {
			return false, errors.New("redis unavailable") // ошибка Redis
		},
	}
	_, mux := newTestHandler(svc, idem)

	body := `{
		"amount": 10000,
		"currency": "RUB",
		"merchant_id": "m_123",
		"payment_method": {"type": "card"}
	}`
	req := makeCreateRequest(body, "key-lock-error")
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	// При ошибке Redis — продолжаем, UNIQUE constraint в БД спасёт.
	// Ожидаем 201 (транзакция создана).
	if rr.Code != http.StatusCreated {
		t.Errorf("status = %d, want 201 (fail-open on Redis error)", rr.Code)
	}
}

func TestWriteJSON_ContentType(t *testing.T) {
	// writeJSON всегда устанавливает Content-Type: application/json.
	svc := &mockPaymentService{
		getFn: func(_ context.Context, _ string) (*domain.Transaction, error) {
			return nil, domain.ErrNotFound
		},
	}
	idem := &mockIdempotency{}
	_, mux := newTestHandler(svc, idem)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/payments/any-id", nil)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	ct := rr.Header().Get("Content-Type")
	if ct != "application/json" {
		t.Errorf("Content-Type = %q, want %q", ct, "application/json")
	}
}
