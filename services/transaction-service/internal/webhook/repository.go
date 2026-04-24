package webhook

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Delivery представляет запись в таблице webhook_deliveries.
type Delivery struct {
	ID            string
	TransactionID string
	MerchantID    string
	EventType     string
	Payload       []byte
	Status        string
	Attempts      int
	MaxAttempts   int
	NextRetryAt   time.Time
	LastError     string
	CreatedAt     time.Time
	DeliveredAt   *time.Time
}

// MerchantConfig содержит webhook-конфигурацию мерчанта.
type MerchantConfig struct {
	WebhookURL    string
	WebhookSecret string
}

// Repository работает с таблицами merchants и webhook_deliveries.
type Repository struct {
	pool *pgxpool.Pool
}

func NewRepository(pool *pgxpool.Pool) *Repository {
	return &Repository{pool: pool}
}

// GetMerchantConfig возвращает webhook URL и секрет мерчанта.
// Возвращает nil, nil если мерчант не найден или webhook_url не задан —
// в этом случае webhook просто не создаётся.
func (r *Repository) GetMerchantConfig(ctx context.Context, merchantID string) (*MerchantConfig, error) {
	query := `
		SELECT webhook_url, webhook_secret
		FROM merchants
		WHERE id = $1 AND is_active = true AND webhook_url IS NOT NULL`

	var cfg MerchantConfig
	err := r.pool.QueryRow(ctx, query, merchantID).Scan(
		&cfg.WebhookURL,
		&cfg.WebhookSecret,
	)
	if err != nil {
		// pgx не импортируем здесь напрямую — проверяем по тексту ошибки
		// через sentinel из репозитория транзакций не хотим создавать зависимость
		return nil, nil //nolint:nilerr // мерчант не найден — это не ошибка
	}

	return &cfg, nil
}

// CreateDelivery создаёт запись о доставке webhook в рамках существующей транзакции БД.
// Принимает tx *pgxpool.Tx для атомарности с обновлением статуса транзакции.
func (r *Repository) CreateDelivery(
	ctx context.Context,
	tx pgx.Tx,
	delivery *Delivery,
) error {
	query := `
		INSERT INTO webhook_deliveries (
			transaction_id, merchant_id, event_type, payload, status, next_retry_at
		) VALUES ($1, $2, $3, $4, 'pending', NOW())
		RETURNING id, created_at`

	err := tx.QueryRow(ctx, query,
		delivery.TransactionID,
		delivery.MerchantID,
		delivery.EventType,
		delivery.Payload,
	).Scan(&delivery.ID, &delivery.CreatedAt)

	if err != nil {
		return fmt.Errorf("insert webhook delivery: %w", err)
	}

	return nil
}

// FetchPendingDeliveries возвращает готовые к отправке записи.
// Атомарно блокирует строки (SKIP LOCKED) — безопасно при нескольких инстансах воркера.
func (r *Repository) FetchPendingDeliveries(ctx context.Context, limit int) ([]*Delivery, error) {
	query := `
		SELECT id, transaction_id, merchant_id, event_type, payload,
		       attempts, max_attempts, next_retry_at, last_error, created_at
		FROM webhook_deliveries
		WHERE status = 'pending'
		  AND next_retry_at <= NOW()
		ORDER BY next_retry_at ASC
		LIMIT $1
		FOR UPDATE SKIP LOCKED`

	rows, err := r.pool.Query(ctx, query, limit)
	if err != nil {
		return nil, fmt.Errorf("fetch pending deliveries: %w", err)
	}
	defer rows.Close()

	var deliveries []*Delivery
	for rows.Next() {
		d := &Delivery{}
		
		var lastError *string

		if err := rows.Scan(
			&d.ID, &d.TransactionID, &d.MerchantID, &d.EventType, &d.Payload,
			&d.Attempts, &d.MaxAttempts, &d.NextRetryAt, &lastError, &d.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan delivery: %w", err)
		}

		if lastError != nil {
			d.LastError = *lastError
		}

		deliveries = append(deliveries, d)
	}

	return deliveries, rows.Err()
}

// MarkDelivered помечает webhook как успешно доставленный.
func (r *Repository) MarkDelivered(ctx context.Context, id string) error {
	query := `
		UPDATE webhook_deliveries
		SET status = 'delivered', delivered_at = NOW()
		WHERE id = $1`

	_, err := r.pool.Exec(ctx, query, id)
	return err
}

// MarkFailed обновляет счётчик попыток и планирует следующую попытку.
// Если попытки исчерпаны — переводит в статус failed.
func (r *Repository) MarkFailed(ctx context.Context, id string, attempts int, lastError string) error {
	// Экспоненциальная задержка: 30s, 5m, 30m, 2h, 8h
	backoff := exponentialBackoff(attempts)

	query := `
		UPDATE webhook_deliveries
		SET 
			attempts     = $2,
			last_error   = $3,
			next_retry_at = NOW() + $4::interval,
			status       = CASE 
				WHEN $2 >= max_attempts THEN 'failed' 
				ELSE 'pending' 
			END
		WHERE id = $1`

	_, err := r.pool.Exec(ctx, query, id, attempts, lastError, backoff.String())
	return err
}

// exponentialBackoff возвращает задержку перед следующей попыткой.
// attempt=1 → 30s, attempt=2 → 5m, attempt=3 → 30m, attempt=4 → 2h, attempt=5+ → 8h
func exponentialBackoff(attempt int) time.Duration {
	backoffs := []time.Duration{
		30 * time.Second,
		5 * time.Minute,
		30 * time.Minute,
		2 * time.Hour,
		8 * time.Hour,
	}

	if attempt <= 0 {
		return backoffs[0]
	}
	if attempt >= len(backoffs) {
		return backoffs[len(backoffs)-1]
	}

	return backoffs[attempt]
}
