package contextkeys

// ContextKey — типизированный ключ для context.Value.
// Использование типизированных ключей предотвращает коллизии
// между пакетами использующими один и тот же контекст.
type ContextKey string

const (
	RequestID    ContextKey = "request_id"
	MerchantInfo ContextKey = "merchant_info"
)
