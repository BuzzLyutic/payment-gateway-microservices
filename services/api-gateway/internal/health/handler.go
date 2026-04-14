package health

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"github.com/redis/go-redis/v9"
)

// Status — статус одной зависимости.
type Status struct {
	Status string `json:"status"`
	Error  string `json:"error,omitempty"`
}

// Response — тело ответа /health.
type Response struct {
	Status       string            `json:"status"`
	Service      string            `json:"service"`
	Dependencies map[string]Status `json:"dependencies"`
}

// Handler проверяет доступность Redis.
// NATS не является зависимостью gateway — только Redis (auth + rate limit).
type Handler struct {
	redis  *redis.Client
	logger *slog.Logger
}

func NewHandler(rdb *redis.Client, logger *slog.Logger) *Handler {
	return &Handler{redis: rdb, logger: logger}
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()

	deps := make(map[string]Status)
	healthy := true

	if err := h.redis.Ping(ctx).Err(); err != nil {
		deps["redis"] = Status{Status: "unavailable", Error: err.Error()}
		healthy = false
		h.logger.WarnContext(ctx, "health: redis unavailable",
			slog.String("error", err.Error()),
		)
	} else {
		deps["redis"] = Status{Status: "ok"}
	}

	resp := Response{
		Status:       "ok",
		Service:      "api-gateway",
		Dependencies: deps,
	}

	statusCode := http.StatusOK
	if !healthy {
		resp.Status = "unavailable"
		statusCode = http.StatusServiceUnavailable
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	json.NewEncoder(w).Encode(resp)
}
