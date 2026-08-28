package companyview

import (
	"errors"
	"testing"
	"time"

	"megane/internal/edgar/financials"
	"megane/internal/filedb"
)

var kamadaCoverage = filedb.Coverage{
	EarliestFilingDate: "2016-01-06",
	LatestFilingDate:   "2026-08-20",
}

func mustTime(t *testing.T, s string) time.Time {
	t.Helper()
	v, err := time.Parse(DateLayout, s)
	if err != nil {
		t.Fatalf("parsing %q: %v", s, err)
	}
	return v
}

func TestClampWindow(t *testing.T) {
	now := mustTime(t, "2026-08-25")
	tests := []struct {
		name     string
		req      Window
		wantFrom string
		wantTo   string
		wantErr  bool
	}{
		{"empty request defaults to 2y back from latest filing", Window{}, "2024-08-20", "2026-08-20", false},
		{"explicit window inside coverage is preserved", Window{From: "2020-01-01", To: "2021-01-01"}, "2020-01-01", "2021-01-01", false},
		{"from before coverage clamps up", Window{From: "1990-01-01", To: "2020-01-01"}, "2016-01-06", "2020-01-01", false},
		{"to after coverage clamps down", Window{From: "2020-01-01", To: "2099-01-01"}, "2020-01-01", "2026-08-20", false},
		{"both outside coverage clamps to full span", Window{From: "1990-01-01", To: "2099-01-01"}, "2016-01-06", "2026-08-20", false},
		{"only to given, from derived 2y back", Window{To: "2020-06-01"}, "2018-06-01", "2020-06-01", false},
		{"only from given, to defaults to latest", Window{From: "2025-01-01"}, "2025-01-01", "2026-08-20", false},
		{"inverted window errors", Window{From: "2021-01-01", To: "2020-01-01"}, "", "", true},
		{"unparseable from errors", Window{From: "not-a-date"}, "", "", true},
		{"unparseable to errors", Window{To: "2020-13-45"}, "", "", true},
		{"window entirely before coverage errors", Window{From: "1990-01-01", To: "1995-01-01"}, "", "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ClampWindow(tt.req, kamadaCoverage, now)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("got %+v, want error", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got.From != tt.wantFrom || got.To != tt.wantTo {
				t.Errorf("got %s..%s, want %s..%s", got.From, got.To, tt.wantFrom, tt.wantTo)
			}
		})
	}
}

func TestClampWindowNoFilings(t *testing.T) {
	_, err := ClampWindow(Window{}, filedb.Coverage{}, time.Now())
	if !errors.Is(err, ErrCompanyHasNoFilings) {
		t.Errorf("err = %v, want ErrCompanyHasNoFilings", err)
	}
}

func testRows() []filedb.FilingRow {
	// Newest-first, as filedb returns them.
	return []filedb.FilingRow{
		{AccessionNumber: "a5", FilingDate: "2026-08-20", Form: "6-K", Category: "quarterly_results", Tier: 1, TierLabel: "MAJOR"},
		{AccessionNumber: "a4", FilingDate: "2026-05-01", Form: "6-K", Category: "current_report", Tier: 2, TierLabel: "MODERATE"},
		{AccessionNumber: "a3", FilingDate: "2025-11-11", Form: "4", Category: "insider_trade", Tier: 3, TierLabel: "MINOR"},
		{AccessionNumber: "a2", FilingDate: "2025-06-06", Form: "6-K", Category: "corporate_action", Tier: 2, TierLabel: "MODERATE"},
		{AccessionNumber: "a1", FilingDate: "2019-01-01", Form: "20-F", Category: "annual_report", Tier: 1, TierLabel: "MAJOR"},
	}
}

func TestBuildEventsWindowBounds(t *testing.T) {
	got := BuildEvents(testRows(), Window{From: "2025-01-01", To: "2026-06-01"}, FilterAll)
	if len(got) != 3 {
		t.Fatalf("got %d events, want 3 (a4, a3, a2)", len(got))
	}
	// Order preserved, newest first.
	if got[0].AccessionNumber != "a4" || got[2].AccessionNumber != "a2" {
		t.Errorf("order = %s..%s, want a4..a2", got[0].AccessionNumber, got[2].AccessionNumber)
	}
	// Inclusive bounds.
	all := BuildEvents(testRows(), Window{From: "2019-01-01", To: "2026-08-20"}, FilterAll)
	if len(all) != 5 {
		t.Errorf("inclusive bounds dropped rows: got %d, want 5", len(all))
	}
}

