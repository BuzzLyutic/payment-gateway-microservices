# Transaction Service

Микросервис управления жизненным циклом платёжных транзакций. Отвечает за создание, хранение и обработку платежей через внешних провайдеров.

## Принцип работы

Сервис разделяет приём запроса и обработку платежа на два независимых процесса:

1. **HTTP API** принимает запрос на создание платежа, сохраняет транзакцию в БД
   со статусом `pending` и немедленно возвращает ответ клиенту.

2. **Background Worker** периодически забирает `pending`-транзакции из БД,
   отправляет их платёжному провайдеру и обновляет статус по результату.

Такой подход обеспечивает быстрый отклик API (< 50 мс) и устойчивость к сбоям —
при перезапуске сервиса необработанные транзакции не теряются.

## Жизненный цикл транзакции

```
pending → processing → captured    (провайдер подтвердил)
pending → processing → failed      (исчерпаны retry)
pending → processing → declined    (провайдер отклонил)
captured → refunded                (возврат — заглушка)
```

Переходы контролируются state machine — недопустимые переходы (например,
`captured → pending`) отклоняются на уровне домена.

## Ключевые алгоритмы

### Атомарный захват транзакций (Worker)

Worker использует паттерн **polling с атомарным захватом**:

```sql
WITH pending AS (
    SELECT id FROM transactions
    WHERE status = 'pending'
    ORDER BY created_at ASC
    LIMIT $1
    FOR UPDATE SKIP LOCKED
)
UPDATE transactions SET status = 'processing'
FROM pending WHERE transactions.id = pending.id
RETURNING ...
```

- `FOR UPDATE SKIP LOCKED` - при нескольких инстансах каждый захватывает уникальный
  набор транзакций без блокировок и дублирования
- Переход `pending → processing` происходит атомарно с выборкой - исключает
  повторную обработку одной транзакции

### Идемпотентность (SET NX Lock)

Клиент обязан передавать заголовок `X-Idempotency-Key`. Защита от дублей
реализована через атомарную операцию `SET key "processing" NX EX ttl` в Redis:

```
Первый запрос:
  SET NX → OK → создаём транзакцию → SET key = tx_id
  
Повторный запрос:
  SET NX → FAIL → GET key →
    "processing" → 409 Conflict (ещё создаётся)
    "tx_id"      → SELECT из БД → 200 OK (актуальные данные)
    
Ошибка создания:
  SET NX → OK → INSERT ошибка → DEL key (откат лока)
```

Ключи хранятся с TTL 24 часа. UNIQUE constraint в PostgreSQL выступает
страховкой при недоступности Redis.

### Retry с exponential backoff

При временных ошибках провайдера (таймаут, 5xx) сервис повторяет запрос:

- До 3 повторных попыток
- Задержка: 100 мс → 200 мс → 400 мс (exponential backoff)
- Терминальные ошибки (decline) не ретраятся
- При исчерпании попыток — статус `failed`

### Mock Provider

Имитация внешнего платёжного провайдера:

| Исход | Вероятность | Поведение |
|---|---|---|
| `captured` | 70% | Успешное списание |
| `transient error` | 20% | Временная ошибка, ретраится |
| `declined` | 10% | Отклонение, терминальное |

Искусственная задержка 100–500 мс. Интерфейс `Provider` позволяет подключить
реальную реализацию без изменения бизнес-логики.

## API

### `GET /health`

Проверка доступности сервиса и зависимостей (PostgreSQL, Redis).

| Статус | Значение |
|---|---|
| `200 OK` | Все зависимости доступны |
| `503 Service Unavailable` | Одна или более зависимостей недоступна |

```json
{
  "status": "healthy",
  "checks": {
    "database": "ok",
    "redis": "ok"
  }
}
```

### `POST /api/v1/payments`

Создание платежа. Возвращает транзакцию со статусом `pending`.

