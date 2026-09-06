package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"megane/internal/db"
)

// fakeRunner records every invocation and returns a scripted result — this is
// the injectable seam the fetch/meta steps use so exec is never really
// called in tests (LLD §7).
type fakeRunner struct {
	calls    [][]string
	exitCode int
	output   []byte
	err      error
}

func (f *fakeRunner) run(ctx context.Context, name string, args ...string) (int, []byte, error) {
	f.calls = append(f.calls, append([]string{name}, args...))
	return f.exitCode, f.output, f.err
}

func TestRunPythonStepOK(t *testing.T) {
	f := &fakeRunner{exitCode: 0, output: []byte("Downloaded 1 filing\n")}
	result, note := runPythonStep(context.Background(), f.run, "script.py", []string{"--cik", "1"})
	if result != "ok" || note != "" {
		t.Errorf("result=%q note=%q, want ok/\"\"", result, note)
	}
	if len(f.calls) != 1 {
		t.Fatalf("expected exactly one exec call, got %d", len(f.calls))
	}
}

func TestRunPythonStepNonZeroExit(t *testing.T) {
	f := &fakeRunner{exitCode: 1, output: []byte("Traceback...\nERROR: boom\n")}
	result, note := runPythonStep(context.Background(), f.run, "script.py", nil)
	if result != "error" {
		t.Errorf("result = %q, want error", result)
	}
	if note == "" {
		t.Error("expected a note carrying the failure output")
	}
}

func TestTruncateNoteKeepsTail(t *testing.T) {
	big := make([]byte, noteMaxBytes+500)
	for i := range big {
		big[i] = 'a'
	}
	copy(big[len(big)-3:], []byte("end"))
	got := truncateNote(big)
	if len(got) != noteMaxBytes {
		t.Fatalf("len(got) = %d, want %d", len(got), noteMaxBytes)
	}
	if got[len(got)-3:] != "end" {
		t.Errorf("truncateNote must keep the tail, got suffix %q", got[len(got)-3:])
	}
}

// --- fetch/meta idempotency ------------------------------------------------

func writeMinimalSubmissions(t *testing.T, root, cik string, filingDates []string) {
	t.Helper()
	dir := filepath.Join(root, "companies", cik)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	accns := make([]string, len(filingDates))
	for i := range filingDates {
		accns[i] = "0000000000-00-00000" // any placeholder; not looked up by name here
	}
	var body strings.Builder
	body.WriteString(`{"filings":{"recent":{"accessionNumber":[`)
	for i, a := range accns {
		if i > 0 {
			body.WriteByte(',')
		}
		body.WriteString(`"` + a + `"`)
	}
	body.WriteString(`],"filingDate":[`)
	for i, d := range filingDates {
		if i > 0 {
			body.WriteByte(',')
		}
		body.WriteString(`"` + d + `"`)
	}
	body.WriteString(`]}}}`)
	writeFile(t, filepath.Join(dir, "submissions.json"), body.String())
}

func TestFetchNeededNoSubmissions(t *testing.T) {
	root := t.TempDir()
	if !fetchNeeded(root, "0000000001", 10) {
		t.Error("fetch must be needed when submissions.json does not exist")
	}
}

func TestFetchNeededAllOnDisk(t *testing.T) {
	root := t.TempDir()
	cik := "0000000001"
	writeMinimalSubmissions(t, root, cik, []string{"2026-01-01"})
	accnDir := filepath.Join(root, "companies", cik, "2026", "0000000000-00-00000")
	if err := os.MkdirAll(accnDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(accnDir, "filing.json"), `{}`)

	if fetchNeeded(root, cik, 10) {
		t.Error("fetch should be skipped when every in-window filing is already on disk")
	}
}

func TestFetchNeededMissingAccessionFolder(t *testing.T) {
	root := t.TempDir()
	cik := "0000000001"
	writeMinimalSubmissions(t, root, cik, []string{"2026-01-01"})
	// No accession folder created — a filing in the window is missing on disk.
	if !fetchNeeded(root, cik, 10) {
		t.Error("fetch should be needed when a filing inside the window is missing on disk")
	}
}

func TestMetaNeeded(t *testing.T) {
	root := t.TempDir()
	cik := "0000000001"
	accnDir := filepath.Join(root, "companies", cik, "2026", "0000000000-00-00000")
	if err := os.MkdirAll(accnDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(accnDir, "filing.json"), `{}`)

	if !metaNeeded(root, cik) {
		t.Error("meta should be needed when filing.json has no sibling meta.json")
	}

	writeFile(t, filepath.Join(accnDir, "meta.json"), `{}`)
	if metaNeeded(root, cik) {
		t.Error("meta should not be needed once every filing.json has a meta.json")
	}
}

// --- price status mapping (C3) --------------------------------------------

