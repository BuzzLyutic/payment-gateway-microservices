package idempotency

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

const defaultTTL = 24 * time.Hour

// ErrDuplicate - запрос с таким ключом уже обработан.
var ErrDuplicate = errors.New("duplicate idempotency key")

// CachedResponse - то, что сохраняем в Redis.
// Храним полный HTTP-ответ, чтобы при повторном запросе вернуть ровно то же самое.
type CachedResponse struct {
	StatusCode int    `json:"status_code"`
	Body       []byte `json:"body"`
}

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

// Check проверяет, есть ли результат для данного ключа.
// Возвращает nil, nil если ключ не найден (первый запрос).
// Возвращает CachedResponse, nil если ключ найден (повторный запрос).
func (s *Store) Check(ctx context.Context, key string) (*CachedResponse, error) {
	data, err := s.client.Get(ctx, s.redisKey(key)).Bytes()
	if errors.Is(err, redis.Nil) {
		return nil, nil // ключа нет - первый запрос
	}
	if err != nil {
		return nil, fmt.Errorf("redis get: %w", err)
	}

	var cached CachedResponse
	if err := json.Unmarshal(data, &cached); err != nil {
		return nil, fmt.Errorf("unmarshal cached response: %w", err)
	}

	return &cached, nil
}

// Save сохраняет результат обработки запроса в Redis с TTL 24 часа.
func (s *Store) Save(ctx context.Context, key string, statusCode int, body []byte) error {
	cached := CachedResponse{
		StatusCode: statusCode,
		Body:       body,
	}

	data, err := json.Marshal(cached)
	if err != nil {
		return fmt.Errorf("marshal cached response: %w", err)
	}

	if err := s.client.Set(ctx, s.redisKey(key), data, s.ttl).Err(); err != nil {
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
