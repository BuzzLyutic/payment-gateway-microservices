package engine_test

import (
	"context"
	"testing"
	"time"

	"github.com/BuzzLyutic/payment-gateway-microservices/services/risk-service/internal/domain"
	"github.com/BuzzLyutic/payment-gateway-microservices/services/risk-service/internal/engine"
	"github.com/BuzzLyutic/payment-gateway-microservices/services/risk-service/internal/events"
	"github.com/redis/go-redis/v9"
	"log/slog"
	"os"
)

func newTestLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stdout, nil))
}

func floatPtr(f float64) *float64 { return &f }

func makeEvent(amount int64, hourUTC int, cardHash, ip string) events.PaymentCreated {
	now := time.Now().UTC()
	created := time.Date(now.Year(), now.Month(), now.Day(),
		hourUTC, 0, 0, 0, time.UTC)
	return events.PaymentCreated{
		TransactionID: "tx-test-001",
		MerchantID:    "merchant-001",
		Amount:        amount,
		Currency:      "RUB",
		PaymentMethod: "card",
		CardHash:      cardHash,
		CustomerIP:    ip,
		CreatedAt:     created,
	}
}

func TestEngine_SimpleRule_HighAmount(t *testing.T) {
	rules := []domain.Rule{
		{
			Name:     "high_amount",
			Type:     domain.RuleTypeSimple,
			Field:    "amount",
			Operator: domain.OperatorGt,
			RawValue: domain.RawValue{Single: floatPtr(10_000_000)},
			Score:    20,
		},
	}

	eng := engine.New(rules, nil, newTestLogger())

	t.Run("triggered", func(t *testing.T) {
		results := eng.Evaluate(context.Background(), makeEvent(15_000_000, 12, "", ""))
		if !results[0].Triggered {
			t.Error("expected rule to trigger")
		}
		if results[0].Score != 20 {
			t.Errorf("expected score 20, got %d", results[0].Score)
		}
	})

	t.Run("not triggered", func(t *testing.T) {
		results := eng.Evaluate(context.Background(), makeEvent(5_000_000, 12, "", ""))
		if results[0].Triggered {
			t.Error("expected rule not to trigger")
		}
		if results[0].Score != 0 {
			t.Errorf("expected score 0, got %d", results[0].Score)
		}
	})
}

func TestEngine_SimpleRule_NightTime(t *testing.T) {
	rules := []domain.Rule{
		{
			Name:     "night_time",
			Type:     domain.RuleTypeSimple,
			Field:    "hour",
			Operator: domain.OperatorBetween,
			RawValue: domain.RawValue{Range: [2]float64{1, 5}, IsList: true},
			Score:    10,
		},
	}
	eng := engine.New(rules, nil, newTestLogger())

	cases := []struct {
		hour      int
		triggered bool
	}{
		{0, false},
		{1, true},
		{3, true},
		{5, true},
		{6, false},
		{12, false},
	}

	for _, tc := range cases {
		results := eng.Evaluate(context.Background(), makeEvent(1000, tc.hour, "", ""))
		if results[0].Triggered != tc.triggered {
			t.Errorf("hour=%d: expected triggered=%v, got=%v",
				tc.hour, tc.triggered, results[0].Triggered)
		}
	}
}

func TestEngine_VelocityRule_EmptyField_Skipped(t *testing.T) {
	// card_hash пустая строка — правило должно пропуститься без обращения к Redis
	rules := []domain.Rule{
		{
			Name:      "velocity_card",
			Type:      domain.RuleTypeVelocity,
			KeyField:  "card_hash",
			Window:    "1h",
			Threshold: 3,
			Score:     30,
		},
	}

	// redis nil — если дойдёт до Redis, тест упадёт с паникой.
	// Это намеренно: проверяем что пустое поле возвращает (false, nil)
	// ДО обращения к Redis.
	eng := engine.New(rules, nil, newTestLogger())
	results := eng.Evaluate(context.Background(), makeEvent(1000, 12, "", ""))

	if results[0].Triggered {
		t.Error("expected rule not to trigger when card_hash is empty")
	}
	if results[0].Score != 0 {
		t.Errorf("expected score 0, got %d", results[0].Score)
	}
}

func TestEngine_VelocityRule_RedisUnavailable_FailOpen(t *testing.T) {
	rules := []domain.Rule{
		{
			Name:      "velocity_merchant",
			Type:      domain.RuleTypeVelocity,
			KeyField:  "merchant_id",
			Window:    "10m",
			Threshold: 5,
			Score:     25,
		},
	}

	rdb := redis.NewClient(&redis.Options{Addr: "localhost:1"})
	defer rdb.Close()

	eng := engine.New(rules, rdb, newTestLogger())
	results := eng.Evaluate(context.Background(), makeEvent(1000, 12, "", ""))

	if results[0].Triggered {
		t.Error("expected fail-open: rule should not trigger when Redis unavailable")
	}
}
