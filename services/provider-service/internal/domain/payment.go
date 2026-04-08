package domain

// ProcessRequest - входные данные для обработки платежа.
type ProcessRequest struct {
	TransactionID string `json:"transaction_id"`
	MerchantID    string `json:"merchant_id"`
	Amount        int64  `json:"amount"`
	Currency      string `json:"currency"`
	PaymentMethod string `json:"payment_method"`
}

// ResultStatus - результат обработки платежа провайдером.
type ResultStatus string

const (
	ResultCaptured ResultStatus = "captured"
	ResultDeclined ResultStatus = "declined"
	ResultFailed   ResultStatus = "failed"
)

// ProcessResult - ответ после обработки платежа.
type ProcessResult struct {
	TransactionID string       `json:"transaction_id"`
	Provider      string       `json:"provider"`
	ProviderTxID  string       `json:"provider_tx_id,omitempty"`
	Status        ResultStatus `json:"status"`
	ErrorMessage  string       `json:"error_message,omitempty"`
	LatencyMs     int64        `json:"latency_ms"`
}
