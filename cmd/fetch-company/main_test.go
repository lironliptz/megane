package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestExitCodesUsage(t *testing.T) {
	if code := run([]string{}); code != 2 {
		t.Errorf("missing -cik: exit = %d, want 2", code)
	}
	if code := run([]string{"-cik", "not-a-cik-at-all-!!"}); code != 2 {
		t.Errorf("invalid -cik: exit = %d, want 2", code)
	}
}

func TestParseSteps(t *testing.T) {
	steps := parseSteps("fetch, meta ,prices")
	if !steps["fetch"] || !steps["meta"] || !steps["prices"] || steps["financials"] {
		t.Errorf("parseSteps result = %+v", steps)
	}
}

// TestRunWritesStatusFile exercises the whole command (no live network — no
// steps requested) and confirms it writes fetch-status.json scoped to the
// company's own folder, with no similar-list/reference-company framing
// anywhere in the schema.
func TestRunWritesStatusFile(t *testing.T) {
	root := t.TempDir()
	code := run([]string{"-cik", "1368514", "-root", root, "-steps", ""})
	if code != 0 {
		t.Fatalf("exit = %d, want 0", code)
	}

	path := filepath.Join(root, "companies", "0001368514", "fetch-status.json")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("status file not written: %v", err)
	}
	var out status
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("decode status: %v", err)
	}
	if out.CIK != "0001368514" {
		t.Errorf("cik = %q, want normalized 0001368514", out.CIK)
	}
	if out.Status != "complete" {
		t.Errorf("status = %q, want complete (no steps requested -> zero errors)", out.Status)
	}
}

func TestRunAcceptsVerboseFlag(t *testing.T) {
	root := t.TempDir()
	code := run([]string{"-cik", "1368514", "-root", root, "-steps", "", "-v"})
	if code != 0 {
		t.Fatalf("exit = %d, want 0", code)
	}
}

func TestExitCodesRejectConflictingModes(t *testing.T) {
	code := run([]string{"-cik", "1368514", "-stock-only", "-sec-only"})
	if code != 2 {
		t.Fatalf("exit = %d, want 2", code)
	}
}

func TestResolveStepsStockOnly(t *testing.T) {
	steps := resolveSteps("fetch,meta,financials,prices", true, false)
	if len(steps) != 1 || !steps["prices"] {
		t.Fatalf("steps = %+v, want prices-only", steps)
	}
}

func TestResolveStepsSecOnly(t *testing.T) {
	steps := resolveSteps("prices", false, true)
	if len(steps) != 3 || !steps["fetch"] || !steps["meta"] || !steps["financials"] {
		t.Fatalf("steps = %+v, want fetch/meta/financials only", steps)
	}
	if steps["prices"] {
		t.Fatalf("steps = %+v, prices must be disabled in sec-only", steps)
	}
}
