package middleware

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"runtime/debug"
)

// Recovery перехватывает паники, логирует stack trace и возвращает 500.
// Должен быть первым в цепочке middleware — оборачивает всё остальное.
func Recovery(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if rec := recover(); rec != nil {
					// Stack trace для диагностики — логируем полностью.
					stack := debug.Stack()

					logger.ErrorContext(r.Context(), "panic recovered",
						slog.Any("panic", rec),
						slog.String("stack", string(stack)),
						slog.String("method", r.Method),
						slog.String("path", r.URL.Path),
						slog.String("request_id", requestIDFromContext(r.Context())),
					)

					// Если заголовки уже отправлены — ничего не можем сделать.
					// http.ResponseWriter буферизует заголовки до первого Write,
					// поэтому в большинстве случаев успеем отправить 500.
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusInternalServerError)
					json.NewEncoder(w).Encode(map[string]string{
						"error": "internal server error",
					})
				}
			}()

			next.ServeHTTP(w, r)
		})
	}
}
