package companyfetch

import (
	"encoding/json"
	"os"
)

// SubmissionsRecent is the subset of SEC's submissions.json `filings.recent`
// block the fetch step's idempotency check needs. Verified field shapes
// against a real fixture (fileDB/companies/0001567529/submissions.json).
// Exported so a caller building its own dry-run survey (cmd/fetch-similar's
// plan.go) can share this shape rather than redeclaring it.
type SubmissionsRecent struct {
	AccessionNumber []string `json:"accessionNumber"`
	FilingDate      []string `json:"filingDate"`
	Form            []string `json:"form"`
	Size            []int64  `json:"size"`
	IsXBRL          []int    `json:"isXBRL"`
	IsInlineXBRL    []int    `json:"isInlineXBRL"`
}

// readSubmissionsRecent reads an on-disk submissions.json (already fetched by
// a prior run) and returns just its `filings.recent` block — used by the
// fetch step's idempotency check, which only needs accession/date pairs, not
// the full survey shape.
func readSubmissionsRecent(path string) (*SubmissionsRecent, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var doc struct {
		Filings struct {
			Recent SubmissionsRecent `json:"recent"`
		} `json:"filings"`
	}
	if err := json.Unmarshal(b, &doc); err != nil {
		return nil, err
	}
	return &doc.Filings.Recent, nil
}

func safeIndex(s []string, i int) string {
	if i < 0 || i >= len(s) {
		return ""
	}
	return s[i]
}
