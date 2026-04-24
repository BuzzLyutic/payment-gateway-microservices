package publisher

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/nats-io/nats.go/jetstream"

	"github.com/BuzzLyutic/payment-gateway-microservices/services/provider-service/internal/events"
)

// JetStreamPublisher — минимальный интерфейс для тестирования.
// Позволяет мокать только Publish вместо всего jetstream.JetStream.
type JetStreamPublisher interface {
	Publish(ctx context.Context, subject string, payload []byte, opts ...jetstream.PublishOpt) (*jetstream.PubAck, error)
}

// Publisher публикует события в NATS JetStream.
type Publisher struct {
	js JetStreamPublisher
}

func New(js jetstream.JetStream) *Publisher {
	return &Publisher{js: js}
}

// NewWithJS создаёт Publisher с кастомным JS (для тестов).
func NewWithJS(js JetStreamPublisher) *Publisher {
	return &Publisher{js: js}
}

// PublishPaymentCompleted публикует результат обработки платежа.
func (p *Publisher) PublishPaymentCompleted(ctx context.Context, event events.PaymentCompleted) error {
	return p.publish(ctx, events.SubjectPaymentCompleted, event)
}

func (p *Publisher) publish(ctx context.Context, subject string, v any) error {
	data, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("marshal event: %w", err)
	}

	_, err = p.js.Publish(ctx, subject, data)
	if err != nil {
		return fmt.Errorf("publish %s: %w", subject, err)
	}

	return nil
}
