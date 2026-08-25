package filedb

import "testing"

func rows() []FilingRow {
	return []FilingRow{
		{AccessionNumber: "a1", FilingDate: "2016-01-06", Year: 2016, Form: "6-K", Category: "current_report", TierLabel: "MINOR"},
		{AccessionNumber: "a2", FilingDate: "2020-05-01", Year: 2020, Form: "20-F", Category: "annual_report", TierLabel: "MAJOR", HasFinancials: true},
		{AccessionNumber: "a3", FilingDate: "2026-08-20", Year: 2026, Form: "6-K", Category: "unknown", TierLabel: "MODERATE"},
		{AccessionNumber: "a4", FilingDate: "2020-07-11", Year: 2020, Form: "6-K", Category: "", TierLabel: "ROUTINE", HasFinancials: true},
	}
}

func TestSummarizeCoverage(t *testing.T) {
	cov, _ := Summarize(rows(), 123)
	if cov.EarliestFilingDate != "2016-01-06" {
		t.Errorf("earliest = %q, want 2016-01-06", cov.EarliestFilingDate)
	}
	if cov.LatestFilingDate != "2026-08-20" {
		t.Errorf("latest = %q, want 2026-08-20", cov.LatestFilingDate)
	}
	if cov.TotalFilings != 4 {
		t.Errorf("total = %d, want 4", cov.TotalFilings)
	}
	if cov.IndexedNotOnDisk != 123 {
		t.Errorf("indexedNotOnDisk = %d, want 123", cov.IndexedNotOnDisk)
	}
	want := []int{2016, 2020, 2026}
	if len(cov.YearsOnDisk) != len(want) {
		t.Fatalf("years = %v, want %v", cov.YearsOnDisk, want)
	}
	for i := range want {
		if cov.YearsOnDisk[i] != want[i] {
			t.Fatalf("years = %v, want %v (sorted, distinct)", cov.YearsOnDisk, want)
		}
	}
}

func TestSummarizeBreakdowns(t *testing.T) {
	_, sum := Summarize(rows(), 0)
	if sum.ByForm["6-K"] != 3 || sum.ByForm["20-F"] != 1 {
		t.Errorf("byForm = %v", sum.ByForm)
	}
	// Empty category folds into the same bucket as an explicit "unknown".
	if sum.ByCategory[CategoryUnknown] != 2 {
		t.Errorf("byCategory[unknown] = %d, want 2 (explicit + empty)", sum.ByCategory[CategoryUnknown])
	}
	if sum.ByCategory["annual_report"] != 1 {
		t.Errorf("byCategory = %v", sum.ByCategory)
	}
	// Four tier labels must survive; nothing collapses to MAJOR/MINOR.
	if len(sum.ByTier) != 4 {
		t.Errorf("byTier = %v, want 4 distinct labels", sum.ByTier)
	}
	for _, label := range []string{"MAJOR", "MODERATE", "MINOR", "ROUTINE"} {
		if sum.ByTier[label] != 1 {
			t.Errorf("byTier[%s] = %d, want 1", label, sum.ByTier[label])
		}
	}
	if sum.HasFinancialsCount != 2 {
		t.Errorf("hasFinancialsCount = %d, want 2", sum.HasFinancialsCount)
	}
}

func TestSummarizeEmpty(t *testing.T) {
	cov, sum := Summarize(nil, 0)
	if cov.TotalFilings != 0 || cov.EarliestFilingDate != "" || cov.LatestFilingDate != "" {
		t.Errorf("empty coverage = %+v", cov)
	}
	// Non-nil so JSON renders {} / [], never null.
	if cov.YearsOnDisk == nil || sum.ByForm == nil || sum.ByCategory == nil || sum.ByTier == nil {
		t.Error("empty input must still produce non-nil maps and slices")
	}
}
