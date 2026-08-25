package crawler

import "sync"

// ExecutionStats counts browser automation actions for one run (optional; nil = disabled).
// Safe for concurrent use from session goroutines.
type ExecutionStats struct {
	mu            sync.Mutex
	Navigations   int64 // Navigate + NavigateLoadIdle (new document loads)
	HistoryBacks  int64 // NavigateBack
	Clicks        int64 // Click, QuickClick, ClickXPath*, ClickCollapse*
	FormFills     int64 // Fill (includes Material autocomplete typing)
	Downloads     int64 // HTTPDownload, WaitDownloadAfter, successful DownloadURLs rows
}

func (e *ExecutionStats) AddNavigations(n int64) {
	if e == nil || n == 0 {
		return
	}
	e.mu.Lock()
	e.Navigations += n
	e.mu.Unlock()
}

func (e *ExecutionStats) AddHistoryBacks(n int64) {
	if e == nil || n == 0 {
		return
	}
	e.mu.Lock()
	e.HistoryBacks += n
	e.mu.Unlock()
}

func (e *ExecutionStats) AddClicks(n int64) {
	if e == nil || n == 0 {
		return
	}
	e.mu.Lock()
	e.Clicks += n
	e.mu.Unlock()
}

func (e *ExecutionStats) AddFormFills(n int64) {
	if e == nil || n == 0 {
		return
	}
	e.mu.Lock()
	e.FormFills += n
	e.mu.Unlock()
}

func (e *ExecutionStats) AddDownloads(n int64) {
	if e == nil || n == 0 {
		return
	}
	e.mu.Lock()
	e.Downloads += n
	e.mu.Unlock()
}

// Snapshot returns a copy for persistence or JSON.
func (e *ExecutionStats) Snapshot() ExecutionStats {
	if e == nil {
		return ExecutionStats{}
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	return ExecutionStats{
		Navigations:  e.Navigations,
		HistoryBacks: e.HistoryBacks,
		Clicks:       e.Clicks,
		FormFills:    e.FormFills,
		Downloads:    e.Downloads,
	}
}
