package domain

import (
	"fmt"
	"time"
)

// Status - тип статуса транзакции.
type Status string

const (
	StatusPending    Status = "pending"
	StatusProcessing Status = "processing"
	StatusCaptured   Status = "captured"
	StatusFailed     Status = "failed"
	StatusDeclined   Status = "declined"
	StatusRefunded   Status = "refunded"
)

// validTransitions - допустимые переходы состояний (state machine).
// Ключ - текущий статус, значение - множество допустимых целевых статусов.
var validTransitions = map[Status]map[Status]bool{
	StatusPending:    {StatusProcessing: true},
	StatusProcessing: {StatusCaptured: true, StatusFailed: true, StatusDeclined: true},
	StatusCaptured:   {StatusRefunded: true},
}

// CanTransitionTo проверяет, допустим ли переход из текущего статуса в целевой.
func (s Status) CanTransitionTo(target Status) bool {
	targets, ok := validTransitions[s]
	if !ok {
		return false
	}
	return targets[target]
}

// Transaction - доменная сущность платёжной транзакции.
type Transaction struct {
	ID             string            `json:"id"`
	IdempotencyKey string            `json:"idempotency_key,omitempty"`
	MerchantID     string            `json:"merchant_id"`
	Amount         int64             `json:"amount"`
	Currency       string            `json:"currency"`
	Status         Status            `json:"status"`
	Description    *string           `json:"description,omitempty"`   
	Provider       *string           `json:"provider,omitempty"`       
	ProviderTxID   *string           `json:"provider_tx_id,omitempty"` 
	ErrorMessage   *string           `json:"error_message,omitempty"`  
	Metadata       map[string]string `json:"metadata,omitempty"`
	CreatedAt      time.Time         `json:"created_at"`
	UpdatedAt      time.Time         `json:"updated_at"`
}

// TransitionTo выполняет переход в новый статус с проверкой допустимости.
func (t *Transaction) TransitionTo(target Status) error {
	if !t.Status.CanTransitionTo(target) {
		return fmt.Errorf("%w: cannot transition from %s to %s",
			ErrInvalidTransition, t.Status, target)
	}
	t.Status = target
	return nil
}
