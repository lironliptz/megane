package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"megane/internal/filedb"
)

// ErrNoUserAgent mirrors financials.Client's guard (prompt 9 D8): SEC
// rejects anonymous clients, so fail fast rather than let a request hang or
// get throttled with an opaque error.
var ErrNoUserAgent = errors.New("fetch-similar: SEC_EDGAR_USER_AGENT is required for SEC requests")

// tickerMap resolves an upper-cased ticker to its zero-padded 10-digit CIK.
type tickerMap map[string]string

// needsTickerMap reports whether any peer requires the map at all — a list
// with only company_id-resolved peers makes zero extra requests (HLD D4).
func needsTickerMap(peers []SimilarCompany) bool {
	for _, p := range peers {
		if (p.CompanyID == nil || strings.TrimSpace(*p.CompanyID) == "") && strings.TrimSpace(p.Ticker) != "" {
			return true
		}
	}
	return false
}

// loadTickerMap fetches SEC's company_tickers.json once per run and indexes
// it by ticker. The payload is a JSON object keyed by array index
// ({"0": {"cik_str": 320193, "ticker": "AAPL", ...}, ...}) — cik_str is an
// int and must be zero-padded to 10 digits to match fileDB's CIK convention.
func loadTickerMap(ctx context.Context, client *http.Client, baseURL, userAgent string) (tickerMap, error) {
	if strings.TrimSpace(userAgent) == "" {
		return nil, ErrNoUserAgent
	}
	url := baseURL + "/files/company_tickers.json"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", userAgent)

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch company_tickers.json: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return nil, fmt.Errorf("fetch company_tickers.json: status %d: %s", resp.StatusCode, string(body))
	}

	var raw map[string]struct {
		CIKStr int    `json:"cik_str"`
		Ticker string `json:"ticker"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, fmt.Errorf("decode company_tickers.json: %w", err)
	}

	out := make(tickerMap, len(raw))
	for _, row := range raw {
		if row.Ticker == "" {
			continue
		}
		out[strings.ToUpper(row.Ticker)] = fmt.Sprintf("%010d", row.CIKStr)
	}
	return out, nil
}

// resolveCIK implements HLD D4 / LLD §4.3: company_id wins when present;
// otherwise a non-empty ticker is looked up in tickers. Never guesses — an
// unresolvable peer comes back !ok with a note explaining why, so the caller
// can record status "skipped" rather than fabricate a CIK.
func resolveCIK(p SimilarCompany, tickers tickerMap) (cik, note string, ok bool) {
	if p.CompanyID != nil && strings.TrimSpace(*p.CompanyID) != "" {
		norm, err := filedb.NormalizeCIK(*p.CompanyID)
		if err != nil {
			return "", fmt.Sprintf("invalid company_id: %v", err), false
		}
		return norm, "", true
	}
	if strings.TrimSpace(p.Ticker) == "" {
		return "", "no company_id and no ticker", false
	}
	if tickers == nil {
		return "", "ticker not in SEC company_tickers", false
	}
	found, hit := tickers[strings.ToUpper(strings.TrimSpace(p.Ticker))]
	if !hit {
		return "", "ticker not in SEC company_tickers", false
	}
	return found, "", true
}
