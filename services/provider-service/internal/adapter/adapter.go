package adapter

import (
	"context"
	"errors"

	"github.com/BuzzLyutic/payment-gateway-microservices/services/provider-service/internal/domain"
)

// ErrTransient - временная ошибка провайдера, можно повторить.
var ErrTransient = errors.New("transient provider error")

// AdapterResult - ответ адаптера провайдера.
// Не содержит имя провайдера и латентность - это добавляет service-слой.
type AdapterResult struct {
	ProviderTxID string
	Status       domain.ResultStatus
	ErrorMessage string
}

// PaymentAdapter - интерфейс адаптера платёжного провайдера.
// Каждый провайдер (mock, stripe) реализует этот интерфейс.
type PaymentAdapter interface {
	ProcessPayment(ctx context.Context, req *domain.ProcessRequest) (*AdapterResult, error)
}
