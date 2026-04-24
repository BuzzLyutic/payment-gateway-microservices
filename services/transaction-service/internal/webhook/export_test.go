package webhook

// ExportComputeSignature открывает computeSignature для тестирования.
func ExportComputeSignature(secret, timestamp string, body []byte) string {
	return computeSignature(secret, timestamp, body)
}
