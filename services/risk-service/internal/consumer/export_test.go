package consumer

import (
	"context"

	"github.com/nats-io/nats.go/jetstream"
)

// ExportHandleMessage открывает handleMessage для white-box тестирования.
// Только для тестов — не использовать в продакшн коде.
func (c *Consumer) ExportHandleMessage(ctx context.Context, msg jetstream.Msg) {
	c.handleMessage(ctx, msg)
}

// ExportIsContextError открывает isContextError для тестирования.
func ExportIsContextError(err error) bool {
	return isContextError(err)
}
