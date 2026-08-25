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

func newTestProvider(t *testing.T, status int, contentType, body string) (*YahooProvider, *string) {
	t.Helper()
	var gotQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.RawQuery
		if contentType != "" {
			w.Header().Set("Content-Type", contentType)
		}
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return &YahooProvider{BaseURL: srv.URL + "/", Client: srv.Client()}, &gotQuery
}

// 1780320600 = 2026-06-01 09:30 America/New_York; gmtoffset -14400.
const happyBody = `{"chart":{"result":[{
 "meta":{"currency":"USD","symbol":"KMDA","exchangeName":"NMS","dataGranularity":"1d","gmtoffset":-14400},
 "timestamp":[1780320600,1780407000],
 "indicators":{
   "quote":[{"open":[10.0,11.0],"high":[10.5,11.5],"low":[9.5,10.5],"close":[10.1,11.1],"volume":[1000,2000]}],
   "adjclose":[{"adjclose":[9.6,10.5]}]
 }}],"error":null}}`

func TestDailyBarsHappyPath(t *testing.T) {
	p, _ := newTestProvider(t, 200, "application/json", happyBody)
	q, err := p.DailyBars(context.Background(), "KMDA",
		time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC), time.Date(2026, 6, 2, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("DailyBars: %v", err)
	}
	if q.Symbol != "KMDA" || q.Currency != "USD" || q.Exchange != "NMS" {
		t.Errorf("meta = %+v", q)
	}
	if len(q.Bars) != 2 {
		t.Fatalf("got %d bars, want 2", len(q.Bars))
	}
	b := q.Bars[0]
	if b.Date != "2026-06-01" {
		t.Errorf("date = %q, want 2026-06-01 (exchange-local)", b.Date)
	}
	if b.Close != 10.1 || b.AdjClose != 9.6 || b.Open != 10.0 || b.High != 10.5 || b.Low != 9.5 || b.Volume != 1000 {
		t.Errorf("bar = %+v", b)
	}
}

// The design's load-bearing guard: never send `range`, always period1/period2.
func TestDailyBarsSendsPeriodsNotRange(t *testing.T) {
	p, query := newTestProvider(t, 200, "application/json", happyBody)
	_, _ = p.DailyBars(context.Background(), "KMDA",
		time.Unix(1000, 0), time.Unix(2000, 0))
	if strings.Contains(*query, "range=") {
		t.Errorf("query %q contains range= — measured to silently return monthly bars", *query)
	}
	for _, want := range []string{"period1=1000", "period2=2000", "interval=1d"} {
		if !strings.Contains(*query, want) {
			t.Errorf("query %q missing %q", *query, want)
		}
	}
}

// Mandatory per HLD §9.2: the only thing standing between a `range=max`
// regression and a chart that looks correct and is wrong.
func TestDailyBarsRejectsNonDailyGranularity(t *testing.T) {
	body := strings.Replace(happyBody, `"dataGranularity":"1d"`, `"dataGranularity":"1mo"`, 1)
	p, _ := newTestProvider(t, 200, "application/json", body)
	_, err := p.DailyBars(context.Background(), "KMDA", time.Unix(0, 0), time.Unix(1, 0))
	if !errors.Is(err, ErrBadGranularity) {
		t.Fatalf("err = %v, want ErrBadGranularity", err)
	}
	if !strings.Contains(err.Error(), "1mo") {
		t.Errorf("error should name the granularity it got: %v", err)
	}
}

// Stooq's failure mode: HTTP 200 carrying an HTML challenge page.
func TestDailyBarsRejectsHTMLWith200(t *testing.T) {
	html := `<!DOCTYPE html><html><head></head><body><noscript>enable JS</noscript></body></html>`
	p, _ := newTestProvider(t, 200, "text/html; charset=utf-8", html)
	_, err := p.DailyBars(context.Background(), "KMDA", time.Unix(0, 0), time.Unix(1, 0))
	if !errors.Is(err, ErrBadResponse) {
		t.Fatalf("err = %v, want ErrBadResponse (must never parse as zero bars)", err)
	}
}

