// Package companyfetch runs the per-company ingestion pipeline shared by
// cmd/fetch-similar (a peer-list batch) and cmd/fetch-company (one CIK, no
// peer-list framing): SEC filings -> meta.json -> financials.json -> stock
// prices, each step skipped when its output already exists unless
// Config.Force is set. Extracted from cmd/fetch-similar's original
// steps.go/submissions.go (prompt 13) so a second caller doesn't duplicate
// the step logic or drift from it.
package companyfetch

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"time"

	"megane/internal/companyview"
	"megane/internal/db"
)

const noteMaxBytes = 4096

// ExecRunner runs one subprocess and reports its outcome. Injectable so the
// fetch/meta steps are testable with a fake rather than a real subprocess.
type ExecRunner func(ctx context.Context, name string, args ...string) (exitCode int, output []byte, err error)

// DefaultRunner is the real implementation: exec.CommandContext, combined
// stdout+stderr. A non-zero exit is not itself a Go error — it's the signal
// the caller uses to mark that step "error" — so only a failure to even
// start the process (binary missing, etc.) is returned as err.
func DefaultRunner(ctx context.Context, name string, args ...string) (int, []byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	out, err := cmd.CombinedOutput()
	if err == nil {
		return 0, out, nil
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return exitErr.ExitCode(), out, nil
	}
	return -1, out, err
}

// Config holds everything a Run needs that isn't specific to one CIK.
type Config struct {
	Root      string
	Years     int
	Force     bool
	Steps     map[string]bool      // "fetch", "meta", "financials", "prices"
	Runner    ExecRunner           // nil => DefaultRunner
	UserAgent string               // SEC_EDGAR_USER_AGENT; empty degrades gap-fill only
	Svc       *companyview.Service // nil => prices step is skipped
	Database  *db.DB               // nil => prices step is skipped
	Verbose   bool
	Logf      func(format string, args ...any) // nil => fmt.Printf(format+"\n", ...)
}

func (c Config) runner() ExecRunner {
	if c.Runner != nil {
		return c.Runner
	}
	return DefaultRunner
}

func (c Config) logf(format string, args ...any) {
	if !c.Verbose {
		return
	}
	if c.Logf != nil {
		c.Logf(format, args...)
		return
	}
	fmt.Printf(format+"\n", args...)
}

// StepResult is one step's outcome.
type StepResult struct {
	Result string // "ok" | "skipped" | "error"
	Note   string
}

// Result is the outcome of ingesting one CIK.
//
// Status rollup: zero step errors -> "complete" (a legitimately skipped step
// — no ticker, nothing new to fetch — is not a failure); some errors with at
// least one success -> "partial"; every attempted step errored -> "error".
// Run never returns "skipped" as a whole-of-run status — a caller that
// resolves a CIK/ticker beforehand (fetch-similar's peer resolution,
// fetch-company's CIK normalization) owns that outcome; Run only ever sees a
// CIK it should actually ingest.
type Result struct {
	CIK             string
	Status          string // complete | partial | error
	Steps           map[string]StepResult
	FilingCount     int
	FinancialsCount int
	StockBarCount   int
	DurationMS      int64
}

// Run ingests one CIK: fetch -> meta -> financials -> prices, each gated by
// cfg.Steps and skipped when idempotent unless cfg.Force.
func Run(ctx context.Context, cfg Config, cik string) Result {
	start := time.Now()
	res := Result{CIK: cik, Steps: map[string]StepResult{}}

	stepOK, stepErr := 0, 0
	record := func(name, result, note string) {
		res.Steps[name] = StepResult{Result: result, Note: note}
		switch result {
		case "ok":
			stepOK++
		case "error":
			stepErr++
		}
	}

	if cfg.Steps["fetch"] {
		cfg.logf("fetch-company: step=fetch start cik=%s", cik)
		result, note := stepFetch(ctx, cfg, cik)
		record("fetch", result, note)
		if note != "" {
			cfg.logf("fetch-company: step=fetch result=%s note=%s", result, note)
		} else {
			cfg.logf("fetch-company: step=fetch result=%s", result)
		}
	}
	if cfg.Steps["meta"] {
		cfg.logf("fetch-company: step=meta start cik=%s", cik)
		result, note := stepMeta(ctx, cfg, cik)
		record("meta", result, note)
		if note != "" {
			cfg.logf("fetch-company: step=meta result=%s note=%s", result, note)
		} else {
			cfg.logf("fetch-company: step=meta result=%s", result)
		}
	}
	if cfg.Steps["financials"] {
		cfg.logf("fetch-company: step=financials start cik=%s", cik)
		result, note := stepFinancials(ctx, cfg, cik)
		record("financials", result, note)
		if note != "" {
			cfg.logf("fetch-company: step=financials result=%s note=%s", result, note)
		} else {
			cfg.logf("fetch-company: step=financials result=%s", result)
		}
	}
	if cfg.Steps["prices"] {
		cfg.logf("fetch-company: step=prices start cik=%s", cik)
		result, note, bars := stepPrices(ctx, cfg, cik)
		res.StockBarCount = bars
		record("prices", result, note)
		if note != "" {
			cfg.logf("fetch-company: step=prices result=%s bars=%d note=%s", result, bars, note)
		} else {
			cfg.logf("fetch-company: step=prices result=%s bars=%d", result, bars)
		}
	}

	res.FilingCount = countFiles(cfg.Root, cik, "filing.json")
	res.FinancialsCount = countFiles(cfg.Root, cik, "financials.json")
	res.DurationMS = time.Since(start).Milliseconds()

	switch {
	case stepErr == 0:
		res.Status = "complete"
	case stepOK > 0:
		res.Status = "partial"
	default:
		res.Status = "error"
	}
	return res
}