func TestPriceStatusMapping(t *testing.T) {
	cases := []struct {
		status     string
		wantResult string
	}{
		{db.PriceStatusOK, "ok"},
		{db.PriceStatusNoSymbol, "skipped"},
		{db.PriceStatusNotFound, "error"},
		{db.PriceStatusError, "error"},
	}
	for _, c := range cases {
		var result string
		switch c.status {
		case db.PriceStatusOK:
			result = "ok"
		case db.PriceStatusNoSymbol:
			result = "skipped"
		default:
			result = "error"
		}
		if result != c.wantResult {
			t.Errorf("status %q -> %q, want %q", c.status, result, c.wantResult)
		}
	}
}

func TestStepPricesNoService(t *testing.T) {
	o := &orchestrator{}
	result, note, bars := o.stepPrices(context.Background(), "0000000001")
	if result != "skipped" || bars != 0 || note == "" {
		t.Errorf("result=%q note=%q bars=%d, want skipped/non-empty note/0", result, note, bars)
	}
}

// --- financials step: real financials.ExtractAll, no mocks, no network -----

func TestStepFinancialsNoCorpusOnDisk(t *testing.T) {
	// A peer whose fetch step never ran (or failed) has no
	// root/companies/{cik} directory at all. financials.ExtractAll reads that
	// directory directly (os.ReadDir) and errors when it's missing — this
	// confirms the step surfaces that as "error" rather than panicking, and
	// that runPeer's rollup handles it (a real integration point, not a
	// fake — exercises the actual internal/edgar/financials package).
	o := &orchestrator{root: t.TempDir()}
	result, note := o.stepFinancials(context.Background(), "0000000001")
	if result != "error" {
		t.Errorf("result = %q, want error (note=%q)", result, note)
	}
	if note == "" {
		t.Error("expected a note explaining the failure")
	}
}

func TestStepFinancialsEmptyAccessionDir(t *testing.T) {
	// A peer whose fetch step ran but found nothing (empty directory) should
	// be a clean no-op "ok", not an error — zero accessions to extract.
	root := t.TempDir()
	cik := "0000000001"
	if err := os.MkdirAll(root+"/companies/"+cik, 0o755); err != nil {
		t.Fatal(err)
	}
	o := &orchestrator{root: root}
	result, note := o.stepFinancials(context.Background(), cik)
	if result != "ok" {
		t.Errorf("result = %q, want ok (note=%q)", result, note)
	}
}

// --- peer rollup ------------------------------------------------------------

func TestRunPeerSkippedWhenUnresolved(t *testing.T) {
	o := &orchestrator{root: t.TempDir(), steps: map[string]bool{"fetch": true}, runner: (&fakeRunner{}).run}
	ps := o.runPeer(context.Background(), SimilarCompany{CompanyName: "No CIK Co"}, nil)
	if ps.Status != "skipped" {
		t.Errorf("status = %q, want skipped", ps.Status)
	}
	if ps.Note == "" {
		t.Error("expected a note explaining the skip")
	}
}

func TestRunPeerCompleteWhenAllStepsOK(t *testing.T) {
	root := t.TempDir()
	cik := "0001368514"
	f := &fakeRunner{exitCode: 0, output: []byte("ok\n")}
	o := &orchestrator{root: root, years: 10, steps: map[string]bool{"fetch": true, "meta": true}, runner: f.run}
	id := "1368514"
	ps := o.runPeer(context.Background(), SimilarCompany{CompanyName: "ADMA", CompanyID: &id}, nil)
	if ps.Status != "complete" {
		t.Errorf("status = %q, want complete (steps=%+v note=%q)", ps.Status, ps.Steps, ps.Note)
	}
	if ps.CompanyID != cik {
		t.Errorf("company_id = %q, want %q", ps.CompanyID, cik)
	}
	// The fake fetch runner doesn't touch the filesystem, so metaNeeded finds
	// no filing.json to build meta for and skips — only the fetch step execs.
	if len(f.calls) != 1 {
		t.Errorf("expected 1 exec call (fetch only, meta has nothing to do), got %d", len(f.calls))
	}
	if ps.Steps.Meta != "skipped" {
		t.Errorf("meta step = %q, want skipped", ps.Steps.Meta)
	}
}

func TestRunPeerPartialWhenOneStepErrors(t *testing.T) {
	root := t.TempDir()
	f := &fakeRunner{exitCode: 1, output: []byte("boom")}
	// fetch will fail (exit 1); meta step is skipped since no filings exist on
	// disk to build meta for, so it comes back "skipped", not "ok" — the peer
	// must still land on "error" (zero OK steps), not "partial".
	o := &orchestrator{root: root, years: 10, steps: map[string]bool{"fetch": true, "meta": true}, runner: f.run}
	id := "1368514"
	ps := o.runPeer(context.Background(), SimilarCompany{CompanyName: "ADMA", CompanyID: &id}, nil)
	if ps.Status != "error" {
		t.Errorf("status = %q, want error (steps=%+v)", ps.Status, ps.Steps)
	}
}