func TestDailyBarsRejectsHTMLWithoutContentType(t *testing.T) {
	p, _ := newTestProvider(t, 200, "", `<!DOCTYPE html><html></html>`)
	_, err := p.DailyBars(context.Background(), "KMDA", time.Unix(0, 0), time.Unix(1, 0))
	if !errors.Is(err, ErrBadResponse) {
		t.Fatalf("err = %v, want ErrBadResponse", err)
	}
}

func TestDailyBarsSymbolNotFound(t *testing.T) {
	body := `{"chart":{"result":null,"error":{"code":"Not Found","description":"No data found, symbol may be delisted"}}}`
	p, _ := newTestProvider(t, 404, "application/json", body)
	_, err := p.DailyBars(context.Background(), "NOSUCH", time.Unix(0, 0), time.Unix(1, 0))
	if !errors.Is(err, ErrSymbolNotFound) {
		t.Fatalf("err = %v, want ErrSymbolNotFound", err)
	}
}

func TestDailyBarsEmptySymbol(t *testing.T) {
	p, _ := newTestProvider(t, 200, "application/json", happyBody)
	if _, err := p.DailyBars(context.Background(), "  ", time.Unix(0, 0), time.Unix(1, 0)); !errors.Is(err, ErrSymbolNotFound) {
		t.Errorf("err = %v, want ErrSymbolNotFound", err)
	}
}

func TestDailyBarsSkipsNullClose(t *testing.T) {
	body := strings.Replace(happyBody, `"close":[10.1,11.1]`, `"close":[null,11.1]`, 1)
	p, _ := newTestProvider(t, 200, "application/json", body)
	q, err := p.DailyBars(context.Background(), "KMDA", time.Unix(0, 0), time.Unix(1, 0))
	if err != nil {
		t.Fatalf("DailyBars: %v", err)
	}
	if len(q.Bars) != 1 || q.Bars[0].Date != "2026-06-02" {
		t.Errorf("bars = %+v, want only the non-null day", q.Bars)
	}
}

func TestDailyBarsMissingResultShape(t *testing.T) {
	for _, body := range []string{
		`{"chart":{"result":[],"error":null}}`,
		`{"chart":{"result":[{"meta":{"dataGranularity":"1d"},"timestamp":[],"indicators":{}}],"error":null}}`,
	} {
		p, _ := newTestProvider(t, 200, "application/json", body)
		if _, err := p.DailyBars(context.Background(), "KMDA", time.Unix(0, 0), time.Unix(1, 0)); !errors.Is(err, ErrBadResponse) {
			t.Errorf("body %q: err = %v, want ErrBadResponse", body, err)
		}
	}
}

func TestGMTOffsetShiftsCalendarDate(t *testing.T) {
	// A UTC+12 listing: its 10:00 local open falls on the PREVIOUS calendar day
	// in UTC, so using the UTC date would file the bar under the wrong day.
	// (For US listings the two agree, which is why this needs a non-US case.)
	body := `{"chart":{"result":[{
	 "meta":{"currency":"NZD","symbol":"X","exchangeName":"NZE","dataGranularity":"1d","gmtoffset":43200},
	 "timestamp":[1780351200],
	 "indicators":{"quote":[{"close":[1.0],"open":[1.0],"high":[1.0],"low":[1.0],"volume":[1]}],"adjclose":[{"adjclose":[1.0]}]}
	}],"error":null}}`
	p, _ := newTestProvider(t, 200, "application/json", body)
	q, err := p.DailyBars(context.Background(), "X", time.Unix(0, 0), time.Unix(1, 0))
	if err != nil {
		t.Fatalf("DailyBars: %v", err)
	}
	utcDate := time.Unix(1780351200, 0).UTC().Format("2006-01-02")
	if utcDate != "2026-06-01" {
		t.Fatalf("fixture drifted: UTC date = %s", utcDate)
	}
	if q.Bars[0].Date != "2026-06-02" {
		t.Errorf("date = %q, want 2026-06-02 (exchange-local); got the UTC date means gmtoffset was ignored", q.Bars[0].Date)
	}
}

func TestProviderName(t *testing.T) {
	if got := NewYahooProvider(nil).Name(); got != "yahoo" {
		t.Errorf("Name() = %q", got)
	}
	if NewFromEnv("").Name() != "yahoo" || NewFromEnv("YAHOO").Name() != "yahoo" {
		t.Error("NewFromEnv should default to yahoo")
	}
}
