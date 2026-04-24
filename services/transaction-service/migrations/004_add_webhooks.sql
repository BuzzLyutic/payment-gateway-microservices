-- Таблица мерчантов
-- Хранит бизнес-конфигурацию: webhook URL и секрет для HMAC-подписи.
-- Отдельно от Redis (apikeys) — там оперативные данные (rate limit, auth).
-- Здесь — персистентная конфигурация, которая не должна теряться при перезапуске Redis.
CREATE TABLE merchants (
    id              VARCHAR(64) PRIMARY KEY,
    webhook_url     TEXT,
    webhook_secret  VARCHAR(128),
    is_active       BOOLEAN NOT NULL DEFAULT true,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Таблица очереди доставки webhook-уведомлений.
-- Реализует паттерн Outbox: запись создаётся атомарно с обновлением статуса транзакции.
-- Отдельный worker читает pending-записи и доставляет HTTP POST мерчанту.
CREATE TABLE webhook_deliveries (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    transaction_id  UUID NOT NULL REFERENCES transactions(id),
    merchant_id     VARCHAR(64) NOT NULL REFERENCES merchants(id),
    event_type      VARCHAR(64) NOT NULL,
    payload         JSONB NOT NULL,
    status          VARCHAR(20) NOT NULL DEFAULT 'pending',
    attempts        INTEGER NOT NULL DEFAULT 0,
    max_attempts    INTEGER NOT NULL DEFAULT 5,
    next_retry_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    last_error      TEXT,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    delivered_at    TIMESTAMPTZ,

    CONSTRAINT chk_status CHECK (status IN ('pending', 'delivered', 'failed'))
);

-- Индекс для воркера: быстрый выбор pending-записей, готовых к отправке.
-- Partial index — включает только строки со статусом pending (меньше размер, быстрее поиск).
CREATE INDEX idx_webhook_deliveries_pending
    ON webhook_deliveries(next_retry_at)
    WHERE status = 'pending';

-- Индекс для аналитики и отладки по транзакции.
CREATE INDEX idx_webhook_deliveries_transaction
    ON webhook_deliveries(transaction_id);

-- Seed тестовых мерчантов.
-- webhook_url указывает на локальный echo-сервер для разработки.
-- В production URL регистрируется мерчантом через API управления.
INSERT INTO merchants (id, webhook_url, webhook_secret) VALUES
    ('merchant_001', 'http://webhook-tester:8080/webhook', 'test_secret_merchant_1'),
    ('merchant_002', 'http://webhook-tester:8080/webhook', 'test_secret_merchant_2');