package webhook_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/BuzzLyutic/payment-gateway-microservices/services/transaction-service/internal/webhook"
)

// Мок репозитория

type mockWorkerRepo struct {
	deliveries    []*webhook.Delivery
	fetchErr      error
	merchantCfg   *webhook.MerchantConfig
	merchantErr   error
	markedDelivered []string
	markedFailed    []failedRecord
}

type failedRecord struct {
	id       string
	attempts int
	lastErr  string
}

func (m *mockWorkerRepo) FetchPendingDeliveries(_ context.Context, limit int) ([]*webhook.Delivery, error) {
	if m.fetchErr != nil {
		return nil, m.fetchErr
	}
	if len(m.deliveries) > limit {
		return m.deliveries[:limit], nil
	}
	return m.deliveries, nil
}

func (m *mockWorkerRepo) GetMerchantConfig(_ context.Context, _ string) (*webhook.MerchantConfig, error) {
	return m.merchantCfg, m.merchantErr
}

func (m *mockWorkerRepo) MarkDelivered(_ context.Context, id string) error {
	m.markedDelivered = append(m.markedDelivered, id)
	return nil
}

func (m *mockWorkerRepo) MarkFailed(_ context.Context, id string, attempts int, lastError string) error {
	m.markedFailed = append(m.markedFailed, failedRecord{id, attempts, lastError})
	return nil
}

// Хелперы

func makeDelivery(id, merchantID string) *webhook.Delivery {
	return &webhook.Delivery{
		ID:          id,
		TransactionID: "tx-" + id,
		MerchantID:  merchantID,
		EventType:   "payment.captured",
		Payload:     []byte(`{"event":"payment.captured","transaction_id":"tx-001"}`),
		Attempts:    0,
		MaxAttempts: 5,
		NextRetryAt: time.Now().Add(-time.Second),
	}
}

// newWorkerWithServer создаёт Worker и httptest.Server — sender будет слать туда.
func newWorkerWithServer(t *testing.T, repo *mockWorkerRepo, statusCode int) (*webhook.Worker, *httptest.Server) {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(statusCode)
	}))
	return webhook.NewWorker(repo, 100*time.Millisecond, 10), server
}

// Тесты NewWorker

func TestNewWorker_NotNil(t *testing.T) {
	w := webhook.NewWorker(&mockWorkerRepo{}, time.Minute, 10)
	if w == nil {
		t.Error("NewWorker returned nil")
	}
}

// Тесты processBatch

// TestProcessBatch_EmptyQueue — нет pending доставок → ничего не происходит.
func TestProcessBatch_EmptyQueue(t *testing.T) {
	repo := &mockWorkerRepo{deliveries: nil}
	w := webhook.NewWorker(repo, time.Minute, 10)

	w.ExportProcessBatch(context.Background())

	if len(repo.markedDelivered) != 0 || len(repo.markedFailed) != 0 {
		t.Error("empty queue must not mark anything")
	}
}

// TestProcessBatch_FetchError — ошибка fetch → не паникуем.
func TestProcessBatch_FetchError(t *testing.T) {
	repo := &mockWorkerRepo{fetchErr: errors.New("db error")}
	w := webhook.NewWorker(repo, time.Minute, 10)

	// Не должно паниковать.
	w.ExportProcessBatch(context.Background())
}

// TestProcessBatch_SuccessDelivery — успешная доставка → MarkDelivered.
func TestProcessBatch_SuccessDelivery(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	repo := &mockWorkerRepo{
		deliveries: []*webhook.Delivery{makeDelivery("d-001", "merch-1")},
		merchantCfg: &webhook.MerchantConfig{
			WebhookURL:    server.URL,
			WebhookSecret: "secret",
		},
	}

	w := webhook.NewWorker(repo, time.Minute, 10)
	w.ExportProcessBatch(context.Background())

	if len(repo.markedDelivered) != 1 {
		t.Errorf("expected 1 MarkDelivered call, got %d", len(repo.markedDelivered))
	}
	if repo.markedDelivered[0] != "d-001" {
		t.Errorf("wrong delivery id: %q", repo.markedDelivered[0])
	}
}

// TestProcessBatch_FailedDelivery — HTTP 500 → MarkFailed.
func TestProcessBatch_FailedDelivery(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	repo := &mockWorkerRepo{
		deliveries: []*webhook.Delivery{makeDelivery("d-002", "merch-1")},
		merchantCfg: &webhook.MerchantConfig{
			WebhookURL:    server.URL,
			WebhookSecret: "secret",
		},
	}

	w := webhook.NewWorker(repo, time.Minute, 10)
	w.ExportProcessBatch(context.Background())

	if len(repo.markedFailed) != 1 {
		t.Errorf("expected 1 MarkFailed call, got %d", len(repo.markedFailed))
	}
	if repo.markedFailed[0].id != "d-002" {
		t.Errorf("wrong delivery id: %q", repo.markedFailed[0].id)
	}
	// Attempts должен инкрементироваться: 0 + 1 = 1.
	if repo.markedFailed[0].attempts != 1 {
		t.Errorf("expected attempts=1, got %d", repo.markedFailed[0].attempts)
	}
}

