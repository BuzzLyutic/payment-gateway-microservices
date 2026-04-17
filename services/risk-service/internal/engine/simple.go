package engine

import (
	"fmt"

	"github.com/BuzzLyutic/payment-gateway-microservices/services/risk-service/internal/domain"
	"github.com/BuzzLyutic/payment-gateway-microservices/services/risk-service/internal/events"
)

// evaluateSimple возвращает true если правило сработало.
func evaluateSimple(rule domain.Rule, event events.PaymentCreated) (bool, error) {
	value, err := extractField(rule.Field, event)
	if err != nil {
		return false, err
	}

	return applyOperator(rule.Operator, value, rule.RawValue)
}

// extractField извлекает числовое значение поля из события.
// Сейчас поддерживаются amount и hour — именно те поля,
// которые валидируются в loader. Новые поля добавляются здесь
// и в loader.validateField одновременно.
func extractField(field string, event events.PaymentCreated) (float64, error) {
	switch field {
	case "amount":
		return float64(event.Amount), nil
	case "hour":
		return float64(event.CreatedAt.UTC().Hour()), nil
	default:
		return 0, fmt.Errorf("%w: %q", domain.ErrUnknownField, field)
	}
}

// applyOperator применяет оператор сравнения.
func applyOperator(op domain.Operator, value float64, raw domain.RawValue) (bool, error) {
	switch op {
	case domain.OperatorGt:
		return value > *raw.Single, nil
	case domain.OperatorLt:
		return value < *raw.Single, nil
	case domain.OperatorEq:
		return value == *raw.Single, nil
	case domain.OperatorGte:
		return value >= *raw.Single, nil
	case domain.OperatorLte:
		return value <= *raw.Single, nil
	case domain.OperatorBetween:
		return value >= raw.Range[0] && value <= raw.Range[1], nil
	default:
		return false, fmt.Errorf("%w: %q", domain.ErrUnknownOperator, op)
	}
}
