package config

import (
	"fmt"
	"os"
	"strconv"
)

type Config struct {
	Port      string
	RedisURL  string
	NatsURL   string
	RulesPath string
	LogLevel  string
}

func Load() (*Config, error) {
	cfg := &Config{
		Port:      getEnv("PORT", "8083"),
		RedisURL:  mustGetEnv("REDIS_URL"),
		NatsURL:   mustGetEnv("NATS_URL"),
		RulesPath: mustGetEnv("RULES_PATH"),
		LogLevel:  getEnv("LOG_LEVEL", "info"),
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
		// намеренно паникуем при старте — misconfiguration не должна
		// приводить к тихому запуску сломанного сервиса
		panic(fmt.Sprintf("required environment variable %q is not set", key))
	}
	return v
}

// getEnvInt — вспомогательная, пригодится позже
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
