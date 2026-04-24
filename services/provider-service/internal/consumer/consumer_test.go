package consumer_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/nats-io/nats.go/jetstream"
	nats "github.com/nats-io/nats.go"

	"github.com/BuzzLyutic/payment-gateway-microservices/services/provider-service/internal/consumer"
	"github.com/BuzzLyutic/payment-gateway-microservices/services/provider-service/internal/domain"
	"github.com/BuzzLyutic/payment-gateway-microservices/services/provider-service/internal/events"
)

// Моки

type mockProcessor struct {
	result  *domain.ProcessResult
	err     error
	called  bool
	lastReq *domain.ProcessRequest
}

func (m *mockProcessor) ProcessPayment(
	_ context.Context,
	req *domain.ProcessRequest,
) (*domain.ProcessResult, error) {
	m.called = true
	m.lastReq = req
	if m.result != nil {
		m.result.TransactionID = req.TransactionID
	}
	return m.result, m.err
}

type mockPublisher struct {
	published []events.PaymentCompleted
	err       error
	called    bool
}

func (m *mockPublisher) PublishPaymentCompleted(
	_ context.Context,
	event events.PaymentCompleted,
) error {
	m.called = true
	m.published = append(m.published, event)
	return m.err
}

// mockMsg реализует jetstream.Msg без реального NATS.
type mockMsg struct {
	data       []byte
	ackCalled  bool
	nakCalled  bool
	termCalled bool
	subject    string
}

func (m *mockMsg) Data() []byte { return m.data }

func (m *mockMsg) Ack() error {
	m.ackCalled = true
	return nil
}

func (m *mockMsg) Nak() error {
	m.nakCalled = true
	return nil
}

func (m *mockMsg) NakWithDelay(_ time.Duration) error {
	m.nakCalled = true
	return nil
}

func (m *mockMsg) Term() error {
	m.termCalled = true
	return nil
}

func (m *mockMsg) TermWithReason(_ string) error {
	m.termCalled = true
	return nil
}

func (m *mockMsg) Metadata() (*jetstream.MsgMetadata, error) {
	return &jetstream.MsgMetadata{}, nil
}

func (m *mockMsg) Subject() string                   { return m.subject }
func (m *mockMsg) Reply() string                     { return "" }
func (m *mockMsg) Headers() nats.Header              { return nil }
func (m *mockMsg) InProgress() error                 { return nil }
func (m *mockMsg) DoubleAck(_ context.Context) error { return nil }

// Хелперы

func makeRiskApprovedBytes(t *testing.T, txID string) []byte {
	t.Helper()
	data, err := json.Marshal(events.PaymentRiskApproved{
		TransactionID: txID,
		MerchantID:    "merch-1",
		Amount:        5000,
		Currency:      "USD",
		PaymentMethod: "card",
		RiskScore:     20,
		EvaluatedAt:   time.Now(),
	})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return data
}

func makeSuccessResult(txID string) *domain.ProcessResult {
	return &domain.ProcessResult{
		TransactionID: txID,
		Provider:      "mock_provider_a",
		ProviderTxID:  "prov-tx-001",
		Status:        domain.ResultCaptured,
		LatencyMs:     120,
	}
}

// Тесты New

func TestNew_NotNil(t *testing.T) {
	c := consumer.New(&mockProcessor{}, &mockPublisher{})
	if c == nil {
		t.Error("New() returned nil")
	}
}

// Тесты handle() через ExportHandle

// TestHandle_HappyPath — успешная обработка → Ack.
func TestHandle_HappyPath(t *testing.T) {
	proc := &mockProcessor{result: makeSuccessResult("tx-001")}
	pub := &mockPublisher{}
	c := consumer.New(proc, pub)

	msg := &mockMsg{
		data:    makeRiskApprovedBytes(t, "tx-001"),
		subject: events.SubjectPaymentRiskApproved,
	}
	c.ExportHandle(msg)

	if !proc.called {
		t.Error("ProcessPayment must be called")
	}
	if !pub.called {
		t.Error("PublishPaymentCompleted must be called")
	}
	if !msg.ackCalled {
		t.Error("Ack must be called on success")
	}
	if msg.nakCalled {
		t.Error("Nak must not be called on success")
	}
}

// TestHandle_InvalidJSON — невалидный JSON → Term, не Nak.
func TestHandle_InvalidJSON(t *testing.T) {
	proc := &mockProcessor{}
	pub := &mockPublisher{}
	c := consumer.New(proc, pub)

	msg := &mockMsg{
		data:    []byte("not-json{"),
		subject: events.SubjectPaymentRiskApproved,
	}
	c.ExportHandle(msg)

	if proc.called {
		t.Error("ProcessPayment must not be called for invalid JSON")
	}
	if pub.called {
		t.Error("Publish must not be called for invalid JSON")
	}
	if !msg.termCalled {
		t.Error("Term must be called for invalid JSON")
	}
	if msg.ackCalled {
		t.Error("Ack must not be called for invalid JSON")
	}
}

