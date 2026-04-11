package loader

import (
    "fmt"
    "time"
)

// parseWindow парсит строку в формате Go duration (10m, 1h, 24h).
// Запрещаем отрицательные и нулевые значения — они не имеют смысла
// для временного окна.
func parseWindow(window string) (time.Duration, error) {
    d, err := time.ParseDuration(window)
    if err != nil {
        return 0, fmt.Errorf("invalid duration format: %w", err)
    }
    if d <= 0 {
        return 0, fmt.Errorf("window must be positive, got %q", window)
    }
    return d, nil
}

// ParseWindow — экспортируемая версия для использования в engine.
func ParseWindow(window string) (time.Duration, error) {
    return parseWindow(window)
}
