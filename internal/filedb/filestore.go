package filedb

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// DefaultCacheTTL bounds how long a cached scan is reused. See the freshness
// note on filingCache for why this, not mtime, is the real backstop.
const DefaultCacheTTL = 5 * time.Minute

// Options configures FileDBStore.
type Options struct {
	// TTL is the cache lifetime. Zero means DefaultCacheTTL.
	TTL time.Duration
}

// FileDBStore is the Phase 1 CompanyStore, reading fileDB/companies/ directly.
//
// It holds two lazily-built caches (design decision D3):
//
//   - the company index, parsed from each submissions.json, which is what
//     search queries — so search stays O(companies), never O(filings);
//   - a per-company filing scan, shared by GetCompany and ListFilings so one
//     page load walks the tree once.
//
// Nothing is walked or stat-ed at construction: a missing fileDB directory must
// not block server start.
type FileDBStore struct {
	root string
	ttl  time.Duration

	mu        sync.RWMutex
	companies []SearchCandidate
	compBuilt time.Time
	filings   map[string]*filingCache

	buildMu  sync.Mutex
	building map[string]*sync.Mutex
}

// filingCache is one company's scanned filings plus its derived aggregates.
//
// Freshness is (age < ttl) AND (company dir mtime unchanged). Be clear about
// the limit: a directory's mtime only moves when its *direct* children change,
// so editing a meta.json three levels down does NOT invalidate this entry — the
// TTL is the real backstop. That is acceptable for a static local corpus and is
// exactly the constraint Phase 2's downloader removes with explicit
// invalidation.
type filingCache struct {
	rows     []FilingRow
	coverage Coverage
	summary  FilingsSummary
	identity Identity
	builtAt  time.Time
	dirMTime time.Time
}

// Compile-time check that the Phase 1 store satisfies the interface Phase 2
// will re-implement.
var _ CompanyStore = (*FileDBStore)(nil)

// NewFileDBStore constructs a store rooted at dir (the folder containing
// "companies/"). It performs no I/O.
func NewFileDBStore(root string, opts Options) *FileDBStore {
	ttl := opts.TTL
	if ttl <= 0 {
		ttl = DefaultCacheTTL
	}
	return &FileDBStore{
		root:     root,
		ttl:      ttl,
		filings:  map[string]*filingCache{},
		building: map[string]*sync.Mutex{},
	}
}

// ParseTTL parses a cache TTL from an env value, falling back to
// DefaultCacheTTL on empty or unparseable input.
func ParseTTL(s string) time.Duration {
	if s == "" {
		return DefaultCacheTTL
	}
	d, err := time.ParseDuration(s)
	if err != nil || d <= 0 {
		slog.Warn("filedb: bad cache TTL, using default", "value", s, "default", DefaultCacheTTL)
		return DefaultCacheTTL
	}
	return d
}

// ListCompanies returns every company in the corpus.
//
// It has no HTTP endpoint in Phase 1; it backs SearchCompanies and is exported
// for the Phase 2 importer.
func (s *FileDBStore) ListCompanies(ctx context.Context) ([]CompanySummary, error) {
	cands, err := s.companyIndex(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]CompanySummary, 0, len(cands))
	for _, c := range cands {
		out = append(out, c.CompanySummary)
	}
	return out, nil
}

// SearchCompanies ranks the cached company index against query.
func (s *FileDBStore) SearchCompanies(ctx context.Context, query string, limit int) ([]CompanySummary, error) {
	cands, err := s.companyIndex(ctx)
	if err != nil {
		return nil, err
	}
	out := RankCompanies(cands, query, limit)
	if out == nil {
		out = []CompanySummary{}
	}
	return out, nil
}

// GetCompany returns identity plus disk-derived coverage and inventory.
func (s *FileDBStore) GetCompany(ctx context.Context, cik string) (*CompanyDetail, error) {
	norm, err := NormalizeCIK(cik)
	if err != nil {
		return nil, err
	}
	fc, err := s.companyCache(ctx, norm)
	if err != nil {
		return nil, err
	}
	return &CompanyDetail{
		Identity:       fc.identity,
		Coverage:       fc.coverage,
		FilingsSummary: fc.summary,
	}, nil
}

// ListFilings returns one page of filings, newest first, plus the unpaginated
// total after filtering.
func (s *FileDBStore) ListFilings(ctx context.Context, cik string, f FilingFilter) (FilingPage, error) {
	norm, err := NormalizeCIK(cik)
	if err != nil {
		return FilingPage{}, err
	}
	fc, err := s.companyCache(ctx, norm)
	if err != nil {
		return FilingPage{}, err
	}

	filtered := make([]FilingRow, 0, len(fc.rows))
	for _, r := range fc.rows {
		if f.Year > 0 && r.Year != f.Year {
			continue
		}
		if f.Form != "" && !strings.EqualFold(r.Form, f.Form) {
			continue
		}
		filtered = append(filtered, r)
	}

	page := FilingPage{Items: []FilingRow{}, Total: len(filtered), Limit: f.Limit, Offset: f.Offset}
	if f.Offset < len(filtered) {
		end := f.Offset + f.Limit
		if f.Limit <= 0 || end > len(filtered) {
			end = len(filtered)
		}
		page.Items = append(page.Items, filtered[f.Offset:end]...)
	}
	return page, nil
}

