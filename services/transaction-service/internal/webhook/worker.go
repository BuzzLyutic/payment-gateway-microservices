package webhook

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"
)

// workerRepo — зависимости воркера от репозитория.
type workerRepo interface {
	FetchPendingDeliveries(ctx context.Context, limit int) ([]*Delivery, error)
	GetMerchantConfig(ctx context.Context, merchantID string) (*MerchantConfig, error)
	MarkDelivered(ctx context.Context, id string) error
	MarkFailed(ctx context.Context, id string, attempts int, lastError string) error
}

// Worker читает pending webhook_deliveries и доставляет их мерчантам.
type Worker struct {
	repo      workerRepo
	sender    *Sender
	interval  time.Duration
	batchSize int
}

func NewWorker(repo workerRepo, interval time.Duration, batchSize int) *Worker {
	return &Worker{
		repo:      repo,
		sender:    NewSender(),
		interval:  interval,
		batchSize: batchSize,
	}
}

// Run запускает воркер. Блокирует до отмены контекста.
func (w *Worker) Run(ctx context.Context) {
	slog.Info("webhook worker started",
		"interval", w.interval,
		"batch_size", w.batchSize,
	)

	ticker := time.NewTicker(w.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			slog.Info("webhook worker stopped")
			return
		case <-ticker.C:
			w.processBatch(ctx)
		}
	}
}

func (w *Worker) processBatch(ctx context.Context) {
	deliveries, err := w.repo.FetchPendingDeliveries(ctx, w.batchSize)
	if err != nil {
		slog.Error("webhook worker: fetch failed", "error", err)
		return
	}

	if len(deliveries) == 0 {
		return
	}

	slog.Info("webhook worker: processing batch", "count", len(deliveries))

	for _, d := range deliveries {
		w.deliver(ctx, d)
	}
}

func (w *Worker) deliver(ctx context.Context, d *Delivery) {
	attempts := d.Attempts + 1

	// Получаем актуальный конфиг мерчанта.
	// Мерчант мог изменить webhook URL между попытками.
	cfg, err := w.repo.GetMerchantConfig(ctx, d.MerchantID)
	if err != nil || cfg == nil {
		slog.Warn("webhook worker: merchant config not found, skipping",
			"merchant_id", d.MerchantID,
			"delivery_id", d.ID,
		)
		// Помечаем как failed — мерчант не настроил webhook
		_ = w.repo.MarkFailed(ctx, d.ID, d.MaxAttempts, "merchant webhook not configured")
		return
	}

	// Логируем payload для отладки (только первые 200 символов)
	preview := previewJSON(d.Payload)
	slog.Debug("webhook worker: sending",
		"delivery_id", d.ID,
		"merchant_id", d.MerchantID,
		"url", cfg.WebhookURL,
		"attempt", attempts,
		"payload_preview", preview,
	)

	sendCtx, cancel := context.WithTimeout(ctx, sendTimeout)
	defer cancel()

	if err := w.sender.Send(sendCtx, cfg.WebhookURL, cfg.WebhookSecret, d.Payload); err != nil {
		slog.Warn("webhook worker: delivery failed",
			"delivery_id", d.ID,
			"merchant_id", d.MerchantID,
			"attempt", attempts,
			"max_attempts", d.MaxAttempts,
			"error", err,
		)
		_ = w.repo.MarkFailed(ctx, d.ID, attempts, err.Error())
		return
	}

	if err := w.repo.MarkDelivered(ctx, d.ID); err != nil {
		slog.Error("webhook worker: failed to mark delivered",
			"delivery_id", d.ID,
			"error", err,
		)
		return
	}

	slog.Info("webhook worker: delivered",
		"delivery_id", d.ID,
		"merchant_id", d.MerchantID,
		"attempt", attempts,
	)
}

// previewJSON возвращает краткое описание payload для логов.
func previewJSON(payload []byte) string {
	var m map[string]any
	if err := json.Unmarshal(payload, &m); err != nil {
		if len(payload) > 200 {
			return string(payload[:200])
		}
		return string(payload)
	}
	event, _ := m["event"].(string)
	txID, _ := m["transaction_id"].(string)
	return "event=" + event + " transaction_id=" + txID
}
