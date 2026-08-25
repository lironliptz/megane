package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"megane/internal/filedb"
)

// stubStore records what the handler passed down and returns canned answers, so
// handler tests need no filesystem.
type stubStore struct {
	err        error
	summaries  []filedb.CompanySummary
	detail     *filedb.CompanyDetail
	page       filedb.FilingPage
	gotQuery   string
	gotLimit   int
	gotCIK     string
	gotFilter  filedb.FilingFilter
	searchCall int
}

func (s *stubStore) ListCompanies(context.Context) ([]filedb.CompanySummary, error) {
	return s.summaries, s.err
}

func (s *stubStore) SearchCompanies(_ context.Context, q string, limit int) ([]filedb.CompanySummary, error) {
	s.searchCall++
	s.gotQuery, s.gotLimit = q, limit
	return s.summaries, s.err
}

func (s *stubStore) GetCompany(_ context.Context, cik string) (*filedb.CompanyDetail, error) {
	s.gotCIK = cik
	if s.err != nil {
		return nil, s.err
	}
	return s.detail, nil
}

func (s *stubStore) ListFilings(_ context.Context, cik string, f filedb.FilingFilter) (filedb.FilingPage, error) {
	s.gotCIK, s.gotFilter = cik, f
	return s.page, s.err
}

func newCompanyTestRouter(store filedb.CompanyStore) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	h := &CompanyHandler{Store: store}
	g := r.Group("/api/companies")
	g.GET("/search", h.Search)
	g.GET("/:cik", h.Get)
	g.GET("/:cik/filings", h.Filings)
	return r
}

func doGet(r *gin.Engine, path string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	r.ServeHTTP(w, req)
	return w
}

func decodeBody(t *testing.T, w *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var body map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("decoding %q: %v", w.Body.String(), err)
	}
	return body
}

func TestSearchRequiresQuery(t *testing.T) {
	store := &stubStore{}
	r := newCompanyTestRouter(store)

	for _, path := range []string{"/api/companies/search", "/api/companies/search?q=", "/api/companies/search?q=%20%20"} {
		w := doGet(r, path)
		if w.Code != http.StatusBadRequest {
			t.Errorf("GET %s = %d, want 400", path, w.Code)
		}
	}
	if store.searchCall != 0 {
		t.Errorf("store was queried %d times for an empty q, want 0", store.searchCall)
	}
}

func TestSearchClampsLimit(t *testing.T) {
	store := &stubStore{summaries: []filedb.CompanySummary{}}
	r := newCompanyTestRouter(store)

	doGet(r, "/api/companies/search?q=kamada&limit=999")
	if store.gotLimit != filedb.MaxSearchResults {
		t.Errorf("limit 999 passed through as %d, want clamp to %d", store.gotLimit, filedb.MaxSearchResults)
	}

	doGet(r, "/api/companies/search?q=kamada")
	if store.gotLimit != filedb.MaxSearchResults {
		t.Errorf("default limit = %d, want %d", store.gotLimit, filedb.MaxSearchResults)
	}

	if w := doGet(r, "/api/companies/search?q=kamada&limit=abc"); w.Code != http.StatusBadRequest {
		t.Errorf("non-numeric limit = %d, want 400", w.Code)
	}
	if w := doGet(r, "/api/companies/search?q=kamada&limit=-1"); w.Code != http.StatusBadRequest {
		t.Errorf("negative limit = %d, want 400", w.Code)
	}
}

func TestSearchEmptyResultIsEmptyArrayNotNull(t *testing.T) {
	r := newCompanyTestRouter(&stubStore{summaries: nil})
	w := doGet(r, "/api/companies/search?q=nomatch")
	if w.Code != http.StatusOK {
		t.Fatalf("code = %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), `"data":[]`) {
		t.Errorf("body = %s, want data:[]", w.Body.String())
	}
}

func TestGetNormalizesCIK(t *testing.T) {
	store := &stubStore{detail: &filedb.CompanyDetail{}}
	r := newCompanyTestRouter(store)

	if w := doGet(r, "/api/companies/1567529"); w.Code != http.StatusOK {
		t.Fatalf("code = %d", w.Code)
	}
	if store.gotCIK != "0001567529" {
		t.Errorf("store received cik %q, want zero-padded 0001567529", store.gotCIK)
	}
}

func TestGetRejectsBadCIK(t *testing.T) {
	r := newCompanyTestRouter(&stubStore{detail: &filedb.CompanyDetail{}})
	for _, cik := range []string{"abc", "12345678901", "1567529abc"} {
		w := doGet(r, "/api/companies/"+cik)
		if w.Code != http.StatusBadRequest {
			t.Errorf("GET /api/companies/%s = %d, want 400", cik, w.Code)
		}
	}
}

func TestGetMapsNotFound(t *testing.T) {
	r := newCompanyTestRouter(&stubStore{err: filedb.ErrCompanyNotFound})
	w := doGet(r, "/api/companies/0001567529")
	if w.Code != http.StatusNotFound {
		t.Errorf("code = %d, want 404", w.Code)
	}
	if body := decodeBody(t, w); body["error"] != "company not found" {
		t.Errorf("error = %v", body["error"])
	}
}

func TestInternalErrorLeaksNoDetail(t *testing.T) {
	secret := "sql: connection to 10.0.0.5 refused"
	r := newCompanyTestRouter(&stubStore{err: errStub(secret)})
	w := doGet(r, "/api/companies/0001567529")
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("code = %d, want 500", w.Code)
	}
	if strings.Contains(w.Body.String(), "10.0.0.5") || strings.Contains(w.Body.String(), "sql:") {
		t.Errorf("500 body leaked internal detail: %s", w.Body.String())
	}
	if body := decodeBody(t, w); body["error"] != "internal error" {
		t.Errorf("error = %v, want fixed string", body["error"])
	}
}

func TestFilingsDefaultsAndFilters(t *testing.T) {
	store := &stubStore{page: filedb.FilingPage{Items: []filedb.FilingRow{}}}
	r := newCompanyTestRouter(store)

	doGet(r, "/api/companies/0001567529/filings")
	if store.gotFilter.Limit != defaultFilingsLimit || store.gotFilter.Offset != 0 {
		t.Errorf("defaults = %+v, want limit %d offset 0", store.gotFilter, defaultFilingsLimit)
	}

	doGet(r, "/api/companies/0001567529/filings?limit=500")
	if store.gotFilter.Limit != maxFilingsLimit {
		t.Errorf("limit = %d, want clamp to %d", store.gotFilter.Limit, maxFilingsLimit)
	}

	doGet(r, "/api/companies/0001567529/filings?year=2026&form=6-K&limit=10&offset=20")
	want := filedb.FilingFilter{Year: 2026, Form: "6-K", Limit: 10, Offset: 20}
	if store.gotFilter != want {
		t.Errorf("filter = %+v, want %+v", store.gotFilter, want)
	}

	for _, bad := range []string{"year=abc", "offset=-5", "limit=x"} {
		if w := doGet(r, "/api/companies/0001567529/filings?"+bad); w.Code != http.StatusBadRequest {
			t.Errorf("filings?%s = %d, want 400", bad, w.Code)
		}
	}
}

func TestFilingsMapsNotFound(t *testing.T) {
	r := newCompanyTestRouter(&stubStore{err: filedb.ErrCompanyNotFound})
	if w := doGet(r, "/api/companies/0001567529/filings"); w.Code != http.StatusNotFound {
		t.Errorf("code = %d, want 404", w.Code)
	}
}

type errStub string

func (e errStub) Error() string { return string(e) }
