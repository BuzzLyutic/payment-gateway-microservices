// Тестовые карты — Stripe test cards, всегда проходят у mock провайдера.
const testCards = [
  '4111111111111111', // Visa — success
  '4242424242424242', // Visa — success
  '5500005555555559', // Mastercard — success
  '371449635398431',  // Amex — success
  '6011111111111117', // Discover — success
  '4000000000000002', // Visa — declined (для тестирования declined пути)
];

const currencies = ['RUB', 'USD', 'EUR'];

// Без внешних зависимостей
function randomIntBetween(min, max) {
  return Math.floor(Math.random() * (max - min + 1)) + min;
}

// Рандомизированные данные предотвращают срабатывание velocity rules
// в risk-сервисе при нагрузочном тестировании.
function randomCard() {
  // Берём только success карты (исключаем последнюю — declined)
  return testCards[randomIntBetween(0, testCards.length - 2)];
}

function randomIP() {
  return [
    randomIntBetween(1, 254),
    randomIntBetween(1, 254),
    randomIntBetween(1, 254),
    randomIntBetween(1, 254),
  ].join('.');
}

function randomEmail() {
  return `user${randomIntBetween(1, 999999)}@loadtest.example.com`;
}

// Генерирует реалистичный платёж с рандомизированными данными.
// amount в минимальных единицах валюты (копейки/центы).
export function paymentPayload(merchantID, opts = {}) {
  const amount   = opts.amount   || randomIntBetween(100, 1000000);
  const currency = opts.currency || currencies[randomIntBetween(0, currencies.length - 1)];

  return JSON.stringify({
    merchant_id: merchantID,
    amount:      amount,
    currency:    currency,
    payment_method: {
      type:        'card',
      card_number: randomCard(),
      exp_month:   12,
      exp_year:    2027,
    },
    customer: {
      email: randomEmail(),
      ip:    randomIP(),
    },
    description: `Load test VU=${__VU} iter=${__ITER}`,
  });
}

// Payload для тестирования declined пути.
export function declinedPaymentPayload(merchantID) {
  return JSON.stringify({
    merchant_id: merchantID,
    amount:      50000,
    currency:    'RUB',
    payment_method: {
      type:        'card',
      card_number: testCards[testCards.length - 1], // declined карта
      exp_month:   12,
      exp_year:    2027,
    },
    customer: {
      email: randomEmail(),
      ip:    randomIP(),
    },
  });
}

// Payload для RUB + SBP — только провайдеры с поддержкой SBP.
export function sbpPaymentPayload(merchantID) {
  return JSON.stringify({
    merchant_id: merchantID,
    amount:      randomIntBetween(1000, 100000),
    currency:    'RUB',
    payment_method: {
      type: 'sbp',
    },
    customer: {
      email: randomEmail(),
      ip:    randomIP(),
    },
  });
}