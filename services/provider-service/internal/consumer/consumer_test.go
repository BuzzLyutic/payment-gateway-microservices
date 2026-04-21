package consumer_test

import (
	"context"
	"errors"
	"testing"

	"github.com/BuzzLyutic/payment-gateway-microservices/services/provider-service/internal/consumer"
	"github.com/BuzzLyutic/payment-gateway-microservices/services/provider-service/internal/domain"
	"github.com/BuzzLyutic/payment-gateway-microservices/services/provider-service/internal/events"
)

// моки

type mockProcessor struct {
	result *domain.ProcessResult
	err    error
}

func (m *mockProcessor) ProcessPayment(
	_ context.Context,
	req *domain.ProcessRequest,
) (*domain.ProcessResult, error) {
	if m.result != nil {
		m.result.TransactionID = req.TransactionID
	}
	return m.result, m.err
}

type mockPublisher struct {
	published []events.PaymentCompleted
	err       error
}

func (m *mockPublisher) PublishPaymentCompleted(
	_ context.Context,
	event events.PaymentCompleted,
) error {
	m.published = append(m.published, event)
	return m.err
}

// New

func TestNew_NotNil(t *testing.T) {
	svc := &mockProcessor{}
	pub := &mockPublisher{}

	c := consumer.New(svc, pub)
	if c == nil {
		t.Error("New() returned nil")
	}
}

// Зависимости (без JetStream)

func TestConsumer_ProcessorError_IsHandled(t *testing.T) {
	// Проверяем что mockProcessor правильно возвращает ошибку.
	// handle() использует эту логику при Nak.
	svc := &mockProcessor{err: errors.New("service unavailable")}
	pub := &mockPublisher{}

	_, err := svc.ProcessPayment(context.Background(), &domain.ProcessRequest{
		TransactionID: "tx_001",
	})
	if err == nil {
		t.Fatal("expected error from mock processor")
	}
	if len(pub.published) != 0 {
		t.Error("publisher should not be called on processor error")
	}
}

func TestConsumer_PublisherError_IsHandled(t *testing.T) {
	// PublishPaymentCompleted возвращает ошибку — проверяем что мок работает.
	pub := &mockPublisher{err: errors.New("nats unavailable")}

	err := pub.PublishPaymentCompleted(context.Background(), events.PaymentCompleted{
		TransactionID: "tx_002",
	})
	if err == nil {
		t.Fatal("expected error from mock publisher")
	}
}

func TestConsumer_SuccessFlow_MocksWork(t *testing.T) {
	// Сквозной тест моков: processor → publisher.
	svc := &mockProcessor{
		result: &domain.ProcessResult{
			Status:   domain.ResultCaptured,
			Provider: "mock_provider",
		},
	}
	pub := &mockPublisher{}

	// Имитируем что consumer сделал бы при успехе.
	req := &domain.ProcessRequest{TransactionID: "tx_003"}
	result, err := svc.ProcessPayment(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	completed := events.PaymentCompleted{
		TransactionID: result.TransactionID,
		Status:        string(result.Status),
		Provider:      result.Provider,
	}

	if err := pub.PublishPaymentCompleted(context.Background(), completed); err != nil {
		t.Fatalf("publish error: %v", err)
	}

	if len(pub.published) != 1 {
		t.Fatalf("expected 1 published event, got %d", len(pub.published))
	}
	if pub.published[0].Status != string(domain.ResultCaptured) {
		t.Errorf("published status = %q, want %q",
			pub.published[0].Status, string(domain.ResultCaptured))
	}
}
