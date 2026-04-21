package config_test

import (
	"log/slog"
	"os"
	"testing"

	"github.com/BuzzLyutic/payment-gateway-microservices/services/provider-service/internal/config"
)

// cleanEnv очищает переменные окружения после теста.
func cleanEnv(t *testing.T, keys ...string) {
	t.Helper()
	for _, k := range keys {
		os.Unsetenv(k)
	}
}

// Load

func TestLoad_Defaults(t *testing.T) {
	// Без env переменных → все дефолты.
	cleanEnv(t,
		"SERVER_PORT", "DB_HOST", "DB_PORT", "DB_USER",
		"DB_PASSWORD", "DB_NAME", "NATS_URL",
		"REDIS_ADDR", "REDIS_PASSWORD", "REDIS_DB", "LOG_LEVEL",
	)

	cfg := config.Load()

	if cfg.Server.Port != "8081" {
		t.Errorf("Port default: expected %q, got %q", "8081", cfg.Server.Port)
	}
	if cfg.Database.Host != "localhost" {
		t.Errorf("DB Host default: expected %q, got %q", "localhost", cfg.Database.Host)
	}
	if cfg.Database.Port != "5432" {
		t.Errorf("DB Port default: expected %q, got %q", "5432", cfg.Database.Port)
	}
	if cfg.Database.User != "payment" {
		t.Errorf("DB User default: expected %q, got %q", "payment", cfg.Database.User)
	}
	if cfg.Database.Name != "provider_db" {
		t.Errorf("DB Name default: expected %q, got %q", "provider_db", cfg.Database.Name)
	}
	if cfg.NATS.URL != "nats://localhost:4222" {
		t.Errorf("NATS URL default: expected %q, got %q", "nats://localhost:4222", cfg.NATS.URL)
	}
	if cfg.Redis.Addr != "localhost:6379" {
		t.Errorf("Redis Addr default: expected %q, got %q", "localhost:6379", cfg.Redis.Addr)
	}
	if cfg.Redis.DB != 1 {
		t.Errorf("Redis DB default: expected 1, got %d", cfg.Redis.DB)
	}
	if cfg.LogLevel != slog.LevelInfo {
		t.Errorf("LogLevel default: expected info, got %v", cfg.LogLevel)
	}
}

func TestLoad_FromEnv(t *testing.T) {
	// Все переменные заданы через env.
	t.Setenv("SERVER_PORT", "9090")
	t.Setenv("DB_HOST", "db.example.com")
	t.Setenv("DB_PORT", "5433")
	t.Setenv("DB_USER", "admin")
	t.Setenv("DB_PASSWORD", "secret123")
	t.Setenv("DB_NAME", "mydb")
	t.Setenv("NATS_URL", "nats://nats.example.com:4222")
	t.Setenv("REDIS_ADDR", "redis.example.com:6379")
	t.Setenv("REDIS_PASSWORD", "redis_secret")
	t.Setenv("REDIS_DB", "2")
	t.Setenv("LOG_LEVEL", "debug")

	cfg := config.Load()

	if cfg.Server.Port != "9090" {
		t.Errorf("Port: expected %q, got %q", "9090", cfg.Server.Port)
	}
	if cfg.Database.Host != "db.example.com" {
		t.Errorf("DB Host: expected %q, got %q", "db.example.com", cfg.Database.Host)
	}
	if cfg.Database.Port != "5433" {
		t.Errorf("DB Port: expected %q, got %q", "5433", cfg.Database.Port)
	}
	if cfg.Database.User != "admin" {
		t.Errorf("DB User: expected %q, got %q", "admin", cfg.Database.User)
	}
	if cfg.Database.Password != "secret123" {
		t.Errorf("DB Password: expected %q, got %q", "secret123", cfg.Database.Password)
	}
	if cfg.Database.Name != "mydb" {
		t.Errorf("DB Name: expected %q, got %q", "mydb", cfg.Database.Name)
	}
	if cfg.NATS.URL != "nats://nats.example.com:4222" {
		t.Errorf("NATS URL: expected %q, got %q", "nats://nats.example.com:4222", cfg.NATS.URL)
	}
	if cfg.Redis.Addr != "redis.example.com:6379" {
		t.Errorf("Redis Addr: expected %q, got %q", "redis.example.com:6379", cfg.Redis.Addr)
	}
	if cfg.Redis.DB != 2 {
		t.Errorf("Redis DB: expected 2, got %d", cfg.Redis.DB)
	}
	if cfg.LogLevel != slog.LevelDebug {
		t.Errorf("LogLevel: expected debug, got %v", cfg.LogLevel)
	}
}

