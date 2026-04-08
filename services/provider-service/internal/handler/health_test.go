package handler_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/BuzzLyutic/payment-gateway-microservices/services/provider-service/internal/handler"
)

type mockPinger struct {
	err error
}

func (m *mockPinger) Ping(_ context.Context) error {
	return m.err
}

func TestHealthHandler_OK(t *testing.T) {
	h := handler.NewHealthHandler(&mockPinger{})

	mux := http.NewServeMux()
	h.Register(mux)

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rr := httptest.NewRecorder()

	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected 200, got: %d", rr.Code)
	}

	var body map[string]any
	if err := json.NewDecoder(rr.Body).Decode(&body); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if body["status"] != "ok" {
		t.Errorf("expected status=ok, got: %v", body["status"])
	}

	components, ok := body["components"].(map[string]any)
	if !ok {
		t.Fatal("expected components object")
	}
	if components["postgresql"] != "up" {
		t.Errorf("expected postgresql=up, got: %v", components["postgresql"])
	}
}

func TestHealthHandler_Degraded(t *testing.T) {
	h := handler.NewHealthHandler(&mockPinger{err: errors.New("connection refused")})

	mux := http.NewServeMux()
	h.Register(mux)

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rr := httptest.NewRecorder()

	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusServiceUnavailable {
		t.Errorf("expected 503, got: %d", rr.Code)
	}

	var body map[string]any
	if err := json.NewDecoder(rr.Body).Decode(&body); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if body["status"] != "degraded" {
		t.Errorf("expected status=degraded, got: %v", body["status"])
	}
}