func TestBuildEventsFilters(t *testing.T) {
	w := Window{From: "2000-01-01", To: "2099-01-01"}
	tests := []struct {
		filter string
		want   []string
	}{
		{FilterAll, []string{"a5", "a4", "a3", "a2", "a1"}},
		{FilterMajor, []string{"a5", "a1"}},                    // quarterly_results, annual_report
		{FilterFinancials, []string{"a5", "a2", "a1"}},         // CORE incl. corporate_action
		{"bogus-lane", []string{"a5", "a4", "a3", "a2", "a1"}}, // unknown filter falls back to all
	}
	for _, tt := range tests {
		got := BuildEvents(testRows(), w, tt.filter)
		if len(got) != len(tt.want) {
			t.Fatalf("filter %q returned %d events, want %d", tt.filter, len(got), len(tt.want))
		}
		for i := range got {
			if got[i].AccessionNumber != tt.want[i] {
				t.Errorf("filter %q = %v, want %v", tt.filter, accs(got), tt.want)
				break
			}
		}
	}
}

func TestBuildEventsAttachesWeightAndWhy(t *testing.T) {
	got := BuildEvents(testRows(), Window{From: "2000-01-01", To: "2099-01-01"}, FilterAll)
	byAcc := map[string]Event{}
	for _, e := range got {
		byAcc[e.AccessionNumber] = e
	}
	if byAcc["a5"].Weight != WeightMajor {
		t.Errorf("quarterly_results weight = %q, want major", byAcc["a5"].Weight)
	}
	if byAcc["a3"].Weight != WeightMinor {
		t.Errorf("insider_trade weight = %q, want minor", byAcc["a3"].Weight)
	}
	if byAcc["a5"].Why == "" {
		t.Error("curated event must carry why text")
	}
	if byAcc["a3"].Why != "" {
		t.Error("non-event must not carry why text")
	}
}

func TestBuildEventsEmptyIsNonNil(t *testing.T) {
	got := BuildEvents(nil, Window{From: "2020-01-01", To: "2020-02-01"}, FilterAll)
	if got == nil {
		t.Error("empty result must be a non-nil slice so JSON renders []")
	}
}

func TestBuildEventsReportPeriod(t *testing.T) {
	rows := []filedb.FilingRow{{
		FilingDate: "2025-03-05", Form: "20-F", Category: "annual_report",
		AccessionNumber: "fy24",
		Financials: &financials.FilingFinancials{
			Period: financials.Period{
				Label: "FY 2024", Duration: "P1Y", EndDate: "2024-12-31", Focus: "FY",
			},
		},
	}, {
		FilingDate: "2025-11-10", Form: "6-K", Category: "quarterly_results",
		AccessionNumber: "q3",
		Financials: &financials.FilingFinancials{
			Period: financials.Period{
				Label: "Q3 2025", Duration: "P3M", EndDate: "2025-09-30", Focus: "Q3",
			},
		},
	}}
	got := BuildEvents(rows, Window{From: "2025-01-01", To: "2025-12-31"}, FilterAll)
	byAcc := map[string]Event{}
	for _, e := range got {
		byAcc[e.AccessionNumber] = e
	}
	fy := byAcc["fy24"].ReportPeriod
	if fy == nil || fy.Label != "FY 2024" || fy.Duration != "P1Y" || fy.EndDate != "2024-12-31" {
		t.Fatalf("annual reportPeriod = %+v, want FY 2024 P1Y", fy)
	}
	q := byAcc["q3"].ReportPeriod
	if q == nil || q.Label != "Q3 2025" || q.Duration != "P3M" || q.Focus != "Q3" {
		t.Fatalf("quarterly reportPeriod = %+v, want Q3 2025 P3M", q)
	}
}

func TestNormalizeFilter(t *testing.T) {
	for in, want := range map[string]string{
		"major": FilterMajor, "financials": FilterFinancials, "all": FilterAll,
		"": FilterAll, "nonsense": FilterAll,
	} {
		if got := NormalizeFilter(in); got != want {
			t.Errorf("NormalizeFilter(%q) = %q, want %q", in, got, want)
		}
	}
}

func accs(evs []Event) []string {
	out := make([]string, len(evs))
	for i, e := range evs {
		out[i] = e.AccessionNumber
	}
	return out
}
