// только для тестов
package consumer

import (
	"github.com/BuzzLyutic/payment-gateway-microservices/services/transaction-service/internal/domain"
	"github.com/nats-io/nats.go/jetstream"
)

func (c *Consumer) ExportHandle(msg jetstream.Msg) {
	c.handle(msg)
}

// Экспортируем для тестов без изменения публичного API.
var MapCompletedStatus = mapCompletedStatus

type MockableMsg = jetstream.Msg

func (c *Consumer) Handle(msg jetstream.Msg)           { c.handle(msg) }
func (c *Consumer) HandleCompleted(msg jetstream.Msg)  { c.handleCompleted(msg) }
func (c *Consumer) HandleRiskBlocked(msg jetstream.Msg) { c.handleRiskBlocked(msg) }

func ExportBuildWebhookPayload(
	transactionID string,
	merchantID string,
	status domain.Status,
	provider *string,
) ([]byte, error) {
	return buildWebhookPayload(transactionID, merchantID, status, provider)
}
