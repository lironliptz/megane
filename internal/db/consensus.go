package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"megane/internal/models"
)

const (
	AnalystCoverageIdle     = "idle"
	AnalystCoverageFetching = "fetching"
	AnalystCoverageOK       = "ok"
	AnalystCoveragePartial  = "partial"
	AnalystCoverageNoData   = "no_data"
	AnalystCoverageError    = "error"
)

type AnalystCoverageRow struct {
	CIK            string
	Status         string
	Provider       string
	LastAttemptAt  string
	LastSuccessAt  string
	NextRetryAfter string
	Note           string
}

func (d *DB) UpsertConsensusPeriods(ctx context.Context, rows []models.ConsensusPeriod) error {
	if len(rows) == 0 {
		return nil
	}
	tx, err := d.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	stmt, err := tx.PrepareContext(ctx, `
		INSERT INTO analyst_period_consensus (
			cik, source, fiscal_year, fiscal_period, period_end, duration, consensus_date,
			announcement_date, revenue_estimate, eps_estimate, eps_actual, eps_surprise,
			eps_surprise_pct, basis, currency, rating_buy_count, rating_hold_count,
			rating_sell_count, source_url, fetched_at, created_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(cik, source, fiscal_year, fiscal_period, duration, consensus_date)
		DO UPDATE SET
			period_end=excluded.period_end,
			announcement_date=excluded.announcement_date,
			revenue_estimate=excluded.revenue_estimate,
			eps_estimate=excluded.eps_estimate,
			eps_actual=excluded.eps_actual,
			eps_surprise=excluded.eps_surprise,
			eps_surprise_pct=excluded.eps_surprise_pct,
			basis=excluded.basis,
			currency=excluded.currency,
			rating_buy_count=excluded.rating_buy_count,
			rating_hold_count=excluded.rating_hold_count,
			rating_sell_count=excluded.rating_sell_count,
			source_url=excluded.source_url,
			fetched_at=excluded.fetched_at`)
	if err != nil {
		return fmt.Errorf("prepare: %w", err)
	}
	defer stmt.Close()

	now := time.Now().UTC()
	for _, r := range rows {
		currency := r.Currency
		if currency == "" {
			currency = "USD"
		}
		fetchedAt := r.FetchedAt
		if fetchedAt.IsZero() {
			fetchedAt = now
		}
		if _, err := stmt.ExecContext(ctx,
			r.CIK, r.Source, r.FiscalYear, r.FiscalPeriod, r.PeriodEnd, r.Duration, r.ConsensusDate,
			nullIfEmpty(r.AnnouncementDate), r.RevenueEstimate, r.EPSEstimate, r.EPSActual, r.EPSSurprise,
			r.EPSSurprisePct, nullIfEmpty(r.Basis), currency, r.RatingBuyCount, r.RatingHoldCount,
			r.RatingSellCount, r.SourceURL, fetchedAt, now,
		); err != nil {
			return fmt.Errorf("upsert consensus row %s/%s/%s: %w", r.CIK, r.FiscalYear, r.FiscalPeriod, err)
		}
	}
	return tx.Commit()
}

func (d *DB) UpsertConsensusSnapshot(ctx context.Context, s models.ConsensusSnapshot) error {
	currency := s.Currency
	if currency == "" {
		currency = "USD"
	}
	fetchedAt := s.FetchedAt
	if fetchedAt.IsZero() {
		fetchedAt = time.Now().UTC()
	}
	_, err := d.ExecContext(ctx, `
		INSERT INTO analyst_snapshot (
			cik, source, as_of, pt_mean, pt_high, pt_low, pt_median, analyst_count,
			rating_buy_count, rating_hold_count, rating_sell_count, currency, fetched_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(cik, source, as_of) DO UPDATE SET
			pt_mean=excluded.pt_mean,
			pt_high=excluded.pt_high,
			pt_low=excluded.pt_low,
			pt_median=excluded.pt_median,
			analyst_count=excluded.analyst_count,
			rating_buy_count=excluded.rating_buy_count,
			rating_hold_count=excluded.rating_hold_count,
			rating_sell_count=excluded.rating_sell_count,
			currency=excluded.currency,
			fetched_at=excluded.fetched_at`,
		s.CIK, s.Source, s.AsOf, s.PTMean, s.PTHigh, s.PTLow, s.PTMedian, s.AnalystCount,
		s.Buy, s.Hold, s.Sell, currency, fetchedAt,
	)
	if err != nil {
		return fmt.Errorf("upsert analyst snapshot: %w", err)
	}
	return nil
}

