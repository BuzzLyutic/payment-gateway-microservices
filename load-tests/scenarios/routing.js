/**
 * Тест Thompson Sampling и O(K) сложности маршрутизации.
 *
 * Цель: убедиться что время маршрутизации не растёт с увеличением числа
 * провайдеров и нагрузки. Select() должен быть O(K) где K = число провайдеров.
 *
 * Также наблюдаем в Grafana как Thompson Sampling адаптируется:
 * - Провайдер с высоким success rate получает больше трафика
 * - При деградации провайдера TS перераспределяет нагрузку
 */
import http from 'k6/http';
import { sleep } from 'k6';
import { Trend, Counter } from 'k6/metrics';
import { config } from '../config/options.js';
import { getMerchant, headers, idempotencyKey } from '../helpers/auth.js';
import { paymentPayload, sbpPaymentPayload } from '../helpers/payloads.js';
import { checkPaymentCreated } from '../helpers/checks.js';

const routingDuration = new Trend('routing_decision_ms');
const providerCardReqs = new Counter('provider_card_requests');
const providerSbpReqs  = new Counter('provider_sbp_requests');

export const options = {
  stages: [
    { duration: '30s', target: 20  },
    { duration: '2m',  target: 20  }, // стабильная нагрузка — TS должен сойтись
    { duration: '30s', target: 100 }, // скачок — как быстро TS адаптируется?
    { duration: '2m',  target: 100 },
    { duration: '30s', target: 0   },
  ],
  thresholds: {
    // Время ответа не должно деградировать с ростом нагрузки.
    // O(K) означает что p95 не растёт при удвоении VU.
    http_req_duration:    ['p(95)<3000'],
    payment_success:      ['rate>0.80'],
  },
  tags: { scenario: 'routing_thompson_sampling' },
};

export default function () {
  const merchant = getMerchant(__VU);

  // Чередуем card и sbp — разные провайдеры, разная статистика TS.
  const isCard = __ITER % 3 !== 0; // 2/3 — card, 1/3 — sbp

  let payload, paymentType;
  if (isCard) {
    payload     = paymentPayload(merchant.merchantID, { currency: 'RUB' });
    paymentType = 'card';
    providerCardReqs.add(1);
  } else {
    payload     = sbpPaymentPayload(merchant.merchantID);
    paymentType = 'sbp';
    providerSbpReqs.add(1);
  }

  const startTime = Date.now();

  const res = http.post(
    `${config.baseURL}${config.apiPath}`,
    payload,
    {
      headers: headers(merchant.apiKey, idempotencyKey(__VU, __ITER)),
      tags:    {
        name:         'routing_test',
        payment_type: paymentType,
      },
    }
  );

  // Фиксируем время ответа как прокси для времени маршрутизации.
  // Реальное время Select() видно в Grafana через метрики provider-service.
  routingDuration.add(Date.now() - startTime);

  checkPaymentCreated(res);

  sleep(Math.random() * 0.5 + 0.2);
}

export function handleSummary(data) {
  const cardReqs = data.metrics.provider_card_requests?.values?.count ?? 0;
  const sbpReqs  = data.metrics.provider_sbp_requests?.values?.count ?? 0;
  const p95      = data.metrics.routing_decision_ms?.values?.['p(95)']?.toFixed(0) ?? '?';

  return {
    stdout: `
# Thompson Sampling Routing Test

| Метрика               | Значение   |
|-----------------------|------------|
| Card запросов         | ${cardReqs}|
| SBP запросов          | ${sbpReqs} |
| Routing p95 (ms)      | ${p95}     |

Детали распределения по провайдерам — смотри Grafana:
  → Payment Success Rate by Provider
  → Thompson Sampling Success Probability
`,
  };
}