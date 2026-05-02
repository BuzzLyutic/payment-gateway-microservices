/**
 * E2E Latency Test — полный цикл транзакции.
 *
 * Измеряет время от создания платежа до получения финального статуса.
 * Это метрика которую видит мерчант, в отличие от gateway latency (15ms)
 * которая отражает только синхронную часть.
 *
 * Результаты при WEBHOOK_INTERVAL=1s:
 *   p50 ≈ 2s, p95 ≈ 3s, p99 ≈ 3s
 *
 * Для изменения интервала воркера без пересборки:
 *   WEBHOOK_INTERVAL=5s docker compose up -d transaction-service
 */
import http from 'k6/http';
import { sleep } from 'k6';
import { Trend, Rate } from 'k6/metrics';
import { config } from '../config/options.js';
import { getMerchant, headers, idempotencyKey } from '../helpers/auth.js';
import { paymentPayload } from '../helpers/payloads.js';

const e2eLatency    = new Trend('e2e_latency_ms');
const e2eSuccess    = new Rate('e2e_success_rate');
const finalStatuses = new Set(['captured', 'declined', 'failed', 'blocked']);

export const options = {
  // Мало VU — каждый VU долго ждёт финального статуса (polling).
  // Увеличение VU не даёт больше данных, только нагружает систему.
  vus:      5,
  duration: '5m',

  thresholds: {
    // 95% транзакций получают финальный статус за 60s
    'e2e_latency_ms':  ['p(95)<60000'],
    'e2e_success_rate': ['rate>0.90'],
  },

  summaryTrendStats: ['min', 'med', 'avg', 'p(50)', 'p(95)', 'p(99)', 'max'],
};

export default function () {
  const merchant = getMerchant(__VU);
  const iKey     = idempotencyKey(__VU, __ITER);
  const startTime = Date.now();

  // Шаг 1 — создаём транзакцию
  const createRes = http.post(
    `${config.baseURL}${config.apiPath}`,
    paymentPayload(merchant.merchantID),
    {
      headers: headers(merchant.apiKey, iKey),
      tags:    { name: 'e2e_create' },
    }
  );

  if (createRes.status !== 201) {
    e2eSuccess.add(false);
    console.error(`Create failed: status=${createRes.status}`);
    return;
  }

  let txID;
  try {
    txID = JSON.parse(createRes.body).id;
  } catch (_) {
    e2eSuccess.add(false);
    console.error('Failed to parse transaction id from response');
    return;
  }

  // Шаг 2 — polling до финального статуса
  const maxWaitMs  = 120 * 1000; // максимум 2 минуты
  const pollEvery  = 1;          // секунд между опросами
  let   finalStatus = null;

  while (Date.now() - startTime < maxWaitMs) {
    sleep(pollEvery);

    const getRes = http.get(
      `${config.baseURL}${config.apiPath}/${txID}`,
      {
        headers: { 'X-API-Key': merchant.apiKey },
        tags:    { name: 'e2e_poll' },
      }
    );

    if (getRes.status !== 200) continue;

    let body;
    try { body = JSON.parse(getRes.body); } catch (_) { continue; }

    if (finalStatuses.has(body.status)) {
      finalStatus = body.status;
      break;
    }
  }

  // Шаг 3 — фиксируем результат
  const elapsed = Date.now() - startTime;

  if (finalStatus !== null) {
    e2eLatency.add(elapsed);
    // captured и declined — ожидаемые финальные статусы (система работала)
    // failed и blocked — тоже финальные, но менее желательные
    e2eSuccess.add(finalStatus === 'captured' || finalStatus === 'declined');
    console.log(`E2E: tx=${txID} status=${finalStatus} time=${elapsed}ms`);
  } else {
    e2eSuccess.add(false);
    console.error(`E2E TIMEOUT: tx=${txID} elapsed=${elapsed}ms — финальный статус не получен`);
  }
}

export function handleSummary(data) {
  const m = data.metrics;

  const min  = m.e2e_latency_ms?.values?.min?.toFixed(0)          ?? '?';
  const p50  = m.e2e_latency_ms?.values?.['p(50)']?.toFixed(0)   ?? '?';
  const p95  = m.e2e_latency_ms?.values?.['p(95)']?.toFixed(0)   ?? '?';
  const p99  = m.e2e_latency_ms?.values?.['p(99)']?.toFixed(0)   ?? '?';
  const max  = m.e2e_latency_ms?.values?.max?.toFixed(0)          ?? '?';
  const succ = ((m.e2e_success_rate?.values?.rate ?? 0) * 100).toFixed(1);
  const total = m.e2e_latency_ms?.values?.count ?? 0;

  const summary = `
# E2E Latency Test Results

Измеряем полный цикл: POST /payments → финальный статус транзакции.
Конфигурация: WEBHOOK_INTERVAL=1s, poll_interval=1s

| Метрика            | Значение   |
|--------------------|------------|
| E2E min (ms)       | ${min}     |
| E2E p50 (ms)       | ${p50}     |
| E2E p95 (ms)       | ${p95}     |
| E2E p99 (ms)       | ${p99}     |
| E2E max (ms)       | ${max}     |
| Success rate       | ${succ}%   |
| Всего транзакций   | ${total}   |

Декомпозиция E2E времени:
  Async pipeline (Risk + Provider): ~500-800ms
  Webhook worker interval:          настраивается (WEBHOOK_INTERVAL)
  Polling артефакт:                 кратно poll_interval (1s)
`;

  return {
    stdout:                    summary,
    'results/e2e_latency.md':  summary,
    'results/e2e_latency.json': JSON.stringify(data, null, 2),
  };
}