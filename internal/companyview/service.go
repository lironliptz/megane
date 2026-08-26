package companyview

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"

	"megane/internal/db"
	"megane/internal/filedb"
	"megane/internal/marketdata"
)

// Config tunes the timeline service.
type Config struct {
	// FetchOnOpen eagerly backfills prices when a company page opens, rather
	// than waiting for the first timeline request. Default off: eager fetching
	// would hit the provider for users who never open the tab.
	FetchOnOpen bool
	// FetchTimeout bounds one outbound provider call (the delta-refresh path:
	// a single bounded [latest+1, today] fetch). Must stay well under the
	// route timeout so a slow provider cannot hold the request open.
	FetchTimeout time.Duration
	// BackfillTimeout bounds the ENTIRE chunked full-history walk (possibly
	// many chunks, each with retries and backoff up to 30s) — deliberately
	// much larger than FetchTimeout, which bounds only one call.
	BackfillTimeout time.Duration
	// ChunkYears / ChunkDelay tune the backfill walker (HLD D10). Zero means
	// marketdata.DefaultChunkYears / marketdata.DefaultChunkDelay.
	ChunkYears int
	ChunkDelay time.Duration
}

const (
	defaultFetchTimeout    = 20 * time.Second
	defaultBackfillTimeout = 5 * time.Minute
)

// Service assembles timelines from filings (filedb), stored prices (db), and a
// market-data provider.
type Service struct {
	store    filedb.CompanyStore
	prices   *db.DB
	provider marketdata.PriceProvider
	fallback marketdata.PriceProvider // optional; nil disables it
	cfg      Config

	fetchMu sync.Mutex
	fetches map[string]*sync.Mutex // per-CIK, so a slow provider cannot pile up
}

// NewService wires the timeline service. provider may be nil, in which case
// timelines are served events-only. fallback may also be nil (no keyed
// secondary provider configured) — the chunked backfill then simply has
// nothing to fall back to on a chunk it cannot get from provider.
func NewService(store filedb.CompanyStore, database *db.DB, provider, fallback marketdata.PriceProvider, cfg Config) *Service {
	if cfg.FetchTimeout <= 0 {
		cfg.FetchTimeout = defaultFetchTimeout
	}
	if cfg.BackfillTimeout <= 0 {
		cfg.BackfillTimeout = defaultBackfillTimeout
	}
	return &Service{
		store:    store,
		prices:   database,
		provider: provider,
		fallback: fallback,
		cfg:      cfg,
		fetches:  map[string]*sync.Mutex{},
	}
}

// Timeline builds the payload for GET /api/companies/:cik/timeline.
//
// Prices are best-effort: a company with no ticker, or a provider that fails,
// yields PriceCoverage == nil and an events-only chart. The events half depends
// on nothing external and must always be served.
func (s *Service) Timeline(ctx context.Context, cik string, req Window, filter string) (*Timeline, error) {
	detail, err := s.store.GetCompany(ctx, cik)
	if err != nil {
		return nil, err
	}

	window, err := ClampWindow(req, detail.Coverage, time.Now().UTC())
	if err != nil {
		return nil, err
	}

	page, err := s.store.ListFilings(ctx, cik, filedb.FilingFilter{Limit: 0, Offset: 0})
	if err != nil {
		return nil, err
	}

	out := &Timeline{
		Window: window,
		Ticker: primaryTicker(detail.Identity.Tickers),
		Prices: []PricePoint{},
		Events: BuildEvents(page.Items, window, NormalizeFilter(filter)),
	}

	out.Prices, out.PriceCoverage = s.pricesFor(ctx, cik, out.Ticker, window)
	return out, nil
}

// primaryTicker is the first US-listed symbol from submissions.json. Foreign
// private issuers (the target population) list under a US ticker.
func primaryTicker(tickers []string) string {
	if len(tickers) == 0 {
		return ""
	}
	return tickers[0]
}

