package consensus

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"megane/internal/edgar/financials"
)

func TestDerivePeriodEnd(t *testing.T) {
	cases := []struct {
		fye, year, period string
		wantEnd, wantDur  string
		ok                bool
	}{
		{"1231", "2025", "Q1", "2025-03-31", "P3M", true},
		{"1231", "2025", "Q4", "2025-12-31", "P3M", true},
		{"1231", "2025", "FY", "2025-12-31", "P1Y", true},
		{"0630", "2025", "Q1", "2024-09-30", "P3M", true},
		{"0630", "2025", "Q2", "2024-12-30", "P3M", true},
		{"", "2025", "Q1", "", "", false},
	}
	for _, tc := range cases {
		gotEnd, gotDur, ok := DerivePeriodEnd(tc.fye, tc.year, tc.period)
		if ok != tc.ok || gotEnd != tc.wantEnd || gotDur != tc.wantDur {
			t.Errorf("DerivePeriodEnd(%q,%q,%q) = (%q,%q,%v), want (%q,%q,%v)",
				tc.fye, tc.year, tc.period, gotEnd, gotDur, ok, tc.wantEnd, tc.wantDur, tc.ok)
		}
	}
}

func TestFYQ4Distinct(t *testing.T) {
	fyEnd, fyDur, okFY := DerivePeriodEnd("1231", "2025", "FY")
	q4End, q4Dur, okQ4 := DerivePeriodEnd("1231", "2025", "Q4")
	if !okFY || !okQ4 {
		t.Fatal("expected both derivations to succeed")
	}
	if fyEnd != q4End {
		t.Fatalf("expected same end date, got FY=%s Q4=%s", fyEnd, q4End)
	}
	if fyDur == q4Dur {
		t.Fatalf("durations must differ: FY=%s Q4=%s", fyDur, q4Dur)
	}
}

func TestDerivePeriodEndMatchesCorpus(t *testing.T) {
	root := filepath.Join("..", "..", "fileDB", "companies", "0001567529")
	if _, err := os.Stat(root); err != nil {
		t.Skip("fixture corpus not present")
	}
	matches := 0
	_ = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || d.Name() != financials.FileName {
			return nil
		}
		f, ferr := financials.Load(filepath.Dir(path))
		if ferr != nil || f == nil {
			return nil
		}
		if f.Period.Focus == "" || f.Period.EndDate == "" {
			return nil
		}
		parsedEnd, perr := time.Parse("2006-01-02", f.Period.EndDate)
		if perr != nil {
			return nil
		}
		year := parsedEnd.Format("2006")
		derivedEnd, dur, ok := DerivePeriodEnd("1231", year, f.Period.Focus)
		if !ok {
			return nil
		}
		if derivedEnd == f.Period.EndDate && dur == f.Period.Duration {
			matches++
		}
		return nil
	})
	if matches == 0 {
		t.Skip("no joinable financials found")
	}
	if matches < 10 {
		t.Fatalf("matches = %d, want at least 10", matches)
	}
}
