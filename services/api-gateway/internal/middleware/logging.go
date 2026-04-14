package middleware

import (
	"log/slog"
	"net/http"
	"time"
)

// responseWriter оборачивает http.ResponseWriter для перехвата статус-кода.
// Стандартный ResponseWriter не даёт прочитать статус после WriteHeader.
type responseWriter struct {
	http.ResponseWriter
	status      int
	wroteHeader bool
}

func wrapResponseWriter(w http.ResponseWriter) *responseWriter {
	return &responseWriter{ResponseWriter: w, status: http.StatusOK}
}

func (rw *responseWriter) WriteHeader(code int) {
	if rw.wroteHeader {
		return
	}
	rw.status = code
	rw.wroteHeader = true
	rw.ResponseWriter.WriteHeader(code)
}

func (rw *responseWriter) Write(b []byte) (int, error) {
	// Если WriteHeader не вызывался явно — статус 200.
	if !rw.wroteHeader {
		rw.WriteHeader(http.StatusOK)
	}
	return rw.ResponseWriter.Write(b)
}

// Logging логирует каждый запрос: метод, путь, статус, latency, merchant_id.
// Располагается после RequestID — чтобы request_id уже был в контексте.
// Располагается до Auth — логируем и неаутентифицированные запросы.
func Logging(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			wrapped := wrapResponseWriter(w)

			next.ServeHTTP(wrapped, r)

			duration := time.Since(start)
			requestID := requestIDFromContext(r.Context())

			// merchant_id доступен только после успешной аутентификации.
			// Для неаутентифицированных запросов будет пустым — это нормально.
			merchantID := ""
			if info := MerchantFromContext(r.Context()); info != nil {
				merchantID = info.MerchantID
			}

			// Уровень лога зависит от статуса ответа.
			// 4xx — info: клиентские ошибки штатны.
			// 5xx — warn: проблема на стороне сервера.
			attrs := []any{
				slog.String("method", r.Method),
				slog.String("path", r.URL.Path),
				slog.Int("status", wrapped.status),
				slog.Int64("latency_ms", duration.Milliseconds()),
				slog.String("request_id", requestID),
				slog.String("merchant_id", merchantID),
				slog.String("remote_addr", r.RemoteAddr),
			}

			switch {
			case wrapped.status >= 500:
				logger.WarnContext(r.Context(), "request completed", attrs...)
			default:
				logger.InfoContext(r.Context(), "request completed", attrs...)
			}
		})
	}
}
