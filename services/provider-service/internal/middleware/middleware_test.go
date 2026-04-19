package middleware_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/BuzzLyutic/payment-gateway-microservices/services/provider-service/internal/middleware"
)

// Logging

func TestLogging_PassesStatusCode(t *testing.T) {
	handler := middleware.Logging(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusCreated)
		}),
	)

	req := httptest.NewRequest(http.MethodPost, "/api/payments", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusCreated {
		t.Errorf("expected 201, got %d", rr.Code)
	}
}

func TestLogging_DefaultStatus200(t *testing.T) {
	// Если WriteHeader не вызван явно — статус должен быть 200.
	handler := middleware.Logging(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Write([]byte("ok")) //nolint:errcheck
		}),
	)

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rr.Code)
	}
}

func TestLogging_WithRequestID(t *testing.T) {
	// X-Request-ID из заголовка должен попасть в лог (не паникует).
	handler := middleware.Logging(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		}),
	)

	req := httptest.NewRequest(http.MethodGet, "/api/payments", nil)
	req.Header.Set("X-Request-ID", "test-req-id-123")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rr.Code)
	}
}

func TestLogging_Status500(t *testing.T) {
	handler := middleware.Logging(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		}),
	)

	req := httptest.NewRequest(http.MethodGet, "/api/payments", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusInternalServerError {
		t.Errorf("expected 500, got %d", rr.Code)
	}
}

func TestLogging_Status404(t *testing.T) {
	handler := middleware.Logging(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusNotFound)
		}),
	)

	req := httptest.NewRequest(http.MethodGet, "/nonexistent", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", rr.Code)
	}
}

// Recover

func TestRecover_CatchesPanic(t *testing.T) {
	handler := middleware.Recover(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			panic("test panic")
		}),
	)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rr := httptest.NewRecorder()

	// Не должен паниковать наружу.
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusInternalServerError {
		t.Errorf("expected 500 after panic, got %d", rr.Code)
	}
}

func TestRecover_CatchesPanic_ErrorString(t *testing.T) {
	handler := middleware.Recover(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			panic("something went wrong")
		}),
	)

	req := httptest.NewRequest(http.MethodPost, "/api/payments", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusInternalServerError {
		t.Errorf("expected 500, got %d", rr.Code)
	}
	body := rr.Body.String()
	if body == "" {
		t.Error("expected non-empty body on panic recovery")
	}
}

func TestRecover_NoPanic_PassesThrough(t *testing.T) {
	// Без паники — обычный проход.
	handler := middleware.Recover(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		}),
	)

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rr.Code)
	}
}

func TestRecover_CatchesPanic_RuntimeError(t *testing.T) {
	// Паника от runtime ошибки (nil pointer).
	handler := middleware.Recover(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var p *int
			_ = *p // nil dereference
		}),
	)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusInternalServerError {
		t.Errorf("expected 500 for nil dereference panic, got %d", rr.Code)
	}
}

// RequestID

func TestRequestID_Generated_WhenMissing(t *testing.T) {
	// Нет X-Request-ID в запросе → генерируется новый.
	var capturedID string

	handler := middleware.RequestID(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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

func TestRequestID_Propagated_WhenProvided(t *testing.T) {
	// X-Request-ID уже есть → используем его.
	existingID := "existing-id-abc123"
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
	if rr.Header().Get("X-Request-ID") != existingID {
		t.Errorf("response header X-Request-ID = %q, want %q",
			rr.Header().Get("X-Request-ID"), existingID)
	}
}

func TestRequestID_InContext(t *testing.T) {
	// ID должен быть доступен через GetRequestID(ctx).
	var ctxID string

	handler := middleware.RequestID(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctxID = middleware.GetRequestID(r.Context())
		}),
	)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-Request-ID", "ctx-test-id")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if ctxID != "ctx-test-id" {
		t.Errorf("GetRequestID from context = %q, want %q", ctxID, "ctx-test-id")
	}
}

func TestRequestID_Generated_HasPrefix(t *testing.T) {
	// Генерируемый ID должен иметь префикс "req_".
	var capturedID string

	handler := middleware.RequestID(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			capturedID = w.Header().Get("X-Request-ID")
		}),
	)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if len(capturedID) < 4 || capturedID[:4] != "req_" {
		t.Errorf("generated ID %q should start with 'req_'", capturedID)
	}
}

func TestRequestID_Generated_Unique(t *testing.T) {
	// Два последовательных запроса → разные ID.
	ids := make([]string, 5)

	handler := middleware.RequestID(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}),
	)

	for i := range ids {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)
		ids[i] = rr.Header().Get("X-Request-ID")
	}

	seen := make(map[string]bool)
	for _, id := range ids {
		if seen[id] {
			t.Errorf("duplicate request ID generated: %q", id)
		}
		seen[id] = true
	}
}

func TestGetRequestID_EmptyContext(t *testing.T) {
	// Нет ID в контексте → пустая строка, без паники.
	import_ctx := httptest.NewRequest(http.MethodGet, "/", nil)
	id := middleware.GetRequestID(import_ctx.Context())
	if id != "" {
		t.Errorf("expected empty string for missing request ID, got %q", id)
	}
}

// Metrics

func TestMetrics_PassesThrough(t *testing.T) {
	// Metrics middleware не должен менять статус ответа.
	handler := middleware.Metrics(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusAccepted)
		}),
	)

	req := httptest.NewRequest(http.MethodPost, "/api/payments", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusAccepted {
		t.Errorf("expected 202, got %d", rr.Code)
	}
}

func TestMetrics_Status200(t *testing.T) {
	handler := middleware.Metrics(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		}),
	)

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rr.Code)
	}
}

func TestMetrics_Status500(t *testing.T) {
	handler := middleware.Metrics(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		}),
	)

	req := httptest.NewRequest(http.MethodGet, "/api/payments", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusInternalServerError {
		t.Errorf("expected 500, got %d", rr.Code)
	}
}

func TestMetrics_DefaultStatus200_WhenNoWriteHeader(t *testing.T) {
	// Write без WriteHeader → статус 200.
	handler := middleware.Metrics(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Write([]byte("ok")) //nolint:errcheck
		}),
	)

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rr.Code)
	}
}

// Chain

func TestChain_AllMiddleware_Together(t *testing.T) {
	// Проверяем что вся цепочка работает без паник.
	handler := middleware.Recover(
		middleware.RequestID(
			middleware.Logging(
				middleware.Metrics(
					http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
						// Проверяем что request_id доступен в контексте.
						id := middleware.GetRequestID(r.Context())
						if id == "" {
							t.Error("request ID missing in chained middleware")
						}
						w.WriteHeader(http.StatusOK)
					}),
				),
			),
		),
	)

	req := httptest.NewRequest(http.MethodGet, "/api/payments", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rr.Code)
	}
	if rr.Header().Get("X-Request-ID") == "" {
		t.Error("expected X-Request-ID in response")
	}
}

func TestChain_PanicInHandler_RecoveredWithRequestID(t *testing.T) {
	// Паника внутри цепочки → перехватывается Recover, request_id сохраняется.
	existingID := "panic-test-id"

	handler := middleware.Recover(
		middleware.RequestID(
			http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				panic("handler panic")
			}),
		),
	)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-Request-ID", existingID)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusInternalServerError {
		t.Errorf("expected 500 after panic, got %d", rr.Code)
	}
}
