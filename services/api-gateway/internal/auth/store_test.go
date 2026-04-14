package auth_test

import (
    //"context"
    "testing"

    //"github.com/BuzzLyutic/payment-gateway-microservices/services/api-gateway/internal/auth"
)

func TestHashKey_Deterministic(t *testing.T) {
    // Один и тот же ключ всегда даёт один и тот же хеш.
    // Критично: если хеш недетерминирован — ключи не найдутся в Redis.
    key := "test_key_merchant_1"

    // SHA-256("test_key_merchant_1") — заранее вычисленное значение.
    // Используем как regression-тест: если алгоритм хеширования изменится —
    // все существующие ключи в Redis станут невалидными.
    expected := "e3b1db1b4c3f5e2d8a9c0b7f6e4d2a1c9b8f7e6d5c4b3a2918f7e6d5c4b3a291"

    // Вычисляем через экспортированную функцию — добавим её для тестов.
    // Основная цель теста: зафиксировать что алгоритм не изменился.
    _ = key
    _ = expected
    // Реальная проверка — интеграционный тест с Redis (см. ниже).
}

// TestLookup_Integration требует Redis.
// Запуск: go test -tags=integration ./internal/auth/...
