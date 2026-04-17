package evaluator_test

import (
	"context"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/BuzzLyutic/payment-gateway-microservices/services/risk-service/internal/domain"
	"github.com/BuzzLyutic/payment-gateway-microservices/services/risk-service/internal/engine"
	"github.com/BuzzLyutic/payment-gateway-microservices/services/risk-service/internal/evaluator"
	"github.com/BuzzLyutic/payment-gateway-microservices/services/risk-service/internal/events"
)

func newTestLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stdout, nil))
}

func floatPtr(f float64) *float64 { return &f }

func makeRules() []domain.Rule {
	return []domain.Rule{
		{
			Name:     "high_amount",
			Type:     domain.RuleTypeSimple,
			Field:    "amount",
			Operator: domain.OperatorGt,
			RawValue: domain.RawValue{Single: floatPtr(10_000_000)},
			Score:    20,
		},
		{
			Name:     "very_high_amount",
			Type:     domain.RuleTypeSimple,
			Field:    "amount",
			Operator: domain.OperatorGt,
			RawValue: domain.RawValue{Single: floatPtr(50_000_000)},
			Score:    35,
		},
		{
			Name:     "night_time",
			Type:     domain.RuleTypeSimple,
			Field:    "hour",
			Operator: domain.OperatorBetween,
			RawValue: domain.RawValue{Range: [2]float64{1, 5}, IsList: true},
			Score:    10,
		},
	}
}

func makeEvent(amount int64, hourUTC int) events.PaymentCreated {
	now := time.Now().UTC()
	created := time.Date(now.Year(), now.Month(), now.Day(),
		hourUTC, 0, 0, 0, time.UTC)
	return events.PaymentCreated{
		TransactionID: "tx-001",
		MerchantID:    "merchant-001",
		Amount:        amount,
		Currency:      "RUB",
		PaymentMethod: "card",
		CreatedAt:     created,
	}
}

func TestEvaluator_Scenarios(t *testing.T) {
	cases := []struct {
		name             string
		amount           int64
		hour             int
		expectedDecision domain.Decision
		expectedScore    int
	}{
		{
			name:             "ordinary payment — approved, score=0",
			amount:           100_000,
			hour:             12,
			expectedDecision: domain.DecisionApproved,
			expectedScore:    0,
		},
		{
			name:             "large payment at night — approved, score=30",
			amount:           20_000_000,
			hour:             3,
			expectedDecision: domain.DecisionApproved,
			expectedScore:    30, // high_amount(20) + night_time(10)
		},
		{
			name:             "very large payment at night — approved, score=65",
			amount:           60_000_000,
			hour:             2,
			expectedDecision: domain.DecisionApproved,
			// high_amount(20) + very_high_amount(35) + night_time(10) = 65
			// 65 < 70 → approved, не blocked
			// блокировка через simple-правила невозможна без velocity по умолчанию
			expectedScore: 65,
		},
		{
			name:             "daytime large payment — approved, score=20",
			amount:           20_000_000,
			hour:             14,
			expectedDecision: domain.DecisionApproved,
			expectedScore:    20, // только high_amount
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			eng := engine.New(makeRules(), nil, newTestLogger())
			eval := evaluator.New(eng, newTestLogger())

			result := eval.Evaluate(context.Background(), makeEvent(tc.amount, tc.hour))

			if result.Decision != tc.expectedDecision {
				t.Errorf("expected decision %q, got %q",
					tc.expectedDecision, result.Decision)
			}
			if result.TotalScore != tc.expectedScore {
				t.Errorf("expected score %d, got %d",
					tc.expectedScore, result.TotalScore)
			}
		})
	}
}

// TestEvaluator_BlockThreshold проверяет точную границу блокировки.
func TestEvaluator_BlockThreshold(t *testing.T) {
	// Score ровно 70 → blocked
	rules := []domain.Rule{
		{
			Name:     "rule_a",
			Type:     domain.RuleTypeSimple,
			Field:    "amount",
			Operator: domain.OperatorGt,
			RawValue: domain.RawValue{Single: floatPtr(0)},
			Score:    70,
		},
	}

	eng := engine.New(rules, nil, newTestLogger())
	eval := evaluator.New(eng, newTestLogger())

	result := eval.Evaluate(context.Background(), makeEvent(1, 12))

	if result.Decision != domain.DecisionBlocked {
		t.Errorf("score=70 must be blocked, got %q", result.Decision)
	}
}
