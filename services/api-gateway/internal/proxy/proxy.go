package proxy

import (
	"log/slog"
	"net/http"
	"net/http/httputil"
	"net/url"
	"time"

	"github.com/BuzzLyutic/payment-gateway-microservices/services/api-gateway/internal/contextkeys"
	"github.com/BuzzLyutic/payment-gateway-microservices/services/api-gateway/internal/middleware"
)

const (
	headerMerchantID = "X-Merchant-ID"
	headerRequestID  = "X-Request-ID"
	proxyTimeout     = 25 * time.Second
)

func New(targetURL string, logger *slog.Logger) (http.Handler, error) {
	target, err := url.Parse(targetURL)
	if err != nil {
		return nil, err
	}

	transport := &http.Transport{
		ResponseHeaderTimeout: proxyTimeout,
		IdleConnTimeout:       90 * time.Second,
		MaxIdleConns:          100,
		MaxIdleConnsPerHost:   10,
	}

	p := &httputil.ReverseProxy{
		Director: func(r *http.Request) {
			r.URL.Scheme = target.Scheme
			r.URL.Host = target.Host
			r.Host = target.Host

			if info := middleware.MerchantFromContext(r.Context()); info != nil {
				r.Header.Set(headerMerchantID, info.MerchantID)
			}

			// Читаем request_id через contextkeys — нет дублирования логики.
			if v, ok := r.Context().Value(contextkeys.RequestID).(string); ok && v != "" {
				r.Header.Set(headerRequestID, v)
			}
		},

		Transport: transport,

		ErrorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
			requestID, _ := r.Context().Value(contextkeys.RequestID).(string)

			merchantID := ""
			if info := middleware.MerchantFromContext(r.Context()); info != nil {
				merchantID = info.MerchantID
			}

			logger.ErrorContext(r.Context(), "proxy: upstream error",
				slog.String("error", err.Error()),
				slog.String("request_id", requestID),
				slog.String("merchant_id", merchantID),
				slog.String("method", r.Method),
				slog.String("path", r.URL.Path),
			)

			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadGateway)
			w.Write([]byte(`{"error":"upstream service unavailable"}`))
		},

		ModifyResponse: func(resp *http.Response) error {
			if resp.StatusCode >= 500 {
				logger.WarnContext(resp.Request.Context(), "proxy: upstream 5xx",
					slog.Int("status", resp.StatusCode),
					slog.String("method", resp.Request.Method),
					slog.String("path", resp.Request.URL.Path),
				)
			}
			return nil
		},
	}

	return p, nil
}