// TestProcessBatch_MerchantNotFound — конфиг мерчанта nil → MarkFailed без HTTP.
func TestProcessBatch_MerchantNotFound(t *testing.T) {
	repo := &mockWorkerRepo{
		deliveries:  []*webhook.Delivery{makeDelivery("d-003", "unknown-merch")},
		merchantCfg: nil, // не найден
	}

	w := webhook.NewWorker(repo, time.Minute, 10)
	w.ExportProcessBatch(context.Background())

	if len(repo.markedFailed) != 1 {
		t.Fatalf("expected 1 MarkFailed, got %d", len(repo.markedFailed))
	}
	// При отсутствии конфига используем MaxAttempts — сразу failed.
	if repo.markedFailed[0].attempts != makeDelivery("d-003", "x").MaxAttempts {
		t.Errorf("expected MaxAttempts on merchant not found")
	}
}

// TestProcessBatch_MultipleBatch — несколько доставок обрабатываются все.
func TestProcessBatch_MultipleBatch(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	repo := &mockWorkerRepo{
		deliveries: []*webhook.Delivery{
			makeDelivery("d-010", "merch-1"),
			makeDelivery("d-011", "merch-1"),
			makeDelivery("d-012", "merch-1"),
		},
		merchantCfg: &webhook.MerchantConfig{
			WebhookURL:    server.URL,
			WebhookSecret: "secret",
		},
	}

	w := webhook.NewWorker(repo, time.Minute, 10)
	w.ExportProcessBatch(context.Background())

	if len(repo.markedDelivered) != 3 {
		t.Errorf("expected 3 MarkDelivered, got %d", len(repo.markedDelivered))
	}
}

// TestProcessBatch_AttemptsIncrement — счётчик попыток инкрементируется.
func TestProcessBatch_AttemptsIncrement(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer server.Close()

	delivery := makeDelivery("d-020", "merch-1")
	delivery.Attempts = 2 // уже было 2 попытки

	repo := &mockWorkerRepo{
		deliveries: []*webhook.Delivery{delivery},
		merchantCfg: &webhook.MerchantConfig{
			WebhookURL:    server.URL,
			WebhookSecret: "secret",
		},
	}

	w := webhook.NewWorker(repo, time.Minute, 10)
	w.ExportProcessBatch(context.Background())

	if len(repo.markedFailed) != 1 {
		t.Fatalf("expected 1 MarkFailed")
	}
	// 2 + 1 = 3.
	if repo.markedFailed[0].attempts != 3 {
		t.Errorf("expected attempts=3, got %d", repo.markedFailed[0].attempts)
	}
}

// TestRun_StopsOnContextCancel — Run завершается при отмене контекста.
func TestRun_StopsOnContextCancel(t *testing.T) {
	repo := &mockWorkerRepo{}
	w := webhook.NewWorker(repo, time.Hour, 10)

	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan struct{})
	go func() {
		w.Run(ctx)
		close(done)
	}()

	cancel()

	select {
	case <-done:
		// OK
	case <-time.After(time.Second):
		t.Fatal("Run did not stop after context cancellation")
	}
}

// Тесты previewJSON

func TestPreviewJSON_ValidPayload(t *testing.T) {
	payload := []byte(`{"event":"payment.captured","transaction_id":"tx-abc"}`)
	preview := webhook.ExportPreviewJSON(payload)

	if preview == "" {
		t.Error("preview must not be empty")
	}
	// Должен содержать event и transaction_id.
	if preview != "event=payment.captured transaction_id=tx-abc" {
		t.Errorf("unexpected preview: %q", preview)
	}
}

func TestPreviewJSON_InvalidJSON(t *testing.T) {
	payload := []byte(`not-json`)
	preview := webhook.ExportPreviewJSON(payload)

	if preview == "" {
		t.Error("preview must not be empty for invalid JSON")
	}
}

func TestPreviewJSON_LongPayload(t *testing.T) {
	// Создаём payload длиннее 200 символов невалидного JSON.
	long := make([]byte, 300)
	for i := range long {
		long[i] = 'x'
	}
	preview := webhook.ExportPreviewJSON(long)
	if len(preview) > 200 {
		t.Errorf("preview must be truncated to 200 chars, got %d", len(preview))
	}
}
