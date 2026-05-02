// Общие пороги качества.
// Если порог нарушен — k6 завершается с ненулевым кодом (CI fails).
export const thresholds = {
  // 95% запросов быстрее 5 секунд (с учётом async обработки)
  http_req_duration: ['p(95)<5000', 'p(99)<10000'],
  // Допускаем до 25% — включает легитимные 429 от rate limiter
  http_req_failed: ['rate<0.25'],
  // Кастомная метрика успешных транзакций (200/201)
  payment_success: ['rate>0.70'],
};

// Ступенчатая нагрузка.
export const stages = {
  // Быстрая проверка работоспособности для CI (~30s)
  smoke: [
    { duration: '30s', target: 5 },
  ],
  // Разогрев перед основным тестом
  warmup: [
    { duration: '30s', target: 10 },
  ],
  // Ступенчатая нагрузка — каждая ступень даёт строку в таблице результатов.
  // Реальные результаты: p95 стабильно 7-8ms до 200 VU, деградация на 500 VU.
  load: [
    { duration: '60s', target: 10  }, // baseline
    { duration: '30s', target: 50  }, // ramp up
    { duration: '60s', target: 50  }, // 50 VU
    { duration: '30s', target: 100 }, // ramp up
    { duration: '60s', target: 100 }, // 100 VU
    { duration: '30s', target: 200 }, // ramp up
    { duration: '60s', target: 200 }, // 200 VU
    { duration: '30s', target: 0   }, // cooldown
  ],
};

// API конфигурация — переопределяется через BASE_URL env переменную.
export const config = {
  baseURL: __ENV.BASE_URL || 'http://localhost:8080',
  apiPath: '/api/v1/payments',
};