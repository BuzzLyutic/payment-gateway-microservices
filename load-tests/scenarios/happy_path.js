import http from 'k6/http';
import { sleep } from 'k6';
import { config, stages, thresholds } from '../config/options.js';
import { getMerchant, headers, idempotencyKey } from '../helpers/auth.js';
import { paymentPayload } from '../helpers/payloads.js';
import { checkPaymentCreated, checkGetPayment } from '../helpers/checks.js';

export const options = {
  stages: stages.load,

  thresholds: {
    http_req_duration: ['p(95)<5000', 'p(99)<10000'],
    http_req_failed:   ['rate<0.25'],
    payment_success:   ['rate>0.70'],
  },

  summaryTrendStats: ['min', 'med', 'avg', 'p(50)', 'p(90)', 'p(95)', 'p(99)', 'max'],

  tags: { scenario: 'happy_path' },
};

export default function () {
  const merchant = getMerchant(__VU);
  const iKey     = idempotencyKey(__VU, __ITER);

  // Создаём платёж.
  const createRes = http.post(
    `${config.baseURL}${config.apiPath}`,
    paymentPayload(merchant.merchantID),
    {
      headers: headers(merchant.apiKey, iKey),
      tags:    { name: 'create_payment' },
    }
  );

  const ok = checkPaymentCreated(createRes);

  // GET — используем поле "id" из ответа.
  if (ok) {
    let txID;
    try {
      txID = JSON.parse(createRes.body).id;
    } catch (_) {}

    if (txID) {
      sleep(0.2);

      const getRes = http.get(
        `${config.baseURL}${config.apiPath}/${txID}`,
        {
          headers: { 'X-API-Key': merchant.apiKey },
          tags:    { name: 'get_payment' },
        }
      );

      checkGetPayment(getRes, txID);
    }
  }

  sleep(Math.random() + 0.5);
}

export function handleSummary(data) {
  const m = data.metrics;

  const p50  = m.http_req_duration?.values?.['p(50)']?.toFixed(0)  ?? '?';
  const p95  = m.http_req_duration?.values?.['p(95)']?.toFixed(0)  ?? '?';
  const p99  = m.http_req_duration?.values?.['p(99)']?.toFixed(0)  ?? '?';
  const rps  = m.http_reqs?.values?.rate?.toFixed(1)               ?? '?';
  const err  = ((m.http_req_failed?.values?.rate  ?? 0) * 100).toFixed(2);
  const succ = ((m.payment_success?.values?.rate  ?? 0) * 100).toFixed(1);

  const summary = `
# Load Test Results — Happy Path

## Summary
| Метрика           | Значение     |
|-------------------|--------------|
| RPS (avg)         | ${rps}       |
| Latency p50 (ms)  | ${p50}       |
| Latency p95 (ms)  | ${p95}       |
| Latency p99 (ms)  | ${p99}       |
| Error rate        | ${err}%      |
| Payment success   | ${succ}%     |

_Запущен: ${new Date().toISOString()}_
`;

  return {
    'stdout':                  summary,
    'results/happy_path.md':   summary,
    'results/happy_path.json': JSON.stringify(data, null, 2),
  };
}