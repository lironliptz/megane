package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"path/filepath"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"megane/internal/companyview"
	"megane/internal/db"
	"megane/internal/filedb"
	"megane/internal/marketdata"
)

// timelineStore is a filedb.CompanyStore with a fixed company and filings.
type timelineStore struct {
	detail *filedb.CompanyDetail
	rows   []filedb.FilingRow
	err    error
}

func (s *timelineStore) ListCompanies(context.Context) ([]filedb.CompanySummary, error) {
	return nil, s.err
}
func (s *timelineStore) SearchCompanies(context.Context, string, int) ([]filedb.CompanySummary, error) {
	return nil, s.err
}
func (s *timelineStore) GetCompany(_ context.Context, _ string) (*filedb.CompanyDetail, error) {
	if s.err != nil {
		return nil, s.err
	}
	return s.detail, nil
}
func (s *timelineStore) ListFilings(_ context.Context, _ string, _ filedb.FilingFilter) (filedb.FilingPage, error) {
	if s.err != nil {
		return filedb.FilingPage{}, s.err
	}
	return filedb.FilingPage{Items: s.rows, Total: len(s.rows)}, nil
}

// failingProvider stands in for a provider that is down or blocked.
type failingProvider struct{ err error }

func (p *failingProvider) Name() string { return "failing" }
func (p *failingProvider) DailyBars(context.Context, string, time.Time, time.Time) (*marketdata.Quote, error) {
	return nil, p.err
}

func kamadaDetail(tickers []string) *filedb.CompanyDetail {
	return &filedb.CompanyDetail{
		Identity: filedb.Identity{CIK: "0001567529", Name: "KAMADA LTD", Tickers: tickers},
		Coverage: filedb.Coverage{EarliestFilingDate: "2016-01-06", LatestFilingDate: "2026-08-20", TotalFilings: 3},
	}
}

func timelineRows() []filedb.FilingRow {
	return []filedb.FilingRow{
		{AccessionNumber: "a3", FilingDate: "2026-08-20", Form: "6-K", Category: "quarterly_results", Tier: 1, TierLabel: "MAJOR"},
		{AccessionNumber: "a2", FilingDate: "2026-05-01", Form: "4", Category: "insider_trade", Tier: 3, TierLabel: "MINOR"},
		{AccessionNumber: "a1", FilingDate: "2020-01-01", Form: "20-F", Category: "annual_report", Tier: 1, TierLabel: "MAJOR"},
	}
}

