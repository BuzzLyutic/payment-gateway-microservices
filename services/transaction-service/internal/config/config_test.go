package config_test

import (
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/BuzzLyutic/payment-gateway-microservices/services/transaction-service/internal/config"
)

func TestLoad_Defaults(t *testing.T) {
	// Очищаем все переменные чтобы получить дефолты.
	keys := []string{
		"SERVER_PORT", "DB_HOST", "DB_PORT", "DB_USER", "DB_PASSWORD", "DB_NAME",
		"REDIS_ADDR", "WORKER_INTERVAL_SEC", "WORKER_BATCH_SIZE", "NATS_URL", "LOG_LEVEL",
	}
	for _, k := range keys {
		os.Unsetenv(k)
	}

	cfg := config.Load()

	if cfg == nil {
		t.Fatal("Load() returned nil")
	}
	if cfg.Server.Port != "8080" {
		t.Errorf("Port default: got %q, want %q", cfg.Server.Port, "8080")
	}
	if cfg.Database.Host != "localhost" {
		t.Errorf("DB Host default: got %q, want %q", cfg.Database.Host, "localhost")
	}
	if cfg.Database.Port != "5432" {
		t.Errorf("DB Port default: got %q, want %q", cfg.Database.Port, "5432")
	}
	if cfg.Database.User != "payment" {
		t.Errorf("DB User default: got %q, want %q", cfg.Database.User, "payment")
	}
	if cfg.Database.Name != "payment_gateway" {
		t.Errorf("DB Name default: got %q, want %q", cfg.Database.Name, "payment_gateway")
	}
	if cfg.Redis.Addr != "localhost:6379" {
		t.Errorf("Redis Addr default: got %q, want %q", cfg.Redis.Addr, "localhost:6379")
	}
	if cfg.Worker.Interval != 2*time.Second {
		t.Errorf("Worker Interval default: got %v, want 2s", cfg.Worker.Interval)
	}
	if cfg.Worker.BatchSize != 10 {
		t.Errorf("Worker BatchSize default: got %d, want 10", cfg.Worker.BatchSize)
	}
	if cfg.NATS.URL != "nats://localhost:4222" {
		t.Errorf("NATS URL default: got %q, want %q", cfg.NATS.URL, "nats://localhost:4222")
	}
	if cfg.LogLevel != slog.LevelInfo {
		t.Errorf("LogLevel default: got %v, want info", cfg.LogLevel)
	}
}

func TestLoad_FromEnv(t *testing.T) {
	t.Setenv("SERVER_PORT", "9090")
	t.Setenv("DB_HOST", "db.prod.example.com")
	t.Setenv("DB_PORT", "5433")
	t.Setenv("DB_USER", "admin")
	t.Setenv("DB_PASSWORD", "supersecret")
	t.Setenv("DB_NAME", "prod_db")
	t.Setenv("REDIS_ADDR", "redis.prod.example.com:6379")
	t.Setenv("WORKER_INTERVAL_SEC", "5")
	t.Setenv("WORKER_BATCH_SIZE", "50")
	t.Setenv("NATS_URL", "nats://nats.prod.example.com:4222")
	t.Setenv("LOG_LEVEL", "debug")

	cfg := config.Load()

	if cfg.Server.Port != "9090" {
		t.Errorf("Port: got %q, want %q", cfg.Server.Port, "9090")
	}
	if cfg.Database.Host != "db.prod.example.com" {
		t.Errorf("DB Host: got %q, want %q", cfg.Database.Host, "db.prod.example.com")
	}
	if cfg.Database.Port != "5433" {
		t.Errorf("DB Port: got %q, want %q", cfg.Database.Port, "5433")
	}
	if cfg.Database.User != "admin" {
		t.Errorf("DB User: got %q, want %q", cfg.Database.User, "admin")
	}
	if cfg.Database.Password != "supersecret" {
		t.Errorf("DB Password: got %q, want %q", cfg.Database.Password, "supersecret")
	}
	if cfg.Database.Name != "prod_db" {
		t.Errorf("DB Name: got %q, want %q", cfg.Database.Name, "prod_db")
	}
	if cfg.Redis.Addr != "redis.prod.example.com:6379" {
		t.Errorf("Redis Addr: got %q, want %q", cfg.Redis.Addr, "redis.prod.example.com:6379")
	}
	if cfg.Worker.Interval != 5*time.Second {
		t.Errorf("Worker Interval: got %v, want 5s", cfg.Worker.Interval)
	}
	if cfg.Worker.BatchSize != 50 {
		t.Errorf("Worker BatchSize: got %d, want 50", cfg.Worker.BatchSize)
	}
	if cfg.NATS.URL != "nats://nats.prod.example.com:4222" {
		t.Errorf("NATS URL: got %q, want %q", cfg.NATS.URL, "nats://nats.prod.example.com:4222")
	}
	if cfg.LogLevel != slog.LevelDebug {
		t.Errorf("LogLevel: got %v, want debug", cfg.LogLevel)
	}
}

