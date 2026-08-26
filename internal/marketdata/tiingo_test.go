package marketdata

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// Structural only — unverified live (no Tiingo API key in this repo). These
// tests confirm URL/param construction, auth header, and field mapping
// against Tiingo's PUBLISHED response shape, not against Tiingo itself.
// See prompt_4_stock_data-lld.md's "Current state" item 8 and D2.

// newTestTiingoProvider returns the provider plus a **http.Request — the
// request itself doesn't exist until DailyBars runs, so the caller must
// deref twice (*reqPtr) after calling DailyBars, mirroring newTestProvider's
// *string-for-gotQuery pattern in yahoo_test.go.
func newTestTiingoProvider(t *testing.T, apiKey string, status int, contentType, body string) (*TiingoProvider, **http.Request) {
	t.Helper()
	var gotReq *http.Request
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotReq = r
		if contentType != "" {
			w.Header().Set("Content-Type", contentType)
		}
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return &TiingoProvider{BaseURL: srv.URL + "/", APIKey: apiKey, Client: srv.Client()}, &gotReq
}

const tiingoHappyBody = `[
 {"date":"2019-01-02T00:00:00.000Z","close":10.1,"high":10.5,"low":9.5,"open":10.0,"volume":1000,"adjClose":9.6},
 {"date":"2019-01-03T00:00:00.000Z","close":11.1,"high":11.5,"low":10.5,"open":11.0,"volume":2000,"adjClose":10.5}
]`

