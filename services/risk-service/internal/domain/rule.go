package domain

// RuleType определяет тип правила.
type RuleType string

const (
	RuleTypeSimple   RuleType = "simple"
	RuleTypeVelocity RuleType = "velocity"
)

// Operator — оператор сравнения для simple-правил.
type Operator string

const (
	OperatorGt      Operator = "gt"
	OperatorLt      Operator = "lt"
	OperatorEq      Operator = "eq"
	OperatorGte     Operator = "gte"
	OperatorLte     Operator = "lte"
	OperatorBetween Operator = "between"
)

// Rule — единое правило из конфига.
// Поля используются в зависимости от типа:
//   simple:   Field, Operator, Value, Score
//   velocity: KeyField, Window, Threshold, Score
type Rule struct {
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Type        RuleType `json:"type"`

	// simple-правило
	Field    string   `json:"field,omitempty"`
	Operator Operator `json:"operator,omitempty"`
	// Value может быть числом или массивом [min, max] для between.
	// Используем json.RawMessage — разбираем вручную при загрузке.
	Value    interface{} `json:"-"`
	RawValue RawValue    `json:"value,omitempty"`

	// velocity-правило
	KeyField  string `json:"key_field,omitempty"`
	Window    string `json:"window,omitempty"`
	Threshold int    `json:"threshold,omitempty"`

	Score int `json:"score"`
}

// RawValue хранит значение до парсинга.
// После загрузки loader заполняет Rule.Value корректным типом.
type RawValue struct {
	Single *float64   // число
	Range  [2]float64 // [min, max] для between
	IsList bool       // true если between
}

// Decision — итоговое решение по транзакции.
type Decision string

const (
	DecisionApproved Decision = "approved"
	DecisionReview   Decision = "review" // для MVP трактуется как approved
	DecisionBlocked  Decision = "blocked"
)

const (
	// Пороги из ТЗ.
	BlockThreshold  = 70
	ReviewThreshold = 40
)

// EvaluationResult — результат оценки одной транзакции.
type EvaluationResult struct {
	TransactionID  string
	TotalScore     int
	Decision       Decision
	TriggeredRules []string
}
