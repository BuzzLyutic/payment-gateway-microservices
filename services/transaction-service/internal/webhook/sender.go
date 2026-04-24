package webhook

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"time"
)

const (
	// sendTimeout — таймаут одного HTTP запроса к мерчанту.
	sendTimeout = 10 * time.Second
)

// Sender отправляет HTTP POST запросы на webhook URL мерчанта.
type Sender struct {
	client *http.Client
}

func NewSender() *Sender {
	return &Sender{
		client: &http.Client{
			Timeout: sendTimeout,
			// Не следуем редиректам — webhook URL должен быть прямым.
			// Редирект может означать неправильно настроенный URL мерчанта.
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
	}
}

// Send отправляет webhook payload на указанный URL.
// Возвращает ошибку если HTTP статус не 2xx.
func (s *Sender) Send(ctx context.Context, webhookURL, secret string, payload []byte) error {
	timestamp := strconv.FormatInt(time.Now().Unix(), 10)
	signature := computeSignature(secret, timestamp, payload)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, webhookURL, bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Webhook-Timestamp", timestamp)
	req.Header.Set("X-Webhook-Signature", "sha256="+signature)

	resp, err := s.client.Do(req)
	if err != nil {
		return fmt.Errorf("http request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	slog.Debug("webhook delivered",
		"url", webhookURL,
		"status", resp.StatusCode,
	)

	return nil
}

// computeSignature вычисляет HMAC-SHA256 подпись.
// Формат подписываемой строки: "{timestamp}.{body}"
// Мерчант может верифицировать подпись, зная секрет:
//
//	mac := hmac.New(sha256.New, []byte(secret))
//	mac.Write([]byte(timestamp + "." + string(body)))
//	expected := hex.EncodeToString(mac.Sum(nil))
//	valid := hmac.Equal([]byte(expected), []byte(receivedSignature))
func computeSignature(secret, timestamp string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(timestamp + "."))
	mac.Write(body)
	return hex.EncodeToString(mac.Sum(nil))
}
