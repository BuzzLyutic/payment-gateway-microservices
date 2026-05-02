import { check } from 'k6';
import { Counter, Rate, Trend } from 'k6/metrics';

// Кастомные метрики — отображаются в итоговом отчёте k6 и Grafana.
export const paymentSuccess = new Rate('payment_success');
export const paymentCreated = new Counter('payments_created_total');
export const paymentErrors  = new Counter('payment_errors_total');
export const gatewayLatency = new Trend('gateway_latency_ms');
export const rateLimitHits  = new Counter('rate_limit_hits_total');

// Проверяет ответ на создание платежа (POST /payments).
// Возвращает true если платёж создан успешно.
export function checkPaymentCreated(res) {
  let body = null;
  try { body = JSON.parse(res.body); } catch (_) {}

  const success = check(res, {
    // 201 — создан, 200 — idempotent повтор существующего ключа
    'status is 200 or 201': (r) => r.status === 200 || r.status === 201,
    // API возвращает поле "id" (не "transaction_id")
    'has id field':         (_) => body !== null && body.id !== undefined && body.id !== '',
    'has status field':     (_) => body !== null && body.status !== undefined,
    'response time < 5s':   (r) => r.timings.duration < 5000,
  });

  paymentSuccess.add(success);
  gatewayLatency.add(res.timings.duration);

  if (success) {
    paymentCreated.add(1);
  } else {
    paymentErrors.add(1);
    // Логируем только первые ошибки — не засоряем вывод при нагрузке
    if (__ITER < 3) {
      console.error(`Payment failed: status=${res.status} body=${res.body.substring(0, 300)}`);
    }
  }

  return success;
}

// Проверяет ответ на получение платежа (GET /payments/{id}).
export function checkGetPayment(res, expectedID) {
  let body = null;
  try { body = JSON.parse(res.body); } catch (_) {}

  return check(res, {
    'get payment status 200':       (r) => r.status === 200,
    'get payment has correct id':   (_) => body !== null && body.id === expectedID,
    'get payment has status field': (_) =>
      body !== null &&
      ['pending', 'processing', 'captured', 'declined', 'failed', 'blocked']
        .includes(body.status),
  });
}

// Проверяет ответ с учётом rate limiting.
// 429 — корректное поведение, не считается ошибкой системы.
export function checkRateLimit(res) {
  if (res.status === 429) {
    rateLimitHits.add(1);
    check(res, {
      'rate limit returns 429': (r) => r.status === 429,
    });
    return 'rate_limited';
  }
  checkPaymentCreated(res);
  return 'ok';
}