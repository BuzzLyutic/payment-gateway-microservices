package worker

import (
	"context"
	"log/slog"
	"time"
)

// Processor - интерфейс, который реализует TransactionService.
// Worker зависит от абстракции, а не от конкретного сервиса.
type Processor interface {
	ProcessPendingPayments(ctx context.Context, limit int) (int, error)
	ResolveStuckPayments(ctx context.Context, threshold time.Duration, limit int) (int, error)
}

// Worker управляет двумя фоновыми задачами:
// 1. Публикация payment.created для pending транзакций
// 2. Перевод застрявших processing транзакций в failed
type Worker struct {
	processor     Processor
	interval      time.Duration
	batchSize     int
	stuckInterval time.Duration
	stuckTimeout  time.Duration
}

func New(processor Processor, interval time.Duration, batchSize int) *Worker {
	return &Worker{
		processor:     processor,
		interval:      interval,
		batchSize:     batchSize,
		stuckInterval: 5 * time.Minute,  // проверяем каждые 5 минут
		stuckTimeout:  10 * time.Minute, // считаем stuck после 10 минут в processing
	}
}

// Run запускает воркер. Блокирует до отмены контекста.
// Вызывать в отдельной горутине.
func (w *Worker) Run(ctx context.Context) {
	slog.Info("worker started",
		"interval", w.interval,
		"batch_size", w.batchSize,
		"stuck_interval", w.stuckInterval,
		"stuck_timeout", w.stuckTimeout,
	)

	pendingTicker := time.NewTicker(w.interval)
	stuckTicker := time.NewTicker(w.stuckInterval)
	defer pendingTicker.Stop()
	defer stuckTicker.Stop()

	for {
		select {
		case <-ctx.Done():
			slog.Info("worker stopped")
			return
		case <-pendingTicker.C:
			w.processBatch(ctx)
		case <-stuckTicker.C:
			w.resolveStuck(ctx)
		}
	}
}

func (w *Worker) processBatch(ctx context.Context) {
	count, err := w.processor.ProcessPendingPayments(ctx, w.batchSize)
	if err != nil {
		slog.Error("worker: batch processing failed", "error", err)
		return
	}

	if count > 0 {
		slog.Info("worker: batch processed", "count", count)
	}
}

func (w *Worker) resolveStuck(ctx context.Context) {
	slog.Debug("worker: checking for stuck transactions")

	count, err := w.processor.ResolveStuckPayments(ctx, w.stuckTimeout, w.batchSize)
	if err != nil {
		slog.Error("worker: stuck resolution failed", "error", err)
		return
	}

	if count > 0 {
		slog.Warn("worker: resolved stuck transactions",
			"count", count,
			"threshold", w.stuckTimeout,
		)
	}
}
