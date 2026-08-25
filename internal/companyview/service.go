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
	// FetchTimeout bounds one outbound provider call. Must stay well under the
	// route timeout so a slow provider cannot hold the request open.
	FetchTimeout time.Duration
}

const defaultFetchTimeout = 20 * time.Second

// Service assembles timelines from filings (filedb), stored prices (db), and a
// market-data provider.
type Service struct {
	store    filedb.CompanyStore
	prices   *db.DB
	provider marketdata.PriceProvider
	cfg      Config

	fetchMu sync.Mutex
	fetches map[string]*sync.Mutex // per-CIK, so a slow provider cannot pile up
}

// NewService wires the timeline service. provider may be nil, in which case
// timelines are served events-only.
func NewService(store filedb.CompanyStore, database *db.DB, provider marketdata.PriceProvider, cfg Config) *Service {
	if cfg.FetchTimeout <= 0 {
		cfg.FetchTimeout = defaultFetchTimeout
	}
	return &Service{
		store:    store,
		prices:   database,
		provider: provider,
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

// ensureCoverage backfills the requested window if stored coverage does not
// already span it. Serialized per CIK.
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
	if covered(cov, w) {
		return nil
	}
	if !cov.ShouldRetry(time.Now().UTC()) {
		// A recent failure; do not hammer the provider.
		return nil
	}

	from, err := time.Parse(DateLayout, w.From)
	if err != nil {
		return err
	}
	to, err := time.Parse(DateLayout, w.To)
	if err != nil {
		return err
	}
	// Widen slightly so an edge trading day is not missed.
	from = from.AddDate(0, 0, -5)
	to = to.AddDate(0, 0, 1)

	fetchCtx, cancel := context.WithTimeout(ctx, s.cfg.FetchTimeout)
	defer cancel()

	quote, err := s.provider.DailyBars(fetchCtx, symbol, from, to)
	if err != nil {
		status := db.PriceStatusError
		if errors.Is(err, marketdata.ErrSymbolNotFound) {
			status = db.PriceStatusNotFound
		}
		_ = s.prices.SetStockPriceCoverage(ctx, cik, symbol, status, err.Error())
		return err
	}
	if len(quote.Bars) == 0 {
		_ = s.prices.SetStockPriceCoverage(ctx, cik, symbol, db.PriceStatusError, "provider returned no bars")
		return nil
	}

	bars := make([]db.PriceBar, 0, len(quote.Bars))
	for _, b := range quote.Bars {
		bars = append(bars, db.PriceBar{
			Date: b.Date, Open: b.Open, High: b.High, Low: b.Low,
			Close: b.Close, AdjClose: b.AdjClose, Volume: b.Volume,
		})
	}
	return s.prices.UpsertStockPrices(ctx, cik, quote.Symbol, bars, quote.Currency, s.provider.Name())
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
