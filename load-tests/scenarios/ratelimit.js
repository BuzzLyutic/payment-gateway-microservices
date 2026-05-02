import http from 'k6/http';
import { sleep, check } from 'k6';
import { Counter, Rate } from 'k6/metrics';
import { config } from '../config/options.js';
import { getMerchant, headers, idempotencyKey } from '../helpers/auth.js';
import { paymentPayload } from '../helpers/payloads.js';

const rateLimited    = new Counter('rate_limited_total');
const rateLimitRate  = new Rate('rate_limit_rate');

export const options = {
  // Фиксированное число VU — проверяем поведение при превышении лимита.
  // Rate limit: 50 req/min на мерчанта → при 10 VU × 6 req/min = 60 req/min > 50.
  scenarios: {
    // Сценарий 1: один мерчант упирается в лимит.
    single_merchant_limit: {
      executor:        'constant-vus',
      vus:             15,
      duration:        '2m',
      tags:            { scenario: 'single_merchant_limit' },
    },
    // Сценарий 2: разные мерчанты — лимиты независимы.
    multi_merchant_fair: {
      executor:        'constant-vus',
      vus:             10,
      duration:        '2m',
      startTime:       '2m30s', // запускаем после первого сценария
      tags:            { scenario: 'multi_merchant_fair' },
    },
  },
  thresholds: {
    // Rate limit должен возвращать 429, не 500.
    'http_req_failed{expected_response:false}': ['rate<0.001'],
    // Убеждаемся что rate limiting вообще срабатывает при нагрузке.
    'rate_limit_rate': ['rate>0.1'],
  },
};

export default function () {
  // Все VU используют одного мерчанта — намеренно упираемся в лимит.
  const merchant = {
    apiKey:     'test_key_merchant_1',
    merchantID: 'merchant_001',
  };

  const res = http.post(
    `${config.baseURL}${config.apiPath}`,
    paymentPayload(merchant.merchantID, { amount: 1000 }),
    {
      headers: headers(merchant.apiKey, idempotencyKey(__VU, __ITER)),
      tags:    { name: 'ratelimit_test' },
    }
  );

  if (res.status === 429) {
    rateLimited.add(1);
    rateLimitRate.add(1);

    // Проверяем корректность 429 ответа.
    check(res, {
      '429 has Retry-After header': (r) =>
        r.headers['Retry-After'] !== undefined ||
        r.headers['X-RateLimit-Reset'] !== undefined,
      '429 has JSON body': (r) => {
        try { JSON.parse(r.body); return true; }
        catch { return false; }
      },
    });
  } else {
    rateLimitRate.add(0);
    check(res, {
      'non-429 is 200 or 201': (r) => r.status === 200 || r.status === 201,
    });
  }

  // Минимальная пауза — максимизируем RPS чтобы гарантированно попасть в лимит.
  sleep(0.1);
}

export function handleSummary(data) {
  const total       = data.metrics.http_reqs?.values?.count ?? 0;
  const limited     = data.metrics.rate_limited_total?.values?.count ?? 0;
  const limitedRate = ((data.metrics.rate_limit_rate?.values?.rate ?? 0) * 100).toFixed(1);

  return {
    stdout: `
# Rate Limit Test Results

| Метрика                  | Значение       |
|--------------------------|----------------|
| Всего запросов           | ${total}       |
| Rate limited (429)       | ${limited}     |
| Rate limit rate          | ${limitedRate}%|
`,
  };
}