func TestTiingoDailyBarsHappyPath(t *testing.T) {
	p, _ := newTestTiingoProvider(t, "test-key", 200, "application/json", tiingoHappyBody)
	q, err := p.DailyBars(context.Background(), "KMDA",
		time.Date(2019, 1, 1, 0, 0, 0, 0, time.UTC), time.Date(2019, 1, 3, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("DailyBars: %v", err)
	}
	if len(q.Bars) != 2 {
		t.Fatalf("got %d bars, want 2", len(q.Bars))
	}
	b := q.Bars[0]
	if b.Date != "2019-01-02" {
		t.Errorf("date = %q, want 2019-01-02 (parsed from RFC3339 timestamp)", b.Date)
	}
	if b.Close != 10.1 || b.Open != 10.0 || b.High != 10.5 || b.Low != 9.5 || b.Volume != 1000 || b.AdjClose != 9.6 {
		t.Errorf("bar = %+v", b)
	}
}

func TestTiingoDailyBarsRequestShape(t *testing.T) {
	p, reqPtr := newTestTiingoProvider(t, "test-key", 200, "application/json", tiingoHappyBody)
	_, err := p.DailyBars(context.Background(), "KMDA",
		time.Date(2019, 1, 1, 0, 0, 0, 0, time.UTC), time.Date(2019, 1, 3, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("DailyBars: %v", err)
	}
	req := *reqPtr
	if got := req.Header.Get("Authorization"); got != "Token test-key" {
		t.Errorf("Authorization header = %q, want %q", got, "Token test-key")
	}
	if !strings.HasSuffix(req.URL.Path, "/KMDA/prices") {
		t.Errorf("path = %q, want .../KMDA/prices", req.URL.Path)
	}
	q := req.URL.Query()
	if q.Get("startDate") != "2019-01-01" || q.Get("endDate") != "2019-01-03" {
		t.Errorf("query = %v, want startDate=2019-01-01 endDate=2019-01-03", q)
	}
}

func TestTiingoDailyBarsNoAPIKeyFailsFast(t *testing.T) {
	p, reqPtr := newTestTiingoProvider(t, "", 200, "application/json", tiingoHappyBody)
	_, err := p.DailyBars(context.Background(), "KMDA", time.Unix(0, 0), time.Unix(1, 0))
	if !errors.Is(err, errTiingoNoAPIKey) {
		t.Fatalf("err = %v, want errTiingoNoAPIKey", err)
	}
	if *reqPtr != nil {
		t.Error("no request should have been made without an API key")
	}
}

func TestTiingoDailyBarsEmptyArrayIsTerminal(t *testing.T) {
	// Documented behavior for a range with no trading days — unverified live.
	p, _ := newTestTiingoProvider(t, "test-key", 200, "application/json", `[]`)
	_, err := p.DailyBars(context.Background(), "KMDA", time.Unix(0, 0), time.Unix(1, 0))
	if !errors.Is(err, ErrNoDataForRange) {
		t.Fatalf("err = %v, want ErrNoDataForRange", err)
	}
}

func TestTiingoDailyBarsNotFound(t *testing.T) {
	body := `{"detail":"Error: You must provide a valid ticker."}`
	p, _ := newTestTiingoProvider(t, "test-key", 404, "application/json", body)
	_, err := p.DailyBars(context.Background(), "NOSUCH", time.Unix(0, 0), time.Unix(1, 0))
	if !errors.Is(err, ErrSymbolNotFound) {
		t.Fatalf("err = %v, want ErrSymbolNotFound", err)
	}
	if !strings.Contains(err.Error(), "valid ticker") {
		t.Errorf("error should carry Tiingo's detail message: %v", err)
	}
}

func TestTiingoDailyBarsAuthRejected(t *testing.T) {
	p, _ := newTestTiingoProvider(t, "bad-key", 401, "application/json", `{"detail":"Error: Please enter a valid Tiingo API token."}`)
	_, err := p.DailyBars(context.Background(), "KMDA", time.Unix(0, 0), time.Unix(1, 0))
	if err == nil || strings.Contains(err.Error(), "not found") {
		t.Fatalf("err = %v, want an auth-rejected error, not ErrSymbolNotFound", err)
	}
}

func TestTiingoDailyBarsRateLimited(t *testing.T) {
	p, _ := newTestTiingoProvider(t, "test-key", 429, "application/json", `{"detail":"rate limited"}`)
	_, err := p.DailyBars(context.Background(), "KMDA", time.Unix(0, 0), time.Unix(1, 0))
	if !errors.Is(err, ErrRateLimited) {
		t.Fatalf("err = %v, want ErrRateLimited", err)
	}
}

func TestTiingoDailyBarsEmptySymbol(t *testing.T) {
	p, _ := newTestTiingoProvider(t, "test-key", 200, "application/json", tiingoHappyBody)
	if _, err := p.DailyBars(context.Background(), "  ", time.Unix(0, 0), time.Unix(1, 0)); !errors.Is(err, ErrSymbolNotFound) {
		t.Errorf("err = %v, want ErrSymbolNotFound", err)
	}
}

func TestTiingoDailyBarsFirstTradeDateAlwaysEmpty(t *testing.T) {
	// The /prices endpoint carries no listing-date metadata (see DailyBars'
	// doc comment) — this is a design fact, not a bug, so pin it in a test.
	p, _ := newTestTiingoProvider(t, "test-key", 200, "application/json", tiingoHappyBody)
	q, err := p.DailyBars(context.Background(), "KMDA", time.Unix(0, 0), time.Unix(1, 0))
	if err != nil {
		t.Fatalf("DailyBars: %v", err)
	}
	if q.FirstTradeDate != "" {
		t.Errorf("FirstTradeDate = %q, want empty (endpoint carries no listing-date metadata)", q.FirstTradeDate)
	}
}

func TestTiingoProviderName(t *testing.T) {
	if got := NewTiingoProvider("k", nil).Name(); got != "tiingo" {
		t.Errorf("Name() = %q, want tiingo", got)
	}
}

func TestNewFromEnvSelectsTiingoByName(t *testing.T) {
	if got := NewFromEnv("tiingo").Name(); got != "tiingo" {
		t.Errorf("NewFromEnv(tiingo).Name() = %q, want tiingo", got)
	}
}
