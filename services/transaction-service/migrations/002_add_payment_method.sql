ALTER TABLE transactions
ADD COLUMN IF NOT EXISTS payment_method VARCHAR(50) NOT NULL DEFAULT 'card';

CREATE INDEX IF NOT EXISTS idx_transactions_payment_method 
ON transactions(payment_method);