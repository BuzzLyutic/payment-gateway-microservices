package engine

import (
	"context"
	"log/slog"

	"github.com/BuzzLyutic/payment-gateway-microservices/services/risk-service/internal/domain"
	"github.com/BuzzLyutic/payment-gateway-microservices/services/risk-service/internal/events"
	"github.com/redis/go-redis/v9"
)

// RuleResult — результат применения одного правила.
type RuleResult struct {
	RuleName  string
	Triggered bool
	Score     int
}

// Engine оценивает набор правил для одного события.
type Engine struct {
	rules  []domain.Rule
	redis  *redis.Client
	logger *slog.Logger
}

func New(rules []domain.Rule, rdb *redis.Client, logger *slog.Logger) *Engine {
	return &Engine{
		rules:  rules,
		redis:  rdb,
		logger: logger,
	}
}

// Evaluate применяет все правила к событию и возвращает результаты.
// Никогда не возвращает ошибку — проблемы логируются,
// сервис продолжает работу в degraded-режиме.
func (e *Engine) Evaluate(ctx context.Context, event events.PaymentCreated) []RuleResult {
	results := make([]RuleResult, 0, len(e.rules))

	for _, rule := range e.rules {
		result := e.applyRule(ctx, rule, event)
		results = append(results, result)
	}

	return results
}

func (e *Engine) applyRule(
	ctx context.Context,
	rule domain.Rule,
	event events.PaymentCreated,
) RuleResult {
	base := RuleResult{RuleName: rule.Name}

	switch rule.Type {
	case domain.RuleTypeSimple:
		triggered, err := evaluateSimple(rule, event)
		if err != nil {
			e.logger.ErrorContext(ctx, "simple rule evaluation failed",
				slog.String("rule", rule.Name),
				slog.String("transaction_id", event.TransactionID),
				slog.String("error", err.Error()),
			)
			return base
		}
		base.Triggered = triggered

	case domain.RuleTypeVelocity:
		triggered, err := evaluateVelocity(ctx, rule, event, e.redis)
		if err != nil {
			// fail-open: Redis недоступен — пропускаем velocity-правило,
			// логируем предупреждение, продолжаем обработку
			e.logger.WarnContext(ctx, "velocity rule skipped",
				slog.String("rule", rule.Name),
				slog.String("transaction_id", event.TransactionID),
				slog.String("reason", err.Error()),
			)
			return base
		}
		base.Triggered = triggered
	}

	if base.Triggered {
		base.Score = rule.Score
	}

	return base
}