func newTimelineRouter(t *testing.T, store filedb.CompanyStore, provider marketdata.PriceProvider) *gin.Engine {
	t.Helper()
	database, err := db.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatalf("db open: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })

	svc := companyview.NewService(store, database, provider, companyview.Config{FetchTimeout: time.Second})
	gin.SetMode(gin.TestMode)
	r := gin.New()
	h := &CompanyHandler{Store: store, Timeline: svc}
	r.GET("/api/companies/:cik/timeline", h.TimelineView)
	return r
}

func decodeTimeline(t *testing.T, raw []byte) struct {
	Data companyview.Timeline `json:"data"`
} {
	t.Helper()
	var body struct {
		Data companyview.Timeline `json:"data"`
	}
	if err := json.Unmarshal(raw, &body); err != nil {
		t.Fatalf("decoding %s: %v", raw, err)
	}
	return body
}

// The core requirement: price failures must not cost the user their events.
func TestTimelineServesEventsWhenProviderFails(t *testing.T) {
	store := &timelineStore{detail: kamadaDetail([]string{"KMDA"}), rows: timelineRows()}
	r := newTimelineRouter(t, store, &failingProvider{err: errors.New("provider exploded")})

	w := doGet(r, "/api/companies/0001567529/timeline?from=2016-01-06&to=2026-08-20")
	if w.Code != http.StatusOK {
		t.Fatalf("code = %d, want 200 — a provider failure is not a request failure", w.Code)
	}
	tl := decodeTimeline(t, w.Body.Bytes()).Data
	if len(tl.Events) != 3 {
		t.Errorf("events = %d, want 3 — events depend on nothing external", len(tl.Events))
	}
	if len(tl.Prices) != 0 {
		t.Errorf("prices = %d, want 0", len(tl.Prices))
	}
	if tl.Ticker != "KMDA" {
		t.Errorf("ticker = %q", tl.Ticker)
	}
}

func TestTimelineNoTickerYieldsNullCoverage(t *testing.T) {
	store := &timelineStore{detail: kamadaDetail(nil), rows: timelineRows()}
	r := newTimelineRouter(t, store, &failingProvider{err: marketdata.ErrSymbolNotFound})

	w := doGet(r, "/api/companies/0001567529/timeline")
	if w.Code != http.StatusOK {
		t.Fatalf("code = %d, want 200", w.Code)
	}
	tl := decodeTimeline(t, w.Body.Bytes()).Data
	if tl.Ticker != "" {
		t.Errorf("ticker = %q, want empty", tl.Ticker)
	}
	if tl.PriceCoverage != nil {
		t.Errorf("priceCoverage = %+v, want null so the UI can show a banner", tl.PriceCoverage)
	}
	if len(tl.Events) == 0 {
		t.Error("a tickerless company must still get its events")
	}
}

func TestTimelineWeightsComputedServerSide(t *testing.T) {
	store := &timelineStore{detail: kamadaDetail([]string{"KMDA"}), rows: timelineRows()}
	r := newTimelineRouter(t, store, &failingProvider{err: errors.New("down")})

	w := doGet(r, "/api/companies/0001567529/timeline?from=2016-01-06&to=2026-08-20")
	tl := decodeTimeline(t, w.Body.Bytes()).Data
	got := map[string]companyview.Weight{}
	for _, e := range tl.Events {
		got[e.AccessionNumber] = e.Weight
	}
	if got["a3"] != companyview.WeightMajor || got["a1"] != companyview.WeightMajor {
		t.Errorf("major events = %v", got)
	}
	if got["a2"] != companyview.WeightMinor {
		t.Errorf("insider_trade weight = %q, want minor", got["a2"])
	}
}

func TestTimelineFilters(t *testing.T) {
	store := &timelineStore{detail: kamadaDetail([]string{"KMDA"}), rows: timelineRows()}
	r := newTimelineRouter(t, store, &failingProvider{err: errors.New("down")})

	for _, tt := range []struct {
		filter string
		want   int
	}{
		{"all", 3}, {"major", 2}, {"financials", 2}, {"bogus", 3},
	} {
		w := doGet(r, "/api/companies/0001567529/timeline?from=2016-01-06&to=2026-08-20&filter="+tt.filter)
		tl := decodeTimeline(t, w.Body.Bytes()).Data
		if len(tl.Events) != tt.want {
			t.Errorf("filter %q returned %d events, want %d", tt.filter, len(tl.Events), tt.want)
		}
	}
}

func TestTimelineWindowClamping(t *testing.T) {
	store := &timelineStore{detail: kamadaDetail([]string{"KMDA"}), rows: timelineRows()}
	r := newTimelineRouter(t, store, &failingProvider{err: errors.New("down")})

	w := doGet(r, "/api/companies/0001567529/timeline?from=1990-01-01")
	tl := decodeTimeline(t, w.Body.Bytes()).Data
	if tl.Window.From != "2016-01-06" {
		t.Errorf("from = %q, want clamp to coverage 2016-01-06", tl.Window.From)
	}
	if tl.Window.To != "2026-08-20" {
		t.Errorf("to = %q, want coverage latest", tl.Window.To)
	}

	// Default window is 2 years back from the latest filing.
	w = doGet(r, "/api/companies/0001567529/timeline")
	tl = decodeTimeline(t, w.Body.Bytes()).Data
	if tl.Window.From != "2024-08-20" {
		t.Errorf("default from = %q, want 2024-08-20", tl.Window.From)
	}
}

func TestTimelineBadInput(t *testing.T) {
	store := &timelineStore{detail: kamadaDetail([]string{"KMDA"}), rows: timelineRows()}
	r := newTimelineRouter(t, store, &failingProvider{err: errors.New("down")})

	for _, path := range []string{
		"/api/companies/abc/timeline",
		"/api/companies/0001567529/timeline?from=not-a-date",
		"/api/companies/0001567529/timeline?to=2020-13-45",
		"/api/companies/0001567529/timeline?from=2021-01-01&to=2020-01-01",
	} {
		if w := doGet(r, path); w.Code != http.StatusBadRequest {
			t.Errorf("GET %s = %d, want 400", path, w.Code)
		}
	}
}

func TestTimelineUnknownCompanyIs404(t *testing.T) {
	store := &timelineStore{err: filedb.ErrCompanyNotFound}
	r := newTimelineRouter(t, store, &failingProvider{err: errors.New("down")})
	if w := doGet(r, "/api/companies/0001567529/timeline"); w.Code != http.StatusNotFound {
		t.Errorf("code = %d, want 404", w.Code)
	}
}

func TestTimelineNilServiceIs503(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	h := &CompanyHandler{Store: &timelineStore{detail: kamadaDetail(nil)}}
	r.GET("/api/companies/:cik/timeline", h.TimelineView)
	if w := doGet(r, "/api/companies/0001567529/timeline"); w.Code != http.StatusServiceUnavailable {
		t.Errorf("code = %d, want 503 (not a panic)", w.Code)
	}
}
