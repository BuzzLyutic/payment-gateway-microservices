-- Таблица провайдеров
CREATE TABLE IF NOT EXISTS providers (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name            VARCHAR(100) UNIQUE NOT NULL,
    type            VARCHAR(50) NOT NULL DEFAULT 'mock',
    status          VARCHAR(20) NOT NULL DEFAULT 'active',
    currencies      TEXT[] NOT NULL DEFAULT '{}',
    payment_methods TEXT[] NOT NULL DEFAULT '{}',
    commission_pct  DECIMAL(5,3) NOT NULL DEFAULT 0.000,
    config          JSONB NOT NULL DEFAULT '{}',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Индексы
CREATE INDEX IF NOT EXISTS idx_providers_status ON providers(status);

-- Автообновление updated_at
CREATE OR REPLACE FUNCTION update_updated_at_column()
RETURNS TRIGGER AS $$
BEGIN
    NEW.updated_at = NOW();
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE OR REPLACE TRIGGER trigger_providers_updated_at
    BEFORE UPDATE ON providers
    FOR EACH ROW
    EXECUTE FUNCTION update_updated_at_column();
