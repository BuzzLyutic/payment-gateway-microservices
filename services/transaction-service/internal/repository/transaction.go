package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"

	"github.com/BuzzLyutic/payment-gateway-microservices/services/transaction-service/internal/domain"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type TransactionRepository struct {
	pool *pgxpool.Pool
}

func New(ctx context.Context, dsn string) (*TransactionRepository, error) {
	config, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}
	config.MaxConns = 10
	config.MinConns = 2
	
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		return nil, fmt.Errorf("failed creating new pool: %w", err)
	}

	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("database ping failed: %w", err)
	}

	slog.Info("successfully connected to PostgreSQL")

	return &TransactionRepository{
		pool: pool,
	}, nil
}

func (tr *TransactionRepository) Ping(ctx context.Context) error {
	return tr.pool.Ping(ctx)
}

func (tr *TransactionRepository) Close() {
	tr.pool.Close()
}


// Create вставляет новую транзакцию и возвращает её с заполненными id, created_at, updated_at.
func (tr *TransactionRepository) Create(ctx context.Context, tx *domain.Transaction) error {
	metadataJSON, err := json.Marshal(tx.Metadata)
	if err != nil {
		return fmt.Errorf("marshal metadata: %w", err)
	}

	query := `
		INSERT INTO transactions (
			idempotency_key, merchant_id, amount, currency, payment_method,
			status, description, card_hash, customer_ip, customer_email, metadata
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
		RETURNING id, created_at, updated_at`

	err = tr.pool.QueryRow(ctx, query,
		tx.IdempotencyKey,
		tx.MerchantID,
		tx.Amount,
		tx.Currency,
		tx.PaymentMethod,
		tx.Status,
		tx.Description,
		tx.CardHash,
		tx.CustomerIP,
		tx.CustomerEmail,
		metadataJSON,
	).Scan(&tx.ID, &tx.CreatedAt, &tx.UpdatedAt)

	if err != nil {
		return fmt.Errorf("insert transaction: %w", err)
	}

	return nil
}


// GetByID возвращает транзакцию по ID.
func (r *TransactionRepository) GetByID(ctx context.Context, id string) (*domain.Transaction, error) {
	query := `
		SELECT id, idempotency_key, merchant_id, amount, currency, payment_method,
		       status, description, provider, provider_tx_id,
		       error_message, card_hash, customer_ip, customer_email,
       		   metadata, created_at, updated_at
		FROM transactions
		WHERE id = $1`

	tx := &domain.Transaction{}
	var metadataJSON []byte

	err := r.pool.QueryRow(ctx, query, id).Scan(
		&tx.ID, &tx.IdempotencyKey, &tx.MerchantID,
		&tx.Amount, &tx.Currency, &tx.PaymentMethod,
		&tx.Status,
		&tx.Description, &tx.Provider, &tx.ProviderTxID,
		&tx.ErrorMessage,
		&tx.CardHash, &tx.CustomerIP, &tx.CustomerEmail,
		&metadataJSON,
		&tx.CreatedAt, &tx.UpdatedAt,
	)

	if errors.Is(err, pgx.ErrNoRows) {
		return nil, domain.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("query transaction: %w", err)
	}

	// Парсим JSONB - map
	if metadataJSON != nil {
		if err := json.Unmarshal(metadataJSON, &tx.Metadata); err != nil {
			return nil, fmt.Errorf("unmarshal metadata: %w", err)
		}
	}

	return tx, nil
}


// FetchPending атомарно выбирает pending-транзакции и переводит их в processing.
// FOR UPDATE SKIP LOCKED безопасно при нескольких инстансах воркера.
func (r *TransactionRepository) FetchPending(ctx context.Context, limit int) ([]*domain.Transaction, error) {
	query := `
		WITH pending AS (
			SELECT id
			FROM transactions
			WHERE status = 'pending'
			ORDER BY created_at ASC
			LIMIT $1
			FOR UPDATE SKIP LOCKED
		)
		UPDATE transactions t
		SET status = 'processing'
		FROM pending p
		WHERE t.id = p.id
		RETURNING t.id, t.idempotency_key, t.merchant_id, t.amount, t.currency,
		          t.payment_method, t.status, t.description, t.provider, t.provider_tx_id,
		          t.error_message, t.card_hash, t.customer_ip, t.customer_email,
				  t.metadata, t.created_at, t.updated_at`

	rows, err := r.pool.Query(ctx, query, limit)
	if err != nil {
		return nil, fmt.Errorf("fetch pending: %w", err)
	}
	defer rows.Close()

	var txns []*domain.Transaction
	for rows.Next() {
		tx := &domain.Transaction{}
		var metadataJSON []byte

		err := rows.Scan(
			&tx.ID, &tx.IdempotencyKey, &tx.MerchantID,
			&tx.Amount, &tx.Currency, &tx.PaymentMethod, 
			&tx.Status,
			&tx.Description, &tx.Provider, &tx.ProviderTxID,
			&tx.ErrorMessage, 
			&tx.CardHash, &tx.CustomerIP, &tx.CustomerEmail,
			&metadataJSON,
			&tx.CreatedAt, &tx.UpdatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("scan transaction: %w", err)
		}

		if metadataJSON != nil {
			if err := json.Unmarshal(metadataJSON, &tx.Metadata); err != nil {
				return nil, fmt.Errorf("unmarshal metadata: %w", err)
			}
		}

		txns = append(txns, tx)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows iteration: %w", err)
	}

	return txns, nil
}

// UpdateStatus обновляет статус и поля провайдера.
func (r *TransactionRepository) UpdateStatus(
	ctx context.Context,
	id string,
	status domain.Status,
	providerName *string,
	providerTxID *string,
	errorMessage *string,
) error {
	query := `
		UPDATE transactions
		SET status = $2, provider = $3, provider_tx_id = $4, error_message = $5
		WHERE id = $1`

	result, err := r.pool.Exec(ctx, query, id, status, providerName, providerTxID, errorMessage)
	if err != nil {
		return fmt.Errorf("update status: %w", err)
	}

	if result.RowsAffected() == 0 {
		return domain.ErrNotFound
	}

	return nil
}
