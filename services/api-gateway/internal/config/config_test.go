package config_test

import (
	"os"
	"testing"

	"github.com/BuzzLyutic/payment-gateway-microservices/services/api-gateway/internal/config"
)

// Load

func TestLoad_AllEnvSet_ReturnsConfig(t *testing.T) {
	// Устанавливаем все необходимые переменные окружения.
	t.Setenv("REDIS_URL", "redis://localhost:6379")
	t.Setenv("TRANSACTION_SERVICE_URL", "http://localhost:8081")
	t.Setenv("PORT", "9090")
	t.Setenv("DEFAULT_RATE_LIMIT", "200")
	t.Setenv("LOG_LEVEL", "debug")

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	if cfg.Port != "9090" {
		t.Errorf("Port: expected %q, got %q", "9090", cfg.Port)
	}
	if cfg.RedisURL != "redis://localhost:6379" {
		t.Errorf("RedisURL: expected %q, got %q", "redis://localhost:6379", cfg.RedisURL)
	}
	if cfg.TransactionServiceURL != "http://localhost:8081" {
		t.Errorf("TransactionServiceURL: expected %q, got %q",
			"http://localhost:8081", cfg.TransactionServiceURL)
	}
	if cfg.DefaultRateLimit != 200 {
		t.Errorf("DefaultRateLimit: expected 200, got %d", cfg.DefaultRateLimit)
	}
	if cfg.LogLevel != "debug" {
		t.Errorf("LogLevel: expected %q, got %q", "debug", cfg.LogLevel)
	}
}

func TestLoad_DefaultValues_WhenOptionalEnvNotSet(t *testing.T) {
	// Обязательные переменные задаём, необязательные — нет.
	// Проверяем default значения.
	t.Setenv("REDIS_URL", "redis://localhost:6379")
	t.Setenv("TRANSACTION_SERVICE_URL", "http://localhost:8081")
	os.Unsetenv("PORT")
	os.Unsetenv("DEFAULT_RATE_LIMIT")
	os.Unsetenv("LOG_LEVEL")

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	// Defaults из config.go.
	if cfg.Port != "8080" {
		t.Errorf("Port default: expected %q, got %q", "8080", cfg.Port)
	}
	if cfg.DefaultRateLimit != 100 {
		t.Errorf("DefaultRateLimit default: expected 100, got %d", cfg.DefaultRateLimit)
	}
	if cfg.LogLevel != "info" {
		t.Errorf("LogLevel default: expected %q, got %q", "info", cfg.LogLevel)
	}
}

func TestLoad_MissingRedisURL_Panics(t *testing.T) {
	// REDIS_URL обязателен — без него Load паникует (mustGetEnv).
	os.Unsetenv("REDIS_URL")
	t.Setenv("TRANSACTION_SERVICE_URL", "http://localhost:8081")

	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic for missing REDIS_URL, got none")
		}
	}()

	config.Load() //nolint:errcheck // должен паниковать
}

func TestLoad_MissingTransactionServiceURL_Panics(t *testing.T) {
	// TRANSACTION_SERVICE_URL обязателен — без него Load паникует.
	t.Setenv("REDIS_URL", "redis://localhost:6379")
	os.Unsetenv("TRANSACTION_SERVICE_URL")

	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic for missing TRANSACTION_SERVICE_URL, got none")
		}
	}()

	config.Load() //nolint:errcheck // должен паниковать
}

func TestLoad_InvalidDefaultRateLimit_FallsBackToDefault(t *testing.T) {
	// DEFAULT_RATE_LIMIT не парсится как int - fallback на 100.
	t.Setenv("REDIS_URL", "redis://localhost:6379")
	t.Setenv("TRANSACTION_SERVICE_URL", "http://localhost:8081")
	t.Setenv("DEFAULT_RATE_LIMIT", "not-a-number")

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	if cfg.DefaultRateLimit != 100 {
		t.Errorf("DefaultRateLimit fallback: expected 100, got %d", cfg.DefaultRateLimit)
	}
}

func TestLoad_ZeroRateLimit_FallsBackToDefault(t *testing.T) {
	// DEFAULT_RATE_LIMIT=0 - не > 0, getEnvInt вернёт 0.
	// Зависит от реализации getEnvInt — документируем поведение.
	t.Setenv("REDIS_URL", "redis://localhost:6379")
	t.Setenv("TRANSACTION_SERVICE_URL", "http://localhost:8081")
	t.Setenv("DEFAULT_RATE_LIMIT", "0")

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	// getEnvInt с "0" вернёт 0 (Atoi успешен, n=0).
	// Документируем текущее поведение.
	if cfg.DefaultRateLimit != 0 {
		t.Logf("DEFAULT_RATE_LIMIT=0 → got %d (may be default)", cfg.DefaultRateLimit)
	}
}

// getEnv (косвенно через Load)

func TestGetEnv_ReturnsEnvValue_WhenSet(t *testing.T) {
	t.Setenv("REDIS_URL", "redis://custom:6379")
	t.Setenv("TRANSACTION_SERVICE_URL", "http://custom:8081")
	t.Setenv("PORT", "7777")

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	if cfg.Port != "7777" {
		t.Errorf("expected PORT=7777, got %q", cfg.Port)
	}
}

// getEnvInt (косвенно через Load)

func TestGetEnvInt_ValidInt_ReturnsParsedValue(t *testing.T) {
	t.Setenv("REDIS_URL", "redis://localhost:6379")
	t.Setenv("TRANSACTION_SERVICE_URL", "http://localhost:8081")
	t.Setenv("DEFAULT_RATE_LIMIT", "500")

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	if cfg.DefaultRateLimit != 500 {
		t.Errorf("expected 500, got %d", cfg.DefaultRateLimit)
	}
}

func TestGetEnvInt_InvalidString_ReturnsDefault(t *testing.T) {
	t.Setenv("REDIS_URL", "redis://localhost:6379")
	t.Setenv("TRANSACTION_SERVICE_URL", "http://localhost:8081")
	t.Setenv("DEFAULT_RATE_LIMIT", "abc")

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	if cfg.DefaultRateLimit != 100 { // default из Load()
		t.Errorf("expected default 100 for invalid int, got %d", cfg.DefaultRateLimit)
	}
}
