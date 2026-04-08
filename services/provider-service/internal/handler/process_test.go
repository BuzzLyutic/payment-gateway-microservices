package handler_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/BuzzLyutic/payment-gateway-microservices/services/provider-service/internal/domain"
	"github.com/BuzzLyutic/payment-gateway-microservices/services/provider-service/internal/handler"
)

type mockProcessor struct {
	result *domain.ProcessResult
	err    error
}

func (m *mockProcessor) ProcessPayment(_ context.Context, _ *domain.ProcessRequest) (*domain.ProcessResult, error) {
	return m.result, m.err
}

func newProcessRequest(t *testing.T, body any) *http.Request {
	t.Helper()

	b, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("failed to marshal request: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/v1/process", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	return req
}

func TestProcessHandler_Success(t *testing.T) {
	processor := &mockProcessor{
		result: &domain.ProcessResult{
			TransactionID: "tx_001",
			Provider:      "mock_provider_a",
			ProviderTxID:  "mock_tx_abc",
			Status:        domain.ResultCaptured,
			LatencyMs:     150,
		},
	}

	h := handler.NewProcessHandler(processor)
	mux := http.NewServeMux()
	h.Register(mux)

	body := map[string]any{
		"transaction_id": "tx_001",
		"merchant_id":    "merchant_1",
		"amount":         10000,
		"currency":       "RUB",
		"payment_method": "card",
	}

	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, newProcessRequest(t, body))

	if rr.Code != http.StatusOK {
		t.Errorf("expected 200, got: %d", rr.Code)
	}

	var result domain.ProcessResult
	if err := json.NewDecoder(rr.Body).Decode(&result); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if result.Status != domain.ResultCaptured {
		t.Errorf("expected captured, got: %v", result.Status)
	}
	if result.Provider != "mock_provider_a" {
		t.Errorf("expected mock_provider_a, got: %v", result.Provider)
	}
	if result.ProviderTxID != "mock_tx_abc" {
		t.Errorf("expected mock_tx_abc, got: %v", result.ProviderTxID)
	}
}

func TestProcessHandler_ValidationError_MissingTransactionID(t *testing.T) {
	h := handler.NewProcessHandler(&mockProcessor{})
	mux := http.NewServeMux()
	h.Register(mux)

	body := map[string]any{
		// transaction_id отсутствует
		"merchant_id":    "merchant_1",
		"amount":         10000,
		"currency":       "RUB",
		"payment_method": "card",
	}

	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, newProcessRequest(t, body))

	if rr.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got: %d", rr.Code)
	}
}

func TestProcessHandler_ValidationError_ZeroAmount(t *testing.T) {
	h := handler.NewProcessHandler(&mockProcessor{})
	mux := http.NewServeMux()
	h.Register(mux)

	body := map[string]any{
		"transaction_id": "tx_001",
		"merchant_id":    "merchant_1",
		"amount":         0,
		"currency":       "RUB",
		"payment_method": "card",
	}

	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, newProcessRequest(t, body))

	if rr.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got: %d", rr.Code)
	}
}

func TestProcessHandler_ValidationError_MissingCurrency(t *testing.T) {
	h := handler.NewProcessHandler(&mockProcessor{})
	mux := http.NewServeMux()
	h.Register(mux)

	body := map[string]any{
		"transaction_id": "tx_001",
		"merchant_id":    "merchant_1",
		"amount":         10000,
		"payment_method": "card",
		// currency отсутствует
	}

	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, newProcessRequest(t, body))

	if rr.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got: %d", rr.Code)
	}
}

func TestProcessHandler_InvalidJSON(t *testing.T) {
	h := handler.NewProcessHandler(&mockProcessor{})
	mux := http.NewServeMux()
	h.Register(mux)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/process",
		bytes.NewBufferString("not json"))
	req.Header.Set("Content-Type", "application/json")

	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got: %d", rr.Code)
	}
}

func TestProcessHandler_ServiceError(t *testing.T) {
	processor := &mockProcessor{
		err: errors.New("database unavailable"),
	}

	h := handler.NewProcessHandler(processor)
	mux := http.NewServeMux()
	h.Register(mux)

	body := map[string]any{
		"transaction_id": "tx_001",
		"merchant_id":    "merchant_1",
		"amount":         10000,
		"currency":       "RUB",
		"payment_method": "card",
	}

	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, newProcessRequest(t, body))

	if rr.Code != http.StatusInternalServerError {
		t.Errorf("expected 500, got: %d", rr.Code)
	}
}

func TestProcessHandler_ContentType(t *testing.T) {
	processor := &mockProcessor{
		result: &domain.ProcessResult{
			TransactionID: "tx_001",
			Status:        domain.ResultCaptured,
		},
	}

	h := handler.NewProcessHandler(processor)
	mux := http.NewServeMux()
	h.Register(mux)

	body := map[string]any{
		"transaction_id": "tx_001",
		"merchant_id":    "merchant_1",
		"amount":         10000,
		"currency":       "RUB",
		"payment_method": "card",
	}

	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, newProcessRequest(t, body))

	ct := rr.Header().Get("Content-Type")
	if ct != "application/json" {
		t.Errorf("expected Content-Type=application/json, got: %v", ct)
	}
}
