// Package marketdata fetches daily stock bars from an external provider.
//
// The interface exists so the provider choice is reversible. Stooq — the
// provider the original design preferred — was rejected on evidence: it answers
// HTTP 200 with a JavaScript proof-of-work browser challenge rather than CSV,
// and cannot be consumed from a Go client. See prompt_3_company_view-hld.md D3.
package marketdata

import (
	"context"
	"errors"
	"time"
)

// Bar is one trading day, with dates already normalized to the exchange's local
// calendar date.
type Bar struct {
	Date     string // YYYY-MM-DD, exchange-local
	Open     float64
	High     float64
	Low      float64
	Close    float64
	AdjClose float64
	Volume   int64
}

// Quote is a symbol's bars plus the metadata needed to store them.
type Quote struct {
	Symbol   string
	Currency string
	Exchange string
	Bars     []Bar
	// FirstTradeDate is the earliest date the provider has data for this
	// symbol (YYYY-MM-DD, exchange-local), or "" when the provider does not
	// expose it. The chunk walker (backfill.go) prefers this as its stop
	// condition over walking until a terminal error.
	FirstTradeDate string
}

// Provider failures. Callers distinguish these because they mean very different
// things: a missing symbol is permanent, a bad response is transient, bad
// granularity means the provider silently gave us the wrong resolution, a
// rate limit is retryable, and "no data for range" is the expected terminal
// condition a chunked backfill walks into once it passes a symbol's listing
// date.
var (
	// ErrSymbolNotFound means the provider has no such symbol (delisted, typo).
	ErrSymbolNotFound = errors.New("marketdata: symbol not found")
	// ErrBadGranularity means the provider returned non-daily data. This is the
	// guard against a chart that looks right and is wrong.
	ErrBadGranularity = errors.New("marketdata: provider returned non-daily data")
	// ErrBadResponse means the payload was not the shape we expect — including a
	// 200 carrying HTML, which must never parse as "zero bars".
	ErrBadResponse = errors.New("marketdata: unexpected response shape")
	// ErrRateLimited means the provider throttled this request (HTTP 429 or
	// equivalent). Retryable with backoff.
	ErrRateLimited = errors.New("marketdata: provider rate-limited the request")
	// ErrNoDataForRange means the provider has no data at all in the
	// requested window — a valid symbol, just nothing before its listing
	// date. Not retryable and not a failure: it is the chunk walker's
	// expected end-of-history signal.
	ErrNoDataForRange = errors.New("marketdata: no data for requested range")
)

// PriceProvider fetches daily OHLC bars for a symbol.
type PriceProvider interface {
	DailyBars(ctx context.Context, symbol string, from, to time.Time) (*Quote, error)
	Name() string
}
