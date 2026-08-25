package crawler

// ProgressCallbacks optional hooks for long crawler runs (HTTP jobs, CLI with DB/UI observers).
type ProgressCallbacks struct {
	// OnPhase is a coarse lifecycle marker (startup, navigating, scrolling, downloading, etc.).
	OnPhase func(phase string)

	// OnProgress reports countable progress: current index and total expected items (0 total = unknown).
	OnProgress func(current, total int)

	// OnDownload fires after each successful file write; return non-nil to abort the run.
	OnDownload func(destPath string) error
}
