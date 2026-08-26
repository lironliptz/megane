package companyview

import (
	"testing"

	"megane/internal/edgar/financials"
)

func f64(v float64) *float64 { return &v }

// verifiedFilings is a hand-built stand-in for the Q3 2025 extraction, so this
// test does not depend on the corpus being present.
func verifiedFilings() *financials.FilingFinancials {
	return &financials.FilingFinancials{
		Period: financials.Period{Label: "Q3 2025"},
		Statements: financials.StatementSets{
			Income: []financials.Line{
				{Key: financials.KeyTotalRevenues, Display: "Total revenues", ValueFmt: "$47.0M",
					Unit: financials.UnitUSD, YoYPct: f64(12.6), Confidence: financials.ConfidenceVerified},
				{Key: financials.KeyEPSBasic, Display: "Basic EPS", ValueFmt: "$0.09",
					Unit: financials.UnitUSDPerShare, YoYPct: f64(28.6), Confidence: financials.ConfidenceVerified},
				{Key: financials.KeyNetIncome, Display: "Net income", ValueFmt: "$5.3M",
					Unit: financials.UnitUSD, YoYPct: f64(37.1), Confidence: financials.ConfidenceVerified},
			},
			Balance: []financials.Line{
				{Key: financials.KeyCash, Display: "Cash and cash equivalents", ValueFmt: "$72.0M",
					Unit: financials.UnitUSD, PriorValue: f64(78435000), PriorLabel: "vs 2024-12-31",
					Confidence: financials.ConfidenceVerified},
			},
		},
	}
}

func TestHighlightsFromFinancialsOrderAndFormat(t *testing.T) {
	h := HighlightsFromFinancials(verifiedFilings())
	if h == nil {
		t.Fatal("nil highlights for a fully verified filing")
	}
	if h.Source != "financials" {
		t.Errorf("source = %q, want financials", h.Source)
	}
	want := []HighlightMetric{
		{"Total revenues", "$47.0M (+12.6% YoY)"},
		{"Basic EPS", "$0.09 (+28.6% YoY)"},
		{"Net income", "$5.3M (+37.1% YoY)"},
		{"Cash and cash equivalents", "$72.0M (vs 2024-12-31: $78.4M)"},
	}
	if len(h.Metrics) != len(want) {
		t.Fatalf("got %d metrics, want %d: %+v", len(h.Metrics), len(want), h.Metrics)
	}
	for i, w := range want {
		if h.Metrics[i] != w {
			t.Errorf("metric %d = %+v, want %+v", i, h.Metrics[i], w)
		}
	}
}

// TestSuspectLinesAreNotRendered is the most important test in this change: a
// metric the extractor could not verify must never reach the modal, even though
// it carries a plausible-looking value.
func TestSuspectLinesAreNotRendered(t *testing.T) {
	fin := &financials.FilingFinancials{
		Period: financials.Period{Label: "Q1 2024"},
		Statements: financials.StatementSets{
			Income: []financials.Line{
				// The mis-tagged filing: a real number, wrong by 1000x.
				{Key: financials.KeyTotalRevenues, Display: "Total revenues", ValueFmt: "$37.7K",
					Unit: financials.UnitUSD, Confidence: financials.ConfidenceSuspect},
				{Key: financials.KeyEPSBasic, Display: "Basic EPS", ValueFmt: "$0.04",
					Unit: financials.UnitUSDPerShare, Confidence: financials.ConfidenceVerified},
			},
		},
	}
	h := HighlightsFromFinancials(fin)
	if h == nil {
		t.Fatal("expected the verified EPS line to survive")
	}
	for _, m := range h.Metrics {
		if m.Label == "Total revenues" {
			t.Errorf("suspect revenue was rendered as %q", m.Value)
		}
	}
	if len(h.Metrics) != 1 || h.Metrics[0].Label != "Basic EPS" {
		t.Errorf("got %+v, want only the verified EPS metric", h.Metrics)
	}
}

func TestHighlightsNilWhenNothingPublishable(t *testing.T) {
	fin := &financials.FilingFinancials{
		Statements: financials.StatementSets{
			Income: []financials.Line{
				{Key: financials.KeyTotalRevenues, ValueFmt: "$37.7K", Confidence: financials.ConfidenceSuspect},
			},
		},
	}
	if h := HighlightsFromFinancials(fin); h != nil {
		t.Errorf("expected nil, got %+v", h)
	}
	if h := HighlightsFromFinancials(nil); h != nil {
		t.Errorf("expected nil for nil financials, got %+v", h)
	}
}

// TestBuildHighlightsPrefersFinancials: financials win over the summary regex,
// and the regex still runs when they are absent or fully withheld.
func TestBuildHighlightsPrefersFinancials(t *testing.T) {
	const summaryWithPct = "KAMADA REPORTS 23% REVENUE GROWTH"

	got := BuildHighlights("quarterly_results", summaryWithPct, verifiedFilings())
	if got == nil || got.Source != "financials" {
		t.Fatalf("expected financials source, got %+v", got)
	}

	got = BuildHighlights("quarterly_results", summaryWithPct, nil)
	if got == nil || got.Source != "summary_parse" {
		t.Fatalf("expected summary_parse fallback, got %+v", got)
	}

	// Fully-withheld financials must fall through to the summary, not blank out.
	withheld := &financials.FilingFinancials{
		Statements: financials.StatementSets{Income: []financials.Line{
			{Key: financials.KeyTotalRevenues, ValueFmt: "$37.7K", Confidence: financials.ConfidenceSuspect},
		}},
	}
	got = BuildHighlights("quarterly_results", summaryWithPct, withheld)
	if got == nil || got.Source != "summary_parse" {
		t.Fatalf("expected fallback to summary_parse, got %+v", got)
	}

	// Non-financial categories stay nil regardless.
	if got := BuildHighlights("governance", summaryWithPct, verifiedFilings()); got != nil {
		t.Errorf("governance should have no highlights, got %+v", got)
	}
}
