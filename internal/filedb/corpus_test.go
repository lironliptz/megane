//go:build corpus

// Package-level regression net for design decision D1 (disk is the source of
// truth, submissions.json is identity-only).
//
// Excluded from `make test` because it reads the real 645 MB fileDB/ tree.
// Run with:
//
//	go test -tags corpus ./internal/filedb/
//
// Every number below was measured from the corpus on 2026-08-25 and is recorded
// in the HLD's data audit. If someone reverts the aggregation to the
// submissions.json index, these fail loudly: the index claims 504 filings from
// 2013-01-24 with 321 6-K and 13 20-F.
package filedb

import (
	"context"
	"os"
	"testing"
)

const (
	corpusRoot = "../../fileDB"
	kamadaCIK  = "0001567529"
)

func skipIfNoCorpus(t *testing.T) {
	t.Helper()
	if _, err := os.Stat(corpusRoot + "/companies/" + kamadaCIK); err != nil {
		t.Skipf("real corpus not present at %s: %v", corpusRoot, err)
	}
}

func TestCorpusKamadaCoverage(t *testing.T) {
	skipIfNoCorpus(t)
	s := NewFileDBStore(corpusRoot, Options{})
	d, err := s.GetCompany(context.Background(), kamadaCIK)
	if err != nil {
		t.Fatalf("GetCompany: %v", err)
	}

	if d.Identity.Name != "KAMADA LTD" {
		t.Errorf("name = %q, want KAMADA LTD", d.Identity.Name)
	}
	if d.Coverage.TotalFilings != 381 {
		t.Errorf("totalFilings = %d, want 381 (disk); 504 means the index leaked in", d.Coverage.TotalFilings)
	}
	if d.Coverage.EarliestFilingDate != "2016-01-06" {
		t.Errorf("earliest = %q, want 2016-01-06; 2013-01-24 means the index leaked in", d.Coverage.EarliestFilingDate)
	}
	if d.Coverage.LatestFilingDate != "2026-08-20" {
		t.Errorf("latest = %q, want 2026-08-20", d.Coverage.LatestFilingDate)
	}
	if d.Coverage.IndexedNotOnDisk != 123 {
		t.Errorf("indexedNotOnDisk = %d, want 123", d.Coverage.IndexedNotOnDisk)
	}
	if len(d.Coverage.YearsOnDisk) != 11 {
		t.Errorf("yearsOnDisk = %v, want 11 years (2016-2026)", d.Coverage.YearsOnDisk)
	}
}

func TestCorpusKamadaBreakdowns(t *testing.T) {
	skipIfNoCorpus(t)
	s := NewFileDBStore(corpusRoot, Options{})
	d, err := s.GetCompany(context.Background(), kamadaCIK)
	if err != nil {
		t.Fatalf("GetCompany: %v", err)
	}

	wantForm := map[string]int{"6-K": 242, "20-F": 11, "SC 13G/A": 34, "4": 26, "3": 20}
	for form, want := range wantForm {
		if got := d.FilingsSummary.ByForm[form]; got != want {
			t.Errorf("byForm[%q] = %d, want %d", form, got, want)
		}
	}
	if len(d.FilingsSummary.ByForm) != 19 {
		t.Errorf("distinct forms = %d, want 19", len(d.FilingsSummary.ByForm))
	}

	wantTier := map[string]int{"MAJOR": 97, "MODERATE": 117, "MINOR": 163, "ROUTINE": 4}
	if len(d.FilingsSummary.ByTier) != len(wantTier) {
		t.Errorf("byTier = %v, want 4 labels", d.FilingsSummary.ByTier)
	}
	for label, want := range wantTier {
		if got := d.FilingsSummary.ByTier[label]; got != want {
			t.Errorf("byTier[%q] = %d, want %d", label, got, want)
		}
	}

	if got := d.FilingsSummary.ByCategory["unknown"]; got != 78 {
		t.Errorf("byCategory[unknown] = %d, want 78", got)
	}
	if got := d.FilingsSummary.ByCategory["current_report"]; got != 80 {
		t.Errorf("byCategory[current_report] = %d, want 80", got)
	}
	if d.FilingsSummary.HasFinancialsCount != 66 {
		t.Errorf("hasFinancialsCount = %d, want 66", d.FilingsSummary.HasFinancialsCount)
	}
}

func TestCorpusSearchFindsKamada(t *testing.T) {
	skipIfNoCorpus(t)
	s := NewFileDBStore(corpusRoot, Options{})
	for _, q := range []string{"Kamada", "kamada", "KMDA", "kmda", "1567529", "0001567529"} {
		got, err := s.SearchCompanies(context.Background(), q, 0)
		if err != nil {
			t.Fatalf("SearchCompanies(%q): %v", q, err)
		}
		if len(got) == 0 || got[0].CIK != kamadaCIK {
			t.Errorf("SearchCompanies(%q) = %v, want CIK %s first", q, got, kamadaCIK)
		}
	}
}

func TestCorpusFilingsPaginateWithoutLoadingAll(t *testing.T) {
	skipIfNoCorpus(t)
	s := NewFileDBStore(corpusRoot, Options{})
	p, err := s.ListFilings(context.Background(), kamadaCIK, FilingFilter{Limit: 25, Offset: 0})
	if err != nil {
		t.Fatalf("ListFilings: %v", err)
	}
	if len(p.Items) != 25 {
		t.Errorf("page size = %d, want 25", len(p.Items))
	}
	if p.Total != 381 {
		t.Errorf("total = %d, want 381", p.Total)
	}
}
