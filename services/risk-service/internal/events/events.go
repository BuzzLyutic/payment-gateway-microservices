package events

import "time"

const (
	StreamName = "PAYMENTS"

	SubjectPaymentCreated      = "payment.created"
	SubjectPaymentRiskApproved = "payment.risk_approved"
	SubjectPaymentRiskBlocked  = "payment.risk_blocked"
	SubjectPaymentCompleted    = "payment.completed"

	ConsumerGroupRiskEvaluator = "risk-evaluator"
)

// PaymentCreated — входящее событие от Transaction Service.
// Поля card_hash, customer_ip, customer_email опциональны —
// при их отсутствии velocity-правила по этим ключам пропускаются.
type PaymentCreated struct {
	TransactionID string    `json:"transaction_id"`
	MerchantID    string    `json:"merchant_id"`
	Amount        int64     `json:"amount"`
	Currency      string    `json:"currency"`
	PaymentMethod string    `json:"payment_method"`
	CardHash      string    `json:"card_hash,omitempty"`
	CustomerIP    string    `json:"customer_ip,omitempty"`
	CustomerEmail string    `json:"customer_email,omitempty"`
	CreatedAt     time.Time `json:"created_at"`
}

// PaymentRiskApproved — исходящее событие, потребляет Provider Service.
type PaymentRiskApproved struct {
	TransactionID  string    `json:"transaction_id"`
	MerchantID     string    `json:"merchant_id"`
	Amount         int64     `json:"amount"`
	Currency       string    `json:"currency"`
	PaymentMethod  string    `json:"payment_method"`
	RiskScore      int       `json:"risk_score"`
	TriggeredRules []string  `json:"triggered_rules"`
	EvaluatedAt    time.Time `json:"evaluated_at"`
}

// PaymentRiskBlocked — исходящее событие, потребляет Transaction Service.
type PaymentRiskBlocked struct {
	TransactionID  string    `json:"transaction_id"`
	RiskScore      int       `json:"risk_score"`
	RiskDecision   string    `json:"risk_decision"`
	TriggeredRules []string  `json:"triggered_rules"`
	EvaluatedAt    time.Time `json:"evaluated_at"`
}
