// provider-service/internal/publisher/publisher_test.go
package publisher_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/nats-io/nats.go/jetstream"

	"github.com/BuzzLyutic/payment-gateway-microservices/services/provider-service/internal/events"
	"github.com/BuzzLyutic/payment-gateway-microservices/services/provider-service/internal/publisher"
)

// Мок

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

// Хелпер

func makeCompletedEvent(txID, status string) events.PaymentCompleted {
	return events.PaymentCompleted{
		TransactionID: txID,
		Status:        status,
		Provider:      "mock_provider_a",
		ProviderTxID:  "prov-tx-001",
		ErrorMessage:  "",
		ProcessedAt:   time.Now(),
	}
}

// Тесты

// TestPublishPaymentCompleted_Success — успешная публикация в правильный subject.
func TestPublishPaymentCompleted_Success(t *testing.T) {
	js := &mockJS{}
	p := publisher.NewWithJS(js)

	event := makeCompletedEvent("tx-001", "captured")

	if err := p.PublishPaymentCompleted(context.Background(), event); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if js.callCount != 1 {
		t.Errorf("expected 1 Publish call, got %d", js.callCount)
	}
	if js.capturedSubject != events.SubjectPaymentCompleted {
		t.Errorf("wrong subject: got %q, want %q",
			js.capturedSubject, events.SubjectPaymentCompleted)
	}
}

// TestPublishPaymentCompleted_PayloadCorrect — проверяем корректность payload.
func TestPublishPaymentCompleted_PayloadCorrect(t *testing.T) {
	js := &mockJS{}
	p := publisher.NewWithJS(js)

	event := makeCompletedEvent("tx-payload", "captured")

	if err := p.PublishPaymentCompleted(context.Background(), event); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var decoded events.PaymentCompleted
	if err := json.Unmarshal(js.capturedData, &decoded); err != nil {
		t.Fatalf("unmarshal captured data: %v", err)
	}

	if decoded.TransactionID != "tx-payload" {
		t.Errorf("wrong transaction_id: %q", decoded.TransactionID)
	}
	if decoded.Status != "captured" {
		t.Errorf("wrong status: %q", decoded.Status)
	}
	if decoded.Provider != "mock_provider_a" {
		t.Errorf("wrong provider: %q", decoded.Provider)
	}
	if decoded.ProcessedAt.IsZero() {
		t.Error("processed_at must not be zero")
	}
}

// TestPublishPaymentCompleted_DeclinedStatus — declined статус тоже публикуется.
func TestPublishPaymentCompleted_DeclinedStatus(t *testing.T) {
	js := &mockJS{}
	p := publisher.NewWithJS(js)

	event := makeCompletedEvent("tx-declined", "declined")
	event.ErrorMessage = "card declined"

	if err := p.PublishPaymentCompleted(context.Background(), event); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var decoded events.PaymentCompleted
	_ = json.Unmarshal(js.capturedData, &decoded)

	if decoded.Status != "declined" {
		t.Errorf("expected declined, got %q", decoded.Status)
	}
	if decoded.ErrorMessage != "card declined" {
		t.Errorf("wrong error_message: %q", decoded.ErrorMessage)
	}
}

// TestPublishPaymentCompleted_NATSError — ошибка NATS пробрасывается наверх.
func TestPublishPaymentCompleted_NATSError(t *testing.T) {
	js := &mockJS{err: errors.New("nats: connection refused")}
	p := publisher.NewWithJS(js)

	err := p.PublishPaymentCompleted(context.Background(), makeCompletedEvent("tx-err", "failed"))
	if err == nil {
		t.Fatal("expected error when NATS fails")
	}
}

// TestPublishPaymentCompleted_ContextCanceled — отменённый контекст.
func TestPublishPaymentCompleted_ContextCanceled(t *testing.T) {
	js := &mockJS{err: context.Canceled}
	p := publisher.NewWithJS(js)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // отменяем сразу

	err := p.PublishPaymentCompleted(ctx, makeCompletedEvent("tx-cancel", "failed"))
	if err == nil {
		t.Fatal("expected error for canceled context")
	}
}
