package auth

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"strconv"

	"github.com/redis/go-redis/v9"
)

const (
	keyPrefix = "apikeys"
)

var (
	ErrMissingKey = errors.New("missing API key")
	ErrInvalidKey = errors.New("invalid or inactive API key")
)

// MerchantInfo — данные мерчанта, извлечённые из Redis по API-ключу.
type MerchantInfo struct {
	MerchantID string
	Name       string
	RateLimit  int
}

// Store отвечает за lookup API-ключей в Redis.
type Store struct {
	rdb          *redis.Client
	defaultLimit int
}

func NewStore(rdb *redis.Client, defaultLimit int) *Store {
	return &Store{rdb: rdb, defaultLimit: defaultLimit}
}

// Lookup проверяет API-ключ и возвращает данные мерчанта.
func (s *Store) Lookup(ctx context.Context, apiKey string) (*MerchantInfo, error) {
	if apiKey == "" {
		return nil, ErrMissingKey
	}

	redisKey := fmt.Sprintf("%s:%s", keyPrefix, HashKey(apiKey))

	fields, err := s.rdb.HGetAll(ctx, redisKey).Result()
	if err != nil {
		return nil, fmt.Errorf("auth: redis hgetall: %w", err)
	}

	if len(fields) == 0 {
		return nil, ErrInvalidKey
	}

	if fields["active"] != "true" {
		return nil, ErrInvalidKey
	}

	merchantID := fields["merchant_id"]
	if merchantID == "" {
		return nil, ErrInvalidKey
	}

	rateLimit := s.defaultLimit
	if raw, ok := fields["rate_limit"]; ok && raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n > 0 {
			rateLimit = n
		}
	}

	return &MerchantInfo{
		MerchantID: merchantID,
		Name:       fields["name"],
		RateLimit:  rateLimit,
	}, nil
}

// HashKey вычисляет SHA-256 хеш API-ключа.
// Экспортирована для использования в seed-скриптах и тестах.
// В Redis хранится хеш — plain-text ключи не утекают при компрометации Redis.
func HashKey(apiKey string) string {
	sum := sha256.Sum256([]byte(apiKey))
	return fmt.Sprintf("%x", sum)
}
