package db

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

const testCIK = "0001567529"

func newPriceDB(t *testing.T) *DB {
	t.Helper()
	database, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
	return database
}

func bars() []PriceBar {
	return []PriceBar{
		{Date: "2026-06-01", Open: 10, High: 10.5, Low: 9.5, Close: 10.1, AdjClose: 9.6, Volume: 1000},
		{Date: "2026-06-02", Open: 11, High: 11.5, Low: 10.5, Close: 11.1, AdjClose: 10.5, Volume: 2000},
		{Date: "2026-06-03", Open: 12, High: 12.5, Low: 11.5, Close: 12.1, AdjClose: 11.5, Volume: 3000},
	}
}

func TestMigrationsV32V33Applied(t *testing.T) {
	d := newPriceDB(t)
	for _, v := range []int{32, 33} {
		var got int
		if err := d.QueryRow(`SELECT 1 FROM schema_migrations WHERE version = ?`, v).Scan(&got); err != nil {
			t.Errorf("migration v%d not recorded: %v", v, err)
		}
	}
	for _, table := range []string{"stock_prices", "stock_price_coverage"} {
		var name string
		if err := d.QueryRow(`SELECT name FROM sqlite_master WHERE type='table' AND name=?`, table).Scan(&name); err != nil {
			t.Errorf("table %s missing: %v", table, err)
		}
	}
}

func TestUpsertStockPricesIsIdempotent(t *testing.T) {
	d := newPriceDB(t)
	ctx := context.Background()

	if err := d.UpsertStockPrices(ctx, testCIK, "KMDA", bars(), "USD", "yahoo"); err != nil {
		t.Fatalf("first upsert: %v", err)
	}
	var n int
	if err := d.QueryRow(`SELECT COUNT(*) FROM stock_prices`).Scan(&n); err != nil || n != 3 {
		t.Fatalf("after first upsert count = %d (err %v), want 3", n, err)
	}

	// Overlapping re-fetch with a corrected close must update, not duplicate.
	updated := bars()
	updated[1].Close = 99.9
	if err := d.UpsertStockPrices(ctx, testCIK, "KMDA", updated, "USD", "yahoo"); err != nil {
		t.Fatalf("second upsert: %v", err)
	}
	if err := d.QueryRow(`SELECT COUNT(*) FROM stock_prices`).Scan(&n); err != nil || n != 3 {
		t.Fatalf("after re-fetch count = %d, want 3 (idempotent)", n)
	}
	var got float64
	if err := d.QueryRow(`SELECT close FROM stock_prices WHERE cik=? AND trade_date=?`,
		testCIK, "2026-06-02").Scan(&got); err != nil || got != 99.9 {
		t.Errorf("close = %v (err %v), want 99.9 — conflict should UPDATE", got, err)
	}
}

func TestStockPricesRangeAndOrder(t *testing.T) {
	d := newPriceDB(t)
	ctx := context.Background()
	if err := d.UpsertStockPrices(ctx, testCIK, "KMDA", bars(), "USD", "yahoo"); err != nil {
		t.Fatal(err)
	}

	rows, err := d.StockPrices(ctx, testCIK, "2026-06-01", "2026-06-02")
	if err != nil {
		t.Fatalf("StockPrices: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("got %d rows, want 2 (inclusive bounds)", len(rows))
	}
	if rows[0].Date != "2026-06-01" || rows[1].Date != "2026-06-02" {
		t.Errorf("rows not oldest-first: %+v", rows)
	}
	if rows[0].AdjClose != 9.6 {
		t.Errorf("adjClose = %v, want 9.6 — the chart plots the adjusted series", rows[0].AdjClose)
	}

	empty, err := d.StockPrices(ctx, testCIK, "2030-01-01", "2030-12-31")
	if err != nil {
		t.Fatalf("empty range: %v", err)
	}
	if empty == nil || len(empty) != 0 {
		t.Errorf("empty range = %+v, want non-nil empty slice", empty)
	}

	other, err := d.StockPrices(ctx, "0000000001", "2026-06-01", "2026-06-03")
	if err != nil || len(other) != 0 {
		t.Errorf("prices leaked across cik: %+v (err %v)", other, err)
	}
}

func TestCoverageWrittenWithPrices(t *testing.T) {
	d := newPriceDB(t)
	ctx := context.Background()
	if err := d.UpsertStockPrices(ctx, testCIK, "KMDA", bars(), "USD", "yahoo"); err != nil {
		t.Fatal(err)
	}
	cov, err := d.StockPriceCoverage(ctx, testCIK)
	if err != nil {
		t.Fatalf("coverage: %v", err)
	}
	if cov == nil {
		t.Fatal("coverage row not written alongside prices")
	}
	if cov.Earliest != "2026-06-01" || cov.Latest != "2026-06-03" {
		t.Errorf("coverage = %s..%s, want 2026-06-01..2026-06-03", cov.Earliest, cov.Latest)
	}
	if cov.Status != PriceStatusOK || cov.Symbol != "KMDA" {
		t.Errorf("coverage = %+v", cov)
	}
}

func TestCoverageAbsentForUnknownCompany(t *testing.T) {
	d := newPriceDB(t)
	cov, err := d.StockPriceCoverage(context.Background(), "0009999999")
	if err != nil {
		t.Fatalf("err = %v, want nil for a company never attempted", err)
	}
	if cov != nil {
		t.Errorf("coverage = %+v, want nil", cov)
	}
}

// A tickerless company must not re-hit the provider on every timeline open.
func TestNoSymbolSuppressesRefetchUntilRetryFloor(t *testing.T) {
	d := newPriceDB(t)
	ctx := context.Background()
	if err := d.SetStockPriceCoverage(ctx, testCIK, "", PriceStatusNoSymbol, "company has no ticker"); err != nil {
		t.Fatal(err)
	}
	cov, err := d.StockPriceCoverage(ctx, testCIK)
	if err != nil || cov == nil {
		t.Fatalf("coverage: %+v (err %v)", cov, err)
	}
	if cov.Status != PriceStatusNoSymbol || cov.Note == "" {
		t.Errorf("coverage = %+v", cov)
	}
	if cov.ShouldRetry(time.Now()) {
		t.Error("a just-recorded no_symbol must not be retried immediately")
	}
	if !cov.ShouldRetry(time.Now().Add(PriceRetryFloor + time.Minute)) {
		t.Error("after the retry floor, a fetch should be attempted again")
	}
	var nilCov *PriceCoverageRow
	if !nilCov.ShouldRetry(time.Now()) {
		t.Error("a company never attempted must be fetchable")
	}
}

func TestUpsertRequiresCIK(t *testing.T) {
	d := newPriceDB(t)
	if err := d.UpsertStockPrices(context.Background(), "", "X", bars(), "USD", "yahoo"); err == nil {
		t.Error("empty cik must error")
	}
}
