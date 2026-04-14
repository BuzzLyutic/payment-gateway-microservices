package middleware

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/redis/go-redis/v9"
)

const rateLimitKeyPrefix = "ratelimit"

// rateLimitScript — Lua-скрипт для атомарного скользящего окна.
//
// Алгоритм (Sorted Set sliding window):
//  1. Удаляем записи старше окна (1 минута)
//  2. Добавляем текущий запрос с score = timestamp_ms
//  3. Считаем количество записей в окне
//  4. Устанавливаем TTL на ключ (120s — окно + запас)
//  5. Возвращаем текущий count
//
// Lua гарантирует атомарность — все четыре операции выполняются
// как одна транзакция. Без Lua между ZREMRANGEBYSCORE и ZADD
// возможна гонка при параллельных запросах от одного мерчанта.
var rateLimitScript = redis.NewScript(`
local key       = KEYS[1]
local now       = tonumber(ARGV[1])
local window_ms = tonumber(ARGV[2])
local member    = ARGV[3]
local ttl_s     = tonumber(ARGV[4])

-- Удаляем записи старше окна
redis.call('ZREMRANGEBYSCORE', key, 0, now - window_ms)

-- Добавляем текущий запрос
redis.call('ZADD', key, now, member)

-- Считаем записи в окне
local count = redis.call('ZCARD', key)

-- Обновляем TTL
redis.call('EXPIRE', key, ttl_s)

return count
`)

// RateLimit проверяет лимит запросов для мерчанта.
// Использует скользящее окно 1 минута через Redis Sorted Set.
// Должен располагаться после Auth — использует MerchantInfo из контекста.
func RateLimit(rdb *redis.Client, logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			info := MerchantFromContext(r.Context())
			if info == nil {
				// Не должно происходить если Auth стоит перед RateLimit.
				// Защитная проверка — пропускаем без ограничений.
				logger.WarnContext(r.Context(), "ratelimit: no merchant in context, skipping")
				next.ServeHTTP(w, r)
				return
			}

			count, err := checkRateLimit(r.Context(), rdb, info.MerchantID)
			if err != nil {
				// Redis недоступен — fail-open: пропускаем запрос.
				// Аналогично velocity в risk-service: доступность важнее
				// строгого rate limiting при сбое инфраструктуры.
				logger.WarnContext(r.Context(), "ratelimit: redis unavailable, skipping",
					slog.String("merchant_id", info.MerchantID),
					slog.String("error", err.Error()),
				)
				next.ServeHTTP(w, r)
				return
			}

			if count > int64(info.RateLimit) {
				requestID := requestIDFromContext(r.Context())
				retryAfter := secondsUntilNextMinute()

				logger.InfoContext(r.Context(), "ratelimit: exceeded",
					slog.String("merchant_id", info.MerchantID),
					slog.String("request_id", requestID),
					slog.Int64("count", count),
					slog.Int("limit", info.RateLimit),
					slog.Int("retry_after_seconds", retryAfter),
				)

				w.Header().Set("Retry-After", fmt.Sprintf("%d", retryAfter))
				writeJSON(w, http.StatusTooManyRequests, map[string]any{
					"error":               "rate limit exceeded",
					"retry_after_seconds": retryAfter,
				})
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// checkRateLimit выполняет Lua-скрипт и возвращает текущий count.
func checkRateLimit(ctx context.Context, rdb *redis.Client, merchantID string) (int64, error) {
	now := time.Now().UnixMilli()
	windowMs := int64(60 * 1000) // 1 минута в миллисекундах
	ttlSeconds := 120            // TTL с запасом — 2 минуты

	key := fmt.Sprintf("%s:%s", rateLimitKeyPrefix, merchantID)

	// member уникален для каждого запроса — timestamp_ms + случайный суффикс
	// из request_id не нужен: нано-точность timestamp достаточна,
	// а при коллизии ZADD просто обновит score существующего member.
	// Для корректности используем timestamp в наносекундах как member.
	member := fmt.Sprintf("%d", time.Now().UnixNano())

	result, err := rateLimitScript.Run(
		ctx, rdb,
		[]string{key},
		now,
		windowMs,
		member,
		ttlSeconds,
	).Int64()
	if err != nil {
		return 0, fmt.Errorf("ratelimit: lua script: %w", err)
	}

	return result, nil
}

// secondsUntilNextMinute возвращает количество секунд до конца текущей минуты.
// Используется в заголовке Retry-After.
func secondsUntilNextMinute() int {
	now := time.Now()
	nextMinute := now.Truncate(time.Minute).Add(time.Minute)
	return int(nextMinute.Sub(now).Seconds()) + 1
}
