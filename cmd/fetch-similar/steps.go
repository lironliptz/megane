package main

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"megane/internal/companyview"
	"megane/internal/db"
	"megane/internal/edgar/financials"
)

const noteMaxBytes = 4096

// execRunner runs one subprocess and reports its outcome. Injectable so the
// fetch/meta steps can be tested with a fake rather than a real subprocess
// (LLD §7 — "steps.go takes an injectable runner").
type execRunner func(ctx context.Context, name string, args ...string) (exitCode int, output []byte, err error)

// defaultRunner is the real implementation: exec.CommandContext, combined
// stdout+stderr (D7). A non-zero exit is not itself a Go error — it's the
// signal the caller uses to mark that step "error" — so only a failure to
// even start the process (binary missing, etc.) is returned as err.
func defaultRunner(ctx context.Context, name string, args ...string) (int, []byte, error) {
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

// truncateNote keeps only the LAST noteMaxBytes of subprocess output — the
// tail is where a Python traceback or the final error line lives.
func truncateNote(b []byte) string {
	s := strings.TrimSpace(string(b))
	if len(s) > noteMaxBytes {
		s = s[len(s)-noteMaxBytes:]
	}
	return s
}

func pythonExe() string {
	if v := strings.TrimSpace(os.Getenv("PYTHON_BIN")); v != "" {
		return v
	}
	return "python3"
}

// runPythonStep executes one Python script step and reduces its outcome to
// (result, note) — never returning a Go error itself, so one peer's failure
// never aborts the batch (D7).
func runPythonStep(ctx context.Context, runner execRunner, script string, args []string) (result, note string) {
	exitCode, out, err := runner(ctx, pythonExe(), append([]string{script}, args...)...)
	if err != nil {
		return "error", err.Error()
	}
	if exitCode != 0 {
		return "error", truncateNote(out)
	}
	return "ok", ""
}

// orchestrator holds the config + collaborators shared across peers in one
// run. svc/database are nil when the prices step isn't requested.
type orchestrator struct {
	root      string
	years     int
	force     bool
	steps     map[string]bool
	runner    execRunner
	userAgent string
	svc       *companyview.Service
	database  *db.DB
}

// fetchNeeded implements the fetch step's idempotency (LLD §4.5): skip only
// when submissions.json exists AND every filing within the --years window
// already has an accession folder on disk. A narrower prior run (fewer
// years) or a missing folder means fetch must run again.
func fetchNeeded(root, cik string, years int) bool {
	subPath := filepath.Join(root, "companies", cik, "submissions.json")
	doc, err := readSubmissionsRecent(subPath)
	if err != nil {
		return true // no submissions.json (or unreadable) — must fetch
	}
	cutoff := time.Now().UTC().Year() - years
	for i, accession := range doc.AccessionNumber {
		fd := safeIndex(doc.FilingDate, i)
		if len(fd) < 4 {
			continue
		}
		year, err := strconv.Atoi(fd[:4])
		if err != nil || year < cutoff {
			continue
		}
		dir := filepath.Join(root, "companies", cik, fd[:4], accession)
		if _, err := os.Stat(filepath.Join(dir, "filing.json")); err != nil {
			return true // a filing inside the window is missing on disk
		}
	}
	return false
}

// metaNeeded reports whether any downloaded accession is missing meta.json.
func metaNeeded(root, cik string) bool {
	base := filepath.Join(root, "companies", cik)
	needed := false
	_ = filepath.WalkDir(base, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || d.Name() != "filing.json" {
			return nil
		}
		if _, err := os.Stat(filepath.Join(filepath.Dir(path), "meta.json")); err != nil {
			needed = true
		}
		return nil
	})
	return needed
}

func countFiles(root, cik, name string) int {
	base := filepath.Join(root, "companies", cik)
	n := 0
	_ = filepath.WalkDir(base, func(path string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() && d.Name() == name {
			n++
		}
		return nil
	})
	return n
}

// stepFetch runs the (generalized) Python fetch script (D1 step 1, D2).
func (o *orchestrator) stepFetch(ctx context.Context, cik string) (result, note string) {
	if !o.force && !fetchNeeded(o.root, cik, o.years) {
		return "skipped", ""
	}
	script := filepath.Join("python", "edgar", "fetch_kamada.py")
	args := []string{"--cik", cik, "--years", strconv.Itoa(o.years), "--root", o.root}
	if o.userAgent != "" {
		args = append(args, "--user-agent", o.userAgent)
	}
	return runPythonStep(ctx, o.runner, script, args)
}

// stepMeta runs build_meta_all.py with a POSITIONAL directory argument — C1:
// the HLD's original --dir invocation would silently no-op (argparse treats
// an unrecognized --dir as an error, not the intended path).
func (o *orchestrator) stepMeta(ctx context.Context, cik string) (result, note string) {
	if !o.force && !metaNeeded(o.root, cik) {
		return "skipped", ""
	}
	script := filepath.Join("python", "edgar", "build_meta_all.py")
	dir := filepath.Join(o.root, "companies", cik)
	args := []string{dir}
	if o.force {
		args = append(args, "--all")
	}
	return runPythonStep(ctx, o.runner, script, args)
}

