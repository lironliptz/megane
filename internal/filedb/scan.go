package filedb

import (
	"encoding/json"

	"log/slog"
	"megane/internal/edgar/financials"
	"os"
	"path/filepath"
	"sort"
	"strconv"
)

// companiesDir is the fixed subdirectory of the fileDB root holding one folder
// per CIK.
const companiesDir = "companies"

// filingJSON is the subset of filing.json this phase reads.
type filingJSON struct {
	AccessionNumber string `json:"accessionNumber"`
	FilingDate      string `json:"filingDate"`
	ReportDate      string `json:"reportDate"`
	Form            string `json:"form"`
}

// metaJSON is the subset of meta.json this phase reads.
type metaJSON struct {
	Tier      int      `json:"tier"`
	TierLabel string   `json:"tierLabel"`
	Category  string   `json:"category"`
	Summary   string   `json:"summary"`
	Tags      []string `json:"tags"`
	Signals   struct {
		HasFinancials bool `json:"hasFinancials"`
		ExhibitCount  int  `json:"exhibitCount"`
	} `json:"signals"`
}

// ScanCompanyFilings walks {root}/companies/{cik}/{year}/{accession}/ and
// flattens each accession folder into one FilingRow.
//
// This is the only function in the package that walks the filing tree; the
// Phase 2 SQLite importer is expected to call it rather than reimplement the
// walk. Rules, each of which the real corpus forced:
//
//   - Non-directory children are skipped: .DS_Store lives inside year folders.
//   - Children of the CIK folder that are not 4-digit years are skipped, which
//     is also how submissions.json itself is skipped.
//   - A missing or unparseable meta.json is tolerated: the row survives with
//     Category "unknown". (Measured presence is 100%, but ~20% of rows carry an
//     explicit "unknown" that must aggregate into the same bucket.)
//   - A missing or unparseable filing.json drops the row and logs at warn:
//     without form and filingDate there is nothing to render.
//   - index.json is not read in this phase.
//
// Rows are returned newest-first by (filingDate, accessionNumber), sorted once
// here so no consumer sorts per request.
//
// A missing company directory returns ErrCompanyNotFound.
func ScanCompanyFilings(root, cik string) ([]FilingRow, error) {
	companyDir := filepath.Join(root, companiesDir, cik)
	years, err := os.ReadDir(companyDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, ErrCompanyNotFound
		}
		return nil, err
	}

	rows := []FilingRow{}
	for _, ye := range years {
		if !ye.IsDir() {
			continue // submissions.json, .DS_Store
		}
		year, err := strconv.Atoi(ye.Name())
		if err != nil || year < 1000 || year > 9999 {
			continue // not a year folder
		}
		yearDir := filepath.Join(companyDir, ye.Name())
		accs, err := os.ReadDir(yearDir)
		if err != nil {
			slog.Warn("filedb: cannot read year dir", "dir", yearDir, "err", err)
			continue
		}
		for _, ae := range accs {
			if !ae.IsDir() {
				continue // .DS_Store
			}
			row, ok := readAccession(filepath.Join(yearDir, ae.Name()), year)
			if !ok {
				continue
			}
			rows = append(rows, row)
		}
	}

	sort.SliceStable(rows, func(i, j int) bool {
		if rows[i].FilingDate != rows[j].FilingDate {
			return rows[i].FilingDate > rows[j].FilingDate
		}
		return rows[i].AccessionNumber > rows[j].AccessionNumber
	})
	return rows, nil
}

// readAccession builds one FilingRow. ok is false when the row must be dropped.
func readAccession(dir string, year int) (FilingRow, bool) {
	var fj filingJSON
	if err := readJSONFile(filepath.Join(dir, "filing.json"), &fj); err != nil {
		slog.Warn("filedb: dropping accession without usable filing.json", "dir", dir, "err", err)
		return FilingRow{}, false
	}
	if fj.Form == "" || fj.FilingDate == "" {
		slog.Warn("filedb: dropping accession missing form or filingDate", "dir", dir)
		return FilingRow{}, false
	}

	acc := fj.AccessionNumber
	if acc == "" {
		acc = filepath.Base(dir)
	}

	// financials.json is optional and absent for most accessions. A read or
	// parse failure is tolerated exactly like a bad meta.json: warn, leave the
	// field nil, keep the row.
	fin, err := financials.Load(dir)
	if err != nil {
		slog.Warn("filedb: ignoring unreadable financials.json", "dir", dir, "err", err)
	}
	row := FilingRow{
		AccessionNumber: acc,
		FilingDate:      fj.FilingDate,
		ReportDate:      fj.ReportDate,
		Form:            fj.Form,
		Year:            year,
		Category:        CategoryUnknown,
		Financials:      fin,
	}

	var mj metaJSON
	if err := readJSONFile(filepath.Join(dir, "meta.json"), &mj); err != nil {
		// Tolerated: keep the filing.json fields, leave category "unknown".
		return row, true
	}
	if mj.Category != "" {
		row.Category = mj.Category
	}
	row.Tier = mj.Tier
	row.TierLabel = mj.TierLabel
	row.Summary = mj.Summary
	row.Tags = mj.Tags
	row.HasFinancials = mj.Signals.HasFinancials
	row.ExhibitCount = mj.Signals.ExhibitCount
	return row, true
}

func readJSONFile(path string, dst any) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, dst)
}
