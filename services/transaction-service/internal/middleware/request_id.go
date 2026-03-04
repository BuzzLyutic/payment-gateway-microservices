package middleware

import (
	"context"
	"net/http"

	"crypto/rand"
	"fmt"
)

type contextKey string

const RequestIDKey contextKey = "request_id"

// RequestID добавляет уникальный идентификатор запроса.
// Если клиент передал X-Request-ID - используем его, иначе генерируем.
func RequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get("X-Request-ID")
		if id == "" {
			id = generateID()
		}

		// Добавляем в контекст - доступно в handler и service
		ctx := context.WithValue(r.Context(), RequestIDKey, id)

		// Добавляем в заголовок ответа - клиент может использовать для отладки
		w.Header().Set("X-Request-ID", id)

		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func generateID() string {
	b := make([]byte, 8)
	rand.Read(b)
	return fmt.Sprintf("req_%x", b)
}
