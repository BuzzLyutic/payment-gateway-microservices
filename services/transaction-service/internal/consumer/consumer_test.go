package consumer_test

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/BuzzLyutic/payment-gateway-microservices/services/transaction-service/internal/consumer"
	"github.com/BuzzLyutic/payment-gateway-microservices/services/transaction-service/internal/domain"
	"github.com/BuzzLyutic/payment-gateway-microservices/services/transaction-service/internal/events"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
)

// мок JetStream сообщения

type mockMsg struct {
	subject string
	data    []byte
	acked   bool
	naked   bool
	termed  bool
	mu      sync.Mutex
}

// TermWithReason implements jetstream.Msg.
func (m *mockMsg) TermWithReason(reason string) error {
	panic("unimplemented")
}

func (m *mockMsg) Subject() string { return m.subject }
func (m *mockMsg) Data() []byte    { return m.data }

func (m *mockMsg) Ack() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.acked = true
	return nil
}

func (m *mockMsg) Nak() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.naked = true
	return nil
}

func (m *mockMsg) Term() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.termed = true
	return nil
}

func (m *mockMsg) NakWithDelay(_ time.Duration) error {
	return m.Nak()
}

func (m *mockMsg) DoubleAck(_ context.Context) error { return nil }
func (m *mockMsg) InProgress() error                 { return nil }
func (m *mockMsg) Metadata() (*jetstream.MsgMetadata, error) {
	return &jetstream.MsgMetadata{}, nil
}
func (m *mockMsg) Headers() nats.Header { return nil }
func (m *mockMsg) Reply() string        { return "" }

// мок репозитория

type mockStatusUpdater struct {
	mu      sync.Mutex
	calls   []updateCall
	errOnID map[string]error // id → ошибка для конкретной транзакции
}

type updateCall struct {
	ID           string
	Status       domain.Status
	Provider     *string
	ProviderTxID *string
	ErrorMessage *string
}

func (m *mockStatusUpdater) UpdateStatus(
	ctx context.Context,
	id string,
	status domain.Status,
	provider *string,
	providerTxID *string,
	errorMessage *string,
) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.calls = append(m.calls, updateCall{
		ID:           id,
		Status:       status,
		Provider:     provider,
		ProviderTxID: providerTxID,
		ErrorMessage: errorMessage,
	})

	if err, ok := m.errOnID[id]; ok {
		return err
	}
	return nil
}

func (m *mockStatusUpdater) lastCall() (updateCall, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.calls) == 0 {
		return updateCall{}, false
	}
	return m.calls[len(m.calls)-1], true
}

// helpers

func marshalEvent(t *testing.T, v any) []byte {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal event: %v", err)
	}
	return data
}

func derefStr(s *string) string {
	if s == nil {
		return "<nil>"
	}
	return *s
}

// mapCompletedStatus

func TestMapCompletedStatus(t *testing.T) {
	// Критичная функция: неправильный маппинг → деньги застрянут
	// или транзакция получит неверный статус.
	cases := []struct {
		input    string
		expected domain.Status
	}{
		{"captured", domain.StatusCaptured},
		{"declined", domain.StatusDeclined},
		{"failed", domain.StatusFailed},
		{"unknown", domain.StatusFailed},    // дефолт → failed, не panic
		{"", domain.StatusFailed},           // пустая строка → failed
		{"CAPTURED", domain.StatusFailed},   // регистр важен
		{"processing", domain.StatusFailed}, // промежуточный статус → failed
	}

	for _, tc := range cases {
		t.Run(tc.input, func(t *testing.T) {
			got := consumer.MapCompletedStatus(tc.input)
			if got != tc.expected {
				t.Errorf("MapCompletedStatus(%q) = %q, want %q",
					tc.input, got, tc.expected)
			}
		})
	}
}

// handleCompleted

