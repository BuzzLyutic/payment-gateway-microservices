package consumer

import "github.com/nats-io/nats.go/jetstream"

// ExportHandle открывает handle для тестирования без реального NATS.
func (c *Consumer) ExportHandle(msg jetstream.Msg) {
	c.handle(msg)
}