**Заголовки:**
- `Content-Type: application/json` (обязательный)
- `X-Idempotency-Key: <string>` (обязательный, уникальный для каждого платежа)

**Тело запроса:**
```json
{
  "amount": 10000,
  "currency": "RUB",
  "merchant_id": "m_abc123",
  "description": "Оплата заказа #123",
  "payment_method": {
    "type": "card",
    "card_number": "4242424242424242",
    "exp_month": 12,
    "exp_year": 2026
  },
  "metadata": {
    "order_id": "order_123"
  }
}
```

**Ответы:**

| Код | Описание |
|---|---|
| `201 Created` | Транзакция создана |
| `200 OK` | Идемпотентный дубль - возвращены актуальные данные |
| `400 Bad Request` | Ошибка валидации или отсутствует `X-Idempotency-Key` |
| `409 Conflict` | Предыдущий запрос с этим ключом ещё обрабатывается |
| `500 Internal Server Error` | Внутренняя ошибка |

### `GET /api/v1/payments/{id}`

Получение текущего состояния платежа.

| Код | Описание |
|---|---|
| `200 OK` | Транзакция найдена |
| `404 Not Found` | Транзакция не существует |

```json
{
  "id": "550e8400-e29b-41d4-a716-446655440000",
  "status": "captured",
  "amount": 10000,
  "currency": "RUB",
  "provider": "mock_provider",
  "created_at": "2025-01-15T10:30:00Z",
  "updated_at": "2025-01-15T10:30:01Z"
}
```

## Конфигурация

Все параметры задаются через переменные окружения. См. [.env.example](.env.example).

| Переменная | По умолчанию | Описание |
|---|---|---|
| `SERVER_PORT` | `8080` | Порт HTTP-сервера |
| `DB_HOST` | `localhost` | Хост PostgreSQL |
| `DB_PORT` | `5432` | Порт PostgreSQL |
| `DB_USER` | `payment` | Пользователь БД |
| `DB_PASSWORD` | `payment_secret` | Пароль БД |
| `DB_NAME` | `payment_gateway` | Имя базы данных |
| `REDIS_ADDR` | `localhost:6379` | Адрес Redis |
| `WORKER_INTERVAL_SEC` | `2` | Интервал опроса воркера (сек) |
| `WORKER_BATCH_SIZE` | `10` | Макс. транзакций за один цикл |
| `LOG_LEVEL` | `info` | Уровень: `debug`, `info`, `warn`, `error` |

## Запуск

```bash
# Docker Compose — из корня монорепо
docker-compose up --build

# Локально — требует PostgreSQL и Redis
cd services/transaction-service
cp .env.example .env
go run ./cmd/api/
```

## Тесты

```bash
cd services/transaction-service
go test ./... -v
```

Покрытие:
- **domain** — state machine, допустимые/недопустимые переходы статусов
- **service** — создание платежа, обработка провайдером, retry при transient-ошибках
- **handler** — HTTP-коды, валидация, идемпотентность, обработка ошибок
- **provider** — распределение исходов, отмена контекста
- **middleware** — request_id, panic recovery, логирование

## Структура проекта

```
├── cmd/api/main.go              # точка входа, инициализация зависимостей
├── internal/
│   ├── config/                  # загрузка конфигурации из ENV
│   ├── domain/                  # Transaction, Status, state machine, ошибки
│   ├── handler/                 # HTTP-обработчики (payment, health)
│   ├── middleware/              # logging, request_id, recover
│   ├── service/                 # бизнес-логика, retry, оркестрация
│   ├── repository/              # PostgreSQL (CRUD, FetchPending, UpdateStatus)
│   ├── idempotency/             # Redis SET NX lock, key → tx_id
│   ├── provider/                # интерфейс Provider, MockProvider
│   └── worker/                  # фоновый polling pending-транзакций
├── migrations/                  # SQL-миграции
├── Dockerfile                   # multi-stage сборка
└── .env.example
```