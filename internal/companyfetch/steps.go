package companyfetch

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"megane/internal/companyview"
	"megane/internal/db"
	"megane/internal/edgar/financials"
)

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
// (result, note) — never returning a Go error itself, so one CIK's failure
// never aborts a caller's batch.
func runPythonStep(ctx context.Context, runner ExecRunner, script string, args []string) (result, note string) {
	exitCode, out, err := runner(ctx, pythonExe(), append([]string{script}, args...)...)
	if err != nil {
		return "error", err.Error()
	}
	if exitCode != 0 {
		return "error", truncateNote(out)
	}
	return "ok", ""
}

func logStepOutput(cfg Config, out []byte) {
	if !cfg.Verbose {
		return
	}
	text := strings.TrimSpace(string(out))
	if text == "" {
		return
	}
	lines := strings.Split(text, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		cfg.logf("fetch-company: output=%s", line)
	}
}

// fetchNeeded implements the fetch step's idempotency: skip only when
// submissions.json exists AND every filing within the --years window
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

// stepFetch runs the (generalized) Python fetch script (prompt 13 D1 step 1, D2).
func stepFetch(ctx context.Context, cfg Config, cik string) (result, note string) {
	if !cfg.Force && !fetchNeeded(cfg.Root, cik, cfg.Years) {
		return "skipped", ""
	}
	script := filepath.Join("python", "edgar", "fetch_kamada.py")
	args := []string{"--cik", cik, "--years", strconv.Itoa(cfg.Years), "--root", cfg.Root}
	if cfg.UserAgent != "" {
		args = append(args, "--user-agent", cfg.UserAgent)
	}
	cfg.logf("fetch-company: exec=%s %s", pythonExe(), strings.Join(append([]string{script}, args...), " "))
	exitCode, out, err := cfg.runner()(ctx, pythonExe(), append([]string{script}, args...)...)
	if err != nil {
		return "error", err.Error()
	}
	logStepOutput(cfg, out)
	if exitCode != 0 {
		return "error", truncateNote(out)
	}
	result, note = "ok", ""
	if cfg.Verbose && note != "" {
		cfg.logf("fetch-company: exec-note=%s", note)
	}
	return result, note
}

// stepMeta runs build_meta_all.py with a POSITIONAL directory argument — C1:
// the HLD's original --dir invocation would silently no-op (argparse treats
// an unrecognized --dir as an error, not the intended path).
func stepMeta(ctx context.Context, cfg Config, cik string) (result, note string) {
	if !cfg.Force && !metaNeeded(cfg.Root, cik) {
		return "skipped", ""
	}
	script := filepath.Join("python", "edgar", "build_meta_all.py")
	dir := filepath.Join(cfg.Root, "companies", cik)
	args := []string{dir}
	if cfg.Force {
		args = append(args, "--all")
	}
	cfg.logf("fetch-company: exec=%s %s", pythonExe(), strings.Join(append([]string{script}, args...), " "))
	exitCode, out, err := cfg.runner()(ctx, pythonExe(), append([]string{script}, args...)...)
	if err != nil {
		return "error", err.Error()
	}
	logStepOutput(cfg, out)
	if exitCode != 0 {
		return "error", truncateNote(out)
	}
	result, note = "ok", ""
	if cfg.Verbose && note != "" {
		cfg.logf("fetch-company: exec-note=%s", note)
	}
	return result, note
}

// stepFinancials runs in-process (C2): financials.ExtractAll then, when a
// user agent is configured, financials.FillGaps. Both functions already
// implement their own per-accession idempotency (skip an accession that
// already has financials.json unless Force) — reusing that is more precise
// than a step-level skip gate, since it can advance some accessions in a
// corpus while leaving already-extracted ones untouched.
func stepFinancials(ctx context.Context, cfg Config, cik string) (result, note string) {
	cfg.logf("fetch-company: financials extract start cik=%s", cik)
	opts := financials.Options{Force: cfg.Force, WriteNoFacts: true}
	if _, err := financials.ExtractAll(ctx, cfg.Root, cik, opts); err != nil {
		return "error", err.Error()
	}
	if cfg.UserAgent == "" {
		return "ok", "gap-fill skipped: SEC_EDGAR_USER_AGENT not set"
	}
	cfg.logf("fetch-company: financials gap-fill start cik=%s", cik)
	client := financials.NewClient(cfg.UserAgent, 7*24*time.Hour)
	if _, err := financials.FillGaps(ctx, cfg.Root, cik, client, opts); err != nil {
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
func stepPrices(ctx context.Context, cfg Config, cik string) (result, note string, barCount int) {
	if cfg.Svc == nil || cfg.Database == nil {
		return "skipped", "no price service configured (db unavailable)", 0
	}
	window := companyview.Window{From: "1900-01-01", To: "2100-01-01"}
	cfg.logf("fetch-company: prices timeline fetch start cik=%s", cik)
	if _, err := cfg.Svc.Timeline(ctx, cik, window, ""); err != nil {
		return "error", err.Error(), 0
	}

	barCount = countBars(ctx, cfg, cik)
	cov, err := cfg.Database.StockPriceCoverage(ctx, cik)
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

func countBars(ctx context.Context, cfg Config, cik string) int {
	rows, err := cfg.Database.StockPrices(ctx, cik, "0000-01-01", "9999-12-31")
	if err != nil {
		return 0
	}
	return len(rows)
}
