package marketdata

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	yahooBaseURL = "https://query1.finance.yahoo.com/v8/finance/chart/"
	// The default Go User-Agent is a common block trigger.
	yahooUserAgent  = "Mozilla/5.0 (compatible; megane/1.0)"
	yahooTimeout    = 15 * time.Second
	wantGranularity = "1d"
	maxResponseSize = 32 << 20 // 32 MB; a full daily history is ~1 MB
)

// YahooProvider reads daily bars from Yahoo's v8 chart endpoint.
//
// Three behaviors here are load-bearing, each traceable to a measured failure:
//
//  1. It always sends period1/period2 and never `range=`. Measured:
//     `range=max&interval=1d` silently returns MONTHLY bars
//     (dataGranularity "1mo", 160 bars instead of 3329).
//  2. It asserts meta.dataGranularity == "1d" and discards the batch otherwise.
//  3. It validates the response shape before trusting it, so a 200 carrying
//     HTML surfaces as an error rather than as an empty result set.
type YahooProvider struct {
	BaseURL string
	Client  *http.Client
}

// NewYahooProvider returns a provider with sane defaults. client may be nil.
func NewYahooProvider(client *http.Client) *YahooProvider {
	if client == nil {
		client = &http.Client{Timeout: yahooTimeout}
	}
	return &YahooProvider{BaseURL: yahooBaseURL, Client: client}
}

// Name is what lands in stock_prices.source.
func (p *YahooProvider) Name() string { return "yahoo" }

// yahooResp is pinned to the measured response shape. Quote arrays are nullable
// (a halted day), hence the pointer element types.
type yahooResp struct {
	Chart struct {
		Result []struct {
			Meta struct {
				Currency        string `json:"currency"`
				Symbol          string `json:"symbol"`
				ExchangeName    string `json:"exchangeName"`
				DataGranularity string `json:"dataGranularity"`
				GMTOffset       int64  `json:"gmtoffset"`
			} `json:"meta"`
			Timestamp  []int64 `json:"timestamp"`
			Indicators struct {
				Quote []struct {
					Open   []*float64 `json:"open"`
					High   []*float64 `json:"high"`
					Low    []*float64 `json:"low"`
					Close  []*float64 `json:"close"`
					Volume []*int64   `json:"volume"`
				} `json:"quote"`
				AdjClose []struct {
					AdjClose []*float64 `json:"adjclose"`
				} `json:"adjclose"`
			} `json:"indicators"`
		} `json:"result"`
		Error *struct {
			Code        string `json:"code"`
			Description string `json:"description"`
		} `json:"error"`
	} `json:"chart"`
}

// DailyBars fetches [from, to] at daily resolution.
func (p *YahooProvider) DailyBars(ctx context.Context, symbol string, from, to time.Time) (*Quote, error) {
	if strings.TrimSpace(symbol) == "" {
		return nil, ErrSymbolNotFound
	}

	// period1/period2 only — never `range`, which silently downgrades to 1mo.
	q := url.Values{}
	q.Set("period1", fmt.Sprintf("%d", from.Unix()))
	q.Set("period2", fmt.Sprintf("%d", to.Unix()))
	q.Set("interval", wantGranularity)
	endpoint := p.BaseURL + url.PathEscape(symbol) + "?" + q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("%w: building request: %v", ErrBadResponse, err)
	}
	req.Header.Set("User-Agent", yahooUserAgent)
	req.Header.Set("Accept", "application/json")

	resp, err := p.Client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("marketdata: yahoo request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseSize))
	if err != nil {
		return nil, fmt.Errorf("%w: reading body: %v", ErrBadResponse, err)
	}

	// Guard 3a: a 200 carrying HTML is a provider failure, not an empty result.
	if ct := resp.Header.Get("Content-Type"); ct != "" && !strings.Contains(ct, "json") {
		return nil, fmt.Errorf("%w: content-type %q (body starts %q)",
			ErrBadResponse, ct, snippet(body))
	}

	var parsed yahooResp
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, fmt.Errorf("%w: %v (body starts %q)", ErrBadResponse, err, snippet(body))
	}

	if parsed.Chart.Error != nil {
		if resp.StatusCode == http.StatusNotFound ||
			strings.EqualFold(parsed.Chart.Error.Code, "Not Found") {
			return nil, fmt.Errorf("%w: %s", ErrSymbolNotFound, parsed.Chart.Error.Description)
		}
		return nil, fmt.Errorf("marketdata: yahoo error %s: %s",
			parsed.Chart.Error.Code, parsed.Chart.Error.Description)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("marketdata: yahoo status %d", resp.StatusCode)
	}

	// Guard 3b: shape.
	if len(parsed.Chart.Result) == 0 {
		return nil, fmt.Errorf("%w: no result object", ErrBadResponse)
	}
	r := parsed.Chart.Result[0]
	if len(r.Indicators.Quote) == 0 {
		return nil, fmt.Errorf("%w: no quote indicators", ErrBadResponse)
	}

	// Guard 2: granularity. Without this, a `range=`-style response would store
	// monthly closes and the chart would look plausible and be wrong.
	if r.Meta.DataGranularity != wantGranularity {
		return nil, fmt.Errorf("%w: got %q, want %q",
			ErrBadGranularity, r.Meta.DataGranularity, wantGranularity)
	}

	quote := r.Indicators.Quote[0]
	var adj []*float64
	if len(r.Indicators.AdjClose) > 0 {
		adj = r.Indicators.AdjClose[0].AdjClose
	}

	out := &Quote{
		Symbol:   r.Meta.Symbol,
		Currency: r.Meta.Currency,
		Exchange: r.Meta.ExchangeName,
		Bars:     make([]Bar, 0, len(r.Timestamp)),
	}
	for i, ts := range r.Timestamp {
		c := at(quote.Close, i)
		if c == nil {
			// Halted or missing day: skip rather than zero-fill. Measured zero
			// occurrences over a full history, so this is defensive.
			continue
		}
		bar := Bar{
			// Exchange-local calendar date. A no-op for US listings (09:30 EDT
			// is same-day in UTC) but correct for a future non-US listing.
			Date:   time.Unix(ts+r.Meta.GMTOffset, 0).UTC().Format("2006-01-02"),
			Close:  *c,
			Open:   deref(at(quote.Open, i)),
			High:   deref(at(quote.High, i)),
			Low:    deref(at(quote.Low, i)),
			Volume: derefInt(at(quote.Volume, i)),
		}
		if a := at(adj, i); a != nil {
			bar.AdjClose = *a
		}
		out.Bars = append(out.Bars, bar)
	}
	return out, nil
}

func at[T any](s []*T, i int) *T {
	if i < 0 || i >= len(s) {
		return nil
	}
	return s[i]
}

func deref(p *float64) float64 {
	if p == nil {
		return 0
	}
	return *p
}

func derefInt(p *int64) int64 {
	if p == nil {
		return 0
	}
	return *p
}

func snippet(b []byte) string {
	const n = 80
	s := strings.TrimSpace(string(b))
	if len(s) > n {
		return s[:n] + "…"
	}
	return s
}

// NewFromEnv selects a provider implementation by name, defaulting to Yahoo.
func NewFromEnv(name string) PriceProvider {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "", "yahoo":
		return NewYahooProvider(nil)
	default:
		// Unknown value: fall back rather than failing startup, and let the
		// caller log it.
		return NewYahooProvider(nil)
	}
}
