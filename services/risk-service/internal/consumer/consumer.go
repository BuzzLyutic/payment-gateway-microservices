package consumer

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"time"

	"github.com/nats-io/nats.go/jetstream"
	"github.com/BuzzLyutic/payment-gateway-microservices/services/risk-service/internal/evaluator"
	"github.com/BuzzLyutic/payment-gateway-microservices/services/risk-service/internal/events"
	"github.com/BuzzLyutic/payment-gateway-microservices/services/risk-service/internal/publisher"
)

const (
	// AckWait должен быть меньше таймаута подтверждения NATS (30s по ТЗ).
	// Оставляем запас на сетевые задержки.
	ackWait = 25 * time.Second

	// maxDeliver — максимум повторных доставок при неудаче.
	maxDeliver = 3
)

// Consumer подписывается на payments.created и orchestrates обработку.
type Consumer struct {
	js        jetstream.JetStream
	eval      *evaluator.Evaluator
	pub       *publisher.Publisher
	logger    *slog.Logger
}

func New(
	js jetstream.JetStream,
	eval *evaluator.Evaluator,
	pub *publisher.Publisher,
	logger *slog.Logger,
) *Consumer {
	return &Consumer{
		js:     js,
		eval:   eval,
		pub:    pub,
		logger: logger,
	}
}

// Start инициализирует JetStream consumer и запускает цикл обработки.
// Блокирует до отмены ctx.
func (c *Consumer) Start(ctx context.Context) error {
	if err := c.ensureStream(ctx); err != nil {
		return err
	}

	cons, err := c.createOrUpdateConsumer(ctx)
	if err != nil {
		return err
	}

	c.logger.Info("risk consumer started",
		slog.String("subject", events.SubjectPaymentCreated),
		slog.String("consumer_group", events.ConsumerGroupRiskEvaluator),
	)

	// Consume запускает горутину доставки сообщений внутри nats.go.
	// Возвращает ConsumeContext, который живёт пока не вызван Stop
	// или не отменён родительский ctx.
	cc, err := cons.Consume(func(msg jetstream.Msg) {
		c.handleMessage(ctx, msg)
	})
	if err != nil {
		return err
	}
	defer cc.Stop()

	// Ждём отмены контекста (graceful shutdown).
	<-ctx.Done()
	c.logger.Info("risk consumer stopping")
	return nil
}

// ensureStream создаёт или обновляет stream PAYMENTS с полным набором subjects.
// Идемпотентная операция — безопасно вызывать при каждом старте.
func (c *Consumer) ensureStream(ctx context.Context) error {
	_, err := c.js.CreateOrUpdateStream(ctx, jetstream.StreamConfig{
		Name: events.StreamName,
		Subjects: []string{
			events.SubjectPaymentCreated,
			events.SubjectPaymentRiskApproved,
			events.SubjectPaymentRiskBlocked,
			events.SubjectPaymentCompleted,
		},
		Storage:   jetstream.FileStorage,
		Retention: jetstream.WorkQueuePolicy,
		MaxAge:    72 * time.Hour,
	})
	if err != nil {
		return err
	}
	return nil
}

// createOrUpdateConsumer создаёт durable consumer для risk-evaluator группы.
func (c *Consumer) createOrUpdateConsumer(ctx context.Context) (jetstream.Consumer, error) {
	return c.js.CreateOrUpdateConsumer(ctx, events.StreamName, jetstream.ConsumerConfig{
		// Durable — consumer переживает перезапуск сервиса.
		// NATS запомнит позицию и доставит непрочитанные сообщения.
		Durable:        events.ConsumerGroupRiskEvaluator,
		FilterSubject:  events.SubjectPaymentCreated,
		AckPolicy:      jetstream.AckExplicitPolicy,
		AckWait:        ackWait,
		MaxDeliver:     maxDeliver,
		DeliverPolicy:  jetstream.DeliverAllPolicy,
	})
}

// handleMessage обрабатывает одно сообщение.
// Ack отправляется только после успешной публикации результата.
// При ошибке — Nak, сообщение будет доставлено повторно.
func (c *Consumer) handleMessage(ctx context.Context, msg jetstream.Msg) {
	meta, _ := msg.Metadata()

	logger := c.logger.With(
		slog.Uint64("nats_sequence", meta.Sequence.Stream),
		slog.Uint64("nats_delivered", uint64(meta.NumDelivered)),
	)

	var event events.PaymentCreated
	if err := json.Unmarshal(msg.Data(), &event); err != nil {
		logger.Error("failed to unmarshal payment.created",
			slog.String("error", err.Error()),
		)
		// Некорректный JSON — повторная доставка не поможет.
		//Term убирает сообщение из очереди без повторной доставки.
		if termErr := msg.Term(); termErr != nil {
			logger.Error("failed to term message", slog.String("error", termErr.Error()))
		}
		return
	}

	logger = logger.With(slog.String("transaction_id", event.TransactionID))

	// Идемпотентность: повторная доставка того же сообщения
	// приведёт к повторной оценке и повторной публикации.
	// NATS JetStream с WorkQueue retention гарантирует at-least-once,
	// поэтому evaluator и publisher должны быть идемпотентны.
	// Evaluator — stateless, всегда даёт тот же результат для тех же данных.
	// Publisher — может опубликовать дубль, Provider Service должен
	// обрабатывать это через idempotency key на своей стороне.
	result := c.eval.Evaluate(ctx, event)

	if err := c.pub.Publish(ctx, event, result); err != nil {
		logger.Error("failed to publish risk result",
			slog.String("decision", string(result.Decision)),
			slog.String("error", err.Error()),
		)
		// Nak с задержкой — даём время NATS восстановиться.
		if nakErr := msg.NakWithDelay(5 * time.Second); nakErr != nil {
			logger.Error("failed to nak message", slog.String("error", nakErr.Error()))
		}
		return
	}

	if err := msg.Ack(); err != nil {
		// Ack не прошёл — NATS доставит сообщение повторно.
		// Это приведёт к повторной оценке и публикации (дубль).
		// Логируем как error — это нештатная ситуация.
		logger.Error("failed to ack message",
			slog.String("error", err.Error()),
		)
		return
	}

	logger.Info("message processed and acked",
		slog.String("decision", string(result.Decision)),
		slog.Int("score", result.TotalScore),
	)
}

// isContextError проверяет, вызвана ли ошибка отменой контекста.
// Используется чтобы не логировать shutdown как ошибку.
func isContextError(err error) bool {
	return errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)
}
