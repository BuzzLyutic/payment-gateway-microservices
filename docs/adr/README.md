# Architecture Decision Records

Журнал архитектурных решений платформы.
Каждый ADR фиксирует одно решение: контекст, выбор, альтернативы, последствия.

| # | Решение | Статус |
|---|---------|--------|
| [001](001-event-driven-architecture.md) | Event-Driven Architecture через NATS JetStream | Accepted |
| [002](002-thompson-sampling-routing.md) | Thompson Sampling для адаптивной маршрутизации | Accepted |
| [003](003-nats-jetstream.md) | NATS JetStream как брокер сообщений | Accepted |
| [004](004-risk-service-fail-open.md) | Fail-open стратегия Risk Service при недоступности Redis | Accepted |
| [005](005-outbox-pattern-webhooks.md) | Outbox Pattern для webhook-уведомлений | Accepted |
| [006](006-database-per-service.md) | Database per Service | Accepted |