package idempotency

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

const defaultTTL = 24 * time.Hour

type Store struct {
	client *redis.Client
	ttl    time.Duration
}

func NewStore(client *redis.Client) *Store {
	return &Store{
		client: client,
		ttl:    defaultTTL,
	}
}

// Check возвращает transaction ID если ключ уже использовался.
// Возвращает "", nil если ключ новый.
func (s *Store) Check(ctx context.Context, key string) (string, error) {
	txID, err := s.client.Get(ctx, s.redisKey(key)).Result()
	if errors.Is(err, redis.Nil) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("redis get: %w", err)
	}

	return txID, nil
}

// Save сохраняет связку idempotency_key → transaction_id.
func (s *Store) Save(ctx context.Context, key string, transactionID string) error {
	if err := s.client.Set(ctx, s.redisKey(key), transactionID, s.ttl).Err(); err != nil {
		return fmt.Errorf("redis set: %w", err)
	}
	return nil
}

// Ping проверяет доступность Redis. Используется в health check.
func (s *Store) Ping(ctx context.Context) error {
	return s.client.Ping(ctx).Err()
}

// Close закрывает соединение с Redis.
func (s *Store) Close() error {
	return s.client.Close()
}

// redisKey - добавляем префикс, чтобы не конфликтовать с другими данными в Redis.
func (s *Store) redisKey(key string) string {
	return "idempotency:" + key
}
