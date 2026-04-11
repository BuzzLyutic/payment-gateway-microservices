package events

import "time"

const (
	SubjectPaymentCreated   = "payment.created"
	SubjectPaymentCompleted = "payment.completed"
	SubjectPaymentRiskApproved = "payment.risk_approved"
	SubjectPaymentRiskBlocked  = "payment.risk_blocked"

	StreamName = "PAYMENTS"
)

// PaymentCreated — публикует Transaction Service, потребляет Provider Service.
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

// PaymentRiskApproved — публикует Risk Service, потребляет Provider Service.
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

// PaymentRiskBlocked — публикует Risk Service, потребляет Transaction Service.
type PaymentRiskBlocked struct {
	TransactionID  string    `json:"transaction_id"`
	RiskScore      int       `json:"risk_score"`
	RiskDecision   string    `json:"risk_decision"`
	TriggeredRules []string  `json:"triggered_rules"`
	EvaluatedAt    time.Time `json:"evaluated_at"`
}

// PaymentCompleted — публикует Provider Service, потребляет Transaction Service.
type PaymentCompleted struct {
	TransactionID string    `json:"transaction_id"`
	Status        string    `json:"status"`
	Provider      string    `json:"provider"`
	ProviderTxID  string    `json:"provider_tx_id"`
	ErrorMessage  string    `json:"error_message"`
	ProcessedAt   time.Time `json:"processed_at"`
}
