package domain

import "errors"

var (
	// ErrNoProviderAvailable - нет провайдера, подходящего под параметры транзакции.
	ErrNoProviderAvailable = errors.New("no provider available for transaction")
)
