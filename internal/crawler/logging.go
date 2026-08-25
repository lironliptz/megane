package crawler

import (
	"log/slog"
	"time"
)

func logStep(log *slog.Logger, step string, attrs ...any) {
	args := append([]any{"step", step}, attrs...)
	log.Info("crawler", args...)
}

func logStepDone(log *slog.Logger, step string, start time.Time, attrs ...any) {
	args := append([]any{
		"step", step,
		"duration_ms", time.Since(start).Milliseconds(),
	}, attrs...)
	log.Info("crawler", args...)
}
