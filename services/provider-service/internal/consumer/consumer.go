package consumer

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"github.com/nats-io/nats.go/jetstream"

	"github.com/BuzzLyutic/payment-gateway-microservices/services/provider-service/internal/domain"
	"github.com/BuzzLyutic/payment-gateway-microservices/services/provider-service/internal/events"
	"github.com/BuzzLyutic/payment-gateway-microservices/services/provider-service/internal/metrics"
)

// PaymentProcessor — интерфейс обработки платежа. Реализует service.Service.
type PaymentProcessor interface {
	ProcessPayment(ctx context.Context, req *domain.ProcessRequest) (*domain.ProcessResult, error)
}

// EventPublisher — интерфейс публикации результата. Реализует publisher.Publisher.
type EventPublisher interface {
	PublishPaymentCompleted(ctx context.Context, event events.PaymentCompleted) error
}

// Consumer слушает payment.created, обрабатывает платёж, публикует payment.completed.
type Consumer struct {
	svc PaymentProcessor
	pub EventPublisher
}

func New(svc PaymentProcessor, pub EventPublisher) *Consumer {
	return &Consumer{svc: svc, pub: pub}
}

// Start запускает консьюмера. Блокирует до отмены контекста.
func (c *Consumer) Start(ctx context.Context, js jetstream.JetStream) error {
	cons, err := js.CreateOrUpdateConsumer(ctx, events.StreamName, jetstream.ConsumerConfig{
		Name:          "provider-processor",
		FilterSubject: events.SubjectPaymentRiskApproved,
		AckPolicy:     jetstream.AckExplicitPolicy,
	})
	if err != nil {
		return err
	}

	cc, err := cons.Consume(func(msg jetstream.Msg) {
		c.handle(msg)
	})
	if err != nil {
		return err
	}
	defer cc.Stop()

	<-ctx.Done()
	return nil
}

func (c *Consumer) handle(msg jetstream.Msg) {
	var event events.PaymentRiskApproved
	if err := json.Unmarshal(msg.Data(), &event); err != nil {
		slog.Error("failed to unmarshal payment.risk_approved", "error", err)
		msg.Term()
		metrics.NATSMessagesProcessed.WithLabelValues(msg.Subject(), "term").Inc()
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
    defer cancel()

	slog.Info("received payment.risk_approved",
		"transaction_id", event.TransactionID,
		"amount", event.Amount,
		"risk_score", event.RiskScore,
	)

	req := &domain.ProcessRequest{
		TransactionID: event.TransactionID,
		MerchantID:    event.MerchantID,
		Amount:        event.Amount,
		Currency:      event.Currency,
		PaymentMethod: event.PaymentMethod,
	}

	result, err := c.svc.ProcessPayment(ctx, req)
	if err != nil {
		slog.Error("failed to process payment",
			"transaction_id", event.TransactionID,
			"error", err,
		)
		msg.Nak()
		metrics.NATSMessagesProcessed.WithLabelValues(msg.Subject(), "nak").Inc()
		return
	}

	completed := events.PaymentCompleted{
		TransactionID: result.TransactionID,
		Status:        string(result.Status),
		Provider:      result.Provider,
		ProviderTxID:  result.ProviderTxID,
		ErrorMessage:  result.ErrorMessage,
		ProcessedAt:   time.Now(),
	}

	if err := c.pub.PublishPaymentCompleted(ctx, completed); err != nil {
		slog.Error("failed to publish payment.completed",
			"transaction_id", event.TransactionID,
			"error", err,
		)
		msg.Nak()
		metrics.NATSMessagesProcessed.WithLabelValues(msg.Subject(), "nak").Inc()
		return
	}

	slog.Info("payment.completed published",
		"transaction_id", result.TransactionID,
		"status", result.Status,
		"provider", result.Provider,
	)

	msg.Ack()
	metrics.NATSMessagesProcessed.WithLabelValues(msg.Subject(), "ack").Inc()
}
