package companyview

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"megane/internal/db"
	"megane/internal/marketdata"
)

const svcTestCIK = "0001567529"

func newServiceTestDB(t *testing.T) *db.DB {
	t.Helper()
	database, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
	return database
}

// fakeProvider scripts a sequence of responses per call, in order — no
// network, and no reliance on marketdata's own (unexported) test double.
type fakeProvider struct {
	name   string
	calls  []struct{ from, to time.Time }
	script []func() (*marketdata.Quote, error)
}

func (f *fakeProvider) Name() string { return f.name }

func (f *fakeProvider) DailyBars(_ context.Context, _ string, from, to time.Time) (*marketdata.Quote, error) {
	i := len(f.calls)
	f.calls = append(f.calls, struct{ from, to time.Time }{from, to})
	if i >= len(f.script) {
		return nil, fmt.Errorf("fakeProvider %s: unscripted call #%d", f.name, i)
	}
	return f.script[i]()
}

func fpOK(q *marketdata.Quote) func() (*marketdata.Quote, error) {
	return func() (*marketdata.Quote, error) { return q, nil }
}
func fpFails(err error) func() (*marketdata.Quote, error) {
	return func() (*marketdata.Quote, error) { return nil, err }
}

// newTestService builds a Service with a real (temp) DB and fake providers,
// bypassing NewService's env-driven timeout defaults so tests run instantly
// (real defaults would still work, just via context timeouts we don't need).
func newTestService(t *testing.T, provider, fallback marketdata.PriceProvider) (*Service, *db.DB) {
	t.Helper()
	database := newServiceTestDB(t)
	svc := NewService(nil, database, provider, fallback, Config{
		FetchTimeout:    time.Second,
		BackfillTimeout: 5 * time.Second,
		ChunkYears:      1,
		ChunkDelay:      0,
	})
	return svc, database
}

func mustWindow(t *testing.T, from, to string) Window {
	t.Helper()
	return Window{From: from, To: to}
}

// First-ever request for a CIK must walk the FULL chunked history, not just
// the requested window — D7's "not just the requested window" rule.
//
// The walk's actual chunk windows depend on real wall-clock "today"
// (backfillFull always walks from time.Now()), so this test's stop
// condition is a scripted terminal ErrNoDataForRange on the second call —
// not a FirstTradeDate/date-arithmetic race against the clock. The bar
// dates below are payload, not window bounds: the fake provider (like the
// real walker) doesn't require them to fall inside the requested from/to.
func TestEnsureCoverageFullBackfillOnEmptyCoverage(t *testing.T) {
	provider := &fakeProvider{name: "yahoo", script: []func() (*marketdata.Quote, error){
		fpOK(&marketdata.Quote{Symbol: "KMDA", Currency: "USD",
			Bars: []marketdata.Bar{{Date: "2024-06-01", Close: 10, Volume: 100}}}), // no FirstTradeDate: walk continues
		fpFails(marketdata.ErrNoDataForRange), // second chunk: terminal, ends the walk
	}}
	svc, database := newTestService(t, provider, nil)
	ctx := context.Background()

	// A NARROW requested window — the backfill must still cover the full
	// history the provider offers, not just this window.
	w := mustWindow(t, "2024-05-01", "2024-06-01")
	if err := svc.ensureCoverage(ctx, svcTestCIK, "KMDA", w); err != nil {
		t.Fatalf("ensureCoverage: %v", err)
	}
	if len(provider.calls) != 2 {
		t.Fatalf("provider called %d times, want 2 (chunked walk, not one window fetch)", len(provider.calls))
	}

	cov, err := database.StockPriceCoverage(ctx, svcTestCIK)
	if err != nil || cov == nil {
		t.Fatalf("coverage: %+v (err %v)", cov, err)
	}
	if cov.Status != db.PriceStatusOK {
		t.Errorf("status = %q, want ok (a terminal stop is expected end-of-history, not a failure)", cov.Status)
	}
	if cov.Earliest != "2024-06-01" || cov.Latest != "2024-06-01" {
		t.Errorf("coverage = %s..%s, want 2024-06-01..2024-06-01 (the one bar that was ever upserted)",
			cov.Earliest, cov.Latest)
	}

	rows, err := database.StockPrices(ctx, svcTestCIK, "2020-01-01", "2030-01-01")
	if err != nil || len(rows) != 1 {
		t.Fatalf("StockPrices = %+v (err %v), want 1 row", rows, err)
	}
	if rows[0].Volume != 100 {
		t.Errorf("volume not passed through: %+v", rows)
	}
}

