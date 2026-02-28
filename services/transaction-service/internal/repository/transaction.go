package repository

import (
	"context"
	"fmt"
	"log/slog"

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
