package companyview

import "testing"

// The 18 distinct summary strings across the 66 quarterly_results /
// annual_report filings in fileDB/companies/0001567529 (measured 2026-08-25),
// not a re-sample. Four of them carry a percentage; none carries a dollar
// figure, which is why BuildHighlights only ever extracts a growth percentage.
func TestBuildHighlightsRealCorpusSummaries(t *testing.T) {
	tests := []struct {
		name      string
		category  string
		summary   string
		wantLabel string
		wantValue string // "" means expect nil
	}{
		// --- no percentage present ---
		{"bare form label", "quarterly_results", "6-K", "", ""},
		{"exhibit 99.1", "quarterly_results", "EXHIBIT 99.1", "", ""},
		{"exhibit typo", "quarterly_results", "EXHIBT 99.1", "", ""},
		{"exhibit 1.1", "quarterly_results", "EXHIBIT 1.1", "", ""},
		{"annual report prose", "annual_report", "Annual report with audited financials and MD&A", "", ""},
		{"fourth quarter 2019", "quarterly_results",
			"KAMADA REPORTS FINANCIAL RESULTS FOR FOURTH QUARTER AND FISCAL YEAR 2019", "", ""},
		{"q1 2026 guidance", "quarterly_results",
			"KAMADA REPORTS FIRST QUARTER 2026 FINANCIAL RESULTS AND AFFIRMS 2026 ANNUAL GUIDANCE; EXPECTING SIGNIFICANTLY STRONGER REMAINDER OF THE YEAR", "", ""},
		// "DOUBLE-DIGIT ... GROWTH" has no digit, so nothing is fabricated.
		{"double-digit growth 2024", "quarterly_results",
			"KAMADA REPORTS RECORD TOP AND BOTTOM LINE 2024 FINANCIAL RESULTS AND AFFIRMS 2025 GUIDANCE REPRESENTING DOUBLE-DIGIT PROFITABLE GROWTH", "", ""},
		{"double-digit organic growth 2025", "quarterly_results",
			"KAMADA REPORTS RECORD TOP AND BOTTOM-LINE 2025 FINANCIAL RESULTS AND AFFIRMS 2026 GUIDANCE REPRESENTING CONTINUED DOUBLE-DIGIT ORGANIC PROFITABLE GROWTH", "", ""},
		{"record-high h1 2026", "quarterly_results",
			"KAMADA REPORTS RECORD-HIGH FIRST HALF AND SECOND QUARTER 2026 FINANCIAL RESULTS, REPRESENTING DOUBLE-DIGIT PROFITABLE GROWTH AND THE STRONGEST IN KAMADA'S HISTORY", "", ""},
		{"q2 2023 reiterates", "quarterly_results",
			"KAMADA REPORTS STRONG SECOND QUARTER AND FIRST HALF 2023 FINANCIAL RESULTS; REITERATES 2023 REVENUE AND PROFITABILITY GUIDANCE", "", ""},
		{"q3 2022 transition", "quarterly_results",
			"KAMADA REPORTS STRONG THIRD QUARTER FINANCIAL RESULTS DEMONSTRATING SUCCESSFUL STRATEGIC TRANSITION AND REITERATES 2022 FINANCIAL GUIDANCE", "", ""},
		{"q1 2020 press release", "quarterly_results",
			"PRESS RELEASE: KAMADA REPORTS FIRST QUARTER 2020 FINANCIAL RESULTS AND HIGHLIGHTS RECENT CORPORATE PROGRESS", "", ""},
		{"q2 2020 press release", "quarterly_results",
			"PRESS RELEASE: KAMADA REPORTS SECOND QUARTER AND FIRST SIX MONTHS OF 2020 FINANCIAL RESULTS, RECENT CORPORATE ACHIEVEMENTS AND STRONG CASH POSITION", "", ""},

		// --- percentage present: "GROWTH OF n%" word order ---
		{"q1 2024 growth of 23%", "quarterly_results",
			"KAMADA REPORTS STRONG FIRST QUARTER 2024 FINANCIAL RESULTS WITH YEAR-OVER-YEAR TOP-LINE GROWTH OF 23% AND A 96% INCREASE IN PROFITABILITY; RAISES FULL YEAR 2024 GUIDANCE",
			"Growth", "23%"},
		{"q1 2025 growth of 17%", "quarterly_results",
			"KAMADA REPORTS STRONG FIRST QUARTER 2025 FINANCIAL RESULTS WITH YEAR OVER YEAR TOP LINE GROWTH OF 17% AND A 54% INCREASE IN PROFITABILITY",
			"Growth", "17%"},

		// --- percentage present: "n% ... GROWTH" word order ---
		{"h1 2025 11% growth", "quarterly_results",
			"KAMADA REPORTS STRONG SECOND QUARTER AND FIRST HALF 2025 FINANCIAL RESULTS WITH 11% YEAR-OVER-YEAR 6-MONTH TOP LINE GROWTH AND A 35% INCREASE IN PROFITABILITY",
			"Growth", "11%"},
		{"q3 2025 over 30% growth", "quarterly_results",
			"KAMADA REPORTS STRONG THIRD QUARTER AND NINE MONTH 2025 FINANCIAL RESULTS WITH OVER 30% YEAR-OVER-YEAR PROFITABILITY GROWTH",
			"Growth", "30%"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := BuildHighlights(tt.category, tt.summary, nil)
			if tt.wantValue == "" {
				if got != nil {
					t.Fatalf("got %+v, want nil (nothing may be fabricated)", got.Metrics)
				}
				return
			}
			if got == nil {
				t.Fatalf("got nil, want %s %s", tt.wantLabel, tt.wantValue)
			}
			if len(got.Metrics) != 1 {
				t.Fatalf("got %d metrics, want exactly 1", len(got.Metrics))
			}
			if got.Metrics[0].Label != tt.wantLabel || got.Metrics[0].Value != tt.wantValue {
				t.Errorf("got {%s %s}, want {%s %s}",
					got.Metrics[0].Label, got.Metrics[0].Value, tt.wantLabel, tt.wantValue)
			}
			if got.Source != "summary_parse" {
				t.Errorf("source = %q, want summary_parse", got.Source)
			}
		})
	}
}

