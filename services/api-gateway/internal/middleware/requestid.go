package middleware

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net/http"

	"github.com/BuzzLyutic/payment-gateway-microservices/services/api-gateway/internal/contextkeys"
)

const headerRequestID = "X-Request-ID"

func RequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestID := r.Header.Get(headerRequestID)
		if requestID == "" {
			requestID = generateID()
		}

		ctx := context.WithValue(r.Context(), contextkeys.RequestID, requestID)
		w.Header().Set(headerRequestID, requestID)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func requestIDFromContext(ctx context.Context) string {
	if v, ok := ctx.Value(contextkeys.RequestID).(string); ok {
		return v
	}
	return ""
}

func generateID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "unknown"
	}
	return hex.EncodeToString(b)
}
