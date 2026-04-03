package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/BuzzLyutic/payment-gateway-microservices/services/provider-service/internal/domain"
	"github.com/BuzzLyutic/payment-gateway-microservices/services/provider-service/internal/middleware"
)

// PaymentProcessor - интерфейс, который реализует service.Service.
type PaymentProcessor interface {
	ProcessPayment(ctx context.Context, req *domain.ProcessRequest) (*domain.ProcessResult, error)
}

type ProcessHandler struct {
	svc PaymentProcessor
}

func NewProcessHandler(svc PaymentProcessor) *ProcessHandler {
	return &ProcessHandler{svc: svc}
}

func (h *ProcessHandler) Register(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/v1/process", h.Process)
}

func (h *ProcessHandler) Process(w http.ResponseWriter, r *http.Request) {
	var req domain.ProcessRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if err := validateRequest(&req); err != nil {
		respondError(w, http.StatusBadRequest, err.Error())
		return
	}

	slog.Info("processing payment request",
		"request_id", middleware.GetRequestID(r.Context()),
		"transaction_id", req.TransactionID,
		"currency", req.Currency,
		"payment_method", req.PaymentMethod,
	)

	result, err := h.svc.ProcessPayment(r.Context(), &req)
	if err != nil {
		slog.Error("process payment error",
			"request_id", middleware.GetRequestID(r.Context()),
			"error", err,
		)
		respondError(w, http.StatusInternalServerError, "internal error")
		return
	}

	respondJSON(w, http.StatusOK, result)
}

func validateRequest(req *domain.ProcessRequest) error {
	switch {
	case req.TransactionID == "":
		return fmt.Errorf("transaction_id is required")
	case req.MerchantID == "":
		return fmt.Errorf("merchant_id is required")
	case req.Amount <= 0:
		return fmt.Errorf("amount must be positive")
	case req.Currency == "":
		return fmt.Errorf("currency is required")
	case req.PaymentMethod == "":
		return fmt.Errorf("payment_method is required")
	}
	return nil
}

func respondJSON(w http.ResponseWriter, code int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(data)
}

func respondError(w http.ResponseWriter, code int, message string) {
	respondJSON(w, code, map[string]string{"error": message})
}
