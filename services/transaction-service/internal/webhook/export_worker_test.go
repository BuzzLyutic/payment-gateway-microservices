package webhook

import "context"

// ExportProcessBatch открывает processBatch для тестирования.
func (w *Worker) ExportProcessBatch(ctx context.Context) {
	w.processBatch(ctx)
}

// ExportPreviewJSON открывает previewJSON для тестирования.
func ExportPreviewJSON(payload []byte) string {
	return previewJSON(payload)
}
