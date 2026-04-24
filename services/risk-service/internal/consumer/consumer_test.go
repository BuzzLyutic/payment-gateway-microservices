package consumer_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/BuzzLyutic/payment-gateway-microservices/services/risk-service/internal/consumer"
	"github.com/BuzzLyutic/payment-gateway-microservices/services/risk-service/internal/domain"
	"github.com/BuzzLyutic/payment-gateway-microservices/services/risk-service/internal/events"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
)

// Моки

type mockEvaluator struct {
	result domain.EvaluationResult
}

func (m *mockEvaluator) Evaluate(_ context.Context, event events.PaymentCreated) domain.EvaluationResult {
	res := m.result
	res.TransactionID = event.TransactionID
	return res
}

type mockPublisher struct {
	called     bool
	err        error
	lastEvent  events.PaymentCreated
	lastResult domain.EvaluationResult
}

func (m *mockPublisher) Publish(
	_ context.Context,
	event events.PaymentCreated,
	result domain.EvaluationResult,
) error {
	m.called = true
	m.lastEvent = event
	m.lastResult = result
	return m.err
}

// mockMsg реализует jetstream.Msg без реального NATS.
type mockMsg struct {
	data       []byte
	ackCalled  bool
	nakCalled  bool
	termCalled bool
	ackErr     error
	nakErr     error
	termErr    error
}

func (m *mockMsg) Data() []byte { return m.data }

func (m *mockMsg) Ack() error {
	m.ackCalled = true
	return m.ackErr
}

func (m *mockMsg) Nak() error {
	m.nakCalled = true
	return nil
}

func (m *mockMsg) NakWithDelay(_ time.Duration) error {
	m.nakCalled = true
	return m.nakErr
}

func (m *mockMsg) Term() error {
	m.termCalled = true
	return m.termErr
}

func (m *mockMsg) TermWithReason(_ string) error {
	m.termCalled = true
	return m.termErr
}

func (m *mockMsg) Metadata() (*jetstream.MsgMetadata, error) {
	return &jetstream.MsgMetadata{}, nil
}

func (m *mockMsg) Subject() string                   { return "payment.created" }
func (m *mockMsg) Reply() string                     { return "" }
func (m *mockMsg) Headers() nats.Header              { return nil }
func (m *mockMsg) InProgress() error                 { return nil }
func (m *mockMsg) DoubleAck(_ context.Context) error { return nil }

// Хелпер

func newTestConsumer(t *testing.T, eval consumer.Evaluator, pub consumer.Publisher) *consumer.Consumer {
	t.Helper()
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelError,
	}))
	return consumer.New(nil, eval, pub, logger)
}

func makeEventBytes(t *testing.T, txID string) []byte {
	t.Helper()
	data, err := json.Marshal(events.PaymentCreated{
		TransactionID: txID,
		MerchantID:    "merchant-42",
		Amount:        5000,
		Currency:      "USD",
		PaymentMethod: "card",
		CreatedAt:     time.Now(),
	})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return data
}

// Тесты handleMessage

// TestHandleMessage_HappyPath — корректное сообщение, publish успешен → Ack.
func TestHandleMessage_HappyPath(t *testing.T) {
	eval := &mockEvaluator{
		result: domain.EvaluationResult{
			Decision:   domain.DecisionApproved,
			TotalScore: 10,
		},
	}
	pub := &mockPublisher{}
	c := newTestConsumer(t, eval, pub)

	msg := &mockMsg{data: makeEventBytes(t, "tx-001")}
	c.ExportHandleMessage(context.Background(), msg)

	if !pub.called {
		t.Error("expected Publish to be called")
	}
	if pub.lastResult.TransactionID != "tx-001" {
		t.Errorf("wrong transaction_id: got %q", pub.lastResult.TransactionID)
	}
	if !msg.ackCalled {
		t.Error("expected Ack to be called after successful publish")
	}
	if msg.nakCalled {
		t.Error("Nak should not be called on success")
	}
}

