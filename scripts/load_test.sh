#!/bin/bash

API_KEY="test_key_merchant_2"
URL="http://localhost:8080/api/v1/payments"

echo "Starting load test: 20 payments"

for i in $(seq 1 20); do
  RESPONSE=$(curl -s -X POST "$URL" \
    -H "Content-Type: application/json" \
    -H "X-API-Key: $API_KEY" \
    -H "X-Idempotency-Key: load-test-$i-$(date +%s%N)" \
    -d "{
      \"merchant_id\": \"merchant_001\",
      \"amount\": $((RANDOM % 50000 + 1000)),
      \"currency\": \"RUB\",
      \"payment_method\": {
        \"type\": \"card\",
        \"card_number\": \"4111111111111111\",
        \"exp_month\": 12,
        \"exp_year\": 2027
      },
      \"customer\": {
        \"email\": \"user@example.com\",
        \"ip\": \"192.168.1.1\"
      }
    }")

  STATUS=$(echo "$RESPONSE" | jq -r '.status // .error // "unknown"')
  echo "Payment $i: $STATUS"
  sleep 0.3
done

echo "Done"