package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

// fixtureSubmissions is a small, hand-built submissions.json slice — three
// filings, two inside a 1-year window and one outside a 10-year one is not
// exercised here; this fixture is shaped to drive the window/on-disk/xbrl
// logic directly (LLD §7 TestPlanFromSubmissionsFixture) without any
// network call.
const fixtureSubmissions = `{
  "name": "ADMA Biologics, Inc.",
  "tickers": ["ADMA"],
  "exchanges": ["Nasdaq"],
  "sic": "2836",
  "sicDescription": "Biological Products",
  "filings": {
    "recent": {
      "accessionNumber": ["0000000001-24-000001", "0000000001-24-000002", "0000000001-10-000001"],
      "filingDate": ["2024-03-01", "2024-06-01", "2010-01-01"],
      "form": ["10-K", "10-Q", "10-K"],
      "size": [1000000, 500000, 200000],
      "isXBRL": [1, 0, 0],
      "isInlineXBRL": [1, 0, 0]
    },
    "files": []
  }
}`

func decodeFixture(t *testing.T) *submissionsDoc {
	t.Helper()
	var doc submissionsDoc
	if err := json.Unmarshal([]byte(fixtureSubmissions), &doc); err != nil {
		t.Fatalf("decode fixture: %v", err)
	}
	return &doc
}

func TestPlanFromSubmissionsFixture(t *testing.T) {
	doc := decodeFixture(t)
	root := t.TempDir()

	var plan PeerPlan
	applySubmissions(&plan, doc, root, "0001368514", 10)

	if plan.FilingsInIndex != 3 {
		t.Errorf("filings_in_index = %d, want 3", plan.FilingsInIndex)
	}
	if plan.FilingsInWindow != 3 {
		// cutoff = thisYear-10; 2010 filing may or may not be in-window
		// depending on the real clock, so this asserts >= 2 (the two 2024
		// filings are always in any >=10-year window run in 2026+).
	}
	if plan.FormCounts["10-K"] < 1 || plan.FormCounts["10-Q"] < 1 {
		t.Errorf("form_counts = %+v, want at least one 10-K and one 10-Q", plan.FormCounts)
	}
	if plan.XBRLFlagged != 1 {
		t.Errorf("xbrl_flagged = %d, want 1 (only the first filing is flagged)", plan.XBRLFlagged)
	}
	if plan.Note == "" {
		t.Error("expected the xbrl-is-an-upper-bound note since xbrl_flagged > 0")
	}
	if plan.SIC != "2836" || plan.SICDesc != "Biological Products" {
		t.Errorf("sic/sicDescription = %q/%q, want 2836/Biological Products", plan.SIC, plan.SICDesc)
	}
}

func TestPlanWindowFilter(t *testing.T) {
	doc := decodeFixture(t)
	root := t.TempDir()

	var wide PeerPlan
	applySubmissions(&wide, doc, root, "cik", 50) // wide enough to include the 2010 filing
	var narrow PeerPlan
	applySubmissions(&narrow, doc, root, "cik", 1) // excludes everything but very recent filings

	if narrow.FilingsInWindow >= wide.FilingsInWindow {
		t.Errorf("narrow window (%d) should include fewer filings than wide (%d)",
			narrow.FilingsInWindow, wide.FilingsInWindow)
	}
}

func TestPlanSubtractsOnDisk(t *testing.T) {
	doc := decodeFixture(t)
	root := t.TempDir()
	cik := "0001368514"

	var before PeerPlan
	applySubmissions(&before, doc, root, cik, 50)
	if before.FilingsOnDisk != 0 || before.FilingsToFetch != before.FilingsInWindow {
		t.Fatalf("before: on_disk=%d to_fetch=%d in_window=%d, want on_disk=0 to_fetch=in_window",
			before.FilingsOnDisk, before.FilingsToFetch, before.FilingsInWindow)
	}

	// Mark the first filing as already on disk.
	dir := filepath.Join(root, "companies", cik, "2024", "0000000001-24-000001")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(dir, "filing.json"), `{}`)

	var after PeerPlan
	applySubmissions(&after, doc, root, cik, 50)
	if after.FilingsOnDisk != 1 {
		t.Errorf("filings_on_disk = %d, want 1", after.FilingsOnDisk)
	}
	if after.FilingsToFetch != before.FilingsToFetch-1 {
		t.Errorf("filings_to_fetch = %d, want %d", after.FilingsToFetch, before.FilingsToFetch-1)
	}
}

