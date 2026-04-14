package idempotency

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

const (
	defaultTTL      = 24 * time.Hour
	lockPlaceholder = "processing"
)

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

// Lock пытается атомарно захватить ключ идемпотентности.
// Возвращает true если ключ новый (можно создавать транзакцию).
// Возвращает false если ключ уже существует (дубль).
func (s *Store) Lock(ctx context.Context, key string) (bool, error) {
	result, err := s.client.SetArgs(ctx, s.redisKey(key), lockPlaceholder, redis.SetArgs{
		Mode: "NX",
		TTL:  s.ttl,
	}).Result()

	if errors.Is(err, redis.Nil) {
		return false, nil // ключ уже существует
	}
	if err != nil {
		return false, fmt.Errorf("redis set nx: %w", err)
	}

	// result == "OK" - ключ успешно установлен
	return result == "OK", nil
}

// SetTransactionID заменяет placeholder "processing" на реальный ID транзакции.
// Вызывается после успешного INSERT в БД.
func (s *Store) SetTransactionID(ctx context.Context, key string, txID string) error {
	_, err := s.client.SetArgs(ctx, s.redisKey(key), txID, redis.SetArgs{
		Mode: "XX",
		TTL:  s.ttl,
	}).Result()

	if errors.Is(err, redis.Nil) {
		// Ключ исчез между Lock и SetTransactionID — маловероятно, но безопасно
		return nil
	}
	if err != nil {
		return fmt.Errorf("redis set xx: %w", err)
	}
	return nil
}

// GetTransactionID возвращает ID транзакции по ключу идемпотентности.
// Возвращает "", nil если ключ не найден.
// Возвращает "processing", nil если транзакция ещё создаётся.
func (s *Store) GetTransactionID(ctx context.Context, key string) (string, error) {
	val, err := s.client.Get(ctx, s.redisKey(key)).Result()
	if errors.Is(err, redis.Nil) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("redis get: %w", err)
	}
	return val, nil
}

// Unlock удаляет ключ. Вызывается если INSERT в БД не удался -
// чтобы следующая попытка могла пройти.
func (s *Store) Unlock(ctx context.Context, key string) error {
	return s.client.Del(ctx, s.redisKey(key)).Err()
}

func (s *Store) Ping(ctx context.Context) error {
	return s.client.Ping(ctx).Err()
}

func (s *Store) Close() error {
	return s.client.Close()
}

func (s *Store) redisKey(key string) string {
	return "idempotency:" + key
}
