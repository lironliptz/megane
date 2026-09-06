package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestStatusPathDerivation(t *testing.T) {
	cases := map[string]string{
		"kamada.json":               "kamada.fetch-status.json",
		"/a/b/kedrion.json":         "/a/b/kedrion.fetch-status.json",
		"./fileDB/similar/foo.json": "fileDB/similar/foo.fetch-status.json",
	}
	for in, want := range cases {
		got := statusPath(in)
		gotClean := filepath.Clean(got)
		wantClean := filepath.Clean(want)
		if gotClean != wantClean {
			t.Errorf("statusPath(%q) = %q, want %q", in, gotClean, wantClean)
		}
	}
}

func TestPlanPathDerivation(t *testing.T) {
	if got := planPath("kamada.json"); got != "kamada.fetch-plan.json" {
		t.Errorf("planPath = %q, want kamada.fetch-plan.json", got)
	}
	// D9: the two derived paths must never collide.
	if statusPath("kamada.json") == planPath("kamada.json") {
		t.Fatal("status and plan paths must differ")
	}
}

func TestStatusRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "kamada.fetch-status.json")

	status := &FetchStatus{
		SourceFile:  "kamada.json",
		Reference:   ReferenceInfo{CompanyName: "Kamada Ltd.", CompanyID: "0001567529", Ticker: "KMDA"},
		GeneratedAt: "2026-09-01T17:00:00Z",
		Config:      RunConfig{Root: "./fileDB", Years: 10, Steps: []string{"fetch", "meta"}, Force: false},
		Peers: []PeerStatus{
			{CompanyName: "ADMA Biologics, Inc.", CompanyID: "0001368514", Ticker: "ADMA", Status: "complete",
				Steps:       StepResults{Resolve: "ok", Submissions: "ok", Filings: "ok", Meta: "ok", Financials: "ok", StockPrices: "ok"},
				FilingCount: 412, FinancialsCount: 58, StockBarCount: 2847},
			{CompanyName: "Kedrion Biopharma (Kedrion SpA)", Ticker: "KEDR", Status: "skipped",
				Note: "no SEC CIK — not a US-primary filer"},
		},
	}
	status.Summary = summarize(status.Peers)

	if err := writeStatus(path, status); err != nil {
		t.Fatalf("writeStatus: %v", err)
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	var got FetchStatus
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if !reflect.DeepEqual(got, *status) {
		t.Errorf("round trip mismatch:\n got=%+v\nwant=%+v", got, *status)
	}
	if got.Summary.Total != 2 || got.Summary.Complete != 1 || got.Summary.Skipped != 1 {
		t.Errorf("summary = %+v, want total=2 complete=1 skipped=1", got.Summary)
	}
	if len(raw) == 0 || raw[len(raw)-1] != '\n' {
		t.Error("expected a trailing newline")
	}
}

func TestSummarizeAllStatuses(t *testing.T) {
	peers := []PeerStatus{
		{Status: "complete"}, {Status: "partial"}, {Status: "skipped"}, {Status: "error"}, {Status: "complete"},
	}
	s := summarize(peers)
	want := Summary{Total: 5, Complete: 2, Partial: 1, Skipped: 1, Error: 1}
	if s != want {
		t.Errorf("summarize = %+v, want %+v", s, want)
	}
}
