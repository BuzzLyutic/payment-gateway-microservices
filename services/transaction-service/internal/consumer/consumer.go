package consumer

import (
	"context"
	"encoding/json"
	"log/slog"

	"github.com/nats-io/nats.go/jetstream"

	"github.com/BuzzLyutic/payment-gateway-microservices/services/transaction-service/internal/domain"
	"github.com/BuzzLyutic/payment-gateway-microservices/services/transaction-service/internal/events"
)

// StatusUpdater — интерфейс для обновления статуса транзакции.
// Реализует repository.
type StatusUpdater interface {
	UpdateStatus(ctx context.Context, id string, status domain.Status, provider *string, providerTxID *string, errorMessage *string) error
}

// Consumer слушает payment.completed и обновляет статус транзакции.
type Consumer struct {
	repo StatusUpdater
}

func New(repo StatusUpdater) *Consumer {
	return &Consumer{repo: repo}
}

// Start запускает консьюмера. Блокирует до отмены контекста.
func (c *Consumer) Start(ctx context.Context, js jetstream.JetStream) error {
	cons, err := js.CreateOrUpdateConsumer(ctx, events.StreamName, jetstream.ConsumerConfig{
		Name: "transaction-updater",
		FilterSubjects: []string{
			events.SubjectPaymentCompleted,
			events.SubjectPaymentRiskBlocked,
		},
		AckPolicy: jetstream.AckExplicitPolicy,
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
	switch msg.Subject() {
	case events.SubjectPaymentCompleted:
		c.handleCompleted(msg)
	case events.SubjectPaymentRiskBlocked:
		c.handleRiskBlocked(msg)
	default:
		slog.Warn("unexpected subject", "subject", msg.Subject())
		msg.Ack()
	}
}

func (c *Consumer) handleCompleted(msg jetstream.Msg) {
	var event events.PaymentCompleted
	if err := json.Unmarshal(msg.Data(), &event); err != nil {
		slog.Error("failed to unmarshal payment.completed", "error", err)
		msg.Term()
		return
	}

	slog.Info("received payment.completed",
		"transaction_id", event.TransactionID,
		"status", event.Status,
		"provider", event.Provider,
	)

	status := mapCompletedStatus(event.Status)

	var providerTxID *string
	if event.ProviderTxID != "" {
		providerTxID = &event.ProviderTxID
	}

	var errorMessage *string
	if event.ErrorMessage != "" {
		errorMessage = &event.ErrorMessage
	}

	provider := event.Provider

	ctx := context.Background()
	if err := c.repo.UpdateStatus(ctx, event.TransactionID, status, &provider, providerTxID, errorMessage); err != nil {
		slog.Error("failed to update transaction status",
			"transaction_id", event.TransactionID,
			"error", err,
		)
		msg.Nak()
		return
	}

	msg.Ack()
}

func (c *Consumer) handleRiskBlocked(msg jetstream.Msg) {
	var event events.PaymentRiskBlocked
	if err := json.Unmarshal(msg.Data(), &event); err != nil {
		slog.Error("failed to unmarshal payment.risk_blocked", "error", err)
		msg.Term()
		return
	}

	slog.Info("received payment.risk_blocked",
		"transaction_id", event.TransactionID,
		"score", event.RiskScore,
		"triggered_rules", event.TriggeredRules,
	)

	ctx := context.Background()
	if err := c.repo.UpdateStatus(ctx, event.TransactionID, domain.StatusBlocked, nil, nil, nil); err != nil {
		slog.Error("failed to update transaction status to blocked",
			"transaction_id", event.TransactionID,
			"error", err,
		)
		msg.Nak()
		return
	}

	msg.Ack()
}

func mapCompletedStatus(s string) domain.Status {
	switch s {
	case "captured":
		return domain.StatusCaptured
	case "declined":
		return domain.StatusDeclined
	default:
		return domain.StatusFailed
	}
}
