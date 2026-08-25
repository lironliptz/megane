package llm

import (
	"context"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

var (
	llmSlotsOnce sync.Once
	llmSlots     chan struct{}
)

func initLLMSlots() {
	n := MaxConcurrent()
	llmSlots = make(chan struct{}, n)
	slog.Info("llm concurrency limit", "max_concurrent", n)
}

// MaxConcurrent returns the global LLM slot limit (default 2).
func MaxConcurrent() int {
	for _, key := range []string{"MAX_CONCURRENT_LLM", "MAX_CONCURENT_LLM"} {
		raw := strings.TrimSpace(os.Getenv(key))
		if raw == "" {
			continue
		}
		n, err := strconv.Atoi(raw)
		if err == nil && n > 0 {
			return n
		}
	}
	return 2
}

// AcquireSlot blocks until an LLM slot is available or ctx is cancelled.
func AcquireSlot(ctx context.Context) (wait time.Duration, err error) {
	llmSlotsOnce.Do(initLLMSlots)
	waitStart := time.Now()
	select {
	case llmSlots <- struct{}{}:
		return time.Since(waitStart), nil
	case <-ctx.Done():
		return time.Since(waitStart), ctx.Err()
	}
}

// ReleaseSlot frees a slot acquired via AcquireSlot.
func ReleaseSlot() {
	if llmSlots == nil {
		return
	}
	select {
	case <-llmSlots:
	default:
	}
}

// InFlight returns the number of active LLM calls holding a slot.
func InFlight() int {
	if llmSlots == nil {
		return 0
	}
	return len(llmSlots)
}

// CompleteGated runs client.Complete with the shared concurrency gate.
func CompleteGated(ctx context.Context, client Client, req Request) (*Response, error) {
	if client == nil {
		return nil, ErrNoClient
	}
	wait, err := AcquireSlot(ctx)
	if err != nil {
		return nil, err
	}
	defer ReleaseSlot()
	if wait > 200*time.Millisecond {
		slog.Info("llm acquired slot after wait",
			"provider", client.Name(),
			"queue_wait", wait.Round(time.Millisecond),
			"in_flight", InFlight(),
			"max_concurrent", MaxConcurrent(),
		)
	}
	return client.Complete(ctx, req)
}
