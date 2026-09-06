package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func strPtr(s string) *string { return &s }

func TestResolveCIK(t *testing.T) {
	tickers := tickerMap{"ADMA": "0001368514"}

	cases := []struct {
		name    string
		peer    SimilarCompany
		wantCIK string
		wantOK  bool
	}{
		{"company_id wins", SimilarCompany{CompanyID: strPtr("1368514")}, "0001368514", true},
		{"null+ticker hits map", SimilarCompany{Ticker: "adma"}, "0001368514", true},
		{"null+no ticker skipped", SimilarCompany{}, "", false},
		{"unknown ticker skipped", SimilarCompany{Ticker: "NOPE"}, "", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			cik, note, ok := resolveCIK(c.peer, tickers)
			if ok != c.wantOK {
				t.Fatalf("ok = %v, want %v (note=%q)", ok, c.wantOK, note)
			}
			if ok && cik != c.wantCIK {
				t.Errorf("cik = %q, want %q", cik, c.wantCIK)
			}
			if !ok && note == "" {
				t.Error("expected a note explaining why the peer was skipped")
			}
		})
	}
}

func TestTickerMapZeroPads(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"0":{"cik_str":320193,"ticker":"AAPL","title":"Apple Inc."}}`))
	}))
	defer srv.Close()

	m, err := loadTickerMap(context.Background(), srv.Client(), srv.URL, "test-agent contact@example.com")
	if err != nil {
		t.Fatalf("loadTickerMap: %v", err)
	}
	if got := m["AAPL"]; got != "0000320193" {
		t.Errorf("m[AAPL] = %q, want 0000320193", got)
	}
	// The map itself stores upper-cased keys only; resolveCIK is what makes a
	// lowercase peer ticker resolve — covered by TestResolveCIK's "null+ticker
	// hits map" case (peer ticker "adma" against map key "ADMA").
}

func TestTickerMapRequiresUserAgent(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	_, err := loadTickerMap(context.Background(), srv.Client(), srv.URL, "")
	if err != ErrNoUserAgent {
		t.Fatalf("err = %v, want ErrNoUserAgent", err)
	}
	if calls != 0 {
		t.Errorf("expected zero HTTP requests when the user agent is empty, got %d", calls)
	}
}

func TestNeedsTickerMap(t *testing.T) {
	if needsTickerMap([]SimilarCompany{{CompanyID: strPtr("1")}}) {
		t.Error("a fully-resolved list should not need the ticker map")
	}
	if !needsTickerMap([]SimilarCompany{{Ticker: "X"}}) {
		t.Error("a null company_id with a ticker should need the map")
	}
	if needsTickerMap([]SimilarCompany{{}}) {
		t.Error("a null company_id with no ticker cannot use the map either way")
	}
}
