package filedb

import (
	"errors"
	"testing"
)

const fixtureCIK = "0000000001"

func TestScanCompanyFilingsSkipsNonDirectories(t *testing.T) {
	rows, err := ScanCompanyFilings(fixtureRoot(t), fixtureCIK)
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	// 4 accession folders exist; the one without filing.json is dropped.
	if len(rows) != 3 {
		t.Fatalf("got %d rows, want 3: %+v", len(rows), rows)
	}
	for _, r := range rows {
		if r.AccessionNumber == ".DS_Store" || r.AccessionNumber == "notayear" {
			t.Errorf("non-accession entry leaked into rows: %q", r.AccessionNumber)
		}
	}
}

func TestScanCompanyFilingsDropsRowWithoutFilingJSON(t *testing.T) {
	rows, err := ScanCompanyFilings(fixtureRoot(t), fixtureCIK)
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	for _, r := range rows {
		if r.AccessionNumber == "0000000001-26-000004" {
			t.Error("accession with meta.json but no filing.json must be dropped")
		}
	}
}

func TestScanCompanyFilingsToleratesMissingMetaJSON(t *testing.T) {
	rows, err := ScanCompanyFilings(fixtureRoot(t), fixtureCIK)
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	var found bool
	for _, r := range rows {
		if r.AccessionNumber == "0000000001-20-000002" {
			found = true
			if r.Form != "20-F" || r.FilingDate != "2020-05-01" {
				t.Errorf("filing.json fields lost: %+v", r)
			}
			if r.Category != CategoryUnknown {
				t.Errorf("category = %q, want %q", r.Category, CategoryUnknown)
			}
			if r.TierLabel != "" {
				t.Errorf("tierLabel = %q, want empty", r.TierLabel)
			}
		}
	}
	if !found {
		t.Error("row with missing meta.json must survive the scan")
	}
}

func TestScanCompanyFilingsNewestFirst(t *testing.T) {
	rows, err := ScanCompanyFilings(fixtureRoot(t), fixtureCIK)
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	for i := 1; i < len(rows); i++ {
		if rows[i-1].FilingDate < rows[i].FilingDate {
			t.Fatalf("rows not newest-first: %v", rows)
		}
	}
	if rows[0].FilingDate != "2026-08-20" {
		t.Errorf("first row = %q, want 2026-08-20", rows[0].FilingDate)
	}
}

func TestScanCompanyFilingsYearFromFolder(t *testing.T) {
	rows, err := ScanCompanyFilings(fixtureRoot(t), fixtureCIK)
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	for _, r := range rows {
		if r.Year < 2016 || r.Year > 2026 {
			t.Errorf("row %s has year %d", r.AccessionNumber, r.Year)
		}
	}
}

func TestScanCompanyFilingsUnknownCompany(t *testing.T) {
	_, err := ScanCompanyFilings(fixtureRoot(t), "0009999999")
	if !errors.Is(err, ErrCompanyNotFound) {
		t.Errorf("err = %v, want ErrCompanyNotFound", err)
	}
}