func (d *DB) ConsensusPeriods(ctx context.Context, cik, source string) ([]models.ConsensusPeriod, error) {
	rows, err := d.QueryContext(ctx, `
		SELECT cik, source, fiscal_year, fiscal_period, period_end, duration, consensus_date,
		       COALESCE(announcement_date,''), revenue_estimate, eps_estimate, eps_actual, eps_surprise,
		       eps_surprise_pct, COALESCE(basis,''), currency, rating_buy_count, rating_hold_count,
		       rating_sell_count, source_url, fetched_at
		FROM analyst_period_consensus
		WHERE cik = ? AND source = ?
		ORDER BY period_end ASC, consensus_date ASC`, cik, source)
	if err != nil {
		return nil, fmt.Errorf("query consensus periods: %w", err)
	}
	defer rows.Close()

	out := make([]models.ConsensusPeriod, 0)
	for rows.Next() {
		var r models.ConsensusPeriod
		if err := rows.Scan(
			&r.CIK, &r.Source, &r.FiscalYear, &r.FiscalPeriod, &r.PeriodEnd, &r.Duration, &r.ConsensusDate,
			&r.AnnouncementDate, &r.RevenueEstimate, &r.EPSEstimate, &r.EPSActual, &r.EPSSurprise,
			&r.EPSSurprisePct, &r.Basis, &r.Currency, &r.RatingBuyCount, &r.RatingHoldCount,
			&r.RatingSellCount, &r.SourceURL, &r.FetchedAt,
		); err != nil {
			return nil, fmt.Errorf("scan consensus period: %w", err)
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (d *DB) ConsensusBefore(ctx context.Context, cik, source, periodEnd, duration, before string) (*models.ConsensusPeriod, error) {
	var r models.ConsensusPeriod
	err := d.QueryRowContext(ctx, `
		SELECT cik, source, fiscal_year, fiscal_period, period_end, duration, consensus_date,
		       COALESCE(announcement_date,''), revenue_estimate, eps_estimate, eps_actual, eps_surprise,
		       eps_surprise_pct, COALESCE(basis,''), currency, rating_buy_count, rating_hold_count,
		       rating_sell_count, source_url, fetched_at
		FROM analyst_period_consensus
		WHERE cik = ? AND source = ? AND period_end = ? AND duration = ? AND consensus_date < ?
		ORDER BY consensus_date DESC
		LIMIT 1`, cik, source, periodEnd, duration, before).
		Scan(
			&r.CIK, &r.Source, &r.FiscalYear, &r.FiscalPeriod, &r.PeriodEnd, &r.Duration, &r.ConsensusDate,
			&r.AnnouncementDate, &r.RevenueEstimate, &r.EPSEstimate, &r.EPSActual, &r.EPSSurprise,
			&r.EPSSurprisePct, &r.Basis, &r.Currency, &r.RatingBuyCount, &r.RatingHoldCount,
			&r.RatingSellCount, &r.SourceURL, &r.FetchedAt,
		)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("consensus before: %w", err)
	}
	return &r, nil
}

func (d *DB) SetAnalystCoverage(ctx context.Context, cik, status, provider, note string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	lastSuccess := ""
	if status == AnalystCoverageOK || status == AnalystCoveragePartial || status == AnalystCoverageNoData {
		lastSuccess = now
	}
	_, err := d.ExecContext(ctx, `
		INSERT INTO analyst_coverage (cik, status, provider, last_attempt_at, last_success_at, note)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(cik) DO UPDATE SET
			status=excluded.status,
			provider=excluded.provider,
			last_attempt_at=excluded.last_attempt_at,
			last_success_at=CASE WHEN excluded.last_success_at != '' THEN excluded.last_success_at ELSE analyst_coverage.last_success_at END,
			note=excluded.note`,
		cik, status, provider, now, lastSuccess, note,
	)
	if err != nil {
		return fmt.Errorf("set analyst coverage: %w", err)
	}
	return nil
}

func (d *DB) AnalystCoverage(ctx context.Context, cik string) (*AnalystCoverageRow, error) {
	var r AnalystCoverageRow
	err := d.QueryRowContext(ctx, `
		SELECT cik, status, provider, COALESCE(last_attempt_at,''), COALESCE(last_success_at,''),
		       COALESCE(next_retry_after,''), COALESCE(note,'')
		FROM analyst_coverage
		WHERE cik = ?`, cik).
		Scan(&r.CIK, &r.Status, &r.Provider, &r.LastAttemptAt, &r.LastSuccessAt, &r.NextRetryAfter, &r.Note)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("analyst coverage: %w", err)
	}
	return &r, nil
}

func nullIfEmpty(v string) any {
	if v == "" {
		return nil
	}
	return v
}