// pricesFor returns stored prices for the window, backfilling from the provider
// when the window is not already covered. It never returns an error: price
// failures degrade the chart, they do not fail the request.
func (s *Service) pricesFor(ctx context.Context, cik, symbol string, w Window) ([]PricePoint, *PriceCoverage) {
	if s.prices == nil {
		return []PricePoint{}, nil
	}

	if symbol == "" {
		_ = s.prices.SetStockPriceCoverage(ctx, cik, "", db.PriceStatusNoSymbol, "company has no ticker")
		return []PricePoint{}, nil
	}

	if err := s.ensureCoverage(ctx, cik, symbol, w); err != nil {
		slog.Warn("companyview: price backfill failed", "cik", cik, "symbol", symbol, "err", err)
	}

	rows, err := s.prices.StockPrices(ctx, cik, w.From, w.To)
	if err != nil {
		slog.Error("companyview: reading stored prices", "cik", cik, "err", err)
		return []PricePoint{}, nil
	}

	points := make([]PricePoint, 0, len(rows))
	for _, r := range rows {
		points = append(points, PricePoint{
			Date: r.Date, Close: r.Close, AdjClose: r.AdjClose, Volume: r.Volume,
		})
	}

	cov, err := s.prices.StockPriceCoverage(ctx, cik)
	if err != nil || cov == nil {
		return points, nil
	}
	pc := &PriceCoverage{
		Earliest: cov.Earliest, Latest: cov.Latest,
		Status: cov.Status, Note: cov.Note,
	}
	if s.provider != nil {
		pc.Source = s.provider.Name()
	}
	if len(points) == 0 && cov.Status != db.PriceStatusOK {
		// Nothing to plot and the last attempt failed: tell the UI why.
		return points, pc
	}
	return points, pc
}

// hasCoverage reports whether any usable price history is already stored —
// the trigger between "full chunked backfill" and "delta refresh" below
// (HLD D7). A row that only records a past failure (no_symbol/not_found/
// error, or ok with no earliest date yet) does not count.
func hasCoverage(cov *db.PriceCoverageRow) bool {
	return cov != nil && cov.Status == db.PriceStatusOK && cov.Earliest != ""
}

// ensureCoverage backfills prices for cik/symbol. Serialized per CIK so a
// slow provider cannot pile up concurrent fetches for the same company.
//
// First-ever request (no usable coverage yet): a full CHUNKED backfill walks
// backward from today to the symbol's first trade date (D7/D10) — not just
// the requested window w, so a later request for a wider window is already
// covered without a second walk.
//
// Subsequent requests: a single bounded delta fetch for [coverage.latest+1,
// today] once the window's upper bound is stale — never a re-walk (D7,
// "RefreshIfStale: single bounded fetch — no walking").
func (s *Service) ensureCoverage(ctx context.Context, cik, symbol string, w Window) error {
	if s.provider == nil {
		return nil
	}

	lock := s.lockFor(cik)
	lock.Lock()
	defer lock.Unlock()

	cov, err := s.prices.StockPriceCoverage(ctx, cik)
	if err != nil {
		return err
	}

	if hasCoverage(cov) {
		if covered(cov, w) {
			return nil
		}
		if !cov.ShouldRetry(time.Now().UTC()) {
			return nil // a recent failed attempt; do not hammer the provider
		}
		return s.refreshDelta(ctx, cik, symbol, cov)
	}

	// cov may be nil here (never attempted) or a past failure; ShouldRetry
	// on a nil receiver returns true, so a first-ever request always proceeds.
	if !cov.ShouldRetry(time.Now().UTC()) {
		return nil
	}
	return s.backfillFull(ctx, cik, symbol)
}

// refreshDelta fetches only the trading days since the last cached one, in
// one bounded call (FetchTimeout) — not a chunk walk, per D7.
func (s *Service) refreshDelta(ctx context.Context, cik, symbol string, cov *db.PriceCoverageRow) error {
	latest, err := time.Parse(DateLayout, cov.Latest)
	if err != nil {
		return err
	}
	from := latest.AddDate(0, 0, 1)
	to := time.Now().UTC()
	if from.After(to) {
		return nil // already current
	}

	fetchCtx, cancel := context.WithTimeout(ctx, s.cfg.FetchTimeout)
	defer cancel()

	quote, err := s.provider.DailyBars(fetchCtx, symbol, from, to)
	if err != nil {
		if errors.Is(err, marketdata.ErrNoDataForRange) {
			// No new trading day yet (e.g. a same-day retry) — not a failure,
			// and coverage.latest is still accurate; leave it as-is.
			return nil
		}
		status := db.PriceStatusError
		if errors.Is(err, marketdata.ErrSymbolNotFound) {
			status = db.PriceStatusNotFound
		}
		_ = s.prices.SetStockPriceCoverage(ctx, cik, symbol, status, err.Error())
		return err
	}
	if len(quote.Bars) == 0 {
		// Nothing new; coverage.latest already reflects reality.
		return nil
	}
	return s.upsertQuote(ctx, cik, symbol, quote, s.provider.Name())
}