func TestHandleCompleted_Captured_UpdatesStatus(t *testing.T) {
	repo := &mockStatusUpdater{}
	c := consumer.New(repo)

	providerTxID := "prov_tx_abc"
	event := events.PaymentCompleted{
		TransactionID: "tx-001",
		Status:        "captured",
		Provider:      "mock_provider",
		ProviderTxID:  providerTxID,
	}

	msg := &mockMsg{
		subject: events.SubjectPaymentCompleted,
		data:    marshalEvent(t, event),
	}

	c.HandleCompleted(msg)

	call, ok := repo.lastCall()
	if !ok {
		t.Fatal("expected UpdateStatus to be called")
	}

	if call.ID != "tx-001" {
		t.Errorf("ID = %q, want %q", call.ID, "tx-001")
	}
	if call.Status != domain.StatusCaptured {
		t.Errorf("Status = %q, want captured", call.Status)
	}
	if call.Provider == nil || *call.Provider != "mock_provider" {
		t.Errorf("Provider = %q, want %q", derefStr(call.Provider), "mock_provider")
	}
	if call.ProviderTxID == nil || *call.ProviderTxID != providerTxID {
		t.Errorf("ProviderTxID = %q, want %q", derefStr(call.ProviderTxID), providerTxID)
	}
	if !msg.acked {
		t.Error("expected message to be Ack'd after successful update")
	}
}

func TestHandleCompleted_Declined_UpdatesStatus(t *testing.T) {
	repo := &mockStatusUpdater{}
	c := consumer.New(repo)

	errorMsg := "insufficient funds"
	event := events.PaymentCompleted{
		TransactionID: "tx-002",
		Status:        "declined",
		Provider:      "mock_provider",
		ErrorMessage:  errorMsg,
	}

	msg := &mockMsg{
		subject: events.SubjectPaymentCompleted,
		data:    marshalEvent(t, event),
	}

	c.HandleCompleted(msg)

	call, ok := repo.lastCall()
	if !ok {
		t.Fatal("expected UpdateStatus to be called")
	}

	if call.Status != domain.StatusDeclined {
		t.Errorf("Status = %q, want declined", call.Status)
	}
	if call.ErrorMessage == nil || *call.ErrorMessage != errorMsg {
		t.Errorf("ErrorMessage = %q, want %q", derefStr(call.ErrorMessage), errorMsg)
	}
	if !msg.acked {
		t.Error("expected message to be Ack'd")
	}
}

func TestHandleCompleted_EmptyProviderTxID_NotSet(t *testing.T) {
	// Если ProviderTxID пустой — передаём nil, не пустую строку.
	// Важно: nil vs "" в БД — разные значения.
	repo := &mockStatusUpdater{}
	c := consumer.New(repo)

	event := events.PaymentCompleted{
		TransactionID: "tx-003",
		Status:        "failed",
		Provider:      "mock_provider",
		ProviderTxID:  "", // пустой
		ErrorMessage:  "", // пустой
	}

	msg := &mockMsg{
		subject: events.SubjectPaymentCompleted,
		data:    marshalEvent(t, event),
	}

	c.HandleCompleted(msg)

	call, ok := repo.lastCall()
	if !ok {
		t.Fatal("expected UpdateStatus to be called")
	}

	// Пустые строки → nil в БД (NULL, не пустая строка)
	if call.ProviderTxID != nil {
		t.Errorf("ProviderTxID: expected nil for empty string, got %q", *call.ProviderTxID)
	}
	if call.ErrorMessage != nil {
		t.Errorf("ErrorMessage: expected nil for empty string, got %q", *call.ErrorMessage)
	}
}

func TestHandleCompleted_InvalidJSON_Terms(t *testing.T) {
	// Некорректный JSON — Term (не Nak), повторная доставка не поможет.
	repo := &mockStatusUpdater{}
	c := consumer.New(repo)

	msg := &mockMsg{
		subject: events.SubjectPaymentCompleted,
		data:    []byte("not valid json {{{"),
	}

	c.HandleCompleted(msg)

	if !msg.termed {
		t.Error("expected message to be Term'd for invalid JSON")
	}
	if msg.acked || msg.naked {
		t.Error("expected only Term, not Ack/Nak")
	}
	if len(repo.calls) != 0 {
		t.Error("UpdateStatus should not be called for invalid JSON")
	}
}

