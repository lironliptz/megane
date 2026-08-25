package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// Price coverage statuses. Anything other than StatusOK means "do not re-fetch
// until the retry floor has passed".
const (
	PriceStatusOK       = "ok"
	PriceStatusNoSymbol = "no_symbol"
	PriceStatusNotFound = "not_found"
	PriceStatusError    = "error"
)

// PriceRetryFloor bounds how often a failing company is re-fetched.
const PriceRetryFloor = 6 * time.Hour

// PriceBar is one daily bar as written to storage.
//
// This package deliberately does not import internal/marketdata: db is a
// low-level package and should not carry provider concepts. The caller converts.
type PriceBar struct {
	Date     string // YYYY-MM-DD
	Open     float64
	High     float64
	Low      float64
	Close    float64
	AdjClose float64
	Volume   int64
}

// PriceRow is one daily bar as read back for the chart.
type PriceRow struct {
	Date     string
	Close    float64
	AdjClose float64
	Volume   int64
}

// PriceCoverageRow records what has been fetched for a company and how the last
// attempt went.
type PriceCoverageRow struct {
	CIK           string
	Symbol        string
	Earliest      string
	Latest        string
	Status        string
	Note          string
	LastAttemptAt time.Time
}

// ShouldRetry reports whether enough time has passed to re-attempt a fetch for a
// company whose last attempt did not succeed.
func (c *PriceCoverageRow) ShouldRetry(now time.Time) bool {
	if c == nil {
		return true
	}
	if c.Status == PriceStatusOK {
		return true // ok rows are extended by range, not blocked
	}
	return now.Sub(c.LastAttemptAt) >= PriceRetryFloor
}

// UpsertStockPrices writes bars idempotently and refreshes the coverage row in
// the same transaction.
//
// Re-fetching an overlapping range must not duplicate rows — the (cik,
// trade_date) primary key plus ON CONFLICT DO UPDATE guarantees that.
func (d *DB) UpsertStockPrices(ctx context.Context, cik, symbol string, bars []PriceBar, currency, source string) error {
	if cik == "" {
		return errors.New("db: UpsertStockPrices requires a cik")
	}
	if currency == "" {
		currency = "USD"
	}

	tx, err := d.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	stmt, err := tx.PrepareContext(ctx, `
		INSERT INTO stock_prices
			(cik, trade_date, open, high, low, close, adj_close, volume, currency, source, fetched_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(cik, trade_date) DO UPDATE SET
			open = excluded.open, high = excluded.high, low = excluded.low,
			close = excluded.close, adj_close = excluded.adj_close,
			volume = excluded.volume, currency = excluded.currency,
			source = excluded.source, fetched_at = excluded.fetched_at`)
	if err != nil {
		return fmt.Errorf("prepare: %w", err)
	}
	defer stmt.Close()

	now := time.Now().UTC()
	for _, b := range bars {
		if b.Date == "" {
			continue
		}
		if _, err := stmt.ExecContext(ctx, cik, b.Date, b.Open, b.High, b.Low,
			b.Close, b.AdjClose, b.Volume, currency, source, now); err != nil {
			return fmt.Errorf("upsert %s/%s: %w", cik, b.Date, err)
		}
	}

	if err := upsertCoverageTx(ctx, tx, cik, symbol, PriceStatusOK, "", now); err != nil {
		return err
	}
	return tx.Commit()
}

// upsertCoverageTx recomputes earliest/latest from the stored rows so coverage
// can never drift from the data it describes.
func upsertCoverageTx(ctx context.Context, tx *sql.Tx, cik, symbol, status, note string, now time.Time) error {
	var earliest, latest sql.NullString
	if err := tx.QueryRowContext(ctx,
		`SELECT MIN(trade_date), MAX(trade_date) FROM stock_prices WHERE cik = ?`, cik).
		Scan(&earliest, &latest); err != nil {
		return fmt.Errorf("coverage range: %w", err)
	}
	_, err := tx.ExecContext(ctx, `
		INSERT INTO stock_price_coverage (cik, symbol, earliest, latest, status, note, last_attempt_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(cik) DO UPDATE SET
			symbol = excluded.symbol, earliest = excluded.earliest, latest = excluded.latest,
			status = excluded.status, note = excluded.note, last_attempt_at = excluded.last_attempt_at`,
		cik, symbol, earliest, latest, status, note, now)
	if err != nil {
		return fmt.Errorf("upsert coverage: %w", err)
	}
	return nil
}

// SetStockPriceCoverage records the outcome of a fetch attempt that produced no
// usable bars (no ticker, delisted symbol, provider error).
func (d *DB) SetStockPriceCoverage(ctx context.Context, cik, symbol, status, note string) error {
	tx, err := d.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	if err := upsertCoverageTx(ctx, tx, cik, symbol, status, note, time.Now().UTC()); err != nil {
		return err
	}
	return tx.Commit()
}

// StockPriceCoverage returns the coverage row, or nil when nothing has been
// attempted for this company.
func (d *DB) StockPriceCoverage(ctx context.Context, cik string) (*PriceCoverageRow, error) {
	var (
		row                        PriceCoverageRow
		earliest, latest, noteNull sql.NullString
	)
	err := d.QueryRowContext(ctx, `
		SELECT cik, symbol, earliest, latest, status, note, last_attempt_at
		FROM stock_price_coverage WHERE cik = ?`, cik).
		Scan(&row.CIK, &row.Symbol, &earliest, &latest, &row.Status, &noteNull, &row.LastAttemptAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read coverage: %w", err)
	}
	row.Earliest, row.Latest, row.Note = earliest.String, latest.String, noteNull.String
	return &row, nil
}

// StockPrices returns bars in [from, to] inclusive, oldest first — the order the
// chart's category axis needs. OHLC is stored but not returned; no view needs it
// yet.
func (d *DB) StockPrices(ctx context.Context, cik, from, to string) ([]PriceRow, error) {
	rows, err := d.QueryContext(ctx, `
		SELECT trade_date, close, COALESCE(adj_close, 0), COALESCE(volume, 0)
		FROM stock_prices
		WHERE cik = ? AND trade_date >= ? AND trade_date <= ?
		ORDER BY trade_date ASC`, cik, from, to)
	if err != nil {
		return nil, fmt.Errorf("query prices: %w", err)
	}
	defer rows.Close()

	out := []PriceRow{}
	for rows.Next() {
		var r PriceRow
		if err := rows.Scan(&r.Date, &r.Close, &r.AdjClose, &r.Volume); err != nil {
			return nil, fmt.Errorf("scan price: %w", err)
		}
		out = append(out, r)
	}
	return out, rows.Err()
}
