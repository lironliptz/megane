package filedb

import (
	"context"
	"errors"
	"testing"
	"time"
)

func newFixtureStore(t *testing.T) *FileDBStore {
	t.Helper()
	return NewFileDBStore(fixtureRoot(t), Options{})
}

func TestGetCompanyIdentity(t *testing.T) {
	s := newFixtureStore(t)
	d, err := s.GetCompany(context.Background(), fixtureCIK)
	if err != nil {
		t.Fatalf("GetCompany: %v", err)
	}
	if d.Identity.Name != "KAMADA TEST LTD" {
		t.Errorf("name = %q", d.Identity.Name)
	}
	if len(d.Identity.Tickers) != 1 || d.Identity.Tickers[0] != "KTST" {
		t.Errorf("tickers = %v", d.Identity.Tickers)
	}
	// The description, not the "L3" code.
	if d.Identity.StateOfIncorporation != "Israel" {
		t.Errorf("stateOfIncorporation = %q, want Israel", d.Identity.StateOfIncorporation)
	}
	// Business address wins over mailing.
	if d.Identity.HQ == nil || d.Identity.HQ.City != "REHOVOT" {
		t.Errorf("hq = %+v, want business address REHOVOT", d.Identity.HQ)
	}
	if len(d.Identity.FormerNames) != 0 {
		t.Errorf("formerNames = %v, want empty for this company", d.Identity.FormerNames)
	}
}

func TestGetCompanyCoverageIsDiskDerived(t *testing.T) {
	s := newFixtureStore(t)
	d, err := s.GetCompany(context.Background(), fixtureCIK)
	if err != nil {
		t.Fatalf("GetCompany: %v", err)
	}
	// Disk holds 3 usable filings; the SEC index claims 5.
	if d.Coverage.TotalFilings != 3 {
		t.Errorf("totalFilings = %d, want 3 (disk), not 5 (index)", d.Coverage.TotalFilings)
	}
	// Earliest must come from disk (2016), not from the index's phantom 2013 row.
	if d.Coverage.EarliestFilingDate != "2016-01-06" {
		t.Errorf("earliest = %q, want 2016-01-06 (disk), not 2013-01-24 (index)", d.Coverage.EarliestFilingDate)
	}
	if d.Coverage.LatestFilingDate != "2026-08-20" {
		t.Errorf("latest = %q, want 2026-08-20", d.Coverage.LatestFilingDate)
	}
	if d.Coverage.IndexedNotOnDisk != 2 {
		t.Errorf("indexedNotOnDisk = %d, want 2", d.Coverage.IndexedNotOnDisk)
	}
	if len(d.Coverage.YearsOnDisk) != 3 {
		t.Errorf("yearsOnDisk = %v, want 3 entries", d.Coverage.YearsOnDisk)
	}
}

func TestGetCompanySummary(t *testing.T) {
	s := newFixtureStore(t)
	d, err := s.GetCompany(context.Background(), fixtureCIK)
	if err != nil {
		t.Fatalf("GetCompany: %v", err)
	}
	if d.FilingsSummary.ByForm["6-K"] != 2 || d.FilingsSummary.ByForm["20-F"] != 1 {
		t.Errorf("byForm = %v", d.FilingsSummary.ByForm)
	}
	// The explicit "unknown" row and the missing-meta row share one bucket.
	if d.FilingsSummary.ByCategory[CategoryUnknown] != 2 {
		t.Errorf("byCategory[unknown] = %d, want 2", d.FilingsSummary.ByCategory[CategoryUnknown])
	}
}

func TestGetCompanyAcceptsUnpaddedCIK(t *testing.T) {
	s := newFixtureStore(t)
	d, err := s.GetCompany(context.Background(), "1")
	if err != nil {
		t.Fatalf("GetCompany(\"1\"): %v", err)
	}
	if d.Identity.CIK != fixtureCIK {
		t.Errorf("cik = %q, want %q", d.Identity.CIK, fixtureCIK)
	}
}

func TestGetCompanyErrors(t *testing.T) {
	s := newFixtureStore(t)
	if _, err := s.GetCompany(context.Background(), "9999999"); !errors.Is(err, ErrCompanyNotFound) {
		t.Errorf("unknown cik err = %v, want ErrCompanyNotFound", err)
	}
	if _, err := s.GetCompany(context.Background(), "../../etc"); !errors.Is(err, ErrInvalidCIK) {
		t.Errorf("traversal cik err = %v, want ErrInvalidCIK", err)
	}
}

func TestListFilingsPagination(t *testing.T) {
	s := newFixtureStore(t)
	ctx := context.Background()

	p, err := s.ListFilings(ctx, fixtureCIK, FilingFilter{Limit: 2, Offset: 0})
	if err != nil {
		t.Fatalf("ListFilings: %v", err)
	}
	if p.Total != 3 || len(p.Items) != 2 {
		t.Fatalf("page 1: total=%d items=%d, want 3/2", p.Total, len(p.Items))
	}
	if p.Items[0].FilingDate != "2026-08-20" {
		t.Errorf("page 1 not newest-first: %q", p.Items[0].FilingDate)
	}

	p2, err := s.ListFilings(ctx, fixtureCIK, FilingFilter{Limit: 2, Offset: 2})
	if err != nil {
		t.Fatalf("ListFilings page 2: %v", err)
	}
	if len(p2.Items) != 1 || p2.Total != 3 {
		t.Errorf("page 2: total=%d items=%d, want 3/1", p2.Total, len(p2.Items))
	}

	// Offset past the end is an empty page, not an error and not nil.
	p3, err := s.ListFilings(ctx, fixtureCIK, FilingFilter{Limit: 2, Offset: 99})
	if err != nil {
		t.Fatalf("ListFilings past end: %v", err)
	}
	if p3.Items == nil || len(p3.Items) != 0 || p3.Total != 3 {
		t.Errorf("past-end page = %+v", p3)
	}
}

