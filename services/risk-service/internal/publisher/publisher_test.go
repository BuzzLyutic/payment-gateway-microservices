package publisher_test

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/BuzzLyutic/payment-gateway-microservices/services/risk-service/internal/domain"
	"github.com/BuzzLyutic/payment-gateway-microservices/services/risk-service/internal/events"
	"github.com/BuzzLyutic/payment-gateway-microservices/services/risk-service/internal/publisher"
	"github.com/nats-io/nats.go/jetstream"
)

type mockJS struct {
	err             error
	capturedSubject string
	capturedData    []byte
	callCount       int
}

func (m *mockJS) Publish(
	_ context.Context,
	subject string,
	data []byte,
	_ ...jetstream.PublishOpt,
) (*jetstream.PubAck, error) {
	m.callCount++
	m.capturedSubject = subject
	m.capturedData = data
	if m.err != nil {
		return nil, m.err
	}
	return &jetstream.PubAck{}, nil
}

func newTestPublisher(js publisher.JetStreamPublisher) *publisher.Publisher {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelError,
	}))
	return publisher.NewWithJS(js, logger)
}

// Тесты Publish

// TestPublish_Approved — решение approved публикует в SubjectPaymentRiskApproved.
func TestPublish_Approved(t *testing.T) {
	js := &mockJS{}
	p := newTestPublisher(js)

	event := makeEvent("tx-approved")
	result := domain.EvaluationResult{
		TransactionID:  "tx-approved",
		Decision:       domain.DecisionApproved,
		TotalScore:     20,
		TriggeredRules: []string{"low_amount"},
	}

	if err := p.Publish(context.Background(), event, result); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if js.capturedSubject != events.SubjectPaymentRiskApproved {
		t.Errorf("wrong subject: got %q, want %q",
			js.capturedSubject, events.SubjectPaymentRiskApproved)
	}

	// Проверяем payload.
	var payload events.PaymentRiskApproved
	if err := json.Unmarshal(js.capturedData, &payload); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if payload.TransactionID != "tx-approved" {
		t.Errorf("wrong transaction_id: %q", payload.TransactionID)
	}
	if payload.RiskScore != 20 {
		t.Errorf("wrong risk_score: %d", payload.RiskScore)
	}
	if payload.MerchantID != event.MerchantID {
		t.Errorf("wrong merchant_id: %q", payload.MerchantID)
	}
}

// TestPublish_Review — решение review тоже идёт в approved subject.
func TestPublish_Review(t *testing.T) {
	js := &mockJS{}
	p := newTestPublisher(js)

	result := domain.EvaluationResult{
		TransactionID: "tx-review",
		Decision:      domain.DecisionReview,
		TotalScore:    50,
	}

	if err := p.Publish(context.Background(), makeEvent("tx-review"), result); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if js.capturedSubject != events.SubjectPaymentRiskApproved {
		t.Errorf("review should publish to approved subject, got %q", js.capturedSubject)
	}
}

// TestPublish_Blocked — решение blocked публикует в SubjectPaymentRiskBlocked.
func TestPublish_Blocked(t *testing.T) {
	js := &mockJS{}
	p := newTestPublisher(js)

	result := domain.EvaluationResult{
		TransactionID:  "tx-blocked",
		Decision:       domain.DecisionBlocked,
		TotalScore:     90,
		TriggeredRules: []string{"high_amount", "velocity_card"},
	}

	if err := p.Publish(context.Background(), makeEvent("tx-blocked"), result); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if js.capturedSubject != events.SubjectPaymentRiskBlocked {
		t.Errorf("wrong subject: got %q, want %q",
			js.capturedSubject, events.SubjectPaymentRiskBlocked)
	}

	var payload events.PaymentRiskBlocked
	if err := json.Unmarshal(js.capturedData, &payload); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if payload.TransactionID != "tx-blocked" {
		t.Errorf("wrong transaction_id: %q", payload.TransactionID)
	}
	if payload.RiskScore != 90 {
		t.Errorf("wrong risk_score: %d", payload.RiskScore)
	}
	if len(payload.TriggeredRules) != 2 {
		t.Errorf("expected 2 rules, got %d", len(payload.TriggeredRules))
	}
}

// TestPublish_UnknownDecision — неизвестное решение возвращает ошибку.
func TestPublish_UnknownDecision(t *testing.T) {
	js := &mockJS{}
	p := newTestPublisher(js)

	result := domain.EvaluationResult{
		TransactionID: "tx-unknown",
		Decision:      domain.Decision("unknown"),
	}

	err := p.Publish(context.Background(), makeEvent("tx-unknown"), result)
	if err == nil {
		t.Fatal("expected error for unknown decision")
	}
	if js.callCount != 0 {
		t.Error("Publish to NATS must not be called for unknown decision")
	}
}

// TestPublish_NATSError — ошибка NATS пробрасывается наверх.
func TestPublish_NATSError(t *testing.T) {
	js := &mockJS{err: errors.New("nats: connection refused")}
	p := newTestPublisher(js)

	result := domain.EvaluationResult{
		TransactionID: "tx-nats-err",
		Decision:      domain.DecisionApproved,
	}

	err := p.Publish(context.Background(), makeEvent("tx-nats-err"), result)
	if err == nil {
		t.Fatal("expected error when NATS fails")
	}
}

// TestPublish_BlockedPayload — проверяем поля blocked payload.
func TestPublish_BlockedPayload(t *testing.T) {
	js := &mockJS{}
	p := newTestPublisher(js)

	result := domain.EvaluationResult{
		TransactionID:  "tx-payload-check",
		Decision:       domain.DecisionBlocked,
		TotalScore:     75,
		TriggeredRules: []string{"rule1"},
	}

	_ = p.Publish(context.Background(), makeEvent("tx-payload-check"), result)

	var payload events.PaymentRiskBlocked
	_ = json.Unmarshal(js.capturedData, &payload)

	if payload.RiskDecision != string(domain.DecisionBlocked) {
		t.Errorf("wrong risk_decision: %q", payload.RiskDecision)
	}
	if payload.EvaluatedAt.IsZero() {
		t.Error("evaluated_at must be set")
	}
}

func makeEvent(txID string) events.PaymentCreated {
	return events.PaymentCreated{
		TransactionID: txID,
		MerchantID:    "merch-1",
		Amount:        2500,
		Currency:      "USD",
		PaymentMethod: "card",
		CreatedAt:     time.Now(),
	}
}