// stepFinancials runs in-process (C2): financials.ExtractAll then, when a
// user agent is configured, financials.FillGaps. Both functions already
// implement their own per-accession idempotency (skip an accession that
// already has financials.json unless Force) — reusing that is more precise
// than a step-level skip gate, since it can advance some accessions in a
// peer's corpus while leaving already-extracted ones untouched.
func (o *orchestrator) stepFinancials(ctx context.Context, cik string) (result, note string) {
	opts := financials.Options{Force: o.force, WriteNoFacts: true}
	if _, err := financials.ExtractAll(ctx, o.root, cik, opts); err != nil {
		return "error", err.Error()
	}
	if o.userAgent == "" {
		return "ok", "gap-fill skipped: SEC_EDGAR_USER_AGENT not set"
	}
	client := financials.NewClient(o.userAgent, 7*24*time.Hour)
	if _, err := financials.FillGaps(ctx, o.root, cik, client, opts); err != nil {
		return "error", err.Error()
	}
	return "ok", ""
}

// stepPrices reuses companyview.Service.Timeline (D5) — the same path a user
// opening the Timeline tab exercises, so ensureCoverage's own idempotency
// (skip when covered, delta-refresh when stale, full backfill only when
// empty) already governs re-runs; no separate skip gate is needed here.
//
// C3: Timeline's own pricesFor deliberately swallows provider failures (a
// design choice — "price failures degrade the chart, they do not fail the
// request"), and does not even surface the coverage row on the no-ticker
// path. So the step's true outcome is read from the DB coverage row
// directly (db.PriceCoverageRow), not from the Timeline response.
func (o *orchestrator) stepPrices(ctx context.Context, cik string) (result, note string, barCount int) {
	if o.svc == nil || o.database == nil {
		return "skipped", "no price service configured (db unavailable)", 0
	}
	window := companyview.Window{From: "1900-01-01", To: "2100-01-01"}
	if _, err := o.svc.Timeline(ctx, cik, window, ""); err != nil {
		return "error", err.Error(), 0
	}

	barCount = o.countBars(ctx, cik)
	cov, err := o.database.StockPriceCoverage(ctx, cik)
	if err != nil || cov == nil {
		return "error", "no coverage row written", barCount
	}
	switch cov.Status {
	case db.PriceStatusOK:
		return "ok", "", barCount
	case db.PriceStatusNoSymbol:
		return "skipped", "no ticker in submissions.json", barCount
	default:
		return "error", cov.Note, barCount
	}
}

func (o *orchestrator) countBars(ctx context.Context, cik string) int {
	rows, err := o.database.StockPrices(ctx, cik, "0000-01-01", "9999-12-31")
	if err != nil {
		return 0
	}
	return len(rows)
}

// setStep writes a step's result into the schema's step map (see
// StepResults' doc comment for the submissions/filings duplication).
func setStep(s *StepResults, name, result string) {
	switch name {
	case "fetch":
		s.Submissions = result
		s.Filings = result
	case "meta":
		s.Meta = result
	case "financials":
		s.Financials = result
	case "prices":
		s.StockPrices = result
	}
}

// runPeer resolves one peer's CIK and runs every requested step, rolling the
// per-step outcomes up into one PeerStatus row.
//
// Rollup rule (a reasonable default — the LLD leaves "skipped" steps
// unaddressed): a peer with zero step errors is "complete" even if a step
// was "skipped" for a legitimate reason (no ticker, nothing new to fetch) —
// skipping is not failing. "partial" means at least one requested step
// errored but at least one other succeeded; "error" means every requested
// step that ran failed.
func (o *orchestrator) runPeer(ctx context.Context, p SimilarCompany, tickers tickerMap) PeerStatus {
	start := time.Now()
	ps := PeerStatus{CompanyName: p.CompanyName, Ticker: p.Ticker}

	cik, note, ok := resolveCIK(p, tickers)
	if !ok {
		ps.Status = "skipped"
		ps.Steps.Resolve = "skipped"
		ps.Note = note
		ps.DurationMS = time.Since(start).Milliseconds()
		return ps
	}
	ps.CompanyID = cik
	ps.Steps.Resolve = "ok"

	var notes []string
	stepOK, stepErr := 0, 0
	record := func(name, result, note string) {
		setStep(&ps.Steps, name, result)
		switch result {
		case "ok":
			stepOK++
		case "error":
			stepErr++
			if note != "" {
				notes = append(notes, name+": "+note)
			}
		default: // skipped
			if note != "" {
				notes = append(notes, name+": "+note)
			}
		}
	}

	if o.steps["fetch"] {
		result, note := o.stepFetch(ctx, cik)
		record("fetch", result, note)
	}
	if o.steps["meta"] {
		result, note := o.stepMeta(ctx, cik)
		record("meta", result, note)
	}
	if o.steps["financials"] {
		result, note := o.stepFinancials(ctx, cik)
		record("financials", result, note)
	}
	if o.steps["prices"] {
		result, note, bars := o.stepPrices(ctx, cik)
		ps.StockBarCount = bars
		record("prices", result, note)
	}

	ps.FilingCount = countFiles(o.root, cik, "filing.json")
	ps.FinancialsCount = countFiles(o.root, cik, "financials.json")
	ps.Note = strings.Join(notes, "; ")
	ps.DurationMS = time.Since(start).Milliseconds()

	switch {
	case stepErr == 0:
		ps.Status = "complete"
	case stepOK > 0:
		ps.Status = "partial"
	default:
		ps.Status = "error"
	}
	return ps
}
