package handlers

import (
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"megane/internal/companyview"
	"megane/internal/db"
	"megane/internal/filedb"
)

// Filings pagination bounds.
const (
	defaultFilingsLimit = 25
	maxFilingsLimit     = 100
)

// CompanyHandler serves the read-only company endpoints backed by a
// filedb.CompanyStore. It knows nothing about the filesystem: swapping in the
// Phase 2 SQLite store requires no change here.
type CompanyHandler struct {
	Store filedb.CompanyStore
	DB    *db.DB
	// Timeline is optional: when nil, the timeline endpoint reports 503 rather
	// than panicking, so the rest of the company view still works.
	Timeline *companyview.Service
}

// Search handles GET /api/companies/search?q=&limit=
func (h *CompanyHandler) Search(c *gin.Context) {
	q := strings.TrimSpace(c.Query("q"))
	if q == "" {
		c.JSON(http.StatusBadRequest, gin.H{"data": nil, "error": "query parameter q is required"})
		return
	}

	limit, ok := parseBoundedInt(c, "limit", filedb.MaxSearchResults, filedb.MaxSearchResults)
	if !ok {
		return
	}

	results, err := h.Store.SearchCompanies(c.Request.Context(), q, limit)
	if err != nil {
		respondStoreErr(c, err)
		return
	}
	if results == nil {
		results = []filedb.CompanySummary{}
	}
	c.JSON(http.StatusOK, gin.H{"data": results, "error": nil})
}

// Get handles GET /api/companies/:cik
func (h *CompanyHandler) Get(c *gin.Context) {
	cik, err := filedb.NormalizeCIK(c.Param("cik"))
	if err != nil {
		respondStoreErr(c, err)
		return
	}

	detail, err := h.Store.GetCompany(c.Request.Context(), cik)
	if err != nil {
		respondStoreErr(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": detail, "error": nil})
}

// Filings handles GET /api/companies/:cik/filings?year=&form=&limit=&offset=
func (h *CompanyHandler) Filings(c *gin.Context) {
	cik, err := filedb.NormalizeCIK(c.Param("cik"))
	if err != nil {
		respondStoreErr(c, err)
		return
	}

	limit, ok := parseBoundedInt(c, "limit", defaultFilingsLimit, maxFilingsLimit)
	if !ok {
		return
	}
	offset, ok := parseBoundedInt(c, "offset", 0, 0)
	if !ok {
		return
	}
	year, ok := parseBoundedInt(c, "year", 0, 0)
	if !ok {
		return
	}

	page, err := h.Store.ListFilings(c.Request.Context(), cik, filedb.FilingFilter{
		Year:   year,
		Form:   strings.TrimSpace(c.Query("form")),
		Limit:  limit,
		Offset: offset,
	})
	if err != nil {
		respondStoreErr(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": page, "error": nil})
}

// TimelineView handles GET /api/companies/:cik/timeline?from=&to=&filter=
//
// Price failures are not request failures: a company with no ticker, or a
// provider that is down, still gets its filing events with priceCoverage null.
func (h *CompanyHandler) TimelineView(c *gin.Context) {
	cik, err := filedb.NormalizeCIK(c.Param("cik"))
	if err != nil {
		respondStoreErr(c, err)
		return
	}
	if h.Timeline == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"data": nil, "error": "timeline service not configured"})
		return
	}

	from, ok := parseISODate(c, "from")
	if !ok {
		return
	}
	to, ok := parseISODate(c, "to")
	if !ok {
		return
	}

	tl, err := h.Timeline.Timeline(c.Request.Context(), cik,
		companyview.Window{From: from, To: to}, c.Query("filter"))
	if err != nil {
		respondStoreErr(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": tl, "error": nil})
}

// Consensus handles GET /api/companies/:cik/consensus.
func (h *CompanyHandler) Consensus(c *gin.Context) {
	if h.DB == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"data": nil, "error": "consensus storage not configured"})
		return
	}
	cik, err := filedb.NormalizeCIK(c.Param("cik"))
	if err != nil {
		respondStoreErr(c, err)
		return
	}

	coverage, err := h.DB.AnalystCoverage(c.Request.Context(), cik)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"data": nil, "error": "internal error"})
		return
	}
	source := c.Query("source")
	if source == "" {
		if coverage != nil && coverage.Provider != "" {
			source = coverage.Provider
		} else {
			source = "finnhub"
		}
	}
	periods, err := h.DB.ConsensusPeriods(c.Request.Context(), cik, source)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"data": nil, "error": "internal error"})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"data": gin.H{
			"coverage": coverage,
			"source":   source,
			"periods":  periods,
			"gaps":     []any{},
		},
		"error": nil,
	})
}

// parseISODate validates an optional YYYY-MM-DD query parameter. An absent value
// is valid and yields "". ok=false means the response is already written.
func parseISODate(c *gin.Context, name string) (string, bool) {
	raw := strings.TrimSpace(c.Query(name))
	if raw == "" {
		return "", true
	}
	if _, err := time.Parse(companyview.DateLayout, raw); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"data": nil, "error": "invalid " + name + " date, want YYYY-MM-DD"})
		return "", false
	}
	return raw, true
}

// parseBoundedInt reads a non-negative integer query parameter.
//
// An absent or empty value yields def. A non-numeric or negative value is a 400
// (and reports ok=false, meaning the response is already written). max <= 0
// means unbounded.
func parseBoundedInt(c *gin.Context, name string, def, max int) (int, bool) {
	raw := strings.TrimSpace(c.Query(name))
	if raw == "" {
		return def, true
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < 0 {
		c.JSON(http.StatusBadRequest, gin.H{"data": nil, "error": "invalid " + name})
		return 0, false
	}
	if max > 0 && n > max {
		n = max
	}
	return n, true
}

// respondStoreErr is the single place the store's sentinel errors become HTTP
// status codes. Internal error detail goes to slog, never to the client.
func respondStoreErr(c *gin.Context, err error) {
	switch {
	case errors.Is(err, filedb.ErrInvalidCIK):
		c.JSON(http.StatusBadRequest, gin.H{"data": nil, "error": "invalid cik"})
	case errors.Is(err, filedb.ErrCompanyNotFound):
		c.JSON(http.StatusNotFound, gin.H{"data": nil, "error": "company not found"})
	case errors.Is(err, companyview.ErrBadWindow):
		c.JSON(http.StatusBadRequest, gin.H{"data": nil, "error": "invalid date window"})
	case errors.Is(err, companyview.ErrCompanyHasNoFilings):
		c.JSON(http.StatusNotFound, gin.H{"data": nil, "error": "company has no filings on file"})
	default:
		slog.Error("company store error", "path", c.Request.URL.Path, "err", err)
		c.JSON(http.StatusInternalServerError, gin.H{"data": nil, "error": "internal error"})
	}
}