// TestHandle_ProcessError — ошибка ProcessPayment → Nak.
func TestHandle_ProcessError(t *testing.T) {
	proc := &mockProcessor{err: errors.New("provider timeout")}
	pub := &mockPublisher{}
	c := consumer.New(proc, pub)

	msg := &mockMsg{
		data:    makeRiskApprovedBytes(t, "tx-err"),
		subject: events.SubjectPaymentRiskApproved,
	}
	c.ExportHandle(msg)

	if !proc.called {
		t.Error("ProcessPayment must be called")
	}
	if pub.called {
		t.Error("Publish must not be called when process fails")
	}
	if !msg.nakCalled {
		t.Error("Nak must be called when process fails")
	}
	if msg.ackCalled {
		t.Error("Ack must not be called when process fails")
	}
}

// TestHandle_PublishError — ошибка публикации → Nak.
func TestHandle_PublishError(t *testing.T) {
	proc := &mockProcessor{result: makeSuccessResult("tx-pub-err")}
	pub := &mockPublisher{err: errors.New("nats: unavailable")}
	c := consumer.New(proc, pub)

	msg := &mockMsg{
		data:    makeRiskApprovedBytes(t, "tx-pub-err"),
		subject: events.SubjectPaymentRiskApproved,
	}
	c.ExportHandle(msg)

	if !proc.called {
		t.Error("ProcessPayment must be called")
	}
	if !pub.called {
		t.Error("Publish must be attempted")
	}
	if msg.ackCalled {
		t.Error("Ack must not be called when publish fails")
	}
	if !msg.nakCalled {
		t.Error("Nak must be called when publish fails")
	}
}

// TestHandle_RequestMapping — поля события маппятся в ProcessRequest корректно.
func TestHandle_RequestMapping(t *testing.T) {
	proc := &mockProcessor{result: makeSuccessResult("tx-map")}
	c := consumer.New(proc, &mockPublisher{})

	event := events.PaymentRiskApproved{
		TransactionID: "tx-map",
		MerchantID:    "merch-99",
		Amount:        9999,
		Currency:      "EUR",
		PaymentMethod: "sbp",
	}
	data, _ := json.Marshal(event)
	c.ExportHandle(&mockMsg{data: data, subject: events.SubjectPaymentRiskApproved})

	if proc.lastReq == nil {
		t.Fatal("ProcessRequest must not be nil")
	}
	if proc.lastReq.TransactionID != "tx-map" {
		t.Errorf("wrong transaction_id: %q", proc.lastReq.TransactionID)
	}
	if proc.lastReq.MerchantID != "merch-99" {
		t.Errorf("wrong merchant_id: %q", proc.lastReq.MerchantID)
	}
	if proc.lastReq.Amount != 9999 {
		t.Errorf("wrong amount: %d", proc.lastReq.Amount)
	}
	if proc.lastReq.Currency != "EUR" {
		t.Errorf("wrong currency: %q", proc.lastReq.Currency)
	}
	if proc.lastReq.PaymentMethod != "sbp" {
		t.Errorf("wrong payment_method: %q", proc.lastReq.PaymentMethod)
	}
}

// TestHandle_CompletedEventMapping — ProcessResult корректно маппится в PaymentCompleted.
func TestHandle_CompletedEventMapping(t *testing.T) {
	proc := &mockProcessor{
		result: &domain.ProcessResult{
			Provider:     "mock_provider_b",
			ProviderTxID: "prov-tx-999",
			Status:       domain.ResultDeclined,
			ErrorMessage: "insufficient funds",
			LatencyMs:    85,
		},
	}
	pub := &mockPublisher{}
	c := consumer.New(proc, pub)

	c.ExportHandle(&mockMsg{
		data:    makeRiskApprovedBytes(t, "tx-completed"),
		subject: events.SubjectPaymentRiskApproved,
	})

	if len(pub.published) != 1 {
		t.Fatalf("expected 1 published event, got %d", len(pub.published))
	}

	got := pub.published[0]
	if got.Status != string(domain.ResultDeclined) {
		t.Errorf("wrong status: %q", got.Status)
	}
	if got.Provider != "mock_provider_b" {
		t.Errorf("wrong provider: %q", got.Provider)
	}
	if got.ErrorMessage != "insufficient funds" {
		t.Errorf("wrong error_message: %q", got.ErrorMessage)
	}
	if got.ProcessedAt.IsZero() {
		t.Error("processed_at must be set")
	}
}
