package domain

import "errors"

var (
	// ErrUnknownRuleType — правило с неизвестным типом в конфиге.
	ErrUnknownRuleType = errors.New("unknown rule type")

	// ErrUnknownOperator — неизвестный оператор в simple-правиле.
	ErrUnknownOperator = errors.New("unknown operator")

	// ErrUnknownField — неизвестное поле транзакции в правиле.
	ErrUnknownField = errors.New("unknown field")

	// ErrUnknownKeyField — неизвестный key_field в velocity-правиле.
	ErrUnknownKeyField = errors.New("unknown key_field")

	// ErrInvalidBetweenValue — некорректное значение для оператора between.
	ErrInvalidBetweenValue = errors.New("between operator requires [min, max] array")

	// ErrRedisUnavailable — Redis недоступен, velocity-проверка пропускается.
	ErrRedisUnavailable = errors.New("redis unavailable")
)
