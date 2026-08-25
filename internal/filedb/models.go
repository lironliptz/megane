// Package filedb provides a read model over the on-disk fileDB/ corpus of
// SEC-style company filings.
//
// Phase 1 (this package) reads directly from the filesystem. Phase 2 replaces
// FileDBStore with a SQLite-backed implementation of the same CompanyStore
// interface; handlers, JSON shapes, and the UI are unchanged by that swap.
package filedb

// CompanySummary is the row shape returned by search and list operations.
type CompanySummary struct {
	CIK       string   `json:"cik"`
	Name      string   `json:"name"`
	Tickers   []string `json:"tickers"`
	Exchanges []string `json:"exchanges"`
}

// Address is the trimmed headquarters location shown on the identity card.
type Address struct {
	City    string `json:"city,omitempty"`
	Country string `json:"country,omitempty"`
}

// Identity is company identity, sourced entirely from submissions.json.
// Nothing counted lives here — see Coverage and FilingsSummary, which are
// derived from the filings actually present on disk.
type Identity struct {
	CIK                  string   `json:"cik"`
	Name                 string   `json:"name"`
	Tickers              []string `json:"tickers"`
	Exchanges            []string `json:"exchanges"`
	SIC                  string   `json:"sic,omitempty"`
	SICDescription       string   `json:"sicDescription,omitempty"`
	StateOfIncorporation string   `json:"stateOfIncorporation,omitempty"`
	FiscalYearEnd        string   `json:"fiscalYearEnd,omitempty"`
	Category             string   `json:"category,omitempty"`
	HQ                   *Address `json:"hq,omitempty"`
	Phone                string   `json:"phone,omitempty"`
	FormerNames          []string `json:"formerNames,omitempty"`
}

// Coverage describes what the corpus actually holds for a company.
//
// IndexedNotOnDisk is the count of filings listed in submissions.json that have
// no accession folder on disk. It is surfaced rather than hidden so the UI can
// say "381 on file, 123 more known to SEC, not yet downloaded".
type Coverage struct {
	EarliestFilingDate string `json:"earliestFilingDate"`
	LatestFilingDate   string `json:"latestFilingDate"`
	YearsOnDisk        []int  `json:"yearsOnDisk"`
	TotalFilings       int    `json:"totalFilings"`
	IndexedNotOnDisk   int    `json:"indexedNotOnDisk"`
}

// FilingsSummary holds the inventory breakdowns.
//
// ByTier is keyed on the meta.json tierLabel and is deliberately open-ended:
// the corpus has four values today (MAJOR, MODERATE, MINOR, ROUTINE) and
// callers must not assume a fixed set.
type FilingsSummary struct {
	ByForm             map[string]int `json:"byForm"`
	ByCategory         map[string]int `json:"byCategory"`
	ByTier             map[string]int `json:"byTier"`
	HasFinancialsCount int            `json:"hasFinancialsCount"`
}

// CompanyDetail is the payload of GET /api/companies/:cik.
type CompanyDetail struct {
	Identity       Identity       `json:"identity"`
	Coverage       Coverage       `json:"coverage"`
	FilingsSummary FilingsSummary `json:"filingsSummary"`
}

// FilingRow is one accession folder, flattened from filing.json + meta.json.
type FilingRow struct {
	AccessionNumber string   `json:"accessionNumber"`
	FilingDate      string   `json:"filingDate"`
	ReportDate      string   `json:"reportDate,omitempty"`
	Form            string   `json:"form"`
	Year            int      `json:"year"`
	Category        string   `json:"category"`
	Tier            int      `json:"tier"`
	TierLabel       string   `json:"tierLabel,omitempty"`
	Summary         string   `json:"summary,omitempty"`
	Tags            []string `json:"tags,omitempty"`
	HasFinancials   bool     `json:"hasFinancials"`
	ExhibitCount    int      `json:"exhibitCount"`
}

// FilingFilter narrows a ListFilings call. The zero value means "no filter".
type FilingFilter struct {
	Year   int
	Form   string
	Limit  int
	Offset int
}

// FilingPage is one page of filings plus the unpaginated total, so the UI can
// render pagination controls without a second request.
type FilingPage struct {
	Items  []FilingRow `json:"items"`
	Total  int         `json:"total"`
	Limit  int         `json:"limit"`
	Offset int         `json:"offset"`
}
