ALTER TABLE transactions
ADD COLUMN IF NOT EXISTS card_hash      VARCHAR(64),
ADD COLUMN IF NOT EXISTS customer_ip    VARCHAR(45),
ADD COLUMN IF NOT EXISTS customer_email VARCHAR(255);

CREATE INDEX IF NOT EXISTS idx_transactions_card_hash
ON transactions(card_hash) WHERE card_hash IS NOT NULL;

CREATE INDEX IF NOT EXISTS idx_transactions_customer_ip
ON transactions(customer_ip) WHERE customer_ip IS NOT NULL;