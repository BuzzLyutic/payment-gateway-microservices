#!/usr/bin/env bash
set -euo pipefail

# Конфигурация
BASE_URL="${BASE_URL:-http://localhost:8080}"
RESULTS_DIR="results/$(date +%Y%m%d_%H%M%S)"

# Цвета
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m'

log()  { echo -e "${GREEN}[$(date +%H:%M:%S)]${NC} $*"; }
warn() { echo -e "${YELLOW}[$(date +%H:%M:%S)]${NC} $*"; }
err()  { echo -e "${RED}[$(date +%H:%M:%S)]${NC} $*" >&2; }

# Проверки
check_dependencies() {
  if ! command -v k6 &>/dev/null; then
    err "k6 не установлен."
    err "Ubuntu/Debian: https://k6.io/docs/get-started/installation/"
    err "macOS: brew install k6"
    exit 1
  fi
  log "k6 $(k6 version | head -1)"
}

check_gateway() {
  log "Проверяем доступность: ${BASE_URL}/health"
  for i in {1..10}; do
    if curl -sf "${BASE_URL}/health" > /dev/null 2>&1; then
      log "Gateway доступен ✓"
      return 0
    fi
    warn "Попытка ${i}/10 — недоступен, ждём 3с..."
    sleep 3
  done
  err "Gateway недоступен. Запусти: make up"
  exit 1
}

# Запуск одного сценария
run_scenario() {
  local name="$1"
  local file="scenarios/$2"

  log "━━━ Запуск: ${name} ━━━"
  mkdir -p "${RESULTS_DIR}"

  BASE_URL="${BASE_URL}" k6 run \
    --out "json=${RESULTS_DIR}/${name}.json" \
    "${file}" \
    2>&1 | tee "${RESULTS_DIR}/${name}.log"

  local code=$?
  if [ $code -eq 0 ]; then
    log "${name}: PASSED ✓"
  else
    warn "${name}: FAILED (exit=${code}) — лог: ${RESULTS_DIR}/${name}.log"
  fi
  return $code
}

# Ступенчатый тест по VU
run_stages() {
  mkdir -p "${RESULTS_DIR}"
  log "Запуск ступенчатого теста: 10 / 50 / 100 / 200 VU по 60s каждая"

  for VU in 10 50 100 200; do
    log "━━━ Ступень: ${VU} VU ━━━"

    BASE_URL="${BASE_URL}" k6 run \
      --vus "${VU}" \
      --duration 60s \
      --summary-trend-stats 'p(50),p(95),p(99),max' \
      --tag "stage=${VU}vu" \
      --out "json=${RESULTS_DIR}/stage_${VU}vu.json" \
      scenarios/happy_path.js \
      2>&1 | tee "${RESULTS_DIR}/stage_${VU}vu.log"

    if [ "${VU}" -lt 200 ]; then
      log "Пауза 65s — сброс rate limit bucket..."
      sleep 65
    fi
  done

  log "Ступенчатый тест завершён. Результаты: ${RESULTS_DIR}/"
}

# Main
main() {
  local mode="${1:-help}"

  check_dependencies
  check_gateway

  mkdir -p "${RESULTS_DIR}"
  log "Результаты: ${RESULTS_DIR}/"
  log "Grafana:    http://localhost:3000"
  echo ""

  case "$mode" in
    smoke)
      log "Smoke test — быстрая проверка работоспособности (~30s, 5 VU)"
      BASE_URL="${BASE_URL}" k6 run \
        --vus 5 \
        --duration 30s \
        --out "json=${RESULTS_DIR}/smoke.json" \
        scenarios/happy_path.js \
        2>&1 | tee "${RESULTS_DIR}/smoke.log"
      ;;

    happy_path)
      run_scenario "happy_path" "happy_path.js"
      ;;

    stages)
      run_stages
      ;;

    ratelimit)
      run_scenario "ratelimit" "ratelimit.js"
      ;;

    routing)
      run_scenario "routing" "routing.js"
      ;;

    e2e)
      run_scenario "e2e_latency" "e2e_latency.js"
      ;;

    stress)
      warn "Стресс-тест: нагрузка до 500 VU, длительность ~16 минут."
      warn "Продолжить? (y/N)"
      read -r confirm
      [ "${confirm}" = "y" ] || { log "Отменено."; exit 0; }
      run_scenario "stress" "stress.js"
      ;;

    all)
      log "Последовательный запуск всех сценариев..."
      run_scenario "happy_path" "happy_path.js" || true
      log "Пауза 70s между сценариями..."; sleep 70

      run_scenario "ratelimit"   "ratelimit.js"   || true
      sleep 70

      run_scenario "routing"     "routing.js"     || true
      sleep 70

      run_scenario "e2e_latency" "e2e_latency.js" || true
      ;;

    help|*)
      echo "Использование: $0 <команда>"
      echo ""
      echo "Команды:"
      echo "  smoke       Быстрая проверка (30s, 5 VU)"
      echo "  happy_path  Основной нагрузочный тест (10→200 VU)"
      echo "  stages      Отдельные ступени 10/50/100/200 VU по 60s"
      echo "  ratelimit   Проверка rate limiting (429 поведение)"
      echo "  routing     Thompson Sampling под нагрузкой"
      echo "  e2e         E2E латентность полного цикла транзакции"
      echo "  stress      Стресс-тест до 500 VU (поиск деградации)"
      echo "  all         Все сценарии последовательно"
      echo ""
      echo "Переменные окружения:"
      echo "  BASE_URL    URL gateway (по умолчанию: http://localhost:8080)"
      echo ""
      echo "Примеры:"
      echo "  ./run.sh smoke"
      echo "  ./run.sh stages"
      echo "  BASE_URL=http://staging.example.com ./run.sh happy_path"
      exit 0
      ;;
  esac

  log "Готово. Результаты: ${RESULTS_DIR}/"
  log "Grafana: http://localhost:3000"
}

main "$@"