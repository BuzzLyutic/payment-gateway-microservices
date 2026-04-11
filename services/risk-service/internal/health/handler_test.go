package health_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"log/slog"
	"os"

	"github.com/BuzzLyutic/payment-gateway-microservices/services/risk-service/internal/health"
	"github.com/nats-io/nats.go"
	"github.com/redis/go-redis/v9"
)

func newTestLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stdout, nil))
}

func TestHealthHandler_RedisDown(t *testing.T) {
	// Redis на нерабочем адресе
	rdb := redis.NewClient(&redis.Options{Addr: "localhost:1"})
	defer rdb.Close()

	// NATS nil — имитируем через мок
	h := health.NewHandler(rdb, &nats.Conn{}, newTestLogger())

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rr := httptest.NewRecorder()

	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusServiceUnavailable {
		t.Errorf("expected 503, got %d", rr.Code)
	}

	var resp health.Response
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if resp.Status != "unavailable" {
		t.Errorf("expected status unavailable, got %q", resp.Status)
	}
	if resp.Dependencies["redis"].Status != "unavailable" {
		t.Error("expected redis to be unavailable")
	}
}
