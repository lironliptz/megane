package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

// StepResults mirrors HLD §3.2 / LLD §4.4 — one outcome per pipeline step.
// "submissions" and "filings" both come from the single fetch exec call
// (P0 has no separate submissions-only step), so they always carry the same
// value; the split is kept for schema forward-compatibility with a future
// Go fetch (P2) that could report them independently.
type StepResults struct {
	Resolve     string `json:"resolve,omitempty"`
	Submissions string `json:"submissions,omitempty"`
	Filings     string `json:"filings,omitempty"`
	Meta        string `json:"meta,omitempty"`
	Financials  string `json:"financials,omitempty"`
	StockPrices string `json:"stock_prices,omitempty"`
}

// PeerStatus is one row of fetch-status.json.
type PeerStatus struct {
	CompanyName     string      `json:"company_name"`
	CompanyID       string      `json:"company_id,omitempty"`
	Ticker          string      `json:"ticker,omitempty"`
	Status          string      `json:"status"` // complete | partial | skipped | error
	Steps           StepResults `json:"steps"`
	FilingCount     int         `json:"filing_count"`
	FinancialsCount int         `json:"financials_count"`
	StockBarCount   int         `json:"stock_bar_count"`
	DurationMS      int64       `json:"duration_ms"`
	Note            string      `json:"note,omitempty"`
}

// Summary rolls up peer statuses for a one-glance read of batch health.
type Summary struct {
	Total    int `json:"total"`
	Complete int `json:"complete"`
	Partial  int `json:"partial"`
	Skipped  int `json:"skipped"`
	Error    int `json:"error"`
}

// RunConfig records the knobs a run used.
type RunConfig struct {
	Root  string   `json:"root"`
	Years int      `json:"years"`
	Steps []string `json:"steps"`
	Force bool     `json:"force"`
}

// ReferenceInfo is the reference company block carried into both output
// files for context — this orchestrator does not ingest it (see
// --include-reference for the opt-in exception).
type ReferenceInfo struct {
	CompanyName string `json:"company_name"`
	CompanyID   string `json:"company_id"`
	Ticker      string `json:"ticker"`
}

// FetchStatus is the v1 schema for {slug}.fetch-status.json (LLD §3.2 / §4.4).
type FetchStatus struct {
	SourceFile  string        `json:"source_file"`
	Reference   ReferenceInfo `json:"reference"`
	GeneratedAt string        `json:"generated_at"`
	Config      RunConfig     `json:"config"`
	Peers       []PeerStatus  `json:"peers"`
	Summary     Summary       `json:"summary"`
}

// statusPath derives {slug}.fetch-status.json from the input path, e.g.
// kamada.json -> kamada.fetch-status.json.
func statusPath(inputPath string) string {
	return derivedPath(inputPath, ".fetch-status.json")
}

// planPath derives {slug}.fetch-plan.json — the dry-run's separate output
// file (D9: a survey must never be able to clobber a real run's status).
func planPath(inputPath string) string {
	return derivedPath(inputPath, ".fetch-plan.json")
}

func derivedPath(inputPath, suffix string) string {
	dir := filepath.Dir(inputPath)
	base := filepath.Base(inputPath)
	base = strings.TrimSuffix(base, filepath.Ext(base))
	return filepath.Join(dir, base+suffix)
}

// summarize rolls peer statuses up into the batch Summary block.
func summarize(peers []PeerStatus) Summary {
	s := Summary{Total: len(peers)}
	for _, p := range peers {
		switch p.Status {
		case "complete":
			s.Complete++
		case "partial":
			s.Partial++
		case "skipped":
			s.Skipped++
		case "error":
			s.Error++
		}
	}
	return s
}

// writeStatus marshals the whole status wholesale each run (never merged
// with a prior run — LLD §4.4) with a trailing newline.
func writeStatus(path string, status *FetchStatus) error {
	return writeJSONFile(path, status)
}

func writeJSONFile(path string, v any) error {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	b = append(b, '\n')
	return os.WriteFile(path, b, 0o644)
}
