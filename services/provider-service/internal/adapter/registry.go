package adapter

import "fmt"

// Registry - реестр адаптеров провайдеров.
// Инициализируется при старте, сервис обращается по имени провайдера.
type Registry struct {
	adapters map[string]PaymentAdapter
}

func NewRegistry() *Registry {
	return &Registry{
		adapters: make(map[string]PaymentAdapter),
	}
}

// Register добавляет адаптер для провайдера.
func (r *Registry) Register(providerName string, adapter PaymentAdapter) {
	r.adapters[providerName] = adapter
}

// Get возвращает адаптер по имени провайдера.
func (r *Registry) Get(providerName string) (PaymentAdapter, error) {
	a, ok := r.adapters[providerName]
	if !ok {
		return nil, fmt.Errorf("adapter not found: %s", providerName)
	}
	return a, nil
}