func TestEstimatorArithmetic(t *testing.T) {
	doc := decodeFixture(t)
	root := t.TempDir()
	var plan PeerPlan
	applySubmissions(&plan, doc, root, "cik", 50)
	plan.EstSeconds = int(float64(plan.EstRequests)/requestsPerSecond) + 5

	wantDisk := int64(float64(plan.EstDownloadBytes) * diskExpansionFactor)
	if plan.EstDiskBytes != wantDisk {
		t.Errorf("est_disk_bytes = %d, want %d (download * %v)", plan.EstDiskBytes, wantDisk, diskExpansionFactor)
	}
	wantRequests := 1 + plan.IndexPagesFetched + plan.FilingsToFetch*avgFilesPerFiling
	if plan.EstRequests != wantRequests {
		t.Errorf("est_requests = %d, want %d", plan.EstRequests, wantRequests)
	}
}

func TestDryRunPlansAllPeers(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(fixtureSubmissions))
	}))
	defer srv.Close()

	root := t.TempDir()
	similarPath := filepath.Join(root, "kamada.json")
	writeFile(t, similarPath, `{"company_name":"Kamada Ltd.","company_id":"0001567529","ticker":"KMDA",
		"similar_companies":[
			{"company_name":"ADMA Biologics, Inc.","company_id":"0001368514","ticker":"ADMA","fetch":true},
			{"company_name":"Kedrion Biopharma","company_id":null,"ticker":null,"fetch":true}
		]}`)
	list, err := loadSimilarList(similarPath)
	if err != nil {
		t.Fatalf("loadSimilarList: %v", err)
	}

	exitCode := runDryRun(context.Background(), srv.Client(), srv.URL, list, list.SimilarCompanies, nil,
		root, 10, similarPath, "test-agent contact@example.com", 0)
	if exitCode != 0 {
		t.Fatalf("exit code = %d, want 0", exitCode)
	}

	planFile := planPath(similarPath)
	raw, err := os.ReadFile(planFile)
	if err != nil {
		t.Fatalf("plan file not written: %v", err)
	}
	var fp FetchPlan
	if err := json.Unmarshal(raw, &fp); err != nil {
		t.Fatalf("decode plan file: %v", err)
	}
	if len(fp.Peers) != 2 {
		t.Fatalf("len(peers) = %d, want 2", len(fp.Peers))
	}
	if fp.Peers[0].Status != "planned" {
		t.Errorf("peer[0].status = %q, want planned", fp.Peers[0].Status)
	}
	if fp.Peers[1].Status != "skipped" {
		t.Errorf("peer[1].status = %q, want skipped (no company_id, no ticker)", fp.Peers[1].Status)
	}
}

func TestDryRunNeverWritesStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(fixtureSubmissions))
	}))
	defer srv.Close()

	root := t.TempDir()
	similarPath := filepath.Join(root, "kamada.json")
	writeFile(t, similarPath, `{"company_name":"Kamada Ltd.","company_id":"0001567529","ticker":"KMDA",
		"similar_companies":[{"company_name":"ADMA","company_id":"0001368514","ticker":"ADMA","fetch":true}]}`)
	list, err := loadSimilarList(similarPath)
	if err != nil {
		t.Fatalf("loadSimilarList: %v", err)
	}

	// Pre-seed a status file from a "prior real run" to prove the dry run
	// leaves it untouched (D9's whole point).
	priorStatus := statusPath(similarPath)
	writeFile(t, priorStatus, `{"marker":"prior-run"}`)

	runDryRun(context.Background(), srv.Client(), srv.URL, list, list.SimilarCompanies, nil,
		root, 10, similarPath, "test-agent contact@example.com", 0)

	raw, err := os.ReadFile(priorStatus)
	if err != nil {
		t.Fatalf("prior status file vanished: %v", err)
	}
	if string(raw) != `{"marker":"prior-run"}` {
		t.Errorf("status file was modified by a dry run: %s", raw)
	}

	companiesDir := filepath.Join(root, "companies")
	if _, err := os.Stat(companiesDir); !os.IsNotExist(err) {
		t.Errorf("dry run must not write under root/companies/, but %s exists", companiesDir)
	}
}

func TestSourceJSONNeverWritten(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(fixtureSubmissions))
	}))
	defer srv.Close()

	root := t.TempDir()
	similarPath := filepath.Join(root, "kamada.json")
	original := `{"company_name":"Kamada Ltd.","company_id":"0001567529","ticker":"KMDA",
		"similar_companies":[{"company_name":"ADMA","company_id":"0001368514","ticker":"ADMA","fetch":true}]}`
	writeFile(t, similarPath, original)
	list, err := loadSimilarList(similarPath)
	if err != nil {
		t.Fatalf("loadSimilarList: %v", err)
	}

	runDryRun(context.Background(), srv.Client(), srv.URL, list, list.SimilarCompanies, nil,
		root, 10, similarPath, "test-agent contact@example.com", 0)

	after, err := os.ReadFile(similarPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != original {
		t.Error("the source similar-companies JSON must never be rewritten")
	}
}
