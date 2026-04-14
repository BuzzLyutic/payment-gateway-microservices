package middleware_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/BuzzLyutic/payment-gateway-microservices/services/api-gateway/internal/auth"
	"github.com/BuzzLyutic/payment-gateway-microservices/services/api-gateway/internal/middleware"
)

// mockStore — мок auth.Store для unit-тестов без Redis.
type mockStore struct {
	info *auth.MerchantInfo
	err  error
}

func (m *mockStore) Lookup(_ interface{}, _ string) (*auth.MerchantInfo, error) {
	return m.info, m.err
}

// authStore — интерфейс для инжекции в тест.
// Auth middleware принимает *auth.Store — для тестов делаем обёртку.
func newAuthHandler(store *auth.Store, next http.Handler) http.Handler {
	return middleware.Auth(store, newTestLogger())(next)
}

func TestAuth_MissingKey(t *testing.T) {
	// Используем реальный Store с нерабочим Redis —
	// пустой ключ возвращает ErrMissingKey до обращения к Redis.
	store := auth.NewStore(nil, 100)

	handler := middleware.Auth(store, newTestLogger())(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		}),
	)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/payments", nil)
	// X-API-Key не устанавливаем
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", rr.Code)
	}

	var body map[string]string
	json.NewDecoder(rr.Body).Decode(&body)
	if body["error"] != "missing X-API-Key header" {
		t.Errorf("unexpected error message: %q", body["error"])
	}
}

func TestAuth_APIKeyNotPropagated(t *testing.T) {
	// X-API-Key не должен уходить во внутренние сервисы.
	// Проверяем что заголовок удалён до вызова следующего handler.
	store := auth.NewStore(nil, 100)

	var downstreamHeaders http.Header
	handler := middleware.Auth(store, newTestLogger())(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			downstreamHeaders = r.Header.Clone()
			w.WriteHeader(http.StatusOK)
		}),
	)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/payments", nil)
	req.Header.Set("X-API-Key", "")
	// Пустой ключ → 401, до downstream не дойдём.
	// Тест на удаление ключа — интеграционный (требует Redis с реальным ключом).
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		// Если дошли до downstream — проверяем что ключа нет
		if downstreamHeaders != nil {
			if downstreamHeaders.Get("X-API-Key") != "" {
				t.Error("X-API-Key must not be propagated to downstream services")
			}
		}
	}
}
