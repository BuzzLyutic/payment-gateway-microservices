package handler

import (
	"context"
	"encoding/json"
	"net/http"
)

// Pinger - интерфейс проверки соединения. Реализует repository.
type Pinger interface {
	Ping(ctx context.Context) error
}

type HealthHandler struct {
	repo Pinger
}

func NewHealthHandler(repo Pinger) *HealthHandler {
	return &HealthHandler{repo: repo}
}

func (h *HealthHandler) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /health", h.Health)
}

func (h *HealthHandler) Health(w http.ResponseWriter, r *http.Request) {
	status := "ok"
	pgStatus := "up"

	if err := h.repo.Ping(r.Context()); err != nil {
		status = "degraded"
		pgStatus = "down"
	}

	w.Header().Set("Content-Type", "application/json")

	if status != "ok" {
		w.WriteHeader(http.StatusServiceUnavailable)
	}

	json.NewEncoder(w).Encode(map[string]any{
		"status": status,
		"components": map[string]string{
			"postgresql": pgStatus,
		},
	})
}
