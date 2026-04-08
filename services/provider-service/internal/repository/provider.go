package repository

import (
	"context"
	"fmt"

	"github.com/BuzzLyutic/payment-gateway-microservices/services/provider-service/internal/domain"
	"github.com/jackc/pgx/v5/pgxpool"
)

type ProviderRepository struct {
	pool *pgxpool.Pool
}

func New(ctx context.Context, dsn string) (*ProviderRepository, error) {
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return nil, fmt.Errorf("pgxpool.New: %w", err)
	}

	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("pool.Ping: %w", err)
	}

	return &ProviderRepository{pool: pool}, nil
}

func (r *ProviderRepository) Close() {
	r.pool.Close()
}

// Ping проверяет соединение с БД. Для health check.
func (r *ProviderRepository) Ping(ctx context.Context) error {
	return r.pool.Ping(ctx)
}

// FindActive возвращает активных провайдеров, поддерживающих указанную валюту и метод оплаты.
func (r *ProviderRepository) FindActive(ctx context.Context, currency, paymentMethod string) ([]*domain.Provider, error) {
	query := `
		SELECT id, name, type, status, currencies, payment_methods,
		       commission_pct, config, created_at, updated_at
		FROM providers
		WHERE status = 'active'
		  AND $1 = ANY(currencies)
		  AND $2 = ANY(payment_methods)
		ORDER BY name
	`

	rows, err := r.pool.Query(ctx, query, currency, paymentMethod)
	if err != nil {
		return nil, fmt.Errorf("query FindActive: %w", err)
	}
	defer rows.Close()

	var providers []*domain.Provider
	for rows.Next() {
		p, err := scanProvider(rows)
		if err != nil {
			return nil, fmt.Errorf("scan provider: %w", err)
		}
		providers = append(providers, p)
	}

	return providers, rows.Err()
}

// GetByName возвращает провайдера по имени.
func (r *ProviderRepository) GetByName(ctx context.Context, name string) (*domain.Provider, error) {
	query := `
		SELECT id, name, type, status, currencies, payment_methods,
		       commission_pct, config, created_at, updated_at
		FROM providers
		WHERE name = $1
	`

	row := r.pool.QueryRow(ctx, query, name)

	p := &domain.Provider{}
	err := row.Scan(
		&p.ID, &p.Name, &p.Type, &p.Status,
		&p.Currencies, &p.PaymentMethods,
		&p.CommissionPct, &p.Config,
		&p.CreatedAt, &p.UpdatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("query GetByName: %w", err)
	}

	return p, nil
}

// FindAll возвращает всех провайдеров без фильтрации.
// Используется при старте для инициализации registry.
func (r *ProviderRepository) FindAll(ctx context.Context) ([]*domain.Provider, error) {
	query := `
		SELECT id, name, type, status, currencies, payment_methods,
		       commission_pct, config, created_at, updated_at
		FROM providers
		ORDER BY name
	`

	rows, err := r.pool.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("query FindAll: %w", err)
	}
	defer rows.Close()

	var providers []*domain.Provider
	for rows.Next() {
		p, err := scanProvider(rows)
		if err != nil {
			return nil, fmt.Errorf("scan provider: %w", err)
		}
		providers = append(providers, p)
	}

	return providers, rows.Err()
}

// scanProvider сканирует строку результата в domain.Provider.
func scanProvider(rows interface{ Scan(dest ...any) error }) (*domain.Provider, error) {
	p := &domain.Provider{}
	err := rows.Scan(
		&p.ID, &p.Name, &p.Type, &p.Status,
		&p.Currencies, &p.PaymentMethods,
		&p.CommissionPct, &p.Config,
		&p.CreatedAt, &p.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	return p, nil
}
