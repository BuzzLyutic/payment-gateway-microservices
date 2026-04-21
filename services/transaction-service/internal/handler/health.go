package handler

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"time"
)

type HealthResponse struct {
	Status string            `json:"status"`
	Checks map[string]string `json:"checks"`
}

// Pinger - интерфейс для проверки доступности зависимости.
// Реализуется репозиторием (PostgreSQL), позже - Redis-клиентом.
type Pinger interface {
	Ping(ctx context.Context) error
}

type HealthHandler struct {
	db    Pinger
	redis Pinger
}

func NewHealthHandler(db, redis Pinger) *HealthHandler {
	return &HealthHandler{
		db:    db,
		redis: redis,
	}
}

func (h *HealthHandler) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /health", h.Health)
}

func (h *HealthHandler) Health(w http.ResponseWriter, r *http.Request) {
	checks := map[string]string{}
	overallStatus := "healthy"

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	// PostgreSQL
	if err := h.db.Ping(ctx); err != nil {
		checks["database"] = "unavailable"
		overallStatus = "unhealthy"
		slog.Error("health check: database unavailable", "error", err)
	} else {
		checks["database"] = "ok"
	}

	// Redis
	if err := h.redis.Ping(ctx); err != nil {
		checks["redis"] = "unavailable"
		overallStatus = "unhealthy"
		slog.Error("health check: redis unavailable", "error", err)
	} else {
		checks["redis"] = "ok"
	}

	resp := HealthResponse{
		Status: overallStatus,
		Checks: checks,
	}

	w.Header().Set("Content-Type", "application/json")

	if overallStatus != "healthy" {
		w.WriteHeader(http.StatusServiceUnavailable)
	} else {
		w.WriteHeader(http.StatusOK)
	}

	if err := json.NewEncoder(w).Encode(resp); err != nil {
		slog.Error("failed to encode health response", "error", err)
	}
}
