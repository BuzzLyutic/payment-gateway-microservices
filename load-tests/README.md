# Load Tests — Payment Gateway

Нагрузочное тестирование платформы с использованием [k6](https://k6.io).

## Структура

```
load-tests/
├── config/
│   └── options.js          # Общие пороги, ступени нагрузки, конфигурация
├── helpers/
│   ├── auth.js             # Мерчанты, заголовки, idempotency keys
│   ├── checks.js           # Проверки ответов, кастомные метрики
│   └── payloads.js         # Генераторы тел запросов
├── scenarios/
│   ├── happy_path.js       # Основной нагрузочный тест
│   ├── ratelimit.js        # Проверка rate limiting
│   ├── routing.js          # Thompson Sampling под нагрузкой
│   ├── stress.js           # Стресс-тест до деградации
│   └── e2e_latency.js      # E2E время полного цикла транзакции
├── results/                # Результаты запусков (gitignore)
├── run.sh                  # Оркестрация тестов
└── README.md
```

## Установка k6

```bash
# Ubuntu / Debian / WSL
sudo gpg --no-default-keyring \
  --keyring /usr/share/keyrings/k6-archive-keyring.gpg \
  --keyserver hkp://keyserver.ubuntu.com:80 \
  --recv-keys C5AD17C747E3415A3642D57D77C6C491D6AC1D69

echo "deb [signed-by=/usr/share/keyrings/k6-archive-keyring.gpg] \
  https://dl.k6.io/deb stable main" \
  | sudo tee /etc/apt/sources.list.d/k6.list

sudo apt-get update && sudo apt-get install k6

# macOS
brew install k6

# Проверка
k6 version
```

## Быстрый старт

```bash
# 1. Запустить платформу
make up

# 2. Smoke test — убедиться что всё работает (30s)
./run.sh smoke

# 3. Основной нагрузочный тест
./run.sh happy_path

# 4. E2E латентность
./run.sh e2e

# 5. Справка по всем командам
./run.sh help
```

## Сценарии

| Команда | VU | Длительность | Что измеряет |
|---------|----|--------------|-------------|
| `smoke` | 5 | 30s | Работоспособность системы |
| `happy_path` | 10→200 | ~8m | Gateway throughput и латентность |
| `stages` | 10/50/100/200 | ~4×60s | Метрики по каждому уровню нагрузки отдельно |
| `ratelimit` | 15 | ~4m | Корректность 429 поведения |
| `routing` | 20→100 | ~6m | Thompson Sampling адаптация |
| `e2e` | 5 | 5m | Полный цикл: создание → финальный статус |
| `stress` | 50→500 | ~16m | Точка деградации, Circuit Breaker |
| `all` | — | ~35m | Все сценарии последовательно |

## Результаты тестирования

### Gateway Latency (синхронная часть)

| Показатель | 10 VU | 50 VU | 100 VU | 200 VU | 500 VU |
|---|---|---|---|---|---|
| RPS | 16 | 81 | 162 | 324 | 742 |
| p50 (ms) | 5 | 5 | 5 | 5 | ~400 |
| p95 (ms) | 8 | 7 | 7 | 8 | 721 |
| p99 (ms) | 14 | 102* | 24 | 31 | — |
| Error rate | 0% | 0% | 0% | 0% | 0% |
| Success rate | 100% | 100% | 100% | 100% | 100% |

*p99=102ms при 50 VU — статистический выброс (один медленный запрос из ~4800).

**Вывод**: горизонтальное плато до 200 VU, деградация начинается на ~300-400 VU.
Узкое место — PostgreSQL connection pool (MaxConns=10).

### E2E Latency (полный цикл транзакции)

Конфигурация: `WEBHOOK_INTERVAL=1s`, `poll_interval=1s`

| Метрика | Значение |
|---------|---------|
| E2E min | ~1s |
| E2E p50 | ~2s |
| E2E p95 | ~3s |
| E2E p99 | ~3s |
| Success rate | 100% |

**Декомпозиция E2E времени:**
```
POST /payments (gateway):        5-15ms
Async pipeline (Risk+Provider):  ~500-800ms
Webhook worker interval:         настраивается (WEBHOOK_INTERVAL)
Polling артефакт:                кратно poll_interval
─────────────────────────────────────────────
Итого при WEBHOOK_INTERVAL=1s:   1-3s
Итого при WEBHOOK_INTERVAL=30s:  ~15-45s
```

### Thompson Sampling

| Провайдер | Реальный success rate | TS оценка | Сходимость |
|-----------|----------------------|-----------|-----------|
| mock_provider_a | 95% | ~0.95 | за ~2 мин |
| mock_provider_b | 85% | ~0.85 | за ~2 мин |
| stripe | 80% | 0.80 | за ~2 мин |

O(K) сложность подтверждена: routing p95 = 8ms при 100 VU и 3 провайдерах.

### Rate Limiter

| Метрика | Значение |
|---------|---------|
| Конфигурация | 1000 req/min на мерчанта |
| 429 rate при превышении | 96.5% |
| Поведение при 429 | Корректный ответ, не 500 |

## Конфигурация

```bash
# Изменить target URL
BASE_URL=http://staging.example.com ./run.sh happy_path

# Изменить webhook interval для E2E теста (без пересборки)
WEBHOOK_INTERVAL=5s docker compose up -d --no-deps transaction-service
./run.sh e2e
```

### Rate limit для тестовых мерчантов

Настраивается в `docker-compose.yml` → `redis-seed`:

```yaml
redis-cli -h redis HSET apikeys:$$KEY1 rate_limit 999999
```

## Grafana дашборды

Открой `http://localhost:3000` во время теста:

| Дашборд | Что смотреть |
|---------|-------------|
| Payment Success Rate by Provider | Как TS адаптируется к провайдерам |
| Payment Latency p95 | Деградация при росте нагрузки |
| Circuit Breaker State | Должен быть Closed при нормальной нагрузке |
| Thompson Sampling Success Probability | Сходимость оценок провайдеров |

## Рекомендации для продакшна

1. **PgBouncer** — сдвинет точку деградации с 300 VU до 1000+ VU
2. **PostgreSQL MaxConns=50** — снизит p95 при высокой нагрузке
3. **WEBHOOK_INTERVAL=5s** — баланс скорости и нагрузки на исходящую сеть
4. **Горизонтальное масштабирование** transaction-service при >300 VU