// Тестовые мерчанты из seed данных.
// Каждый мерчант имеет свой rate limit bucket — чередуем чтобы не упираться в лимит.
const merchants = [
  {
    apiKey:     'test_key_merchant_1',
    merchantID: 'merchant_001',
  },
  {
    apiKey:     'test_key_merchant_2',
    merchantID: 'merchant_002',
  },
];

// Выбираем мерчанта по номеру VU — каждый VU всегда использует одного мерчанта.
// Это важно для корректного тестирования rate limiting:
// если один VU быстро исчерпает лимит, тест продолжается на другом мерчанте.
export function getMerchant(vuID) {
  return merchants[vuID % merchants.length];
}

// Стандартные заголовки для всех запросов.
export function headers(apiKey, idempotencyKey) {
  return {
    'Content-Type':       'application/json',
    'X-API-Key':          apiKey,
    'X-Idempotency-Key':  idempotencyKey,
  };
}

// Уникальный idempotency key для каждого запроса.
// Формат: {vuID}-{iteration}-{timestamp}
// Гарантирует уникальность даже при параллельных VU.
export function idempotencyKey(vuID, iteration) {
  return `load-test-${vuID}-${iteration}-${Date.now()}`;
}
