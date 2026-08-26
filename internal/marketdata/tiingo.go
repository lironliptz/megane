package marketdata

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	tiingoBaseURL = "https://api.tiingo.com/tiingo/daily/"
	tiingoTimeout = 15 * time.Second
)

// errTiingoNoAPIKey is a local, non-retryable configuration error — it must
// never be mistaken for ErrRateLimited by the chunk walker (backfill.go),
// or a misconfigured fallback would retry forever instead of failing fast.
var errTiingoNoAPIKey = errors.New("marketdata: tiingo provider has no API key configured")

// TiingoProvider reads daily bars from Tiingo's EOD prices endpoint.
//
// Unverified live (per prompt_4_stock_data-lld.md §"Current state" item 8):
// the field mapping and auth header are pinned to Tiingo's published API
// docs, not to a live probe — this repo has no Tiingo API key. Structural
// tests (tiingo_test.go) cover URL/param construction and JSON mapping
// against a local fixture server; they cannot confirm Tiingo's real auth or
// rate-limit behavior.
type TiingoProvider struct {
	BaseURL string
	APIKey  string
	Client  *http.Client
}

// NewTiingoProvider returns a provider with sane defaults. client may be nil.
// apiKey may be empty — DailyBars then fails fast with errTiingoNoAPIKey
// instead of making a request Tiingo would reject anyway.
func NewTiingoProvider(apiKey string, client *http.Client) *TiingoProvider {
	if client == nil {
		client = &http.Client{Timeout: tiingoTimeout}
	}
	return &TiingoProvider{BaseURL: tiingoBaseURL, APIKey: apiKey, Client: client}
}

// Name is what lands in stock_prices.source.
func (p *TiingoProvider) Name() string { return "tiingo" }

// tiingoBar is one element of Tiingo's JSON array response. Date is a full
// timestamp string per Tiingo's docs (e.g. "2019-01-02T00:00:00.000Z"), not
// a bare date — only the date portion is used.
type tiingoBar struct {
	Date     string   `json:"date"`
	Open     float64  `json:"open"`
	High     float64  `json:"high"`
	Low      float64  `json:"low"`
	Close    float64  `json:"close"`
	Volume   int64    `json:"volume"`
	AdjClose *float64 `json:"adjClose"`
}

// tiingoErrorBody is Tiingo's documented error shape: {"detail": "..."}.
type tiingoErrorBody struct {
	Detail string `json:"detail"`
}

// DailyBars fetches [from, to] at daily resolution.
//
// FirstTradeDate is deliberately left unset: the /prices endpoint this LLD
// documents carries no listing-date metadata (that lives on a separate
// Tiingo overview endpoint, out of scope here). The chunk walker still
// terminates correctly via ErrNoDataForRange (see below) or via a
// Yahoo-discovered FirstTradeDate from an earlier chunk.
func (p *TiingoProvider) DailyBars(ctx context.Context, symbol string, from, to time.Time) (*Quote, error) {
	if strings.TrimSpace(symbol) == "" {
		return nil, ErrSymbolNotFound
	}
	if strings.TrimSpace(p.APIKey) == "" {
		return nil, errTiingoNoAPIKey
	}

	q := url.Values{}
	q.Set("startDate", from.Format("2006-01-02"))
	q.Set("endDate", to.Format("2006-01-02"))
	q.Set("format", "json")
	endpoint := p.BaseURL + url.PathEscape(symbol) + "/prices?" + q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("%w: building request: %v", ErrBadResponse, err)
	}
	req.Header.Set("Authorization", "Token "+p.APIKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := p.Client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("marketdata: tiingo request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusTooManyRequests {
		return nil, fmt.Errorf("%w: HTTP 429", ErrRateLimited)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseSize))
	if err != nil {
		return nil, fmt.Errorf("%w: reading body: %v", ErrBadResponse, err)
	}

	switch resp.StatusCode {
	case http.StatusOK:
		// fall through to parsing below
	case http.StatusNotFound:
		return nil, fmt.Errorf("%w: %s", ErrSymbolNotFound, tiingoDetail(body))
	case http.StatusUnauthorized, http.StatusForbidden:
		return nil, fmt.Errorf("marketdata: tiingo auth rejected (status %d): %s",
			resp.StatusCode, tiingoDetail(body))
	default:
		return nil, fmt.Errorf("marketdata: tiingo status %d: %s", resp.StatusCode, tiingoDetail(body))
	}

	var bars []tiingoBar
	if err := json.Unmarshal(body, &bars); err != nil {
		return nil, fmt.Errorf("%w: %v (body starts %q)", ErrBadResponse, err, snippet(body))
	}

	// Documented Tiingo behavior for a range with no trading days (e.g.
	// entirely before listing): 200 + an empty JSON array. Unverified live —
	// this is the chunk walker's terminal condition when Yahoo's
	// firstTradeDate is unavailable and Tiingo is serving a chunk.
	if len(bars) == 0 {
		return nil, ErrNoDataForRange
	}

	out := &Quote{Symbol: strings.ToUpper(symbol), Currency: "USD", Bars: make([]Bar, 0, len(bars))}
	for _, b := range bars {
		date := b.Date
		if t, err := time.Parse(time.RFC3339, b.Date); err == nil {
			date = t.Format("2006-01-02")
		} else if len(date) >= 10 {
			date = date[:10] // "2019-01-02..." -> "2019-01-02"
		}
		bar := Bar{
			Date: date, Open: b.Open, High: b.High, Low: b.Low, Close: b.Close, Volume: b.Volume,
		}
		if b.AdjClose != nil {
			bar.AdjClose = *b.AdjClose
		}
		out.Bars = append(out.Bars, bar)
	}
	return out, nil
}

func tiingoDetail(body []byte) string {
	var e tiingoErrorBody
	if json.Unmarshal(body, &e) == nil && e.Detail != "" {
		return e.Detail
	}
	return snippet(body)
}
