package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"megane/internal/companyfetch"
)

// submissionsFilesEntry is one entry in submissions.json's `filings.files`
// (older-filing index pages SEC pushes out of `recent` past ~1000 rows).
type submissionsFilesEntry struct {
	Name string `json:"name"`
}

// submissionsDoc is the top-level shape of CIK{cik}.json. The `recent` shape
// is shared with the fetch step's idempotency check (companyfetch.Run) via
// companyfetch.SubmissionsRecent, so both readers agree on one field set.
type submissionsDoc struct {
	Name      string   `json:"name"`
	Tickers   []string `json:"tickers"`
	Exchanges []string `json:"exchanges"`
	SIC       string   `json:"sic"`
	SICDesc   string   `json:"sicDescription"`
	Filings   struct {
		Recent companyfetch.SubmissionsRecent `json:"recent"`
		Files  []submissionsFilesEntry        `json:"files"`
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
