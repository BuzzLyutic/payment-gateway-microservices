// только для тестов
package consumer

import "github.com/nats-io/nats.go/jetstream"

// Экспортируем для тестов без изменения публичного API.
var MapCompletedStatus = mapCompletedStatus

type MockableMsg = jetstream.Msg

func (c *Consumer) Handle(msg jetstream.Msg)           { c.handle(msg) }
func (c *Consumer) HandleCompleted(msg jetstream.Msg)  { c.handleCompleted(msg) }
func (c *Consumer) HandleRiskBlocked(msg jetstream.Msg) { c.handleRiskBlocked(msg) }
