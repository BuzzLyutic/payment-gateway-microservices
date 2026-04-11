package publisher

import (
    "context"
    "encoding/json"
    "fmt"
    "log/slog"
    "time"

    "github.com/nats-io/nats.go/jetstream"
    "github.com/BuzzLyutic/payment-gateway-microservices/services/risk-service/internal/domain"
    "github.com/BuzzLyutic/payment-gateway-microservices/services/risk-service/internal/events"
)

// Publisher публикует результаты оценки рисков в NATS JetStream.
type Publisher struct {
    js     jetstream.JetStream
    logger *slog.Logger
}

func New(js jetstream.JetStream, logger *slog.Logger) *Publisher {
    return &Publisher{js: js, logger: logger}
}

// Publish публикует approved или blocked в зависимости от решения.
func (p *Publisher) Publish(
    ctx context.Context,
    event events.PaymentCreated,
    result domain.EvaluationResult,
) error {
    switch result.Decision {
    case domain.DecisionApproved, domain.DecisionReview:
        return p.publishApproved(ctx, event, result)
    case domain.DecisionBlocked:
        return p.publishBlocked(ctx, result)
    default:
        return fmt.Errorf("publisher: unknown decision %q", result.Decision)
    }
}

func (p *Publisher) publishApproved(
    ctx context.Context,
    event events.PaymentCreated,
    result domain.EvaluationResult,
) error {
    payload := events.PaymentRiskApproved{
        TransactionID:  result.TransactionID,
        MerchantID:     event.MerchantID,
        Amount:         event.Amount,
        Currency:       event.Currency,
        PaymentMethod:  event.PaymentMethod,
        RiskScore:      result.TotalScore,
        TriggeredRules: result.TriggeredRules,
        EvaluatedAt:    time.Now().UTC(),
    }

    if err := p.publish(ctx, events.SubjectPaymentRiskApproved, payload); err != nil {
        return err
    }

    p.logger.InfoContext(ctx, "published risk approved",
        slog.String("transaction_id", result.TransactionID),
        slog.Int("score", result.TotalScore),
    )

    return nil
}

func (p *Publisher) publishBlocked(
    ctx context.Context,
    result domain.EvaluationResult,
) error {
    payload := events.PaymentRiskBlocked{
        TransactionID:  result.TransactionID,
        RiskScore:      result.TotalScore,
        RiskDecision:   string(domain.DecisionBlocked),
        TriggeredRules: result.TriggeredRules,
        EvaluatedAt:    time.Now().UTC(),
    }

    if err := p.publish(ctx, events.SubjectPaymentRiskBlocked, payload); err != nil {
        return err
    }

    p.logger.InfoContext(ctx, "published risk blocked",
        slog.String("transaction_id", result.TransactionID),
        slog.Int("score", result.TotalScore),
        slog.Any("triggered_rules", result.TriggeredRules),
    )

    return nil
}

// publish сериализует payload и отправляет в NATS.
func (p *Publisher) publish(ctx context.Context, subject string, payload any) error {
    data, err := json.Marshal(payload)
    if err != nil {
        return fmt.Errorf("publisher: marshal %q: %w", subject, err)
    }

    if _, err := p.js.Publish(ctx, subject, data); err != nil {
        return fmt.Errorf("publisher: publish %q: %w", subject, err)
    }

    return nil
}
