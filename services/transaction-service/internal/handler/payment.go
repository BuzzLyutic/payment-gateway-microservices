package handler

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/BuzzLyutic/payment-gateway-microservices/services/transaction-service/internal/domain"
	"github.com/BuzzLyutic/payment-gateway-microservices/services/transaction-service/internal/service"
)

// PaymentService — интерфейс бизнес-логики.
type PaymentService interface {
	CreatePayment(ctx context.Context, req service.CreatePaymentRequest) (*domain.Transaction, error)
	GetPayment(ctx context.Context, id string) (*domain.Transaction, error)
}

// IdempotencyStore — интерфейс хранилища идемпотентности.
type IdempotencyStore interface {
	Lock(ctx context.Context, key string) (bool, error)
	SetTransactionID(ctx context.Context, key string, txID string) error
	GetTransactionID(ctx context.Context, key string) (string, error)
	Unlock(ctx context.Context, key string) error
}

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

type PaymentResponse struct {
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
	svc        PaymentService
	idempotent IdempotencyStore
}

func NewPaymentHandler(svc PaymentService, idempotent IdempotencyStore) *PaymentHandler {
	return &PaymentHandler{
		svc:        svc,
		idempotent: idempotent,
	}
}

func (h *PaymentHandler) Register(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/v1/payments", h.CreatePayment)
	mux.HandleFunc("GET /api/v1/payments/{id}", h.GetPayment)
}

// CreatePayment - POST /api/v1/payments
func (h *PaymentHandler) CreatePayment(w http.ResponseWriter, r *http.Request) {
	idempotencyKey := r.Header.Get("X-Idempotency-Key")
	if idempotencyKey == "" {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{
			Error: "X-Idempotency-Key header is required",
		})
		return
	}

	// Пытаемся захватить ключ
	locked, err := h.idempotent.Lock(r.Context(), idempotencyKey)
	if err != nil {
		slog.Error("idempotency lock failed", "error", err)
		// Redis недоступен - продолжаем, UNIQUE constraint в БД спасёт
	}

	if !locked && err == nil {
		// Ключ уже существует - это повторный запрос
		txID, err := h.idempotent.GetTransactionID(r.Context(), idempotencyKey)
		if err != nil {
			slog.Error("failed to get idempotent tx id", "error", err)
			writeJSON(w, http.StatusInternalServerError, ErrorResponse{
				Error: "internal server error",
			})
			return
		}

		// Транзакция ещё создаётся другим запросом
		if txID == "processing" {
			writeJSON(w, http.StatusConflict, ErrorResponse{
				Error: "request is being processed",
			})
			return
		}

		// Достаём актуальные данные из БД
		slog.Info("idempotent request detected", "key", idempotencyKey, "tx_id", txID)
		tx, err := h.svc.GetPayment(r.Context(), txID)
		if err != nil {
			slog.Error("failed to fetch idempotent transaction", "error", err)
			writeJSON(w, http.StatusInternalServerError, ErrorResponse{
				Error: "internal server error",
			})
			return
		}

		writeJSON(w, http.StatusOK, toPaymentResponse(tx))
		return
	}

	// Новый запрос - декодируем и валидируем
	var req CreatePaymentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.idempotent.Unlock(r.Context(), idempotencyKey) // откатываем лок
		writeJSON(w, http.StatusBadRequest, ErrorResponse{
			Error: "invalid request body",
		})
		return
	}

	if problems := validateCreatePayment(req); len(problems) > 0 {
		h.idempotent.Unlock(r.Context(), idempotencyKey) // откатываем лок
		writeJSON(w, http.StatusBadRequest, ErrorResponse{
			Error:   "validation failed",
			Details: strings.Join(problems, "; "),
		})
		return
	}

	// Создаем транзакцию
	tx, err := h.svc.CreatePayment(r.Context(), service.CreatePaymentRequest{
		IdempotencyKey: idempotencyKey,
		MerchantID:     req.MerchantID,
		Amount:         req.Amount,
		Currency:       req.Currency,
		Description:    req.Description,
		Metadata:       req.Metadata,
	})
	if err != nil {
		h.idempotent.Unlock(r.Context(), idempotencyKey) // откатываем лок
		slog.Error("create payment failed", "error", err)
		writeJSON(w, http.StatusInternalServerError, ErrorResponse{
			Error: "internal server error",
		})
		return
	}

	// Заменяем placeholder на реальный ID
	if err := h.idempotent.SetTransactionID(r.Context(), idempotencyKey, tx.ID); err != nil {
		slog.Error("failed to set idempotency tx id", "error", err)
		// Не критично - транзакция уже создана
	}

	writeJSON(w, http.StatusCreated, toPaymentResponse(tx))
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

	writeJSON(w, http.StatusOK, toPaymentResponse(tx))
}

// Вспомогательные функции

func toPaymentResponse(tx *domain.Transaction) PaymentResponse {
	resp := PaymentResponse{
		ID:        tx.ID,
		Status:    string(tx.Status),
		Amount:    tx.Amount,
		Currency:  tx.Currency,
		CreatedAt: tx.CreatedAt.Format("2006-01-02T15:04:05Z"),
		UpdatedAt: tx.UpdatedAt.Format("2006-01-02T15:04:05Z"),
	}
	if tx.Provider != nil {
		resp.Provider = *tx.Provider
	}
	return resp
}

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
