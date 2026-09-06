package consensus

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestFinnhubNormalize(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/stock/earnings":
			_, _ = w.Write([]byte(`[
				{"actual":0.12,"estimate":0.10,"surprise":0.02,"surprisePercent":20.0,"year":2025,"quarter":2,"period":"2025-06-30"},
				{"actual":0.35,"estimate":null,"surprise":null,"surprisePercent":null,"year":2025,"quarter":0,"period":"2025-12-31"}
			]`))
		case "/api/v1/stock/price-target":
			_, _ = w.Write([]byte(`{"targetHigh":13.0,"targetLow":9.0,"targetMean":11.0,"targetMedian":11.2}`))
		case "/api/v1/stock/recommendation":
			_, _ = w.Write([]byte(`[{"buy":2,"hold":1,"sell":0,"strongBuy":1,"strongSell":0,"period":"2026-09-01"}]`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	f, err := NewFinnhub("k", srv.Client())
	if err != nil {
		t.Fatal(err)
	}
	f.base = srv.URL + "/api/v1"
	rows, err := f.FetchEarnings(context.Background(), "KMDA", time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC), time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 {
		t.Fatalf("rows = %d, want 2", len(rows))
	}
	if rows[0].FiscalPeriod != "Q2" || rows[0].Duration != "P3M" {
		t.Fatalf("row[0] period = %s/%s", rows[0].FiscalPeriod, rows[0].Duration)
	}
	if rows[1].FiscalPeriod != "FY" || rows[1].Duration != "P1Y" {
		t.Fatalf("row[1] period = %s/%s", rows[1].FiscalPeriod, rows[1].Duration)
	}
	if rows[1].EPSEstimate != nil {
		t.Fatal("missing estimate must stay nil")
	}

	s, err := f.FetchSnapshot(context.Background(), "KMDA")
	if err != nil {
		t.Fatal(err)
	}
	if s.Buy != 3 || s.Hold != 1 || s.Sell != 0 {
		t.Fatalf("snapshot ratings = %+v", s)
	}
}

func TestNoKeyIsErrorNotNoData(t *testing.T) {
	var hits atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`[]`))
	}))
	defer srv.Close()

	f := &Finnhub{apiKey: "", base: srv.URL, client: srv.Client()}
	_, err := f.FetchEarnings(context.Background(), "KMDA", time.Now().AddDate(-1, 0, 0), time.Now())
	if !errors.Is(err, ErrNoAPIKey) {
		t.Fatalf("err = %v, want ErrNoAPIKey", err)
	}
	if hits.Load() != 0 {
		t.Fatalf("expected zero HTTP requests, got %d", hits.Load())
	}
}
