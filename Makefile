.PHONY: up down build test test-integration logs gateway-health

up:
	docker-compose up --build -d

down:
	docker-compose down

build:
	docker-compose build

# Unit-тесты — без зависимостей
test:
	cd services/api-gateway && go test ./...
	cd services/risk-service && go test ./...
	cd services/transaction-service && go test ./...

# Интеграционные — требуют Redis на localhost:6379
test-integration:
	cd services/api-gateway && go test -tags=integration -v ./...
	cd services/risk-service && go test -tags=integration -v ./...

logs:
	docker-compose logs -f api-gateway transaction-service risk-service provider-service

# Проверка health всех сервисов
health:
	@echo "=== API Gateway ===" && curl -s http://localhost:8080/health | jq
	@echo "=== Risk Service ===" && curl -s http://localhost:8083/health | jq

# Тестовый платёж через gateway
test-payment:
	curl -s -X POST http://localhost:8080/api/v1/payments \
		-H "Content-Type: application/json" \
		-H "X-API-Key: test_key_merchant_1" \
		-H "X-Idempotency-Key: test-$(shell date +%s)" \
		-d '{"merchant_id":"merchant_001","amount":100000,"currency":"RUB","payment_method":{"type":"card","card_number":"4111111111111111","exp_month":12,"exp_year":2027},"customer":{"email":"user@example.com","ip":"192.168.1.1"}}' | jq

# Тест rate limiting
test-ratelimit:
	@for i in $$(seq 1 55); do \
		STATUS=$$(curl -s -o /dev/null -w "%{http_code}" \
			-X POST http://localhost:8080/api/v1/payments \
			-H "Content-Type: application/json" \
			-H "X-API-Key: test_key_merchant_2" \
			-d '{"merchant_id":"merchant_002","amount":1000,"currency":"RUB","payment_method":{"type":"card","card_number":"4111111111111111","exp_month":12,"exp_year":2027}}'); \
		echo "Request $$i: $$STATUS"; \
	done