// A second request, once coverage already exists and is stale (latest <
// window.to), must fetch ONLY [latest+1, today] — a single bounded call,
// never a re-walk (D7: "no walking").
func TestEnsureCoverageDeltaFetchWhenStale(t *testing.T) {
	svc, database := newTestService(t, nil, nil)
	ctx := context.Background()

	// Seed coverage as if a prior full backfill already ran.
	if err := database.UpsertStockPrices(ctx, svcTestCIK, "KMDA",
		[]db.PriceBar{{Date: "2023-01-01", Close: 8}, {Date: "2024-01-01", Close: 9}},
		"USD", "yahoo"); err != nil {
		t.Fatal(err)
	}

	delta := &fakeProvider{name: "yahoo", script: []func() (*marketdata.Quote, error){
		fpOK(&marketdata.Quote{Symbol: "KMDA", Currency: "USD",
			Bars: []marketdata.Bar{{Date: "2024-06-01", Close: 11, Volume: 200}}}),
	}}
	svc.provider = delta

	w := mustWindow(t, "2024-01-01", "2024-06-01")
	if err := svc.ensureCoverage(ctx, svcTestCIK, "KMDA", w); err != nil {
		t.Fatalf("ensureCoverage: %v", err)
	}
	if len(delta.calls) != 1 {
		t.Fatalf("provider called %d times, want exactly 1 (delta, not a chunk walk)", len(delta.calls))
	}
	if got := delta.calls[0].from.Format(DateLayout); got != "2024-01-02" {
		t.Errorf("delta from = %s, want 2024-01-02 (coverage.latest + 1 day)", got)
	}

	cov, err := database.StockPriceCoverage(ctx, svcTestCIK)
	if err != nil || cov == nil || cov.Latest != "2024-06-01" {
		t.Fatalf("coverage = %+v (err %v), want latest=2024-06-01", cov, err)
	}
}

// Already-covered window: zero external calls (HLD §8 "second request same
// day: zero external HTTP").
func TestEnsureCoverageNoOpWhenAlreadyCovered(t *testing.T) {
	svc, database := newTestService(t, nil, nil)
	ctx := context.Background()
	if err := database.UpsertStockPrices(ctx, svcTestCIK, "KMDA",
		[]db.PriceBar{{Date: "2024-01-01", Close: 8}, {Date: "2024-06-01", Close: 9}},
		"USD", "yahoo"); err != nil {
		t.Fatal(err)
	}
	provider := &fakeProvider{name: "yahoo"} // empty script: any call fails the test
	svc.provider = provider

	w := mustWindow(t, "2024-02-01", "2024-05-01")
	if err := svc.ensureCoverage(ctx, svcTestCIK, "KMDA", w); err != nil {
		t.Fatalf("ensureCoverage: %v", err)
	}
	if len(provider.calls) != 0 {
		t.Errorf("provider called %d times, want 0 (window already covered)", len(provider.calls))
	}
}

// A failed attempt must not be retried before PriceRetryFloor — protects the
// provider from being hammered by every page view of a broken symbol.
func TestEnsureCoverageRespectsRetryFloor(t *testing.T) {
	provider := &fakeProvider{name: "yahoo", script: []func() (*marketdata.Quote, error){
		fpFails(marketdata.ErrSymbolNotFound),
	}}
	svc, database := newTestService(t, provider, nil)
	ctx := context.Background()

	w := mustWindow(t, "2024-01-01", "2024-06-01")
	if err := svc.ensureCoverage(ctx, svcTestCIK, "NOSUCH", w); err == nil {
		t.Fatal("ensureCoverage: want the not-found error surfaced")
	}
	if len(provider.calls) != 1 {
		t.Fatalf("provider called %d times, want 1", len(provider.calls))
	}
	cov, err := database.StockPriceCoverage(ctx, svcTestCIK)
	if err != nil || cov == nil || cov.Status != db.PriceStatusNotFound {
		t.Fatalf("coverage = %+v (err %v), want status=not_found", cov, err)
	}

	// Immediately retrying must NOT call the provider again.
	if err := svc.ensureCoverage(ctx, svcTestCIK, "NOSUCH", w); err != nil {
		t.Fatalf("ensureCoverage (within retry floor): %v", err)
	}
	if len(provider.calls) != 1 {
		t.Errorf("provider called %d times, want still 1 (retry floor not yet elapsed)", len(provider.calls))
	}
}