// DSN

func TestDSN_Format(t *testing.T) {
	dc := config.DatabaseConfig{
		User:     "myuser",
		Password: "mypassword",
		Host:     "localhost",
		Port:     "5432",
		Name:     "mydb",
	}

	got := dc.DSN()
	want := "postgres://myuser:mypassword@localhost:5432/mydb?sslmode=disable"

	if got != want {
		t.Errorf("DSN() = %q, want %q", got, want)
	}
}

func TestDSN_ContainsAllParts(t *testing.T) {
	dc := config.DatabaseConfig{
		User:     "user",
		Password: "pass",
		Host:     "db-host",
		Port:     "5432",
		Name:     "payment_gateway",
	}

	got := dc.DSN()

	parts := []string{"user", "pass", "db-host", "5432", "payment_gateway", "sslmode=disable"}
	for _, part := range parts {
		found := false
		for i := 0; i <= len(got)-len(part); i++ {
			if got[i:i+len(part)] == part {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("DSN() = %q, should contain %q", got, part)
		}
	}
}

// parseLogLevel

func TestLoad_LogLevels(t *testing.T) {
	cases := []struct {
		input    string
		expected slog.Level
	}{
		{"debug", slog.LevelDebug},
		{"info", slog.LevelInfo},
		{"warn", slog.LevelWarn},
		{"error", slog.LevelError},
		{"unknown", slog.LevelInfo}, // default
		{"", slog.LevelInfo},        // empty → default через getEnv fallback
	}

	for _, tc := range cases {
		t.Run(tc.input, func(t *testing.T) {
			if tc.input == "" {
				os.Unsetenv("LOG_LEVEL")
			} else {
				t.Setenv("LOG_LEVEL", tc.input)
			}
			// Нужны обязательные переменные чтобы Load не падал
			// (в этом config нет panic — все с дефолтами)
			cfg := config.Load()
			if cfg.LogLevel != tc.expected {
				t.Errorf("LOG_LEVEL=%q: got %v, want %v",
					tc.input, cfg.LogLevel, tc.expected)
			}
		})
	}
}

// getEnvInt

func TestLoad_WorkerIntervalInvalidInt_FallsBackToDefault(t *testing.T) {
	t.Setenv("WORKER_INTERVAL_SEC", "not-a-number")
	os.Unsetenv("WORKER_BATCH_SIZE")

	cfg := config.Load()

	// Fallback = 2 секунды
	if cfg.Worker.Interval != 2*time.Second {
		t.Errorf("invalid WORKER_INTERVAL_SEC: got %v, want 2s", cfg.Worker.Interval)
	}
}

func TestLoad_WorkerBatchSizeInvalidInt_FallsBackToDefault(t *testing.T) {
	t.Setenv("WORKER_BATCH_SIZE", "abc")

	cfg := config.Load()

	if cfg.Worker.BatchSize != 10 {
		t.Errorf("invalid WORKER_BATCH_SIZE: got %d, want 10", cfg.Worker.BatchSize)
	}
}

func TestLoad_WorkerBatchSizeZero(t *testing.T) {
	t.Setenv("WORKER_BATCH_SIZE", "0")

	cfg := config.Load()

	// 0 — валидный int, не fallback
	if cfg.Worker.BatchSize != 0 {
		t.Errorf("WORKER_BATCH_SIZE=0: got %d, want 0", cfg.Worker.BatchSize)
	}
}
