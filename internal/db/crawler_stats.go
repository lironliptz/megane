package db

import (
	"fmt"
	"time"
)

// CrawlerRunStat is one persisted crawler CLI/session run (aggregated counters).
type CrawlerRunStat struct {
	ID            int64     `json:"id"`
	Source        string    `json:"source"`
	StartedAt     time.Time `json:"started_at"`
	EndedAt       time.Time `json:"ended_at"`
	Navigations   int64     `json:"navigations"`
	HistoryBacks  int64     `json:"history_backs"`
	Clicks        int64     `json:"clicks"`
	FormFills     int64     `json:"form_fills"`
	Downloads     int64     `json:"downloads"`
}

// CrawlerStatsSummary is totals across all stored runs.
type CrawlerStatsSummary struct {
	Runs          int64 `json:"runs"`
	Navigations   int64 `json:"navigations"`
	HistoryBacks  int64 `json:"history_backs"`
	Clicks        int64 `json:"clicks"`
	FormFills     int64 `json:"form_fills"`
	Downloads     int64 `json:"downloads"`
}

// InsertCrawlerRun appends one run's counters (e.g. after a CLI finishes).
func (d *DB) InsertCrawlerRun(source string, startedAt, endedAt time.Time, navigations, historyBacks, clicks, formFills, downloads int64) (int64, error) {
	res, err := d.Exec(`INSERT INTO crawler_run_stats (
		source, started_at, ended_at, navigations, history_backs, clicks, form_fills, downloads
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		source, startedAt.UTC(), endedAt.UTC(), navigations, historyBacks, clicks, formFills, downloads)
	if err != nil {
		return 0, fmt.Errorf("insert crawler_run_stats: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, err
	}
	return id, nil
}

// CrawlerRunSummary returns aggregate totals.
func (d *DB) CrawlerRunSummary() (CrawlerStatsSummary, error) {
	var s CrawlerStatsSummary
	row := d.QueryRow(`SELECT
		COUNT(*),
		COALESCE(SUM(navigations), 0),
		COALESCE(SUM(history_backs), 0),
		COALESCE(SUM(clicks), 0),
		COALESCE(SUM(form_fills), 0),
		COALESCE(SUM(downloads), 0)
		FROM crawler_run_stats`)
	err := row.Scan(&s.Runs, &s.Navigations, &s.HistoryBacks, &s.Clicks, &s.FormFills, &s.Downloads)
	if err != nil {
		return s, fmt.Errorf("crawler run summary: %w", err)
	}
	return s, nil
}

// ListRecentCrawlerRuns returns the most recent rows (for admin UI).
func (d *DB) ListRecentCrawlerRuns(limit int) ([]CrawlerRunStat, error) {
	if limit <= 0 {
		limit = 50
	}
	if limit > 500 {
		limit = 500
	}
	rows, err := d.Query(`SELECT id, source, started_at, ended_at, navigations, history_backs, clicks, form_fills, downloads
		FROM crawler_run_stats ORDER BY started_at DESC LIMIT ?`, limit)
	if err != nil {
		return nil, fmt.Errorf("list crawler runs: %w", err)
	}
	defer rows.Close()

	var out []CrawlerRunStat
	for rows.Next() {
		var r CrawlerRunStat
		if err := rows.Scan(&r.ID, &r.Source, &r.StartedAt, &r.EndedAt,
			&r.Navigations, &r.HistoryBacks, &r.Clicks, &r.FormFills, &r.Downloads); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}