// ---------------------------------------------------------------------------
// caches
// ---------------------------------------------------------------------------

func (s *FileDBStore) companyIndex(ctx context.Context) ([]SearchCandidate, error) {
	s.mu.RLock()
	if s.companies != nil && time.Since(s.compBuilt) < s.ttl {
		defer s.mu.RUnlock()
		return s.companies, nil
	}
	s.mu.RUnlock()

	if err := ctx.Err(); err != nil {
		return nil, err
	}

	cands, err := s.buildCompanyIndex()
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	s.companies, s.compBuilt = cands, time.Now()
	s.mu.Unlock()
	return cands, nil
}

func (s *FileDBStore) buildCompanyIndex() ([]SearchCandidate, error) {
	dir := filepath.Join(s.root, companiesDir)
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			// A missing corpus is an empty corpus, not a server error: search
			// returns nothing and detail 404s.
			slog.Warn("filedb: companies directory not found", "dir", dir)
			return []SearchCandidate{}, nil
		}
		return nil, err
	}

	cands := make([]SearchCandidate, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		cik, err := NormalizeCIK(e.Name())
		if err != nil {
			continue // not a CIK folder
		}
		sub, err := parseSubmissions(submissionsPath(s.root, cik))
		if err != nil {
			slog.Warn("filedb: skipping company without usable submissions.json", "cik", cik, "err", err)
			continue
		}
		cands = append(cands, SearchCandidate{
			CompanySummary: CompanySummary{
				CIK:       cik,
				Name:      sub.Name,
				Tickers:   nonNilStrings(sub.Tickers),
				Exchanges: nonNilStrings(sub.Exchanges),
			},
			FormerNames: sub.formerNameStrings(),
		})
	}
	return cands, nil
}

// companyCache returns a fresh per-company cache entry, building it under a
// per-CIK lock so N concurrent first requests do one walk, not N.
func (s *FileDBStore) companyCache(ctx context.Context, cik string) (*filingCache, error) {
	if fc := s.cachedIfFresh(cik); fc != nil {
		return fc, nil
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	lock := s.buildLock(cik)
	lock.Lock()
	defer lock.Unlock()

	// Another goroutine may have built it while we waited.
	if fc := s.cachedIfFresh(cik); fc != nil {
		return fc, nil
	}

	fc, err := s.buildCompanyCache(cik)
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	s.filings[cik] = fc
	s.mu.Unlock()
	return fc, nil
}

func (s *FileDBStore) cachedIfFresh(cik string) *filingCache {
	s.mu.RLock()
	fc, ok := s.filings[cik]
	s.mu.RUnlock()
	if !ok || time.Since(fc.builtAt) >= s.ttl {
		return nil
	}
	if mt, err := companyDirMTime(s.root, cik); err == nil && !mt.Equal(fc.dirMTime) {
		return nil
	}
	return fc
}

func (s *FileDBStore) buildLock(cik string) *sync.Mutex {
	s.buildMu.Lock()
	defer s.buildMu.Unlock()
	l, ok := s.building[cik]
	if !ok {
		l = &sync.Mutex{}
		s.building[cik] = l
	}
	return l
}

func (s *FileDBStore) buildCompanyCache(cik string) (*filingCache, error) {
	sub, err := parseSubmissions(submissionsPath(s.root, cik))
	if err != nil {
		return nil, err
	}
	rows, err := ScanCompanyFilings(s.root, cik)
	if err != nil {
		return nil, err
	}

	onDisk := make(map[string]struct{}, len(rows))
	for _, r := range rows {
		onDisk[r.AccessionNumber] = struct{}{}
	}
	indexedNotOnDisk := 0
	for acc := range sub.indexedAccessions() {
		if _, ok := onDisk[acc]; !ok {
			indexedNotOnDisk++
		}
	}

	cov, summary := Summarize(rows, indexedNotOnDisk)
	mtime, _ := companyDirMTime(s.root, cik)
	return &filingCache{
		rows:     rows,
		coverage: cov,
		summary:  summary,
		identity: sub.identity(cik),
		builtAt:  time.Now(),
		dirMTime: mtime,
	}, nil
}

func companyDirMTime(root, cik string) (time.Time, error) {
	fi, err := os.Stat(filepath.Join(root, companiesDir, cik))
	if err != nil {
		return time.Time{}, err
	}
	return fi.ModTime(), nil
}
