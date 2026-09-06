package main

import (
	"path/filepath"
	"testing"
)

func TestExitCodesUsageAndParse(t *testing.T) {
	if code := run([]string{}); code != 2 {
		t.Errorf("missing -similar: exit = %d, want 2", code)
	}

	dir := t.TempDir()
	bad := filepath.Join(dir, "bad.json")
	writeFile(t, bad, `not json`)
	if code := run([]string{"-similar", bad}); code != 2 {
		t.Errorf("malformed JSON: exit = %d, want 2", code)
	}
}

func TestParseStepsAndSortedSteps(t *testing.T) {
	steps := parseSteps("fetch, meta ,prices")
	if !steps["fetch"] || !steps["meta"] || !steps["prices"] || steps["financials"] {
		t.Errorf("parseSteps result = %+v", steps)
	}
	got := sortedSteps(steps)
	want := []string{"fetch", "meta", "prices"} // allSteps order: fetch, meta, financials, prices
	if len(got) != len(want) {
		t.Fatalf("sortedSteps = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("sortedSteps[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}
