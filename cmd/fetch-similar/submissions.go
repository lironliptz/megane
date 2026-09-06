package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
)

// submissionsRecent is the subset of SEC's submissions.json `filings.recent`
// block this orchestrator reads. Verified field shapes against a real fixture
// (fileDB/companies/0001567529/submissions.json): isXBRL/isInlineXBRL are
// ints (0/1), size is an int (bytes), sic is a string.
type submissionsRecent struct {
	AccessionNumber []string `json:"accessionNumber"`
	FilingDate      []string `json:"filingDate"`
	Form            []string `json:"form"`
	Size            []int64  `json:"size"`
	IsXBRL          []int    `json:"isXBRL"`
	IsInlineXBRL    []int    `json:"isInlineXBRL"`
}

type submissionsFilesEntry struct {
	Name string `json:"name"`
}

// submissionsDoc is the top-level shape of CIK{cik}.json.
type submissionsDoc struct {
	Name      string   `json:"name"`
	Tickers   []string `json:"tickers"`
	Exchanges []string `json:"exchanges"`
	SIC       string   `json:"sic"`
	SICDesc   string   `json:"sicDescription"`
	Filings   struct {
		Recent submissionsRecent       `json:"recent"`
		Files  []submissionsFilesEntry `json:"files"`
	} `json:"filings"`
}

// fetchSubmissions performs the ONE request a dry run makes per peer —
// submissions.json only, no filing documents (D9).
func fetchSubmissions(ctx context.Context, client *http.Client, baseURL, cik, userAgent string) (*submissionsDoc, error) {
	url := fmt.Sprintf("%s/submissions/CIK%s.json", baseURL, cik)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", userAgent)

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch submissions: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return nil, fmt.Errorf("fetch submissions: status %d: %s", resp.StatusCode, string(body))
	}
	var doc submissionsDoc
	if err := json.NewDecoder(resp.Body).Decode(&doc); err != nil {
		return nil, fmt.Errorf("decode submissions: %w", err)
	}
	return &doc, nil
}

// readSubmissionsRecent reads an on-disk submissions.json (already fetched by
// a prior run) and returns just its `filings.recent` block — used by the
// fetch step's idempotency check (steps.go), which only needs accession/date
// pairs, not the full survey shape.
func readSubmissionsRecent(path string) (*submissionsRecent, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var doc struct {
		Filings struct {
			Recent submissionsRecent `json:"recent"`
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

func safeIndexInt64(s []int64, i int) int64 {
	if i < 0 || i >= len(s) {
		return 0
	}
	return s[i]
}

func safeIndexInt(s []int, i int) int {
	if i < 0 || i >= len(s) {
		return 0
	}
	return s[i]
}