// TestHandleMessage_InvalidJSON — невалидный JSON → Term, не Nak.
func TestHandleMessage_InvalidJSON(t *testing.T) {
	pub := &mockPublisher{}
	c := newTestConsumer(t, &mockEvaluator{}, pub)

	msg := &mockMsg{data: []byte("not-json{")}
	c.ExportHandleMessage(context.Background(), msg)

	if pub.called {
		t.Error("Publish must not be called for invalid JSON")
	}
	if !msg.termCalled {
		t.Error("Term must be called for invalid JSON (no retry)")
	}
	if msg.ackCalled {
		t.Error("Ack must not be called for invalid JSON")
	}
}

// TestHandleMessage_PublishError — publish возвращает ошибку → Nak, не Ack.
func TestHandleMessage_PublishError(t *testing.T) {
	eval := &mockEvaluator{
		result: domain.EvaluationResult{
			Decision:   domain.DecisionBlocked,
			TotalScore: 90,
		},
	}
	pub := &mockPublisher{err: errors.New("nats unavailable")}
	c := newTestConsumer(t, eval, pub)

	msg := &mockMsg{data: makeEventBytes(t, "tx-002")}
	c.ExportHandleMessage(context.Background(), msg)

	if !pub.called {
		t.Error("Publish must be called")
	}
	if msg.ackCalled {
		t.Error("Ack must not be called when publish fails")
	}
	if !msg.nakCalled {
		t.Error("Nak must be called when publish fails")
	}
}

// TestHandleMessage_AckError — Ack возвращает ошибку → только логируем, не паникуем.
func TestHandleMessage_AckError(t *testing.T) {
	c := newTestConsumer(t, &mockEvaluator{}, &mockPublisher{})

	msg := &mockMsg{
		data:   makeEventBytes(t, "tx-003"),
		ackErr: errors.New("ack failed"),
	}
	// Не должно паниковать.
	c.ExportHandleMessage(context.Background(), msg)

	if !msg.ackCalled {
		t.Error("Ack must be attempted")
	}
}

// TestHandleMessage_DecisionBlocked — проверяем, что blocked-решение
// передаётся в Publisher корректно.
func TestHandleMessage_DecisionBlocked(t *testing.T) {
	eval := &mockEvaluator{
		result: domain.EvaluationResult{
			Decision:       domain.DecisionBlocked,
			TotalScore:     80,
			TriggeredRules: []string{"high_amount", "velocity_card"},
		},
	}
	pub := &mockPublisher{}
	c := newTestConsumer(t, eval, pub)

	msg := &mockMsg{data: makeEventBytes(t, "tx-blocked")}
	c.ExportHandleMessage(context.Background(), msg)

	if pub.lastResult.Decision != domain.DecisionBlocked {
		t.Errorf("expected blocked, got %q", pub.lastResult.Decision)
	}
	if len(pub.lastResult.TriggeredRules) != 2 {
		t.Errorf("expected 2 triggered rules, got %d", len(pub.lastResult.TriggeredRules))
	}
	if !msg.ackCalled {
		t.Error("Ack must be called even for blocked decisions")
	}
}

// TestHandleMessage_TermError — Term тоже может вернуть ошибку, не паникуем.
func TestHandleMessage_TermError(t *testing.T) {
	c := newTestConsumer(t, &mockEvaluator{}, &mockPublisher{})

	msg := &mockMsg{
		data:    []byte("bad json"),
		termErr: errors.New("term failed"),
	}
	// Не должно паниковать.
	c.ExportHandleMessage(context.Background(), msg)

	if !msg.termCalled {
		t.Error("Term must be attempted for invalid JSON")
	}
}

// Тесты isContextError

func TestIsContextError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{
			name: "context canceled",
			err:  context.Canceled,
			want: true,
		},
		{
			name: "context deadline exceeded",
			err:  context.DeadlineExceeded,
			want: true,
		},
		{
			name: "wrapped context canceled",
			err:  fmt.Errorf("outer: %w", context.Canceled),
			want: true,
		},
		{
			name: "other error",
			err:  errors.New("some error"),
			want: false,
		},
		{
			name: "nil error",
			err:  nil,
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := consumer.ExportIsContextError(tt.err)
			if got != tt.want {
				t.Errorf("isContextError(%v) = %v, want %v", tt.err, got, tt.want)
			}
		})
	}
}
