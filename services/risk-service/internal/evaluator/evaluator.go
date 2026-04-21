package evaluator

import (
	"context"
	"log/slog"
	"time"

	"github.com/BuzzLyutic/payment-gateway-microservices/services/risk-service/internal/domain"
	"github.com/BuzzLyutic/payment-gateway-microservices/services/risk-service/internal/engine"
	"github.com/BuzzLyutic/payment-gateway-microservices/services/risk-service/internal/events"
)

// Evaluator оркестрирует engine, считает score и принимает решение.
type Evaluator struct {
	engine *engine.Engine
	logger *slog.Logger
}

func New(eng *engine.Engine, logger *slog.Logger) *Evaluator {
	return &Evaluator{engine: eng, logger: logger}
}

// Evaluate — главный метод. Принимает событие, возвращает результат оценки.
func (e *Evaluator) Evaluate(
	ctx context.Context,
	event events.PaymentCreated,
) domain.EvaluationResult {
	start := time.Now()

	results := e.engine.Evaluate(ctx, event)

	totalScore := 0
	triggeredRules := make([]string, 0)

	for _, r := range results {
		if r.Triggered {
			totalScore += r.Score
			triggeredRules = append(triggeredRules, r.RuleName)
		}
	}

	decision := makeDecision(totalScore)

	e.logger.InfoContext(ctx, "risk evaluation complete",
		slog.String("transaction_id", event.TransactionID),
		slog.String("merchant_id", event.MerchantID),
		slog.Int("score", totalScore),
		slog.String("decision", string(decision)),
		slog.Any("triggered_rules", triggeredRules),
		slog.Duration("elapsed", time.Since(start)),
	)

	return domain.EvaluationResult{
		TransactionID:  event.TransactionID,
		TotalScore:     totalScore,
		Decision:       decision,
		TriggeredRules: triggeredRules,
	}
}

// makeDecision применяет пороги из ТЗ.
// review трактуется как approved для MVP.
func makeDecision(score int) domain.Decision {
	switch {
	case score >= domain.BlockThreshold:
		return domain.DecisionBlocked
	case score >= domain.ReviewThreshold:
		// для MVP review → approved
		// когда появится ручная проверка — меняем только здесь
		return domain.DecisionApproved
	default:
		return domain.DecisionApproved
	}
}
