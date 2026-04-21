package worker_test

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/BuzzLyutic/payment-gateway-microservices/services/transaction-service/internal/worker"
)

type mockProcessor struct {
	callCount atomic.Int64
	results   []processorResult
	idx       atomic.Int64
}

type processorResult struct {
	count int
	err   error
}

func (m *mockProcessor) ProcessPendingPayments(_ context.Context, limit int) (int, error) {
	m.callCount.Add(1)
	i := m.idx.Add(1) - 1

	if int(i) < len(m.results) {
		r := m.results[i]
		return r.count, r.err
	}
	return 0, nil
}

// New

func TestWorker_New_NotNil(t *testing.T) {
	p := &mockProcessor{}
	w := worker.New(p, time.Second, 10)
	if w == nil {
		t.Fatal("New() returned nil")
	}
}

// Run

func TestWorker_Run_StopsOnContextCancel(t *testing.T) {
	// Worker должен завершиться при отмене контекста.
	// Критично: утечка горутины если не останавливается.
	p := &mockProcessor{}
	w := worker.New(p, 100*time.Millisecond, 10)

	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan struct{})
	go func() {
		w.Run(ctx)
		close(done)
	}()

	cancel()

	select {
	case <-done:
		// OK — завершился
	case <-time.After(time.Second):
		t.Error("worker did not stop after context cancellation (goroutine leak?)")
	}
}

func TestWorker_Run_CallsProcessorOnTick(t *testing.T) {
	// Воркер должен вызывать ProcessPendingPayments на каждый тик.
	p := &mockProcessor{}
	w := worker.New(p, 50*time.Millisecond, 10)

	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Millisecond)
	defer cancel()

	w.Run(ctx)

	calls := p.callCount.Load()
	// За 180ms с интервалом 50ms ожидаем 2-3 вызова.
	if calls < 2 {
		t.Errorf("expected >= 2 processor calls in 180ms, got %d", calls)
	}
}

func TestWorker_Run_PassesBatchSizeToProcessor(t *testing.T) {
	// Воркер должен передавать правильный batchSize.
	var capturedLimit int

	p := &captureProcessor{
		captureFn: func(limit int) {
			capturedLimit = limit
		},
	}

	const batchSize = 42
	w := worker.New(p, 50*time.Millisecond, batchSize)

	ctx, cancel := context.WithTimeout(context.Background(), 80*time.Millisecond)
	defer cancel()

	w.Run(ctx)

	if capturedLimit != batchSize {
		t.Errorf("batchSize passed to processor = %d, want %d", capturedLimit, batchSize)
	}
}

func TestWorker_Run_ContinuesAfterProcessorError(t *testing.T) {
	// Ошибка ProcessPendingPayments не должна останавливать воркер.
	// Критично: один сбой БД не должен убивать весь воркер.
	p := &mockProcessor{
		results: []processorResult{
			{0, errors.New("db connection lost")},
			{0, errors.New("db connection lost")},
			{5, nil}, // восстановился
		},
	}

	w := worker.New(p, 50*time.Millisecond, 10)

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	w.Run(ctx)

	calls := p.callCount.Load()
	if calls < 3 {
		t.Errorf("worker stopped after error: got %d calls, want >= 3", calls)
	}
}

func TestWorker_Run_DoesNotCallProcessorBeforeFirstTick(t *testing.T) {
	// Воркер НЕ должен вызывать processor сразу при старте —
	// только после первого тика. Иначе при старте сервиса
	// будет лишний запрос к БД.
	p := &mockProcessor{}

	// Очень большой интервал — тик не наступит за время теста.
	w := worker.New(p, 10*time.Second, 10)

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	w.Run(ctx)

	if calls := p.callCount.Load(); calls != 0 {
		t.Errorf("processor called %d times before first tick, want 0", calls)
	}
}

// captureProcessor

type captureProcessor struct {
	captureFn func(limit int)
}

func (c *captureProcessor) ProcessPendingPayments(_ context.Context, limit int) (int, error) {
	if c.captureFn != nil {
		c.captureFn(limit)
	}
	return 0, nil
}
