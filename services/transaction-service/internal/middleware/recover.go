package middleware

import (
	"log/slog"
	"net/http"
	"runtime/debug"
)

// Recover перехватывает паники в handler.
// Без него паника в горутине HTTP-хэндлера убьёт весь сервер.
func Recover(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if err := recover(); err != nil {
				slog.Error("panic recovered",
					"error", err,
					"path", r.URL.Path,
					"stack", string(debug.Stack()),
				)
				http.Error(w, `{"error":"internal server error"}`, http.StatusInternalServerError)
			}
		}()

		next.ServeHTTP(w, r)
	})
}
