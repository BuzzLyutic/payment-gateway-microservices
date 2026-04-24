INSERT INTO providers (name, type, status, currencies, payment_methods, commission_pct, config)
VALUES (
    'stripe_sandbox',
    'stripe',
    'active',
    ARRAY['RUB', 'USD', 'EUR'],
    ARRAY['card'],
    1.4,  -- реальная комиссия Stripe: 1.4% для европейских карт
    '{}'::jsonb
)
ON CONFLICT (name) DO NOTHING;