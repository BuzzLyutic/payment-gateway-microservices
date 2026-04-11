package health

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"github.com/nats-io/nats.go"
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
	Dependencies map[string]Status `json:"dependencies"`
}

// Handler проверяет доступность Redis и NATS.
type Handler struct {
	redis  *redis.Client
	nats   *nats.Conn
	logger *slog.Logger
}

func NewHandler(rdb *redis.Client, nc *nats.Conn, logger *slog.Logger) *Handler {
	return &Handler{redis: rdb, nats: nc, logger: logger}
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()

	deps := make(map[string]Status)
	healthy := true

	// Проверка Redis
	if err := h.redis.Ping(ctx).Err(); err != nil {
		deps["redis"] = Status{Status: "unavailable", Error: err.Error()}
		healthy = false
		h.logger.WarnContext(ctx, "health check: redis unavailable",
			slog.String("error", err.Error()),
		)
	} else {
		deps["redis"] = Status{Status: "ok"}
	}

	// Проверка NATS
	if !h.nats.IsConnected() {
		deps["nats"] = Status{Status: "unavailable"}
		healthy = false
		h.logger.WarnContext(ctx, "health check: nats unavailable")
	} else {
		deps["nats"] = Status{Status: "ok"}
	}

	resp := Response{
		Status:       "ok",
		Dependencies: deps,
	}

	statusCode := http.StatusOK
	if !healthy {
		resp.Status = "unavailable"
		statusCode = http.StatusServiceUnavailable
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)

	if err := json.NewEncoder(w).Encode(resp); err != nil {
		h.logger.ErrorContext(ctx, "health: failed to write response",
			slog.String("error", err.Error()),
		)
	}
}
