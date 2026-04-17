package router

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

const (
	// Префикс ключей в Redis
	redisKeyPrefix = "thompson:provider:"

	// TTL ключей — 7 дней.
	// Если провайдер не использовался неделю — статистика устарела.
	redisTTL = 7 * 24 * time.Hour

	// Сохраняем в Redis каждые N транзакций на провайдера.
	// Компромисс: 10 — не слишком часто (не нагружаем Redis),
	// не слишком редко (при рестарте теряем максимум 10 транзакций).
	persistEvery = 10
)

// statsSnapshot — сериализуемое представление ProviderStats для Redis.
// Отдельная структура от ProviderStats чтобы не тащить mutex в JSON.
type statsSnapshot struct {
	Alpha     float64   `json:"alpha"`
	Beta      float64   `json:"beta"`
	Latencies []float64 `json:"latencies"`
}

// Store управляет персистентностью статистики Thompson Sampling в Redis.
type Store struct {
	client *redis.Client
}

func NewStore(addr, password string, db int) *Store {
	client := redis.NewClient(&redis.Options{
		Addr:     addr,
		Password: password,
		DB:       db,
	})
	return &Store{client: client}
}

// Ping проверяет соединение с Redis.
func (s *Store) Ping(ctx context.Context) error {
	return s.client.Ping(ctx).Err()
}

// Close закрывает соединение.
func (s *Store) Close() error {
	return s.client.Close()
}

// Save сохраняет статистику провайдера в Redis.
func (s *Store) Save(ctx context.Context, providerName string, alpha, beta float64, latencies []float64) error {
	snapshot := statsSnapshot{
		Alpha:     alpha,
		Beta:      beta,
		Latencies: latencies,
	}

	data, err := json.Marshal(snapshot)
	if err != nil {
		return fmt.Errorf("marshal stats: %w", err)
	}

	key := redisKeyPrefix + providerName
	if err := s.client.Set(ctx, key, data, redisTTL).Err(); err != nil {
		return fmt.Errorf("redis set %s: %w", key, err)
	}

	return nil
}

// Load загружает статистику провайдера из Redis.
// Возвращает nil, nil если ключ не найден — это нормально для нового провайдера.
func (s *Store) Load(ctx context.Context, providerName string) (*statsSnapshot, error) {
	key := redisKeyPrefix + providerName

	data, err := s.client.Get(ctx, key).Bytes()
	if err == redis.Nil {
		// Ключ не найден — новый провайдер, начинаем с априорного Beta(1,1)
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("redis get %s: %w", key, err)
	}

	var snapshot statsSnapshot
	if err := json.Unmarshal(data, &snapshot); err != nil {
		return nil, fmt.Errorf("unmarshal stats: %w", err)
	}

	return &snapshot, nil
}

// LoadAll загружает статистику для списка провайдеров при старте сервиса.
// Провайдеры без сохранённой статистики получают априорное Beta(1,1).
func (s *Store) LoadAll(ctx context.Context, providerNames []string) (map[string]*statsSnapshot, error) {
	result := make(map[string]*statsSnapshot, len(providerNames))

	for _, name := range providerNames {
		snapshot, err := s.Load(ctx, name)
		if err != nil {
			// Логируем но не прерываем — лучше начать с нуля чем упасть
			return nil, fmt.Errorf("load stats for %s: %w", name, err)
		}
		if snapshot != nil {
			result[name] = snapshot
		}
	}

	return result, nil
}
