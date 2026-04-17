package engine

import (
	"context"
	"errors"
	"fmt"
	"net"
	"time"

	"github.com/BuzzLyutic/payment-gateway-microservices/services/risk-service/internal/domain"
	"github.com/BuzzLyutic/payment-gateway-microservices/services/risk-service/internal/events"
	"github.com/BuzzLyutic/payment-gateway-microservices/services/risk-service/internal/loader"
	"github.com/redis/go-redis/v9"
)

const redisKeyPrefix = "velocity"

// evaluateVelocity возвращает (сработало, ошибка).
// При недоступности Redis возвращает (false, ErrRedisUnavailable) —
// fail-open: velocity-правило пропускается, simple-правила продолжают работать.
func evaluateVelocity(
	ctx context.Context,
	rule domain.Rule,
	event events.PaymentCreated,
	rdb *redis.Client,
) (bool, error) {
	keyValue, ok := extractKeyField(rule.KeyField, event)
	if !ok {
		// поле nil (card_hash, customer_ip не переданы) —
		// нет данных для проверки, не блокируем
		return false, nil
	}

	window, err := loader.ParseWindow(rule.Window)
	if err != nil {
		// уже провалидировано в loader, сюда не должны попасть
		return false, fmt.Errorf("velocity: parse window: %w", err)
	}

	key := buildKey(rule.KeyField, keyValue)

	triggered, err := checkAndIncrement(ctx, rdb, key, window, rule.Threshold)
	if err != nil {
		if isRedisUnavailable(err) {
			return false, domain.ErrRedisUnavailable
		}
		return false, fmt.Errorf("velocity: redis: %w", err)
	}

	return triggered, nil
}

// checkAndIncrement — Вариант A: INCR + EXPIRE.
// Атомарно инкрементирует счётчик, устанавливает TTL при первом создании,
// возвращает true если count > threshold.
func checkAndIncrement(
	ctx context.Context,
	rdb *redis.Client,
	key string,
	window time.Duration,
	threshold int,
) (bool, error) {
	// INCR атомарен в Redis — безопасно при параллельных запросах.
	count, err := rdb.Incr(ctx, key).Result()
	if err != nil {
		return false, err
	}

	// TTL устанавливаем только при первом создании ключа (count == 1).
	// Если ставить EXPIRE на каждый INCR — окно будет сдвигаться
	// при каждой транзакции, что некорректно.
	if count == 1 {
		if err := rdb.Expire(ctx, key, window).Err(); err != nil {
			return false, err
		}
	}

	return count > int64(threshold), nil
}

// extractKeyField возвращает строковое значение ключевого поля и флаг наличия.
// Пустая строка трактуется как отсутствие данных — velocity-правило пропускается.
func extractKeyField(keyField string, event events.PaymentCreated) (string, bool) {
	switch keyField {
	case "merchant_id":
		// merchant_id обязателен — всегда присутствует
		if event.MerchantID == "" {
			return "", false
		}
		return event.MerchantID, true
	case "card_hash":
		if event.CardHash == "" {
			return "", false
		}
		return event.CardHash, true
	case "customer_ip":
		if event.CustomerIP == "" {
			return "", false
		}
		return event.CustomerIP, true
	default:
		return "", false
	}
}

// buildKey формирует Redis-ключ по спецификации:
// velocity:{key_field}:{key_value}
func buildKey(keyField, keyValue string) string {
	return fmt.Sprintf("%s:%s:%s", redisKeyPrefix, keyField, keyValue)
}

// isRedisUnavailable определяет, является ли ошибка недоступностью Redis,
// а не логической ошибкой (неверный тип, контекст отменён и т.д.)
func isRedisUnavailable(err error) bool {
	if err == nil {
		return false
	}
	// context.DeadlineExceeded и context.Canceled — не недоступность Redis,
	// а отмена со стороны вызывающего кода. Не трактуем как fail-open.
	if errors.Is(err, context.DeadlineExceeded) ||
		errors.Is(err, context.Canceled) {
		return false
	}
	// redis.ErrClosed — клиент закрыт
	if errors.Is(err, redis.ErrClosed) {
		return true
	}
	// Сетевые ошибки подключения — Redis недоступен
	var netErr net.Error
	if errors.As(err, &netErr) {
		return true
	}
	return true
}
