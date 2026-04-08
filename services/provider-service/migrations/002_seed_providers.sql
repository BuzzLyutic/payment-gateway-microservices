INSERT INTO providers (name, type, status, currencies, payment_methods, commission_pct, config)
VALUES
    (
        'mock_provider_a',
        'mock',
        'active',
        ARRAY['RUB', 'USD', 'EUR'],
        ARRAY['card', 'sbp'],
        2.500,
        '{"success_rate": 95, "min_latency_ms": 150, "max_latency_ms": 250}'
    ),
    (
        'mock_provider_b',
        'mock',
        'active',
        ARRAY['RUB', 'USD'],
        ARRAY['card'],
        1.500,
        '{"success_rate": 85, "min_latency_ms": 70, "max_latency_ms": 130}'
    ),
    (
        'mock_provider_c',
        'mock',
        'active',
        ARRAY['RUB'],
        ARRAY['card', 'sbp'],
        1.000,
        '{"success_rate": 75, "min_latency_ms": 30, "max_latency_ms": 70}'
    )
ON CONFLICT (name) DO NOTHING;
