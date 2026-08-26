package companyview

import (
	"os"
	"testing"

	"megane/internal/filedb"
)

// TestTimelineDefinitionOfDone exercises the whole read path against the real
// corpus: ScanCompanyFilings loads each accession's financials.json, BuildEvents
// attaches highlights, and the two Definition-of-Done markers are asserted.
//
// Skipped when the corpus is not on disk (it is gitignored), so the package
// still tests in a fresh checkout.
func TestTimelineDefinitionOfDone(t *testing.T) {
	const root = "../../fileDB"
	const cik = "0001567529"
	if _, err := os.Stat(root + "/companies/" + cik); err != nil {
		t.Skip("corpus not present; run cmd/edgar-financials against fileDB first")
	}
	rows, err := filedb.ScanCompanyFilings(root, cik)
	if err != nil {
		t.Fatal(err)
	}
	events := BuildEvents(rows, Window{From: "2000-01-01", To: "2100-01-01"}, FilterAll)

	byDate := map[string]Event{}
	for _, e := range events {
		if _, seen := byDate[e.FilingDate]; !seen || e.Highlights != nil {
			byDate[e.FilingDate] = e
		}
	}

	// --- the positive case: a fully verified quarterly filing ---------------
	q3, ok := byDate["2025-11-10"]
	if !ok {
		t.Fatal("no event on 2025-11-10")
	}
	if q3.Highlights == nil {
		t.Fatal("2025-11-10: no highlights")
	}
	if q3.Highlights.Source != "financials" {
		t.Errorf("2025-11-10: source = %q, want financials", q3.Highlights.Source)
	}
	want := map[string]string{
		"Total revenues":            "$47.0M (+12.6% YoY)",
		"Basic EPS":                 "$0.09 (+28.6% YoY)",
		"Net income":                "$5.3M (+37.1% YoY)",
		"Cash and cash equivalents": "$72.0M (vs 2024-12-31: $78.4M)",
	}
	got := map[string]string{}
	for _, m := range q3.Highlights.Metrics {
		got[m.Label] = m.Value
	}
	for label, w := range want {
		if got[label] != w {
			t.Errorf("2025-11-10 %q = %q, want %q", label, got[label], w)
		}
	}

	// --- the negative case: the mis-tagged filing must withhold -------------
	q1, ok := byDate["2024-05-08"]
	if !ok {
		t.Fatal("no event on 2024-05-08")
	}
	for _, m := range highlightMetrics(q1) {
		if m.Label == "Total revenues" {
			t.Errorf("2024-05-08 rendered a withheld revenue metric: %q", m.Value)
		}
		if m.Value == "$37.7K" || m.Value == "$37,736" {
			t.Errorf("2024-05-08 rendered the mis-tagged value: %q", m.Value)
		}
	}

	// --- a non-financial filing keeps the existing empty state --------------
	for _, e := range events {
		if e.Category == "governance" && e.Highlights != nil {
			t.Errorf("governance event %s has highlights: %+v", e.FilingDate, e.Highlights)
		}
	}

	t.Logf("2025-11-10 metrics: %+v", q3.Highlights.Metrics)
	t.Logf("2024-05-08 metrics: %+v", highlightMetrics(q1))
}

func highlightMetrics(e Event) []HighlightMetric {
	if e.Highlights == nil {
		return nil
	}
	return e.Highlights.Metrics
}
