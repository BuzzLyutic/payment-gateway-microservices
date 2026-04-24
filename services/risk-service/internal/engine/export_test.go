package engine

import (
	"context"
	"time"

	"github.com/redis/go-redis/v9"
)

// ExportCheckAndIncrement открывает checkAndIncrement для тестирования.
func ExportCheckAndIncrement(
	ctx context.Context,
	rdb *redis.Client,
	key string,
	window time.Duration,
	threshold int,
) (bool, error) {
	return checkAndIncrement(ctx, rdb, key, window, threshold)
}
