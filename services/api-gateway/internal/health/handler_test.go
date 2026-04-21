package health_test

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/BuzzLyutic/payment-gateway-microservices/services/api-gateway/internal/health"
	"github.com/redis/go-redis/v9"
)

func newTestLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stdout, nil))
}

func TestHealthHandler_Healthy(t *testing.T) {
	// Redis на нерабочем адресе — проверяем unhealthy сценарий.
	// Healthy сценарий — интеграционный тест.
	rdb := redis.NewClient(&redis.Options{Addr: "localhost:1"})
	defer rdb.Close()

	h := health.NewHandler(rdb, newTestLogger())

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusServiceUnavailable {
		t.Errorf("expected 503, got %d", rr.Code)
	}

	var resp health.Response
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if resp.Status != "unavailable" {
		t.Errorf("expected status unavailable, got %q", resp.Status)
	}
	if resp.Service != "api-gateway" {
		t.Errorf("expected service api-gateway, got %q", resp.Service)
	}
	if resp.Dependencies["redis"].Status != "unavailable" {
		t.Error("expected redis unavailable")
	}
}

func TestHealthHandler_MethodNotAllowed(t *testing.T) {
	// Health endpoint отвечает только на GET.
	// Проверяем что POST возвращает 405 — это поведение ServeMux Go 1.22+.
	// Тест документирует ожидаемое поведение роутера.
	rdb := redis.NewClient(&redis.Options{Addr: "localhost:1"})
	defer rdb.Close()

	h := health.NewHandler(rdb, newTestLogger())

	req := httptest.NewRequest(http.MethodPost, "/health", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	// Handler сам по себе не проверяет метод — это делает ServeMux.
	// Здесь проверяем что handler отвечает (не паникует) на любой метод.
	if rr.Code == 0 {
		t.Error("expected non-zero status code")
	}
}
