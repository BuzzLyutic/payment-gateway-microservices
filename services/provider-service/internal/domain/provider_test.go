package domain_test

import (
	"testing"

	"github.com/BuzzLyutic/payment-gateway-microservices/services/provider-service/internal/domain"
)

func TestProvider_SupportsTransaction(t *testing.T) {
	provider := &domain.Provider{
		Status:         domain.ProviderStatusActive,
		Currencies:     []string{"RUB", "USD", "EUR"},
		PaymentMethods: []string{"card", "sbp"},
	}

	tests := []struct {
		name          string
		currency      string
		paymentMethod string
		want          bool
	}{
		{
			name:          "supported currency and method",
			currency:      "RUB",
			paymentMethod: "card",
			want:          true,
		},
		{
			name:          "supported currency, unsupported method",
			currency:      "RUB",
			paymentMethod: "crypto",
			want:          false,
		},
		{
			name:          "unsupported currency, supported method",
			currency:      "JPY",
			paymentMethod: "card",
			want:          false,
		},
		{
			name:          "both unsupported",
			currency:      "JPY",
			paymentMethod: "crypto",
			want:          false,
		},
		{
			name:          "sbp supported",
			currency:      "RUB",
			paymentMethod: "sbp",
			want:          true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := provider.SupportsTransaction(tt.currency, tt.paymentMethod)
			if got != tt.want {
				t.Errorf("SupportsTransaction(%q, %q) = %v, want %v",
					tt.currency, tt.paymentMethod, got, tt.want)
			}
		})
	}
}

func TestProvider_SupportsTransaction_InactiveProvider(t *testing.T) {
	provider := &domain.Provider{
		Status:         domain.ProviderStatusInactive,
		Currencies:     []string{"RUB"},
		PaymentMethods: []string{"card"},
	}

	if provider.SupportsTransaction("RUB", "card") {
		t.Error("inactive provider should not support any transaction")
	}
}
