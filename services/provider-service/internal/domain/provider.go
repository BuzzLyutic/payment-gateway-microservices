package domain

import "time"

// ProviderStatus - статус провайдера.
type ProviderStatus string

const (
	ProviderStatusActive   ProviderStatus = "active"
	ProviderStatusInactive ProviderStatus = "inactive"
)

// Provider - доменная сущность платёжного провайдера.
type Provider struct {
	ID             string            `json:"id"`
	Name           string            `json:"name"`
	Type           string            `json:"type"`            // mock, stripe
	Status         ProviderStatus    `json:"status"`
	Currencies     []string          `json:"currencies"`      // [RUB, USD, EUR]
	PaymentMethods []string          `json:"payment_methods"` // [card, sbp]
	CommissionPct  float64           `json:"commission_pct"`
	Config 		   map[string]any    `json:"config,omitempty"`
	CreatedAt      time.Time         `json:"created_at"`
	UpdatedAt      time.Time         `json:"updated_at"`
}

// SupportsTransaction проверяет, может ли провайдер обработать транзакцию
// с указанной валютой и методом оплаты.
func (p *Provider) SupportsTransaction(currency, paymentMethod string) bool {
	return p.Status == ProviderStatusActive &&
		contains(p.Currencies, currency) &&
		contains(p.PaymentMethods, paymentMethod)
}

func contains(slice []string, val string) bool {
	for _, s := range slice {
		if s == val {
			return true
		}
	}
	return false
}
