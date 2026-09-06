package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"megane/internal/db"
	"megane/internal/models"
)

func writeFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestUsageErrors(t *testing.T) {
	if code := run([]string{}); code != 2 {
		t.Fatalf("empty args => %d, want 2", code)
	}
	if code := run([]string{"--cik", "1", "--similar", "x.json"}); code != 2 {
		t.Fatalf("mutually exclusive => %d, want 2", code)
	}
}

func TestDryRunWritesPlanOnly(t *testing.T) {
	root := t.TempDir()
	dbPath := filepath.Join(root, "t.db")
	similarPath := filepath.Join(root, "similar", "kamada.json")
	writeFile(t, similarPath, `{
		"similar_companies":[
			{"company_name":"A","company_id":"0000000001","ticker":"A","fetch":true},
			{"company_name":"B","company_id":"0000000002","ticker":"B","fetch":true},
			{"company_name":"C","company_id":"0000000003","ticker":"C","fetch":true},
			{"company_name":"D","company_id":"0000000004","ticker":"D","fetch":false}
		]
	}`)

	t.Setenv("DB_PATH", dbPath)
	t.Setenv("FILEDB_DIR", root)
	t.Setenv("CONSENSUS_PROVIDER", "finnhub")
	code := run([]string{"--similar", similarPath, "--dry-run"})
	if code != 0 {
		t.Fatalf("dry-run exit = %d", code)
	}

	planPath := filepath.Join(root, "similar", "kamada.consensus-plan.json")
	raw, err := os.ReadFile(planPath)
	if err != nil {
		t.Fatalf("plan missing: %v", err)
	}
	var plan dryRunPlan
	if err := json.Unmarshal(raw, &plan); err != nil {
		t.Fatalf("decode plan: %v", err)
	}
	if len(plan.Targets) != 3 {
		t.Fatalf("targets = %d, want 3", len(plan.Targets))
	}

	database, err := db.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	rows, err := database.ConsensusPeriods(context.Background(), "0000000001", "finnhub")
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 0 {
		t.Fatalf("dry run wrote rows: %d", len(rows))
	}
	if _, err := os.Stat(filepath.Join(root, "companies", "0000000001", "consensus.json")); !os.IsNotExist(err) {
		t.Fatal("dry run wrote consensus sidecar unexpectedly")
	}
}

func TestRowWithoutPeriodEndIsDropped(t *testing.T) {
	rows := []models.ConsensusPeriod{
		{CIK: "1", Source: "finnhub", FiscalYear: "2025", FiscalPeriod: "Q3", PeriodEnd: "", Duration: "P3M", ConsensusDate: "2025-09-30"},
		{CIK: "1", Source: "finnhub", FiscalYear: "2025", FiscalPeriod: "Q2", PeriodEnd: "2025-06-30", Duration: "P3M", ConsensusDate: "2025-06-29"},
	}
	filtered, dropped := filterRowsWithPeriod(rows)
	if dropped != 1 || len(filtered) != 1 {
		t.Fatalf("filtered=%d dropped=%d", len(filtered), dropped)
	}
}

func TestNoKeyIsCoverageError(t *testing.T) {
	root := t.TempDir()
	dbPath := filepath.Join(root, "t.db")
	cik := "0001567529"
	writeFile(t, filepath.Join(root, "companies", cik, "submissions.json"), `{
		"name":"Kamada Ltd.",
		"tickers":["KMDA"],
		"fiscalYearEnd":"1231"
	}`)

	t.Setenv("DB_PATH", dbPath)
	t.Setenv("FILEDB_DIR", root)
	t.Setenv("FINNHUB_API_KEY", "")
	code := run([]string{"--cik", cik})
	if code != 1 {
		t.Fatalf("exit=%d, want 1", code)
	}
	database, err := db.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	cov, err := database.AnalystCoverage(context.Background(), cik)
	if err != nil {
		t.Fatal(err)
	}
	if cov == nil || cov.Status != db.AnalystCoverageError {
		t.Fatalf("coverage=%+v, want status=error", cov)
	}
}
