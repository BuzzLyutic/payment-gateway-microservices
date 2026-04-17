package middleware_test

import (
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/BuzzLyutic/payment-gateway-microservices/services/api-gateway/internal/middleware"
)

func newTestLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stdout, nil))
}

func TestRecovery_CatchesPanic(t *testing.T) {
	handler := middleware.Recovery(newTestLogger())(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			panic("test panic")
		}),
	)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rr := httptest.NewRecorder()

	// Не должен паниковать — Recovery должен перехватить
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusInternalServerError {
		t.Errorf("expected 500, got %d", rr.Code)
	}
}

func TestRequestID_Generated(t *testing.T) {
	var capturedID string

	handler := middleware.RequestID(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// request_id должен быть в заголовке ответа
			capturedID = w.Header().Get("X-Request-ID")
		}),
	)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if capturedID == "" {
		t.Error("expected X-Request-ID to be generated")
	}
	if rr.Header().Get("X-Request-ID") == "" {
		t.Error("expected X-Request-ID in response header")
	}
}

func TestRequestID_Propagated(t *testing.T) {
	existingID := "existing-request-id-123"
	var capturedID string

	handler := middleware.RequestID(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			capturedID = w.Header().Get("X-Request-ID")
		}),
	)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-Request-ID", existingID)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if capturedID != existingID {
		t.Errorf("expected propagated ID %q, got %q", existingID, capturedID)
	}
}

func TestLogging_DoesNotChangeStatus(t *testing.T) {
	handler := middleware.Logging(newTestLogger())(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusCreated)
		}),
	)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/payments", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusCreated {
		t.Errorf("expected 201, got %d", rr.Code)
	}
}

func TestChain_RecoveryRequestIDLogging(t *testing.T) {
	// Проверяем что цепочка middleware работает вместе корректно.
	logger := newTestLogger()

	handler := middleware.Recovery(logger)(
		middleware.RequestID(
			middleware.Logging(logger)(
				http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.WriteHeader(http.StatusOK)
				}),
			),
		),
	)

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rr.Code)
	}
	if rr.Header().Get("X-Request-ID") == "" {
		t.Error("expected X-Request-ID in response")
	}
}
