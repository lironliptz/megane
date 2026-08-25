package crawler

import (
	"context"
	"math/rand/v2"
	"time"
)

// PauseBetweenSteps sleeps StepDelay plus random jitter (0..JitterFraction * StepDelay).
func PauseBetweenSteps(ctx context.Context, cfg *Config) error {
	if cfg.StepDelay <= 0 {
		return nil
	}
	jitter := time.Duration(0)
	if cfg.JitterFraction > 0 {
		jitter = time.Duration(rand.Float64() * cfg.JitterFraction * float64(cfg.StepDelay))
	}
	d := cfg.StepDelay + jitter
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}