func TestBuildHighlightsCategoryGate(t *testing.T) {
	withPct := "REPORTS STRONG RESULTS WITH TOP-LINE GROWTH OF 23% YEAR OVER YEAR"
	// Only the two financial-results categories are eligible, even when the
	// summary would otherwise match.
	for _, cat := range []string{
		"current_report", "business_deal", "governance", "insider_trade",
		"unknown", "regulatory_clinical", "corporate_action", "",
	} {
		if got := BuildHighlights(cat, withPct, nil); got != nil {
			t.Errorf("category %q returned %+v, want nil", cat, got.Metrics)
		}
	}
	for _, cat := range []string{"quarterly_results", "annual_report"} {
		if got := BuildHighlights(cat, withPct, nil); got == nil {
			t.Errorf("category %q returned nil, want a metric", cat)
		}
	}
}

func TestBuildHighlightsEmptySummary(t *testing.T) {
	if got := BuildHighlights("quarterly_results", "", nil); got != nil {
		t.Errorf("empty summary returned %+v, want nil", got)
	}
}

// The leftmost match wins when a headline carries several percentages — the
// documented trade-off (LLD §6): usually, but not always, the top-line figure.
func TestBuildHighlightsPicksLeftmostMatch(t *testing.T) {
	s := "RESULTS WITH TOP-LINE GROWTH OF 23% AND A 96% INCREASE IN PROFITABILITY"
	got := BuildHighlights("quarterly_results", s, nil)
	if got == nil || got.Metrics[0].Value != "23%" {
		t.Fatalf("got %+v, want the leftmost 23%%", got)
	}

	// Reverse order: the "n% ... word" form appears first here.
	s2 := "RESULTS WITH 11% YEAR-OVER-YEAR GROWTH AND A 35% INCREASE IN PROFITABILITY"
	got2 := BuildHighlights("quarterly_results", s2, nil)
	if got2 == nil || got2.Metrics[0].Value != "11%" {
		t.Fatalf("got %+v, want the leftmost 11%%", got2)
	}
}

func TestBuildHighlightsDeclineLabel(t *testing.T) {
	for _, s := range []string{
		"RESULTS SHOWING A 12% DECREASE IN REVENUE",
		"RESULTS SHOWING A DECREASE OF 12% IN REVENUE",
	} {
		got := BuildHighlights("quarterly_results", s, nil)
		if got == nil {
			t.Fatalf("summary %q returned nil", s)
		}
		if got.Metrics[0].Label != "Decline" || got.Metrics[0].Value != "12%" {
			t.Errorf("summary %q gave {%s %s}, want {Decline 12%%}",
				s, got.Metrics[0].Label, got.Metrics[0].Value)
		}
	}
}
