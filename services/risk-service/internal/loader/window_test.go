package loader_test

import (
	"testing"
	"time"

	"github.com/BuzzLyutic/payment-gateway-microservices/services/risk-service/internal/loader"
)

// ParseWindow

func TestParseWindow_ValidDurations(t *testing.T) {
	cases := []struct {
		input    string
		expected time.Duration
	}{
		{"10m", 10 * time.Minute},
		{"1h", time.Hour},
		{"24h", 24 * time.Hour},
		{"30s", 30 * time.Second},
		{"5m", 5 * time.Minute},
	}

	for _, tc := range cases {
		t.Run(tc.input, func(t *testing.T) {
			got, err := loader.ParseWindow(tc.input)
			if err != nil {
				t.Fatalf("ParseWindow(%q) unexpected error: %v", tc.input, err)
			}
			if got != tc.expected {
				t.Errorf("ParseWindow(%q) = %v, want %v", tc.input, got, tc.expected)
			}
		})
	}
}

func TestParseWindow_InvalidFormat_ReturnsError(t *testing.T) {
	cases := []string{
		"invalid",
		"abc",
		"",
		"10",    // без единицы
		"1 hour", // с пробелом
	}

	for _, tc := range cases {
		t.Run(tc, func(t *testing.T) {
			_, err := loader.ParseWindow(tc)
			if err == nil {
				t.Errorf("ParseWindow(%q) expected error, got nil", tc)
			}
		})
	}
}

func TestParseWindow_ZeroDuration_ReturnsError(t *testing.T) {
	_, err := loader.ParseWindow("0s")
	if err == nil {
		t.Error("ParseWindow(\"0s\") expected error for zero duration, got nil")
	}
}

func TestParseWindow_NegativeDuration_ReturnsError(t *testing.T) {
	_, err := loader.ParseWindow("-10m")
	if err == nil {
		t.Error("ParseWindow(\"-10m\") expected error for negative duration, got nil")
	}
}
