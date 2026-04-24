package webhook_test

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/BuzzLyutic/payment-gateway-microservices/services/transaction-service/internal/webhook"
)

// Хелперы

// verifySignature повторяет логику мерчанта для проверки подписи.
func verifySignature(secret, timestamp string, body []byte, receivedSig string) bool {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(timestamp + "."))
	mac.Write(body)
	expected := hex.EncodeToString(mac.Sum(nil))
	return hmac.Equal([]byte(expected), []byte(receivedSig))
}

// Тесты Send

// TestSend_Success — успешная доставка, сервер возвращает 200.
func TestSend_Success(t *testing.T) {
	var (
		receivedBody      []byte
		receivedTimestamp string
		receivedSignature string
	)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("expected Content-Type application/json")
		}

		receivedTimestamp = r.Header.Get("X-Webhook-Timestamp")
		sig := r.Header.Get("X-Webhook-Signature")
		// Срезаем "sha256=" префикс
		if len(sig) > 7 {
			receivedSignature = sig[7:]
		}

		buf := make([]byte, r.ContentLength)
		r.Body.Read(buf)
		receivedBody = buf

		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	sender := webhook.NewSender()
	payload := []byte(`{"event":"payment.captured","transaction_id":"tx-001"}`)
	secret := "test-secret-key"

	err := sender.Send(context.Background(), server.URL, secret, payload)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Проверяем подпись.
	if !verifySignature(secret, receivedTimestamp, receivedBody, receivedSignature) {
		t.Error("signature verification failed")
	}
}

// TestSend_Non2xxStatus — сервер возвращает 500 → ошибка.
func TestSend_Non2xxStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	sender := webhook.NewSender()
	err := sender.Send(context.Background(), server.URL, "secret", []byte(`{}`))
	if err == nil {
		t.Fatal("expected error for 500 status")
	}
}

// TestSend_404Status — сервер возвращает 404 → ошибка.
func TestSend_404Status(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	sender := webhook.NewSender()
	err := sender.Send(context.Background(), server.URL, "secret", []byte(`{}`))
	if err == nil {
		t.Fatal("expected error for 404 status")
	}
}

// TestSend_201Status — 201 тоже считается успехом (2xx).
func TestSend_201Status(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
	}))
	defer server.Close()

	sender := webhook.NewSender()
	err := sender.Send(context.Background(), server.URL, "secret", []byte(`{}`))
	if err != nil {
		t.Fatalf("201 must be treated as success: %v", err)
	}
}

// TestSend_InvalidURL — невалидный URL → ошибка на этапе запроса.
func TestSend_InvalidURL(t *testing.T) {
	sender := webhook.NewSender()
	err := sender.Send(context.Background(), "http://localhost:0/webhook", "secret", []byte(`{}`))
	if err == nil {
		t.Fatal("expected error for unreachable URL")
	}
}

// TestSend_ContextCanceled — отменённый контекст → ошибка.
func TestSend_ContextCanceled(t *testing.T) {
	// Сервер с задержкой — успеем отменить контекст.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(100 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // отменяем сразу

	sender := webhook.NewSender()
	err := sender.Send(ctx, server.URL, "secret", []byte(`{}`))
	if err == nil {
		t.Fatal("expected error for canceled context")
	}
}

// TestSend_NoRedirectFollow — 301 не следует редиректу.
func TestSend_NoRedirectFollow(t *testing.T) {
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer target.Close()

	// Сервер, который редиректит на target.
	redirectServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL, http.StatusMovedPermanently)
	}))
	defer redirectServer.Close()

	sender := webhook.NewSender()
	// Должна вернуть ошибку — 301 не 2xx, редиректы не следуются.
	err := sender.Send(context.Background(), redirectServer.URL, "secret", []byte(`{}`))
	if err == nil {
		t.Fatal("expected error: redirects must not be followed")
	}
}

// TestSend_HeadersSet — все обязательные заголовки присутствуют.
func TestSend_HeadersSet(t *testing.T) {
	var headers http.Header

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		headers = r.Header.Clone()
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	sender := webhook.NewSender()
	_ = sender.Send(context.Background(), server.URL, "secret", []byte(`{}`))

	required := []string{"Content-Type", "X-Webhook-Timestamp", "X-Webhook-Signature"}
	for _, h := range required {
		if headers.Get(h) == "" {
			t.Errorf("missing required header: %s", h)
		}
	}

	sig := headers.Get("X-Webhook-Signature")
	if len(sig) < 7 || sig[:7] != "sha256=" {
		t.Errorf("signature must start with 'sha256=', got: %q", sig)
	}
}

// Тесты computeSignature

// TestComputeSignature_Deterministic — одни данные → одна подпись.
func TestComputeSignature_Deterministic(t *testing.T) {
	sig1 := webhook.ExportComputeSignature("secret", "1234567890", []byte(`{"test":1}`))
	sig2 := webhook.ExportComputeSignature("secret", "1234567890", []byte(`{"test":1}`))

	if sig1 != sig2 {
		t.Errorf("signature not deterministic: %q != %q", sig1, sig2)
	}
}

// TestComputeSignature_DifferentSecrets — разные секреты → разные подписи.
func TestComputeSignature_DifferentSecrets(t *testing.T) {
	payload := []byte(`{"event":"payment.captured"}`)
	ts := "1234567890"

	sig1 := webhook.ExportComputeSignature("secret-a", ts, payload)
	sig2 := webhook.ExportComputeSignature("secret-b", ts, payload)

	if sig1 == sig2 {
		t.Error("different secrets must produce different signatures")
	}
}

// TestComputeSignature_DifferentTimestamps — разные timestamp → разные подписи.
func TestComputeSignature_DifferentTimestamps(t *testing.T) {
	payload := []byte(`{"event":"payment.captured"}`)

	sig1 := webhook.ExportComputeSignature("secret", "111", payload)
	sig2 := webhook.ExportComputeSignature("secret", "222", payload)

	if sig1 == sig2 {
		t.Error("different timestamps must produce different signatures")
	}
}

// TestComputeSignature_HexEncoded — результат является валидным hex.
func TestComputeSignature_HexEncoded(t *testing.T) {
	sig := webhook.ExportComputeSignature("secret", "ts", []byte(`body`))

	decoded, err := hex.DecodeString(sig)
	if err != nil {
		t.Errorf("signature is not valid hex: %v", err)
	}
	// SHA256 = 32 байта.
	if len(decoded) != 32 {
		t.Errorf("expected 32-byte SHA256, got %d bytes", len(decoded))
	}
}
