package main

import (
	"context"
	"testing"

	"megane/internal/companyfetch"
)

// fakeRunner records every invocation and returns a scripted result — the
// step logic itself is tested in internal/companyfetch; these tests only
// cover this package's adapter: peer resolution + mapping a
// companyfetch.Result into a PeerStatus row.
type fakeRunner struct {
	calls    [][]string
	exitCode int
	output   []byte
}

func (f *fakeRunner) run(ctx context.Context, name string, args ...string) (int, []byte, error) {
	f.calls = append(f.calls, append([]string{name}, args...))
	return f.exitCode, f.output, nil
}

func TestRunPeerSkippedWhenUnresolved(t *testing.T) {
	cfg := companyfetch.Config{Root: t.TempDir(), Steps: map[string]bool{"fetch": true}, Runner: (&fakeRunner{}).run}
	ps := runPeer(context.Background(), cfg, SimilarCompany{CompanyName: "No CIK Co"}, nil)
	if ps.Status != "skipped" {
		t.Errorf("status = %q, want skipped", ps.Status)
	}
	if ps.Note == "" {
		t.Error("expected a note explaining the skip")
	}
}

func TestRunPeerMapsCompanyfetchResult(t *testing.T) {
	root := t.TempDir()
	f := &fakeRunner{exitCode: 0, output: []byte("ok\n")}
	cfg := companyfetch.Config{Root: root, Years: 10, Steps: map[string]bool{"fetch": true}, Runner: f.run}
	id := "1368514"
	ps := runPeer(context.Background(), cfg, SimilarCompany{CompanyName: "ADMA", CompanyID: &id}, nil)

	if ps.CompanyID != "0001368514" {
		t.Errorf("company_id = %q, want normalized 0001368514", ps.CompanyID)
	}
	if ps.Status != "complete" {
		t.Errorf("status = %q, want complete (steps=%+v note=%q)", ps.Status, ps.Steps, ps.Note)
	}
	if ps.Steps.Resolve != "ok" || ps.Steps.Submissions != "ok" || ps.Steps.Filings != "ok" {
		t.Errorf("steps = %+v, want resolve/submissions/filings all ok", ps.Steps)
	}
	if len(f.calls) != 1 {
		t.Errorf("expected exactly 1 exec call (fetch), got %d", len(f.calls))
	}
}
