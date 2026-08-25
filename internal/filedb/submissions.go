package filedb

import (
	"log/slog"
	"os"
	"path/filepath"
)

// submissionsFile is the subset of the SEC submissions.json shape this phase
// reads. Per design decision D1 it supplies identity only: nothing counted or
// dated on the company page comes from here except IndexedNotOnDisk.
type submissionsFile struct {
	CIK                             string   `json:"cik"`
	Name                            string   `json:"name"`
	Tickers                         []string `json:"tickers"`
	Exchanges                       []string `json:"exchanges"`
	SIC                             string   `json:"sic"`
	SICDescription                  string   `json:"sicDescription"`
	Category                        string   `json:"category"`
	FiscalYearEnd                   string   `json:"fiscalYearEnd"`
	StateOfIncorporationDescription string   `json:"stateOfIncorporationDescription"`
	Phone                           string   `json:"phone"`
	Addresses                       struct {
		Business submissionsAddress `json:"business"`
		Mailing  submissionsAddress `json:"mailing"`
	} `json:"addresses"`
	// SEC ships formerNames as objects, not strings.
	FormerNames []struct {
		Name string `json:"name"`
	} `json:"formerNames"`
	Filings struct {
		Recent struct {
			AccessionNumber []string `json:"accessionNumber"`
		} `json:"recent"`
		Files []struct {
			Name string `json:"name"`
		} `json:"files"`
	} `json:"filings"`
}

type submissionsAddress struct {
	City                      string `json:"city"`
	StateOrCountryDescription string `json:"stateOrCountryDescription"`
}

func submissionsPath(root, cik string) string {
	return filepath.Join(root, companiesDir, cik, "submissions.json")
}

// parseSubmissions reads and decodes one company's submissions.json.
// A missing file is reported as ErrCompanyNotFound: a CIK folder without it is
// not a usable company.
func parseSubmissions(path string) (*submissionsFile, error) {
	var s submissionsFile
	if err := readJSONFile(path, &s); err != nil {
		if os.IsNotExist(err) {
			return nil, ErrCompanyNotFound
		}
		return nil, err
	}
	return &s, nil
}

// identity maps submissions.json onto the identity card payload.
//
// Empty optional fields are left empty so their `omitempty` tags drop them from
// JSON entirely — the UI omits absent fields rather than rendering "—".
func (s *submissionsFile) identity(cik string) Identity {
	id := Identity{
		CIK:                  cik,
		Name:                 s.Name,
		Tickers:              nonNilStrings(s.Tickers),
		Exchanges:            nonNilStrings(s.Exchanges),
		SIC:                  s.SIC,
		SICDescription:       s.SICDescription,
		StateOfIncorporation: s.StateOfIncorporationDescription, // description, not the "L3" code
		FiscalYearEnd:        s.FiscalYearEnd,
		Category:             s.Category,
		Phone:                s.Phone,
	}
	for _, fn := range s.FormerNames {
		if fn.Name != "" {
			id.FormerNames = append(id.FormerNames, fn.Name)
		}
	}
	addr := s.Addresses.Business
	if addr.City == "" && addr.StateOrCountryDescription == "" {
		addr = s.Addresses.Mailing
	}
	if addr.City != "" || addr.StateOrCountryDescription != "" {
		id.HQ = &Address{City: addr.City, Country: addr.StateOrCountryDescription}
	}
	return id
}

// formerNameStrings is the matchable form of formerNames for search.
func (s *submissionsFile) formerNameStrings() []string {
	out := make([]string, 0, len(s.FormerNames))
	for _, fn := range s.FormerNames {
		if fn.Name != "" {
			out = append(out, fn.Name)
		}
	}
	return out
}

// indexedAccessions is the set of accession numbers the SEC index claims for
// this company. Measured against the sample corpus this is a superset of what
// is on disk (504 indexed vs 381 folders), which is what IndexedNotOnDisk
// reports.
//
// filings.files (the SEC's overflow shards) is measured empty for the sample
// company and is deliberately not fetched; a non-empty value only affects the
// IndexedNotOnDisk count, so it warns rather than failing.
func (s *submissionsFile) indexedAccessions() map[string]struct{} {
	set := make(map[string]struct{}, len(s.Filings.Recent.AccessionNumber))
	for _, a := range s.Filings.Recent.AccessionNumber {
		if a != "" {
			set[a] = struct{}{}
		}
	}
	if len(s.Filings.Files) > 0 {
		slog.Warn("filedb: submissions.json has overflow shards which are not read",
			"cik", s.CIK, "shards", len(s.Filings.Files))
	}
	return set
}

func nonNilStrings(in []string) []string {
	if in == nil {
		return []string{}
	}
	return in
}
