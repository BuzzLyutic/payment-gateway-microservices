package engine_test

import (
	"context"
	"testing"
	"time"

	"github.com/BuzzLyutic/payment-gateway-microservices/services/risk-service/internal/domain"
	"github.com/BuzzLyutic/payment-gateway-microservices/services/risk-service/internal/engine"
	"github.com/BuzzLyutic/payment-gateway-microservices/services/risk-service/internal/events"
)

// floatPtr и newTestLogger уже определены в engine_test.go —
// используем их напрямую (один пакет engine_test).

func makeEventWithTime(amount int64, hour int, cardHash, ip string) events.PaymentCreated {
	now := time.Now().UTC()
	created := time.Date(now.Year(), now.Month(), now.Day(),
		hour, 0, 0, 0, time.UTC)
	return events.PaymentCreated{
		TransactionID: "tx-simple-001",
		MerchantID:    "merchant-001",
		Amount:        amount,
		Currency:      "RUB",
		PaymentMethod: "card",
		CardHash:      cardHash,
		CustomerIP:    ip,
		CreatedAt:     created,
	}
}

// applyOperator — покрываем все операторы

func TestEngine_SimpleRule_AllOperators(t *testing.T) {
	cases := []struct {
		name      string
		operator  domain.Operator
		rawValue  domain.RawValue
		amount    int64
		triggered bool
	}{
		// OperatorGt: уже покрыт в engine_test.go, но добавим граничные случаи
		{
			name:      "gt: equal value not triggered",
			operator:  domain.OperatorGt,
			rawValue:  domain.RawValue{Single: floatPtr(1000)},
			amount:    1000,
			triggered: false, // 1000 > 1000 = false
		},
		// OperatorLt
		{
			name:      "lt: value less than threshold",
			operator:  domain.OperatorLt,
			rawValue:  domain.RawValue{Single: floatPtr(5000)},
			amount:    1000,
			triggered: true, // 1000 < 5000
		},
		{
			name:      "lt: value equal threshold not triggered",
			operator:  domain.OperatorLt,
			rawValue:  domain.RawValue{Single: floatPtr(1000)},
			amount:    1000,
			triggered: false, // 1000 < 1000 = false
		},
		{
			name:      "lt: value greater not triggered",
			operator:  domain.OperatorLt,
			rawValue:  domain.RawValue{Single: floatPtr(500)},
			amount:    1000,
			triggered: false, // 1000 < 500 = false
		},
		// OperatorEq
		{
			name:      "eq: exact match",
			operator:  domain.OperatorEq,
			rawValue:  domain.RawValue{Single: floatPtr(1000)},
			amount:    1000,
			triggered: true,
		},
		{
			name:      "eq: no match",
			operator:  domain.OperatorEq,
			rawValue:  domain.RawValue{Single: floatPtr(999)},
			amount:    1000,
			triggered: false,
		},
		// OperatorGte
		{
			name:      "gte: equal triggers",
			operator:  domain.OperatorGte,
			rawValue:  domain.RawValue{Single: floatPtr(1000)},
			amount:    1000,
			triggered: true, // 1000 >= 1000
		},
		{
			name:      "gte: greater triggers",
			operator:  domain.OperatorGte,
			rawValue:  domain.RawValue{Single: floatPtr(500)},
			amount:    1000,
			triggered: true, // 1000 >= 500
		},
		{
			name:      "gte: less not triggered",
			operator:  domain.OperatorGte,
			rawValue:  domain.RawValue{Single: floatPtr(2000)},
			amount:    1000,
			triggered: false, // 1000 >= 2000 = false
		},
		// OperatorLte
		{
			name:      "lte: equal triggers",
			operator:  domain.OperatorLte,
			rawValue:  domain.RawValue{Single: floatPtr(1000)},
			amount:    1000,
			triggered: true, // 1000 <= 1000
		},
		{
			name:      "lte: less triggers",
			operator:  domain.OperatorLte,
			rawValue:  domain.RawValue{Single: floatPtr(2000)},
			amount:    1000,
			triggered: true, // 1000 <= 2000
		},
		{
			name:      "lte: greater not triggered",
			operator:  domain.OperatorLte,
			rawValue:  domain.RawValue{Single: floatPtr(500)},
			amount:    1000,
			triggered: false, // 1000 <= 500 = false
		},
		// OperatorBetween — уже покрыт в engine_test.go
		{
			name:     "between: boundary min triggers",
			operator: domain.OperatorBetween,
			rawValue: domain.RawValue{
				Range:  [2]float64{1000, 5000},
				IsList: true,
			},
			amount:    1000,
			triggered: true, // 1000 >= 1000 && 1000 <= 5000
		},
		{
			name:     "between: boundary max triggers",
			operator: domain.OperatorBetween,
			rawValue: domain.RawValue{
				Range:  [2]float64{1000, 5000},
				IsList: true,
			},
			amount:    5000,
			triggered: true, // 5000 >= 1000 && 5000 <= 5000
		},
		{
			name:     "between: outside range not triggered",
			operator: domain.OperatorBetween,
			rawValue: domain.RawValue{
				Range:  [2]float64{1000, 5000},
				IsList: true,
			},
			amount:    500,
			triggered: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rules := []domain.Rule{
				{
					Name:     "test_rule",
					Type:     domain.RuleTypeSimple,
					Field:    "amount",
					Operator: tc.operator,
					RawValue: tc.rawValue,
					Score:    10,
				},
			}

			eng := engine.New(rules, nil, newTestLogger())
			results := eng.Evaluate(
				context.Background(),
				makeEventWithTime(tc.amount, 12, "", ""),
			)

			if results[0].Triggered != tc.triggered {
				t.Errorf("operator=%q amount=%d: triggered=%v, want=%v",
					tc.operator, tc.amount, results[0].Triggered, tc.triggered)
			}
		})
	}
}

