package db

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"megane/internal/models"
)

func newConsensusDB(t *testing.T) *DB {
	t.Helper()
	database, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
	return database
}

func floatPtr(v float64) *float64 { return &v }

func TestMigrationsV34PlusApplied(t *testing.T) {
	d := newConsensusDB(t)
	for _, v := range []int{34, 35, 36, 37} {
		var got int
		if err := d.QueryRow(`SELECT 1 FROM schema_migrations WHERE version = ?`, v).Scan(&got); err != nil {
			t.Fatalf("migration v%d not recorded: %v", v, err)
		}
	}
}

func TestUpsertIsIdempotent(t *testing.T) {
	d := newConsensusDB(t)
	ctx := context.Background()
	rows := []models.ConsensusPeriod{
		{
			CIK: "0001567529", Source: "finnhub", FiscalYear: "2025", FiscalPeriod: "Q3",
			PeriodEnd: "2025-09-30", Duration: "P3M", ConsensusDate: "2025-09-29",
			EPSEstimate: floatPtr(0.09), EPSActual: floatPtr(0.10), EPSSurprise: floatPtr(0.01),
			Currency: "USD", FetchedAt: time.Now().UTC(),
		},
	}
	if err := d.UpsertConsensusPeriods(ctx, rows); err != nil {
		t.Fatal(err)
	}
	updated := rows
	updated[0].EPSEstimate = floatPtr(0.11)
	if err := d.UpsertConsensusPeriods(ctx, updated); err != nil {
		t.Fatal(err)
	}
	got, err := d.ConsensusPeriods(ctx, "0001567529", "finnhub")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("rows = %d, want 1", len(got))
	}
	if got[0].EPSEstimate == nil || *got[0].EPSEstimate != 0.11 {
		t.Fatalf("updated estimate missing: %+v", got[0].EPSEstimate)
	}
}

func TestSourcesDoNotCollide(t *testing.T) {
	d := newConsensusDB(t)
	ctx := context.Background()
	a := models.ConsensusPeriod{
		CIK: "0001567529", Source: "finnhub", FiscalYear: "2025", FiscalPeriod: "Q3",
		PeriodEnd: "2025-09-30", Duration: "P3M", ConsensusDate: "2025-09-29", Currency: "USD",
	}
	b := a
	b.Source = "nasdaq_api"
	if err := d.UpsertConsensusPeriods(ctx, []models.ConsensusPeriod{a, b}); err != nil {
		t.Fatal(err)
	}
	fh, err := d.ConsensusPeriods(ctx, "0001567529", "finnhub")
	if err != nil || len(fh) != 1 {
		t.Fatalf("finnhub rows=%d err=%v", len(fh), err)
	}
	nd, err := d.ConsensusPeriods(ctx, "0001567529", "nasdaq_api")
	if err != nil || len(nd) != 1 {
		t.Fatalf("nasdaq rows=%d err=%v", len(nd), err)
	}
}

func TestConsensusBeforeExcludesLookahead(t *testing.T) {
	d := newConsensusDB(t)
	ctx := context.Background()
	rows := []models.ConsensusPeriod{
		{
			CIK: "0001567529", Source: "finnhub", FiscalYear: "2025", FiscalPeriod: "Q3",
			PeriodEnd: "2025-09-30", Duration: "P3M", ConsensusDate: "2025-10-01", Currency: "USD",
		},
		{
			CIK: "0001567529", Source: "finnhub", FiscalYear: "2025", FiscalPeriod: "Q3",
			PeriodEnd: "2025-09-30", Duration: "P3M", ConsensusDate: "2025-09-20", Currency: "USD",
		},
	}
	if err := d.UpsertConsensusPeriods(ctx, rows); err != nil {
		t.Fatal(err)
	}
	got, err := d.ConsensusBefore(ctx, "0001567529", "finnhub", "2025-09-30", "P3M", "2025-09-30")
	if err != nil {
		t.Fatal(err)
	}
	if got == nil || got.ConsensusDate != "2025-09-20" {
		t.Fatalf("got %#v", got)
	}
}
