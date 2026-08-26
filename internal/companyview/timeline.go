package companyview

import (
	"errors"
	"time"

	"megane/internal/filedb"
)

// DateLayout is the wire and storage format for all dates in this package.
const DateLayout = "2006-01-02"

// defaultWindowYears is the default timeline span when the client sends no range.
const defaultWindowYears = 2

// Event filters accepted by the timeline endpoint.
const (
	FilterAll        = "all"
	FilterMajor      = "major"
	FilterFinancials = "financials"
)

// ErrBadWindow means the requested window could not be interpreted or was inverted.
var ErrBadWindow = errors.New("companyview: invalid window")

// Window is an inclusive date range in DateLayout form.
type Window struct {
	From string `json:"from"`
	To   string `json:"to"`
}

// Event is one filing rendered as a timeline marker or an event card.
//
// Tier and TierLabel are carried for display only. Per D1 they are functions of
// Category and never participate in a filter predicate.
type Event struct {
	FilingDate      string   `json:"filingDate"`
	Form            string   `json:"form"`
	Category        string   `json:"category"`
	Tier            int      `json:"tier"`
	TierLabel       string   `json:"tierLabel,omitempty"`
	Summary         string   `json:"summary,omitempty"`
	Tags            []string `json:"tags,omitempty"`
	AccessionNumber string   `json:"accessionNumber"`
	Weight          Weight   `json:"weight"`
	Why             string   `json:"why,omitempty"`
	// Highlights is a best-effort financial callout parsed from Summary; nil
	// when nothing could be extracted. Additive and optional, like Why.
	Highlights *EventHighlight `json:"highlights,omitempty"`
}

// PricePoint is one trading day as shipped to the chart. OHLC is stored but not
// sent until a candlestick view needs it.
type PricePoint struct {
	Date     string  `json:"date"`
	Close    float64 `json:"close"`
	AdjClose float64 `json:"adjClose,omitempty"`
	Volume   int64   `json:"volume,omitempty"`
}

// PriceCoverage describes what price history is stored for a company. It is nil
// in the payload when no prices could be obtained, which is how the UI knows to
// render an events-only chart with a banner.
type PriceCoverage struct {
	Earliest string `json:"earliest"`
	Latest   string `json:"latest"`
	Source   string `json:"source,omitempty"`
	Status   string `json:"status,omitempty"`
	Note     string `json:"note,omitempty"`
}

// Timeline is the payload of GET /api/companies/:cik/timeline.
type Timeline struct {
	Window        Window         `json:"window"`
	Ticker        string         `json:"ticker"`
	Prices        []PricePoint   `json:"prices"`
	Events        []Event        `json:"events"`
	PriceCoverage *PriceCoverage `json:"priceCoverage"`
}

// ClampWindow resolves the requested window against what the corpus actually
// holds.
//
// The window is bounded by *filing* coverage, not price coverage: this page is
// about filings, and price history typically starts earlier (2013 vs 2016 for
// the sample company). An absent To defaults to the latest filing date; an
// absent From defaults to defaultWindowYears before To.
//
// now is injected so the default window is deterministic under test.
func ClampWindow(req Window, coverage filedb.Coverage, now time.Time) (Window, error) {
	lo, hi := coverage.EarliestFilingDate, coverage.LatestFilingDate
	if lo == "" || hi == "" {
		// No filings on disk: nothing to clamp against.
		return Window{}, ErrCompanyHasNoFilings
	}

	to := hi
	if req.To != "" {
		t, err := time.Parse(DateLayout, req.To)
		if err != nil {
			return Window{}, ErrBadWindow
		}
		to = t.Format(DateLayout)
	}

	from := ""
	if req.From != "" {
		f, err := time.Parse(DateLayout, req.From)
		if err != nil {
			return Window{}, ErrBadWindow
		}
		from = f.Format(DateLayout)
	} else {
		t, err := time.Parse(DateLayout, to)
		if err != nil {
			return Window{}, ErrBadWindow
		}
		from = t.AddDate(-defaultWindowYears, 0, 0).Format(DateLayout)
	}

	if from > to {
		return Window{}, ErrBadWindow
	}

	// Clamp into coverage. String comparison is correct for YYYY-MM-DD.
	if from < lo {
		from = lo
	}
	if to > hi {
		to = hi
	}
	if from > to {
		// The requested window lies entirely outside coverage.
		return Window{}, ErrBadWindow
	}
	return Window{From: from, To: to}, nil
}

// ErrCompanyHasNoFilings means the company exists but has no filings on disk,
// so no window can be derived.
var ErrCompanyHasNoFilings = errors.New("companyview: company has no filings")

// BuildEvents projects filing rows into timeline events within the window.
//
// filedb returns rows newest-first, and that order is preserved, so no re-sort
// happens here. Filtering is server-side so the default two-year window ships
// roughly 96 events rather than all 381.
func BuildEvents(rows []filedb.FilingRow, w Window, filter string) []Event {
	keep := predicateFor(filter)
	out := []Event{}
	for _, r := range rows {
		if r.FilingDate < w.From || r.FilingDate > w.To {
			continue
		}
		weight := WeightFor(r.Category)
		if !keep(r.Category, weight) {
			continue
		}
		out = append(out, Event{
			FilingDate:      r.FilingDate,
			Form:            r.Form,
			Category:        r.Category,
			Tier:            r.Tier,
			TierLabel:       r.TierLabel,
			Summary:         r.Summary,
			Tags:            r.Tags,
			AccessionNumber: r.AccessionNumber,
			Weight:          weight,
			Why:             WhyItMatters(r.Category),
			Highlights:      BuildHighlights(r.Category, r.Summary, r.Financials),
		})
	}
	return out
}

// predicateFor maps a filter name to its keep-predicate. An unrecognized filter
// falls back to "all" rather than erroring: a bad lane name should not break the
// page.
func predicateFor(filter string) func(category string, w Weight) bool {
	switch filter {
	case FilterMajor:
		return func(_ string, w Weight) bool { return w == WeightMajor }
	case FilterFinancials:
		return func(c string, _ Weight) bool { return IsEvent(c) }
	default:
		return func(string, Weight) bool { return true }
	}
}

// NormalizeFilter returns a known filter name, defaulting to FilterAll.
func NormalizeFilter(s string) string {
	switch s {
	case FilterMajor, FilterFinancials, FilterAll:
		return s
	default:
		return FilterAll
	}
}