func TestHandleCompleted_RepoError_Naks(t *testing.T) {
	// Ошибка БД → Nak, сообщение будет доставлено повторно.
	// Критично: не должен Ack при ошибке сохранения.
	repo := &mockStatusUpdater{
		errOnID: map[string]error{
			"tx-004": errors.New("database connection lost"),
		},
	}
	c := consumer.New(repo)

	event := events.PaymentCompleted{
		TransactionID: "tx-004",
		Status:        "captured",
		Provider:      "mock_provider",
	}

	msg := &mockMsg{
		subject: events.SubjectPaymentCompleted,
		data:    marshalEvent(t, event),
	}

	c.HandleCompleted(msg)

	if msg.acked {
		t.Error("CRITICAL: message Ack'd despite DB error — transaction status lost!")
	}
	if !msg.naked {
		t.Error("expected Nak on DB error for retry")
	}
}

// handleRiskBlocked

func TestHandleRiskBlocked_UpdatesToBlocked(t *testing.T) {
	// Заблокированная транзакция — статус должен стать blocked.
	// Критично: если не обновится — транзакция останется в processing вечно.
	repo := &mockStatusUpdater{}
	c := consumer.New(repo)

	event := events.PaymentRiskBlocked{
		TransactionID:  "tx-risk-001",
		RiskScore:      85,
		RiskDecision:   "blocked",
		TriggeredRules: []string{"high_amount", "night_time"},
	}

	msg := &mockMsg{
		subject: events.SubjectPaymentRiskBlocked,
		data:    marshalEvent(t, event),
	}

	c.HandleRiskBlocked(msg)

	call, ok := repo.lastCall()
	if !ok {
		t.Fatal("expected UpdateStatus to be called")
	}

	if call.ID != "tx-risk-001" {
		t.Errorf("ID = %q, want %q", call.ID, "tx-risk-001")
	}
	if call.Status != domain.StatusBlocked {
		t.Errorf("Status = %q, want blocked", call.Status)
	}
	// При блокировке нет провайдера и ошибки провайдера.
	if call.Provider != nil {
		t.Errorf("Provider should be nil for risk blocked, got %q", *call.Provider)
	}
	if call.ProviderTxID != nil {
		t.Errorf("ProviderTxID should be nil for risk blocked")
	}
	if !msg.acked {
		t.Error("expected Ack after successful block")
	}
}

func TestHandleRiskBlocked_InvalidJSON_Terms(t *testing.T) {
	repo := &mockStatusUpdater{}
	c := consumer.New(repo)

	msg := &mockMsg{
		subject: events.SubjectPaymentRiskBlocked,
		data:    []byte(`{"invalid`),
	}

	c.HandleRiskBlocked(msg)

	if !msg.termed {
		t.Error("expected Term for invalid JSON")
	}
	if len(repo.calls) != 0 {
		t.Error("UpdateStatus should not be called for invalid JSON")
	}
}

func TestHandleRiskBlocked_RepoError_Naks(t *testing.T) {
	repo := &mockStatusUpdater{
		errOnID: map[string]error{
			"tx-risk-002": errors.New("db unavailable"),
		},
	}
	c := consumer.New(repo)

	event := events.PaymentRiskBlocked{
		TransactionID: "tx-risk-002",
	}

	msg := &mockMsg{
		subject: events.SubjectPaymentRiskBlocked,
		data:    marshalEvent(t, event),
	}

	c.HandleRiskBlocked(msg)

	if msg.acked {
		t.Error("CRITICAL: Ack'd despite DB error")
	}
	if !msg.naked {
		t.Error("expected Nak on DB error")
	}
}

// handle (dispatch)

func TestHandle_UnknownSubject_Acks(t *testing.T) {
	// Неизвестный subject — Ack (не зависаем), логируем warn.
	repo := &mockStatusUpdater{}
	c := consumer.New(repo)

	msg := &mockMsg{
		subject: "payments.unknown.subject",
		data:    []byte(`{}`),
	}

	c.Handle(msg)

	if !msg.acked {
		t.Error("expected Ack for unknown subject (don't block queue)")
	}
	if len(repo.calls) != 0 {
		t.Error("UpdateStatus should not be called for unknown subject")
	}
}
