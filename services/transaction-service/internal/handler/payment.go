package handler

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/BuzzLyutic/payment-gateway-microservices/services/transaction-service/internal/domain"
	"github.com/BuzzLyutic/payment-gateway-microservices/services/transaction-service/internal/service"
)

// запрос / ответ

type CreatePaymentRequest struct {
	Amount        int64             `json:"amount"`
	Currency      string            `json:"currency"`
	Description   string            `json:"description"`
	MerchantID    string            `json:"merchant_id"`
	PaymentMethod PaymentMethodDTO  `json:"payment_method"`
	Metadata      map[string]string `json:"metadata"`
}

type PaymentMethodDTO struct {
	Type       string `json:"type"`
	CardNumber string `json:"card_number"`
	ExpMonth   int    `json:"exp_month"`
	ExpYear    int    `json:"exp_year"`
}

type CreatePaymentResponse struct {
	ID        string `json:"id"`
	Status    string `json:"status"`
	Amount    int64  `json:"amount"`
	Currency  string `json:"currency"`
	CreatedAt string `json:"created_at"`
}

type GetPaymentResponse struct {
	ID        string `json:"id"`
	Status    string `json:"status"`
	Amount    int64  `json:"amount"`
	Currency  string `json:"currency"`
	Provider  string `json:"provider,omitempty"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
}

type ErrorResponse struct {
	Error   string `json:"error"`
	Details string `json:"details,omitempty"`
}


type PaymentHandler struct {
	svc *service.TransactionService
}

func NewPaymentHandler(svc *service.TransactionService) *PaymentHandler {
	return &PaymentHandler{svc: svc}
}

func (h *PaymentHandler) Register(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/v1/payments", h.CreatePayment)
	mux.HandleFunc("GET /api/v1/payments/{id}", h.GetPayment)
}

// CreatePayment - POST /api/v1/payments
func (h *PaymentHandler) CreatePayment(w http.ResponseWriter, r *http.Request) {
	// Декодируем тело
	var req CreatePaymentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{
			Error: "invalid request body",
		})
		return
	}

	// Валидация
	if problems := validateCreatePayment(req); len(problems) > 0 {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{
			Error:   "validation failed",
			Details: strings.Join(problems, "; "),
		})
		return
	}

	// Idempotency key - обязателен
	idempotencyKey := r.Header.Get("X-Idempotency-Key")
	if idempotencyKey == "" {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{
			Error: "X-Idempotency-Key header is required",
		})
		return
	}

	// Создаём платёж
	tx, err := h.svc.CreatePayment(r.Context(), service.CreatePaymentRequest{
		IdempotencyKey: idempotencyKey,
		MerchantID:     req.MerchantID,
		Amount:         req.Amount,
		Currency:       req.Currency,
		Description:    req.Description,
		Metadata:       req.Metadata,
	})
	if err != nil {
		slog.Error("create payment failed", "error", err)
		writeJSON(w, http.StatusInternalServerError, ErrorResponse{
			Error: "internal server error",
		})
		return
	}

	writeJSON(w, http.StatusCreated, CreatePaymentResponse{
		ID:        tx.ID,
		Status:    string(tx.Status),
		Amount:    tx.Amount,
		Currency:  tx.Currency,
		CreatedAt: tx.CreatedAt.Format("2006-01-02T15:04:05Z"),
	})
}

// GetPayment - GET /api/v1/payments/{id}
func (h *PaymentHandler) GetPayment(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{
			Error: "payment id is required",
		})
		return
	}

	tx, err := h.svc.GetPayment(r.Context(), id)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			writeJSON(w, http.StatusNotFound, ErrorResponse{
				Error: "payment not found",
			})
			return
		}
		slog.Error("get payment failed", "error", err, "id", id)
		writeJSON(w, http.StatusInternalServerError, ErrorResponse{
			Error: "internal server error",
		})
		return
	}

	resp := GetPaymentResponse{
		ID:        tx.ID,
		Status:    string(tx.Status),
		Amount:    tx.Amount,
		Currency:  tx.Currency,
		CreatedAt: tx.CreatedAt.Format("2006-01-02T15:04:05Z"),
		UpdatedAt: tx.UpdatedAt.Format("2006-01-02T15:04:05Z"),
	}

	// Безопасное разыменование указателя
	if tx.Provider != nil {
		resp.Provider = *tx.Provider
	}

	writeJSON(w, http.StatusOK, resp)
}

// Вспомогательные функции

func validateCreatePayment(req CreatePaymentRequest) []string {
	var problems []string

	if req.Amount <= 0 {
		problems = append(problems, "amount must be positive")
	}
	if req.Currency == "" {
		problems = append(problems, "currency is required")
	}
	if req.MerchantID == "" {
		problems = append(problems, "merchant_id is required")
	}

	return problems
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		slog.Error("failed to write response", "error", err)
	}
}
