package loader

import (
    "encoding/json"
    "fmt"
    "os"

    "github.com/BuzzLyutic/payment-gateway-microservices/services/risk-service/internal/domain"
)

// rawRule — промежуточная структура для парсинга JSON.
// Нужна потому что поле value может быть числом или массивом —
// стандартный декодер не может это разрешить в одну структуру.
type rawRule struct {
    Name        string          `json:"name"`
    Description string          `json:"description"`
    Type        string          `json:"type"`
    Field       string          `json:"field,omitempty"`
    Operator    string          `json:"operator,omitempty"`
    Value       json.RawMessage `json:"value,omitempty"`
    KeyField    string          `json:"key_field,omitempty"`
    Window      string          `json:"window,omitempty"`
    Threshold   int             `json:"threshold,omitempty"`
    Score       int             `json:"score"`
}

type rawConfig struct {
    Rules []rawRule `json:"rules"`
}

// Load читает JSON-файл по переданному пути,
// парсит и валидирует правила.
// Вызывается один раз при старте сервиса.
func Load(path string) ([]domain.Rule, error) {
    data, err := os.ReadFile(path)
    if err != nil {
        return nil, fmt.Errorf("loader: read file %q: %w", path, err)
    }

    var cfg rawConfig
    if err := json.Unmarshal(data, &cfg); err != nil {
        return nil, fmt.Errorf("loader: parse json: %w", err)
    }

    if len(cfg.Rules) == 0 {
        return nil, fmt.Errorf("loader: no rules found in %q", path)
    }

    rules := make([]domain.Rule, 0, len(cfg.Rules))
    for i, raw := range cfg.Rules {
        rule, err := parseRule(raw)
        if err != nil {
            return nil, fmt.Errorf("loader: rule[%d] %q: %w", i, raw.Name, err)
        }
        rules = append(rules, rule)
    }

    return rules, nil
}

// parseRule преобразует rawRule в domain.Rule с полной валидацией.
func parseRule(raw rawRule) (domain.Rule, error) {
    if raw.Name == "" {
        return domain.Rule{}, fmt.Errorf("name is required")
    }
    if raw.Score <= 0 {
        return domain.Rule{}, fmt.Errorf("score must be positive")
    }

    // пустой type трактуем как simple — по спецификации ТЗ
    ruleType := domain.RuleType(raw.Type)
    if ruleType == "" {
        ruleType = domain.RuleTypeSimple
    }

    switch ruleType {
    case domain.RuleTypeSimple:
        return parseSimpleRule(raw)
    case domain.RuleTypeVelocity:
        return parseVelocityRule(raw)
    default:
        return domain.Rule{}, fmt.Errorf("%w: %q", domain.ErrUnknownRuleType, raw.Type)
    }
}

func parseSimpleRule(raw rawRule) (domain.Rule, error) {
    if err := validateField(raw.Field); err != nil {
        return domain.Rule{}, err
    }

    op := domain.Operator(raw.Operator)
    if err := validateOperator(op); err != nil {
        return domain.Rule{}, err
    }

    rawVal, err := parseValue(raw.Value, op)
    if err != nil {
        return domain.Rule{}, err
    }

    return domain.Rule{
        Name:        raw.Name,
        Description: raw.Description,
        Type:        domain.RuleTypeSimple,
        Field:       raw.Field,
        Operator:    op,
        RawValue:    rawVal,
        Score:       raw.Score,
    }, nil
}

func parseVelocityRule(raw rawRule) (domain.Rule, error) {
    if err := validateKeyField(raw.KeyField); err != nil {
        return domain.Rule{}, err
    }
    if raw.Window == "" {
        return domain.Rule{}, fmt.Errorf("window is required for velocity rule")
    }
    if raw.Threshold <= 0 {
        return domain.Rule{}, fmt.Errorf("threshold must be positive")
    }

    // Валидируем window — должен быть корректным Go duration.
    // time.ParseDuration вызовем здесь, чтобы упасть при старте,
    // а не в рантайме при первой проверке.
    if _, err := parseWindow(raw.Window); err != nil {
        return domain.Rule{}, fmt.Errorf("invalid window %q: %w", raw.Window, err)
    }

    return domain.Rule{
        Name:        raw.Name,
        Description: raw.Description,
        Type:        domain.RuleTypeVelocity,
        KeyField:    raw.KeyField,
        Window:      raw.Window,
        Threshold:   raw.Threshold,
        Score:       raw.Score,
    }, nil
}

// parseValue разбирает json.RawMessage в зависимости от оператора.
// Для between ожидаем массив [min, max], для остальных — число.
func parseValue(raw json.RawMessage, op domain.Operator) (domain.RawValue, error) {
    if len(raw) == 0 {
        return domain.RawValue{}, fmt.Errorf("value is required for simple rule")
    }

    if op == domain.OperatorBetween {
        var arr []float64
        if err := json.Unmarshal(raw, &arr); err != nil || len(arr) != 2 {
            return domain.RawValue{}, fmt.Errorf(
                "%w: got %s", domain.ErrInvalidBetweenValue, string(raw),
            )
        }
        if arr[0] >= arr[1] {
            return domain.RawValue{}, fmt.Errorf(
                "%w: min (%v) must be less than max (%v)",
                domain.ErrInvalidBetweenValue, arr[0], arr[1],
            )
        }
        return domain.RawValue{
            Range:  [2]float64{arr[0], arr[1]},
            IsList: true,
        }, nil
    }

    var num float64
    if err := json.Unmarshal(raw, &num); err != nil {
        return domain.RawValue{}, fmt.Errorf(
            "value must be a number for operator %q, got %s", op, string(raw),
        )
    }
    return domain.RawValue{
        Single: &num,
    }, nil
}

func validateField(field string) error {
    switch field {
    case "amount", "hour":
        return nil
    default:
        return fmt.Errorf("%w: %q (allowed: amount, hour)", domain.ErrUnknownField, field)
    }
}

func validateOperator(op domain.Operator) error {
    switch op {
    case domain.OperatorGt, domain.OperatorLt, domain.OperatorEq,
        domain.OperatorGte, domain.OperatorLte, domain.OperatorBetween:
        return nil
    default:
        return fmt.Errorf("%w: %q", domain.ErrUnknownOperator, op)
    }
}

func validateKeyField(keyField string) error {
    switch keyField {
    case "merchant_id", "card_hash", "customer_ip":
        return nil
    default:
        return fmt.Errorf("%w: %q (allowed: merchant_id, card_hash, customer_ip)",
            domain.ErrUnknownKeyField, keyField)
    }
}
