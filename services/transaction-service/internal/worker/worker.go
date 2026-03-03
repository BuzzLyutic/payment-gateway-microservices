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
}

type Worker struct {
	processor Processor
	interval  time.Duration
	batchSize int
}

func New(processor Processor, interval time.Duration, batchSize int) *Worker {
	return &Worker{
		processor: processor,
		interval:  interval,
		batchSize: batchSize,
	}
}

// Run запускает воркер. Блокирует до отмены контекста.
// Вызывать в отдельной горутине.
func (w *Worker) Run(ctx context.Context) {
	slog.Info("worker started",
		"interval", w.interval,
		"batch_size", w.batchSize,
	)

	ticker := time.NewTicker(w.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			slog.Info("worker stopped")
			return
		case <-ticker.C:
			w.processBatch(ctx)
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
