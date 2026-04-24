package consumer

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/nats-io/nats.go/jetstream"

	"github.com/BuzzLyutic/payment-gateway-microservices/services/transaction-service/internal/domain"
	"github.com/BuzzLyutic/payment-gateway-microservices/services/transaction-service/internal/events"
	"github.com/BuzzLyutic/payment-gateway-microservices/services/transaction-service/internal/webhook"
)

// StatusUpdater — интерфейс для обновления статуса транзакции.
// Реализует repository.
type StatusUpdater interface {
	UpdateStatus(ctx context.Context, id string, status domain.Status, provider *string, providerTxID *string, errorMessage *string) error
}

// transactionalUpdater — внутренний интерфейс для атомарного обновления.
// Реализуется реальным репозиторием; в тестах подменяется моком.
type transactionalUpdater interface {
	UpdateStatusInTx(ctx context.Context, tx pgx.Tx, id string, status domain.Status, provider *string, providerTxID *string, errorMessage *string) error
	GetMerchantIDByTxID(ctx context.Context, tx pgx.Tx, transactionID string) (string, error)
}

// Consumer слушает payment.completed и обновляет статус транзакции.
type Consumer struct {
	// simpleUpdater используется в тестах (старый интерфейс).
	simpleUpdater StatusUpdater
	// txUpdater используется в production для атомарного обновления.
	txUpdater   transactionalUpdater
	webhookRepo *webhook.Repository
	pool        *pgxpool.Pool
}

// New создаёт consumer для тестов — только простое обновление статуса.
func New(repo StatusUpdater) *Consumer {
	return &Consumer{simpleUpdater: repo}
}

// NewWithWebhook создаёт production consumer с поддержкой webhook.
func NewWithWebhook(
	txUpdater transactionalUpdater,
	webhookRepo *webhook.Repository,
	pool *pgxpool.Pool,
) *Consumer {
	return &Consumer{
		txUpdater:   txUpdater,
		webhookRepo: webhookRepo,
		pool:        pool,
	}
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

// Handle — публичный для тестов через export_test.go.
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

	if err := c.doUpdate(ctx, event.TransactionID, status, &provider, providerTxID, errorMessage); err != nil {
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
	)

	ctx := context.Background()
	if err := c.doUpdate(ctx, event.TransactionID, domain.StatusBlocked, nil, nil, nil); err != nil {
		slog.Error("failed to update transaction status to blocked",
			"transaction_id", event.TransactionID,
			"error", err,
		)
		msg.Nak()
		return
	}

	msg.Ack()
}

// doUpdate выбирает стратегию обновления:
// - в тестах: простое UpdateStatus через simpleUpdater
// - в production: атомарное обновление + webhook через txUpdater
func (c *Consumer) doUpdate(
	ctx context.Context,
	transactionID string,
	status domain.Status,
	provider *string,
	providerTxID *string,
	errorMessage *string,
) error {
	// Режим тестирования — простое обновление без webhook
	if c.simpleUpdater != nil {
		return c.simpleUpdater.UpdateStatus(ctx, transactionID, status, provider, providerTxID, errorMessage)
	}

	// Production режим — атомарное обновление с webhook
	return c.updateStatusWithWebhook(ctx, transactionID, status, provider, providerTxID, errorMessage)
}

// updateStatusWithWebhook атомарно обновляет статус транзакции
// и создаёт запись в webhook_deliveries в одной транзакции БД.
func (c *Consumer) updateStatusWithWebhook(
	ctx context.Context,
	transactionID string,
	status domain.Status,
	provider *string,
	providerTxID *string,
	errorMessage *string,
) error {
	// Начинаем транзакцию БД
	tx, err := c.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck // rollback при панике или ошибке

	// 1. Обновляем статус транзакции
	updateQuery := `
		UPDATE transactions
		SET status = $2, provider = $3, provider_tx_id = $4, error_message = $5
		WHERE id = $1`

	result, err := tx.Exec(ctx, updateQuery, transactionID, status, provider, providerTxID, errorMessage)
	if err != nil {
		return fmt.Errorf("update status: %w", err)
	}
	if result.RowsAffected() == 0 {
		return domain.ErrNotFound
	}

	// 2. Получаем merchant_id для создания webhook
	// Читаем из текущей транзакции — видим только что обновлённую запись
	var merchantID string
	if err := tx.QueryRow(ctx,
		"SELECT merchant_id FROM transactions WHERE id = $1",
		transactionID,
	).Scan(&merchantID); err != nil {
		return fmt.Errorf("get merchant_id: %w", err)
	}

	// 3. Проверяем, настроен ли webhook для мерчанта
	cfg, err := c.webhookRepo.GetMerchantConfig(ctx, merchantID)
	if err != nil {
		// Не блокируем основную логику из-за webhook
		slog.Warn("failed to get merchant webhook config, skipping webhook",
			"merchant_id", merchantID,
			"error", err,
		)
	}

	// 4. Если webhook настроен — создаём delivery атомарно
	if cfg != nil {
		payload, err := buildWebhookPayload(transactionID, merchantID, status, provider)
		if err != nil {
			slog.Error("failed to build webhook payload", "error", err)
			// Не прерываем транзакцию — статус важнее webhook
		} else {
			delivery := &webhook.Delivery{
				TransactionID: transactionID,
				MerchantID:    merchantID,
				EventType:     "payment." + string(status),
				Payload:       payload,
			}
			if err := c.webhookRepo.CreateDelivery(ctx, tx, delivery); err != nil {
				slog.Error("failed to create webhook delivery, skipping",
					"transaction_id", transactionID,
					"error", err,
				)
				// Намеренно не прерываем — webhook не должен блокировать обновление статуса
			}
		}
	}

	// 5. Коммитим обе операции атомарно
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit: %w", err)
	}

	return nil
}

// buildWebhookPayload формирует JSON payload для webhook уведомления.
func buildWebhookPayload(
	transactionID string,
	merchantID string,
	status domain.Status,
	provider *string,
) ([]byte, error) {
	providerStr := ""
	if provider != nil {
		providerStr = *provider
	}

	payload := map[string]any{
		"event":          "payment." + string(status),
		"transaction_id": transactionID,
		"merchant_id":    merchantID,
		"status":         string(status),
		"provider":       providerStr,
		"timestamp":      time.Now().UTC().Format(time.RFC3339),
	}

	return json.Marshal(payload)
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