func TestListFilingsFilters(t *testing.T) {
	s := newFixtureStore(t)
	ctx := context.Background()

	p, err := s.ListFilings(ctx, fixtureCIK, FilingFilter{Year: 2026, Limit: 10})
	if err != nil {
		t.Fatalf("year filter: %v", err)
	}
	if p.Total != 1 || p.Items[0].Year != 2026 {
		t.Errorf("year filter = %+v", p)
	}

	p, err = s.ListFilings(ctx, fixtureCIK, FilingFilter{Form: "6-k", Limit: 10})
	if err != nil {
		t.Fatalf("form filter: %v", err)
	}
	if p.Total != 2 {
		t.Errorf("form filter (case-insensitive) total = %d, want 2", p.Total)
	}
}

func TestSearchCompanies(t *testing.T) {
	s := newFixtureStore(t)
	ctx := context.Background()

	tests := []struct{ q, wantName string }{
		{"kamada", "KAMADA TEST LTD"},        // name prefix
		{"KTST", "KAMADA TEST LTD"},          // exact ticker
		{"1", "KAMADA TEST LTD"},             // cik prefix, zeros stripped
		{"apple", "APPLE TEST INC"},          // second company
		{"kamada systems", "ZETA TEST CORP"}, // former name
	}
	for _, tt := range tests {
		got, err := s.SearchCompanies(ctx, tt.q, 0)
		if err != nil {
			t.Fatalf("SearchCompanies(%q): %v", tt.q, err)
		}
		if len(got) == 0 || got[0].Name != tt.wantName {
			t.Errorf("SearchCompanies(%q) top = %v, want %q", tt.q, got, tt.wantName)
		}
	}

	// No match is an empty slice, never nil — the handler marshals it as [].
	got, err := s.SearchCompanies(ctx, "zzzznomatch", 0)
	if err != nil {
		t.Fatalf("no-match search: %v", err)
	}
	if got == nil || len(got) != 0 {
		t.Errorf("no-match search = %v, want empty non-nil slice", got)
	}
}

func TestListCompanies(t *testing.T) {
	s := newFixtureStore(t)
	got, err := s.ListCompanies(context.Background())
	if err != nil {
		t.Fatalf("ListCompanies: %v", err)
	}
	if len(got) != 3 {
		t.Errorf("got %d companies, want 3", len(got))
	}
}

func TestMissingCorpusIsEmptyNotError(t *testing.T) {
	s := NewFileDBStore(t.TempDir(), Options{})
	got, err := s.SearchCompanies(context.Background(), "anything", 0)
	if err != nil {
		t.Fatalf("missing corpus must not error: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("got %v, want no results", got)
	}
	if _, err := s.GetCompany(context.Background(), fixtureCIK); !errors.Is(err, ErrCompanyNotFound) {
		t.Errorf("missing corpus detail err = %v, want ErrCompanyNotFound", err)
	}
}

func TestCompanyCacheIsReused(t *testing.T) {
	root := fixtureRoot(t)
	s := NewFileDBStore(root, Options{TTL: time.Hour})
	ctx := context.Background()

	if _, err := s.GetCompany(ctx, fixtureCIK); err != nil {
		t.Fatalf("first GetCompany: %v", err)
	}
	s.mu.RLock()
	first := s.filings[fixtureCIK]
	s.mu.RUnlock()
	if first == nil {
		t.Fatal("cache entry not stored after first call")
	}

	if _, err := s.ListFilings(ctx, fixtureCIK, FilingFilter{Limit: 1}); err != nil {
		t.Fatalf("ListFilings: %v", err)
	}
	s.mu.RLock()
	second := s.filings[fixtureCIK]
	s.mu.RUnlock()
	if first != second {
		t.Error("GetCompany and ListFilings must share one cache entry, not rescan")
	}
}

func TestExpiredCacheRebuilds(t *testing.T) {
	s := NewFileDBStore(fixtureRoot(t), Options{TTL: time.Nanosecond})
	ctx := context.Background()

	if _, err := s.GetCompany(ctx, fixtureCIK); err != nil {
		t.Fatalf("first: %v", err)
	}
	s.mu.RLock()
	first := s.filings[fixtureCIK]
	s.mu.RUnlock()

	time.Sleep(time.Millisecond)
	if _, err := s.GetCompany(ctx, fixtureCIK); err != nil {
		t.Fatalf("second: %v", err)
	}
	s.mu.RLock()
	second := s.filings[fixtureCIK]
	s.mu.RUnlock()
	if first == second {
		t.Error("expired entry must be rebuilt")
	}
}

func TestParseTTL(t *testing.T) {
	if got := ParseTTL(""); got != DefaultCacheTTL {
		t.Errorf("ParseTTL(\"\") = %v", got)
	}
	if got := ParseTTL("nonsense"); got != DefaultCacheTTL {
		t.Errorf("ParseTTL(bad) = %v, want default", got)
	}
	if got := ParseTTL("-5m"); got != DefaultCacheTTL {
		t.Errorf("ParseTTL(negative) = %v, want default", got)
	}
	if got := ParseTTL("90s"); got != 90*time.Second {
		t.Errorf("ParseTTL(90s) = %v", got)
	}
}