// A chunk walk that gets SOME bars before a hard failure must leave the
// coverage row's status OK (with the partial span), not downgrade it to
// error — see the doc comment on backfillFull for why.
func TestEnsureCoveragePartialBackfillStaysOK(t *testing.T) {
	boom := fmt.Errorf("marketdata: unexpected response shape")
	provider := &fakeProvider{name: "yahoo", script: []func() (*marketdata.Quote, error){
		fpOK(&marketdata.Quote{Symbol: "KMDA", Currency: "USD",
			Bars: []marketdata.Bar{{Date: "2024-06-01", Close: 10}}}), // no FirstTradeDate: walk continues
		fpFails(boom), // second chunk: hard, non-retryable failure
	}}
	svc, database := newTestService(t, provider, nil)
	ctx := context.Background()

	w := mustWindow(t, "2024-05-01", "2024-06-01")
	if err := svc.ensureCoverage(ctx, svcTestCIK, "KMDA", w); err == nil {
		t.Fatal("ensureCoverage: want the walk's error surfaced to the caller")
	}

	cov, err := database.StockPriceCoverage(ctx, svcTestCIK)
	if err != nil || cov == nil {
		t.Fatalf("coverage: %+v (err %v)", cov, err)
	}
	if cov.Status != db.PriceStatusOK {
		t.Errorf("status = %q, want %q — partial progress from the first chunk must not be discarded",
			cov.Status, db.PriceStatusOK)
	}
	if cov.Earliest != "2024-06-01" || cov.Latest != "2024-06-01" {
		t.Errorf("coverage = %s..%s, want the one successfully-upserted bar's date", cov.Earliest, cov.Latest)
	}
}

// The fallback provider is actually wired through to the chunk walker — a
// chunk the primary cannot serve is served by fallback instead. Primary's
// failure here is deliberately NOT ErrRateLimited: that path already has
// real-time-backoff coverage in marketdata/backfill_test.go (with an
// injected Sleep); this test only needs to prove Service wires s.fallback
// through at all, without paying the real 2s+8s+30s backoff ladder
// (marketdata.Backfiller's retry/backoff constants are not
// Service-configurable, by design — see Config's doc comment).
func TestEnsureCoverageUsesFallbackOnPrimaryExhaustion(t *testing.T) {
	primaryErr := fmt.Errorf("marketdata: unexpected response shape")
	primary := &fakeProvider{name: "yahoo", script: []func() (*marketdata.Quote, error){
		fpFails(primaryErr),                   // non-retryable: falls back immediately, no backoff sleep
		fpFails(marketdata.ErrNoDataForRange), // next chunk: end the walk cleanly
	}}
	fallback := &fakeProvider{name: "tiingo", script: []func() (*marketdata.Quote, error){
		fpOK(&marketdata.Quote{Symbol: "KMDA", Currency: "USD",
			Bars: []marketdata.Bar{{Date: "2024-06-01", Close: 10}}}),
	}}
	svc, database := newTestService(t, primary, fallback)
	ctx := context.Background()

	w := mustWindow(t, "2024-05-01", "2024-06-01")
	if err := svc.ensureCoverage(ctx, svcTestCIK, "KMDA", w); err != nil {
		t.Fatalf("ensureCoverage: %v", err)
	}
	if len(fallback.calls) != 1 {
		t.Fatalf("fallback called %d times, want 1", len(fallback.calls))
	}
	cov, err := database.StockPriceCoverage(ctx, svcTestCIK)
	if err != nil || cov == nil || cov.Status != db.PriceStatusOK {
		t.Fatalf("coverage = %+v (err %v)", cov, err)
	}
}
