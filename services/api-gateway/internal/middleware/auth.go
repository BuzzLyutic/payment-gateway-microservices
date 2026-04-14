package middleware

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"github.com/BuzzLyutic/payment-gateway-microservices/services/api-gateway/internal/auth"
)

const headerAPIKey = "X-API-Key"

// Auth проверяет API-ключ и добавляет MerchantInfo в контекст.
// При ошибке — возвращает 401 и прерывает цепочку.
func Auth(store *auth.Store, logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			apiKey := r.Header.Get(headerAPIKey)

			info, err := store.Lookup(r.Context(), apiKey)
			if err != nil {
				requestID := requestIDFromContext(r.Context())

				var msg string
				switch {
				case errors.Is(err, auth.ErrMissingKey):
					msg = "missing X-API-Key header"
				case errors.Is(err, auth.ErrInvalidKey):
					msg = "invalid or inactive API key"
				default:
					// Redis недоступен или другая инфраструктурная ошибка.
					// Логируем как error — это не клиентская проблема.
					logger.ErrorContext(r.Context(), "auth: lookup failed",
						slog.String("request_id", requestID),
						slog.String("error", err.Error()),
					)
					writeJSON(w, http.StatusInternalServerError,
						map[string]string{"error": "internal server error"},
					)
					return
				}

				logger.InfoContext(r.Context(), "auth: rejected",
					slog.String("request_id", requestID),
					slog.String("reason", msg),
					slog.String("remote_addr", r.RemoteAddr),
				)

				writeJSON(w, http.StatusUnauthorized,
					map[string]string{"error": msg},
				)
				return
			}

			// Аутентификация успешна — добавляем merchant в контекст.
			// Logging middleware прочитает его оттуда для логирования.
			ctx := WithMerchant(r.Context(), info)

			// X-API-Key не пробрасываем во внутренние сервисы —
			// удаляем из запроса до проксирования.
			r.Header.Del(headerAPIKey)

			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// writeJSON — вспомогательная функция записи JSON-ответа.
// Используется во всех middleware для единообразия ответов.
func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(body)
}
