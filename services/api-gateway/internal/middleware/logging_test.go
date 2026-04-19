package middleware_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/BuzzLyutic/payment-gateway-microservices/services/api-gateway/internal/auth"
	"github.com/BuzzLyutic/payment-gateway-microservices/services/api-gateway/internal/middleware"
)


func TestLogging_Write_ImplicitStatus200(t *testing.T) {
	handler := middleware.Logging(newTestLogger())(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Write без WriteHeader — должен неявно выставить 200.
			w.Write([]byte(`{"ok":true}`)) //nolint:errcheck
		}),
	)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/payments", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected 200 after implicit Write, got %d", rr.Code)
	}
	if rr.Body.Len() == 0 {
		t.Error("expected non-empty body")
	}
}

func TestLogging_Write_AfterExplicitWriteHeader(t *testing.T) {
	handler := middleware.Logging(newTestLogger())(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusCreated)
			w.Write([]byte(`{"created":true}`)) //nolint:errcheck
		}),
	)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/payments", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusCreated {
		t.Errorf("expected 201, got %d", rr.Code)
	}
}

func TestLogging_Status500_DoesNotPanic(t *testing.T) {
	handler := middleware.Logging(newTestLogger())(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		}),
	)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/payments", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusInternalServerError {
		t.Errorf("expected 500, got %d", rr.Code)
	}
}

func TestLogging_WithMerchantInContext(t *testing.T) {
	info := &auth.MerchantInfo{
		MerchantID: "merchant-log-test",
		RateLimit:  100,
	}

	handler := middleware.Logging(newTestLogger())(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		}),
	)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/payments", nil)
	ctx := middleware.WithMerchant(req.Context(), info)
	req = req.WithContext(ctx)

	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rr.Code)
	}
}

func TestWriteHeader_CalledTwice_SecondIgnored(t *testing.T) {
	handler := middleware.Logging(newTestLogger())(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			w.WriteHeader(http.StatusInternalServerError) // игнорируется
		}),
	)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected 200 (second WriteHeader ignored), got %d", rr.Code)
	}
}