func TestLoad_NotNil(t *testing.T) {
	cfg := config.Load()
	if cfg == nil {
		t.Error("Load() returned nil")
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
	expected := "postgres://myuser:mypassword@localhost:5432/mydb?sslmode=disable"

	if got != expected {
		t.Errorf("DSN() = %q, want %q", got, expected)
	}
}

func TestDSN_WithSpecialChars(t *testing.T) {
	// Спецсимволы в DSN — не паникует, формирует строку.
	dc := config.DatabaseConfig{
		User:     "user",
		Password: "p@ss!word",
		Host:     "db-host",
		Port:     "5432",
		Name:     "payment_db",
	}

	got := dc.DSN()
	if got == "" {
		t.Error("DSN() returned empty string")
	}
	// Должен содержать хост и имя БД.
	if !contains(got, "db-host") {
		t.Errorf("DSN() = %q, should contain host", got)
	}
	if !contains(got, "payment_db") {
		t.Errorf("DSN() = %q, should contain db name", got)
	}
}

// parseLogLevel (косвенно через Load)

func TestLoad_LogLevel_Debug(t *testing.T) {
	t.Setenv("LOG_LEVEL", "debug")
	cfg := config.Load()
	if cfg.LogLevel != slog.LevelDebug {
		t.Errorf("expected debug, got %v", cfg.LogLevel)
	}
}

func TestLoad_LogLevel_Warn(t *testing.T) {
	t.Setenv("LOG_LEVEL", "warn")
	cfg := config.Load()
	if cfg.LogLevel != slog.LevelWarn {
		t.Errorf("expected warn, got %v", cfg.LogLevel)
	}
}

func TestLoad_LogLevel_Error(t *testing.T) {
	t.Setenv("LOG_LEVEL", "error")
	cfg := config.Load()
	if cfg.LogLevel != slog.LevelError {
		t.Errorf("expected error, got %v", cfg.LogLevel)
	}
}

func TestLoad_LogLevel_Info(t *testing.T) {
	t.Setenv("LOG_LEVEL", "info")
	cfg := config.Load()
	if cfg.LogLevel != slog.LevelInfo {
		t.Errorf("expected info, got %v", cfg.LogLevel)
	}
}

func TestLoad_LogLevel_Unknown_DefaultsToInfo(t *testing.T) {
	// Неизвестный уровень → info (default ветка).
	t.Setenv("LOG_LEVEL", "trace")
	cfg := config.Load()
	if cfg.LogLevel != slog.LevelInfo {
		t.Errorf("unknown level: expected info, got %v", cfg.LogLevel)
	}
}

// getEnvInt (косвенно через Redis.DB)

func TestLoad_RedisDB_InvalidInt_FallsBackToDefault(t *testing.T) {
	t.Setenv("REDIS_DB", "not-a-number")
	cfg := config.Load()
	// Дефолт = 1.
	if cfg.Redis.DB != 1 {
		t.Errorf("invalid REDIS_DB: expected fallback 1, got %d", cfg.Redis.DB)
	}
}

func TestLoad_RedisDB_Zero(t *testing.T) {
	t.Setenv("REDIS_DB", "0")
	cfg := config.Load()
	if cfg.Redis.DB != 0 {
		t.Errorf("REDIS_DB=0: expected 0, got %d", cfg.Redis.DB)
	}
}

func TestLoad_RedisDB_ValidInt(t *testing.T) {
	t.Setenv("REDIS_DB", "5")
	cfg := config.Load()
	if cfg.Redis.DB != 5 {
		t.Errorf("REDIS_DB=5: expected 5, got %d", cfg.Redis.DB)
	}
}

// helpers

func contains(s, substr string) bool {
	return len(s) >= len(substr) &&
		(s == substr || len(s) > 0 && containsStr(s, substr))
}

func containsStr(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
