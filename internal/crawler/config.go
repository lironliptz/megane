package crawler

import (
	"log/slog"
	"time"
)

// Backend selects how HTTP/HTML is retrieved.
type Backend string

const (
	BackendStatic  Backend = "static"  // Colly — no JS execution
	BackendBrowser Backend = "browser" // Chromium via rod
)

// Config controls pacing, timeouts, logging, and download paths.
type Config struct {
	StepDelay       time.Duration // minimum pause between imperative steps
	JitterFraction  float64       // 0..1 random extra fraction of StepDelay (uniform)
	UserAgent       string
	DownloadDir     string
	NavigateTimeout time.Duration // page.Navigate + initial load
	ActionTimeout   time.Duration // wait element / click / fill
	Logger          *slog.Logger
	Headless        bool // browser backend only; false = visible window for debugging
	Devtools        bool // browser only: launcher.Devtools(true) opens DevTools for tabs
	// ChromeBin, if set, is passed to rod's launcher as the Chromium/Chrome executable path.
	ChromeBin string
	// Stats, when set, records navigations, clicks, form fills, and downloads for admin reporting.
	Stats *ExecutionStats
	// Progress optional hooks for long runs (phase labels, countable progress, per-download).
	Progress *ProgressCallbacks
}

// DefaultConfig returns conservative defaults suitable for polite crawling.
func DefaultConfig() *Config {
	return &Config{
		StepDelay:       800 * time.Millisecond,
		JitterFraction:  0.35,
		UserAgent:       "Mozilla/5.0 (compatible; jump-starter-crawler/1.0)",
		DownloadDir:     "downloads",
		NavigateTimeout: 90 * time.Second,
		ActionTimeout:   60 * time.Second,
		Logger:          slog.Default(),
		Headless:        true,
	}
}

func (c *Config) logger() *slog.Logger {
	if c.Logger != nil {
		return c.Logger
	}
	return slog.Default()
}
