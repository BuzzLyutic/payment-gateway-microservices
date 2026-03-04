package domain

import (
	"testing"
)

func TestStatus_CanTransitionTo(t *testing.T) {
	tests := []struct {
		name   string
		from   Status
		to     Status
		expect bool
	}{
		{"pending to processing", StatusPending, StatusProcessing, true},
		{"processing to captured", StatusProcessing, StatusCaptured, true},
		{"processing to failed", StatusProcessing, StatusFailed, true},
		{"processing to declined", StatusProcessing, StatusDeclined, true},
		{"captured to refunded", StatusCaptured, StatusRefunded, true},

		// Недопустимые переходы
		{"pending to captured", StatusPending, StatusCaptured, false},
		{"pending to failed", StatusPending, StatusFailed, false},
		{"captured to pending", StatusCaptured, StatusPending, false},
		{"failed to captured", StatusFailed, StatusCaptured, false},
		{"declined to captured", StatusDeclined, StatusCaptured, false},
		{"refunded to pending", StatusRefunded, StatusPending, false},
		{"captured to captured", StatusCaptured, StatusCaptured, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.from.CanTransitionTo(tt.to)
			if got != tt.expect {
				t.Errorf("(%s).CanTransitionTo(%s) = %v, want %v",
					tt.from, tt.to, got, tt.expect)
			}
		})
	}
}

func TestTransaction_TransitionTo(t *testing.T) {
	t.Run("valid transition updates status", func(t *testing.T) {
		tx := &Transaction{Status: StatusPending}

		err := tx.TransitionTo(StatusProcessing)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if tx.Status != StatusProcessing {
			t.Errorf("status = %s, want %s", tx.Status, StatusProcessing)
		}
	})

	t.Run("invalid transition returns error", func(t *testing.T) {
		tx := &Transaction{Status: StatusPending}

		err := tx.TransitionTo(StatusCaptured)
		if err == nil {
			t.Fatal("expected error, got nil")
		}
	})

	t.Run("full lifecycle pending → processing → captured", func(t *testing.T) {
		tx := &Transaction{Status: StatusPending}

		if err := tx.TransitionTo(StatusProcessing); err != nil {
			t.Fatalf("pending → processing: %v", err)
		}
		if err := tx.TransitionTo(StatusCaptured); err != nil {
			t.Fatalf("processing → captured: %v", err)
		}
		if tx.Status != StatusCaptured {
			t.Errorf("final status = %s, want captured", tx.Status)
		}
	})
}