// extractField — покрываем ветки hour и unknown

func TestEngine_SimpleRule_FieldHour(t *testing.T) {
	// hour уже частично покрыт, но проверяем крайние значения
	rules := []domain.Rule{
		{
			Name:     "hour_rule",
			Type:     domain.RuleTypeSimple,
			Field:    "hour",
			Operator: domain.OperatorEq,
			RawValue: domain.RawValue{Single: floatPtr(0)},
			Score:    5,
		},
	}

	eng := engine.New(rules, nil, newTestLogger())

	// hour=0 (полночь UTC)
	results := eng.Evaluate(
		context.Background(),
		makeEventWithTime(1000, 0, "", ""),
	)

	if !results[0].Triggered {
		t.Error("hour=0: expected rule to trigger for eq(0)")
	}
}

func TestEngine_SimpleRule_UnknownField_DoesNotPanic(t *testing.T) {
	// Неизвестное поле — applyRule логирует ошибку и возвращает base (не паникует).
	rules := []domain.Rule{
		{
			Name:     "bad_field_rule",
			Type:     domain.RuleTypeSimple,
			Field:    "unknown_field", // невалидное поле
			Operator: domain.OperatorGt,
			RawValue: domain.RawValue{Single: floatPtr(100)},
			Score:    10,
		},
	}

	eng := engine.New(rules, nil, newTestLogger())
	results := eng.Evaluate(
		context.Background(),
		makeEventWithTime(1000, 12, "", ""),
	)

	// Не паникует, возвращает незасработавшее правило.
	if results[0].Triggered {
		t.Error("unknown field: expected rule NOT to trigger")
	}
	if results[0].Score != 0 {
		t.Errorf("unknown field: expected score 0, got %d", results[0].Score)
	}
}

// applyOperator: unknown operator

func TestEngine_SimpleRule_UnknownOperator_DoesNotPanic(t *testing.T) {
	// Неизвестный оператор — ошибка логируется, правило не срабатывает.
	rules := []domain.Rule{
		{
			Name:     "bad_op_rule",
			Type:     domain.RuleTypeSimple,
			Field:    "amount",
			Operator: domain.Operator("unknown_op"),
			RawValue: domain.RawValue{Single: floatPtr(100)},
			Score:    10,
		},
	}

	eng := engine.New(rules, nil, newTestLogger())
	results := eng.Evaluate(
		context.Background(),
		makeEventWithTime(1000, 12, "", ""),
	)

	if results[0].Triggered {
		t.Error("unknown operator: expected rule NOT to trigger")
	}
}