// backfillFull walks the symbol's entire history in chunks (D7/D10) via
// marketdata.Backfiller, upserting after every chunk so a crash mid-walk
// leaves partial-but-usable coverage rather than nothing. Falls back to
// s.fallback per chunk when s.provider is exhausted for that chunk.
//
// A walk that fails after at least one chunk succeeded deliberately does NOT
// downgrade the coverage row's status to error: UpsertStockPrices already
// recomputed earliest/latest from whatever chunks landed, and that partial
// span is real, usable data. Overwriting it to "error" would make hasCoverage
// false on the next request and re-walk the whole history from today again —
// wasteful, and it would re-hit the very same chunk that just failed. This
// mid-series-hole risk is accepted explicitly (HLD §7 Risks: "outer
// earliest/latest cannot detect mid-series holes; acceptable MVP"). Only a
// walk that never got a single bar records a failure, so ShouldRetry's floor
// still protects a genuinely broken symbol from being hit on every request.
func (s *Service) backfillFull(ctx context.Context, cik, symbol string) error {
	fetchCtx, cancel := context.WithTimeout(ctx, s.cfg.BackfillTimeout)
	defer cancel()

	bf := &marketdata.Backfiller{
		Primary:    s.provider,
		Fallback:   s.fallback,
		ChunkYears: s.cfg.ChunkYears,
		ChunkDelay: s.cfg.ChunkDelay,
	}

	var upsertErr error
	var gotAnyBars bool
	_, walkErr := bf.Backward(fetchCtx, symbol, time.Now().UTC(), func(q *marketdata.Quote, source string) error {
		if err := s.upsertQuote(ctx, cik, symbol, q, source); err != nil {
			upsertErr = err
			return err
		}
		gotAnyBars = true
		return nil
	})
	if upsertErr != nil {
		return upsertErr
	}
	if walkErr != nil {
		if gotAnyBars {
			// Partial success: leave the OK coverage row from the last
			// upsert as-is (see doc comment above).
			return walkErr
		}
		status := db.PriceStatusError
		if errors.Is(walkErr, marketdata.ErrSymbolNotFound) {
			status = db.PriceStatusNotFound
		}
		_ = s.prices.SetStockPriceCoverage(ctx, cik, symbol, status, walkErr.Error())
		return walkErr
	}
	if !gotAnyBars {
		_ = s.prices.SetStockPriceCoverage(ctx, cik, symbol, db.PriceStatusError, "provider returned no bars")
	}
	return nil
}

// upsertQuote converts and writes one provider Quote, recording which
// provider actually served it (source may be the fallback's name, not
// s.provider.Name(), when a chunk was served by the fallback).
func (s *Service) upsertQuote(ctx context.Context, cik, symbol string, q *marketdata.Quote, source string) error {
	bars := make([]db.PriceBar, 0, len(q.Bars))
	for _, b := range q.Bars {
		bars = append(bars, db.PriceBar{
			Date: b.Date, Open: b.Open, High: b.High, Low: b.Low,
			Close: b.Close, AdjClose: b.AdjClose, Volume: b.Volume,
		})
	}
	sym := symbol
	if q.Symbol != "" {
		sym = q.Symbol
	}
	return s.prices.UpsertStockPrices(ctx, cik, sym, bars, q.Currency, source)
}

// covered reports whether stored prices already span the window.
func covered(cov *db.PriceCoverageRow, w Window) bool {
	if cov == nil || cov.Status != db.PriceStatusOK {
		return false
	}
	if cov.Earliest == "" || cov.Latest == "" {
		return false
	}
	return cov.Earliest <= w.From && cov.Latest >= w.To
}

func (s *Service) lockFor(cik string) *sync.Mutex {
	s.fetchMu.Lock()
	defer s.fetchMu.Unlock()
	l, ok := s.fetches[cik]
	if !ok {
		l = &sync.Mutex{}
		s.fetches[cik] = l
	}
	return l
}
