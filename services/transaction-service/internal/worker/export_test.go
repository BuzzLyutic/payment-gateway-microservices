package worker

import "context"

// ExportResolveStuck открывает resolveStuck для тестирования.
func (w *Worker) ExportResolveStuck(ctx context.Context) {
	w.resolveStuck(ctx)
}
