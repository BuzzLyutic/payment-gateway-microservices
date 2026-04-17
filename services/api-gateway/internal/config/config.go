package config

import (
	"fmt"
	"os"
	"strconv"
)

type Config struct {
	Port                  string
	RedisURL              string
	TransactionServiceURL string
	DefaultRateLimit      int
	LogLevel              string
}

func Load() (*Config, error) {
	cfg := &Config{
		Port:                  getEnv("PORT", "8080"),
		RedisURL:              mustGetEnv("REDIS_URL"),
		TransactionServiceURL: mustGetEnv("TRANSACTION_SERVICE_URL"),
		DefaultRateLimit:      getEnvInt("DEFAULT_RATE_LIMIT", 100),
		LogLevel:              getEnv("LOG_LEVEL", "info"),
	}
	return cfg, nil
}

func getEnv(key, defaultVal string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return defaultVal
}

func mustGetEnv(key string) string {
	v := os.Getenv(key)
	if v == "" {
		panic(fmt.Sprintf("required environment variable %q is not set", key))
	}
	return v
}

func getEnvInt(key string, defaultVal int) int {
	v := os.Getenv(key)
	if v == "" {
		return defaultVal
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return defaultVal
	}
	return n
}
