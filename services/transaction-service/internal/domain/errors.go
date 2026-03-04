package domain

import "errors"

var (
	// ErrNotFound - транзакция не найдена.
	ErrNotFound = errors.New("transaction not found")

	// ErrInvalidTransition - недопустимый переход состояния.
	ErrInvalidTransition = errors.New("invalid status transition")

	// ErrDuplicateIdempotencyKey - повторный запрос с тем же ключом.
	ErrDuplicateIdempotencyKey = errors.New("duplicate idempotency key")
)
