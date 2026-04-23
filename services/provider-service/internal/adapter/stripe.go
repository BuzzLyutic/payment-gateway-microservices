package adapter

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/stripe/stripe-go/v76"
	"github.com/stripe/stripe-go/v76/paymentintent"

	"github.com/BuzzLyutic/payment-gateway-microservices/services/provider-service/internal/domain"
)

// StripeAdapter реализует PaymentAdapter для Stripe.
// Использует PaymentIntent API — стандартный способ приёма платежей в Stripe.
type StripeAdapter struct {
	apiKey string
}

func NewStripeAdapter(apiKey string) *StripeAdapter {
	return &StripeAdapter{apiKey: apiKey}
}

func (a *StripeAdapter) ProcessPayment(ctx context.Context, req *domain.ProcessRequest) (*AdapterResult, error) {
	stripe.Key = a.apiKey

	// Stripe работает в минимальных единицах валюты (копейки для RUB).
	// Amount уже в минимальных единицах — передаём как есть.
	params := &stripe.PaymentIntentParams{
		Amount:   stripe.Int64(req.Amount),
		Currency: stripe.String(normalizeCurrency(req.Currency)),
		// confirm=true — создаём и сразу подтверждаем в одном запросе.
		// Для sandbox это даёт синхронный ответ без webhook.
		Confirm: stripe.Bool(true),
		// AutomaticPaymentMethods с allow_redirects=never —
		// отключаем редиректы (3DS и т.д.) для sandbox-тестирования.
		AutomaticPaymentMethods: &stripe.PaymentIntentAutomaticPaymentMethodsParams{
			Enabled:        stripe.Bool(true),
			AllowRedirects: stripe.String("never"),
		},
		// Передаём transaction_id как metadata для сверки
		Metadata: map[string]string{
			"transaction_id": req.TransactionID,
			"merchant_id":    req.MerchantID,
		},
	}

	// Добавляем тестовый метод оплаты для sandbox.
	// pm_card_visa — всегда успешная карта в тестовом режиме Stripe.
	// В продакшене здесь будет реальный PaymentMethod ID от клиента.
	params.PaymentMethod = stripe.String("pm_card_visa")

	slog.Info("stripe: creating payment intent",
		"transaction_id", req.TransactionID,
		"amount", req.Amount,
		"currency", req.Currency,
	)

	pi, err := paymentintent.New(params)
	if err != nil {
		return nil, a.handleStripeError(err)
	}

	slog.Info("stripe: payment intent created",
		"transaction_id", req.TransactionID,
		"payment_intent_id", pi.ID,
		"status", pi.Status,
	)

	return mapStripeResult(pi), nil
}

// handleStripeError классифицирует ошибки Stripe.
// Stripe возвращает структурированные ошибки через stripe.Error.
func (a *StripeAdapter) handleStripeError(err error) error {
	var stripeErr *stripe.Error
	if !errors.As(err, &stripeErr) {
		// Неизвестная ошибка — считаем transient
		return fmt.Errorf("%w: %v", ErrTransient, err)
	}

	slog.Warn("stripe error",
		"code", stripeErr.Code,
		"type", stripeErr.Type,
		"message", stripeErr.Msg,
	)

	switch stripeErr.Type {
	case stripe.ErrorTypeCard:
		// Ошибки карты (insufficient_funds, card_declined и т.д.) —
		// терминальные, retry бессмысленен
		return fmt.Errorf("card error: %s", stripeErr.Msg)

	case stripe.ErrorTypeInvalidRequest:
		// Неверные параметры запроса — наша ошибка, retry не поможет
		return fmt.Errorf("invalid request: %s", stripeErr.Msg)

	case stripe.ErrorTypeAPI:
		// Проблемы на стороне Stripe или сети — transient, можно retry
		return fmt.Errorf("%w: stripe api error: %s", ErrTransient, stripeErr.Msg)

	default:
		// Неизвестный тип — безопаснее считать transient
		return fmt.Errorf("%w: stripe error: %s", ErrTransient, stripeErr.Msg)
	}
}

// mapStripeResult переводит статус Stripe PaymentIntent в ResultStatus.
func mapStripeResult(pi *stripe.PaymentIntent) *AdapterResult {
	switch pi.Status {
	case stripe.PaymentIntentStatusSucceeded:
		return &AdapterResult{
			ProviderTxID: pi.ID,
			Status:       domain.ResultCaptured,
		}

	case stripe.PaymentIntentStatusRequiresPaymentMethod,
		stripe.PaymentIntentStatusCanceled:
		// Платёж отклонён — терминальный статус
		declineReason := "payment declined"
		if pi.LastPaymentError != nil {
			declineReason = pi.LastPaymentError.Msg
		}
		return &AdapterResult{
			ProviderTxID: pi.ID,
			Status:       domain.ResultDeclined,
			ErrorMessage: declineReason,
		}

	default:
		// requires_action, requires_confirmation и т.д. —
		// для sandbox с allow_redirects=never не должны появляться,
		// но на всякий случай обрабатываем
		return &AdapterResult{
			ProviderTxID: pi.ID,
			Status:       domain.ResultFailed,
			ErrorMessage: fmt.Sprintf("unexpected payment intent status: %s", pi.Status),
		}
	}
}

// normalizeCurrency приводит валюту к формату Stripe (lowercase).
func normalizeCurrency(currency string) string {
	switch currency {
	case "RUB":
		return "rub"
	case "USD":
		return "usd"
	case "EUR":
		return "eur"
	default:
		return currency
	}
}
