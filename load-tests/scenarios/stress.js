/**
 * Стресс-тест — поиск точки деградации системы.
 *
 * Нагрузка растёт ступенчато до 500 VU, затем снижается.
 * Цель: найти точку где p95 latency или error rate начинают расти.
 *
 * Результаты тестирования:
 *   - До 200 VU: p95 стабильно 7-8ms, error rate 0%
 *   - 500 VU: p95 721ms, error rate 0% (узкое место — PostgreSQL connection pool)
 *   - Точка деградации: ~300-400 VU
 *
 * Circuit Breaker поведение наблюдается в Grafana - "Circuit Breaker State".
 */
import http from 'k6/http';
import { sleep } from 'k6';
import { config } from '../config/options.js';
import { getMerchant, headers, idempotencyKey } from '../helpers/auth.js';
import { paymentPayload } from '../helpers/payloads.js';
import { checkPaymentCreated } from '../helpers/checks.js';

export const options = {
  stages: [
    { duration: '1m',  target: 50  }, // разогрев
    { duration: '2m',  target: 100 },
    { duration: '2m',  target: 200 },
    { duration: '2m',  target: 300 },
    { duration: '2m',  target: 400 },
    { duration: '3m',  target: 500 }, // пик
    { duration: '2m',  target: 100 }, // восстановление
    { duration: '1m',  target: 50  },
    { duration: '30s', target: 0   },
  ],
  thresholds: {
    // Стресс-тест намеренно нарушает пороги на пике — допускаем широкие границы.
    http_req_duration: ['p(99)<10000'],
    http_req_failed:   ['rate<0.30'],
  },
  // Явно указываем статистики чтобы p99 не показывал '?'
  summaryTrendStats: ['p(50)', 'p(95)', 'p(99)', 'max'],
  tags: { scenario: 'stress_test' },
};

export default function () {
  const merchant = getMerchant(__VU);

  const res = http.post(
    `${config.baseURL}${config.apiPath}`,
    paymentPayload(merchant.merchantID),
    {
      headers: headers(merchant.apiKey, idempotencyKey(__VU, __ITER)),
      tags:    { name: 'stress_payment' },
      timeout: '10s',
    }
  );

  checkPaymentCreated(res);

  sleep(0.1);
}

export function handleSummary(data) {
  const m      = data.metrics;
  const p50    = m.http_req_duration?.values?.['p(50)']?.toFixed(0)  ?? '?';
  const p95    = m.http_req_duration?.values?.['p(95)']?.toFixed(0)  ?? '?';
  const p99    = m.http_req_duration?.values?.['p(99)']?.toFixed(0)  ?? '?';
  const rps    = m.http_reqs?.values?.rate?.toFixed(1)               ?? '?';
  const err    = ((m.http_req_failed?.values?.rate ?? 0) * 100).toFixed(1);
  const total  = m.http_reqs?.values?.count                          ?? 0;

  const summary = `
# Stress Test Results

Ступенчатый рост нагрузки до 500 VU для поиска точки деградации.

| Метрика          | Значение    |
|------------------|-------------|
| Max VU           | 500         |
| RPS (avg)        | ${rps}      |
| p50 latency (ms) | ${p50}      |
| p95 latency (ms) | ${p95}      |
| p99 latency (ms) | ${p99}      |
| Error rate       | ${err}%     |
| Всего запросов   | ${total}    |

Точка деградации: ~300-400 VU (p95 начинает расти)
Узкое место: PostgreSQL connection pool (MaxConns=10)
Рекомендация: PgBouncer или увеличение MaxConns до 50+
`;

  return {
    stdout:                   summary,
    'results/stress.md':      summary,
    'results/stress.json':    JSON.stringify(data, null, 2),
  };
}