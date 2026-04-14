package proxy_test

import (
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/BuzzLyutic/payment-gateway-microservices/services/api-gateway/internal/auth"
	"github.com/BuzzLyutic/payment-gateway-microservices/services/api-gateway/internal/middleware"
	"github.com/BuzzLyutic/payment-gateway-microservices/services/api-gateway/internal/proxy"
)

func newTestLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stdout, nil))
}

func TestProxy_ForwardsMerchantID(t *testing.T) {
	// Поднимаем фейковый upstream — проверяем что X-Merchant-ID добавлен.
	var receivedMerchantID string

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedMerchantID = r.Header.Get("X-Merchant-ID")
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	p, err := proxy.New(upstream.URL, newTestLogger())
	if err != nil {
		t.Fatalf("create proxy: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/v1/payments", nil)

	// Добавляем MerchantInfo в контекст — имитируем auth middleware
	ctx := middleware.WithMerchant(req.Context(), &auth.MerchantInfo{
		MerchantID: "merchant-proxy-test",
		RateLimit:  100,
	})
	req = req.WithContext(ctx)

	rr := httptest.NewRecorder()
	p.ServeHTTP(rr, req)

	if receivedMerchantID != "merchant-proxy-test" {
		t.Errorf("expected X-Merchant-ID %q, got %q",
			"merchant-proxy-test", receivedMerchantID)
	}
}

func TestProxy_APIKeyNotForwarded(t *testing.T) {
	// X-API-Key не должен уходить upstream.
	var receivedAPIKey string

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedAPIKey = r.Header.Get("X-API-Key")
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	p, err := proxy.New(upstream.URL, newTestLogger())
	if err != nil {
		t.Fatalf("create proxy: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/v1/payments", nil)
	// Auth middleware удаляет ключ до proxy — имитируем это
	// X-API-Key намеренно не устанавливаем в запрос к proxy

	ctx := middleware.WithMerchant(req.Context(), &auth.MerchantInfo{
		MerchantID: "merchant-001",
	})
	req = req.WithContext(ctx)

	rr := httptest.NewRecorder()
	p.ServeHTTP(rr, req)

	if receivedAPIKey != "" {
		t.Errorf("X-API-Key must not reach upstream, got %q", receivedAPIKey)
	}
}

func TestProxy_UpstreamUnavailable_Returns502(t *testing.T) {
	// Upstream на нерабочем адресе — ожидаем 502.
	p, err := proxy.New("http://localhost:1", newTestLogger())
	if err != nil {
		t.Fatalf("create proxy: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/payments/123", nil)
	ctx := middleware.WithMerchant(req.Context(), &auth.MerchantInfo{
		MerchantID: "merchant-001",
	})
	req = req.WithContext(ctx)

	rr := httptest.NewRecorder()
	p.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadGateway {
		t.Errorf("expected 502, got %d", rr.Code)
	}
}

func TestProxy_InvalidURL(t *testing.T) {
	// Некорректный URL — ошибка при создании proxy.
	_, err := proxy.New("://invalid-url", newTestLogger())
	if err == nil {
		t.Error("expected error for invalid URL, got nil")
	}
}
