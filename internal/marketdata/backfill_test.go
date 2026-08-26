package marketdata

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"
)

// fakeProvider scripts a sequence of responses per call, in order, so a test
// can assert exactly which chunk windows the walker requested and how it
// reacted to each response — no network involved.
type fakeProvider struct {
	name  string
	calls []struct{ from, to time.Time }
	// script[i] answers the i-th call. Extra calls beyond len(script) fail
	// the test loudly rather than reusing the last entry silently.
	script []func() (*Quote, error)
}

func (f *fakeProvider) Name() string { return f.name }

func (f *fakeProvider) DailyBars(_ context.Context, _ string, from, to time.Time) (*Quote, error) {
	i := len(f.calls)
	f.calls = append(f.calls, struct{ from, to time.Time }{from, to})
	if i >= len(f.script) {
		return nil, fmt.Errorf("fakeProvider %s: unscripted call #%d", f.name, i)
	}
	return f.script[i]()
}

func ok(q *Quote) func() (*Quote, error) { return func() (*Quote, error) { return q, nil } }
func fails(err error) func() (*Quote, error) {
	return func() (*Quote, error) { return nil, err }
}

func noSleep(time.Duration) {} // tests never wait for real

func d(s string) time.Time {
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		panic(err)
	}
	return t
}

// The preferred stop condition: a chunk's Quote carries FirstTradeDate, and
// the walker stops as soon as a chunk's start reaches it — without an extra
// call past that point.
func TestBackwardStopsAtFirstTradeDate(t *testing.T) {
	primary := &fakeProvider{name: "yahoo", script: []func() (*Quote, error){
		ok(&Quote{Bars: []Bar{{Date: "2024-06-01", Close: 1}}, FirstTradeDate: "2022-01-01"}),
		ok(&Quote{Bars: []Bar{{Date: "2022-01-01", Close: 1}}, FirstTradeDate: "2022-01-01"}),
	}}
	bf := &Backfiller{Primary: primary, ChunkYears: 2, ChunkDelay: 0, Sleep: noSleep}

	var chunks int
	ft, err := bf.Backward(context.Background(), "X", d("2024-06-01"), func(q *Quote, _ string) error {
		chunks++
		return nil
	})
	if err != nil {
		t.Fatalf("Backward: %v", err)
	}
	if ft != "2022-01-01" {
		t.Errorf("firstTradeDate = %q, want 2022-01-01", ft)
	}
	if chunks != 2 {
		t.Errorf("onChunk called %d times, want 2 (must stop once chunkStart reaches firstTradeDate)", chunks)
	}
	if len(primary.calls) != 2 {
		t.Fatalf("primary called %d times, want exactly 2 (no extra call past firstTradeDate)", len(primary.calls))
	}
	// The second (and final) chunk's start must be on or before the
	// discovered firstTradeDate — that is what triggered the stop.
	if primary.calls[1].from.After(d("2022-01-01")) {
		t.Errorf("second chunk start %v is after firstTradeDate, stop condition mis-evaluated",
			primary.calls[1].from)
	}
}

// The defensive stop when FirstTradeDate is never available (e.g. an
// all-Tiingo walk): ErrNoDataForRange from the provider ends the walk
// cleanly, not as an error.
func TestBackwardStopsOnTerminalNoData(t *testing.T) {
	primary := &fakeProvider{name: "yahoo", script: []func() (*Quote, error){
		ok(&Quote{Bars: []Bar{{Date: "2024-06-01", Close: 1}}}), // no FirstTradeDate this time
		fails(ErrNoDataForRange),
	}}
	bf := &Backfiller{Primary: primary, ChunkYears: 2, ChunkDelay: 0, Sleep: noSleep}

	var chunks int
	ft, err := bf.Backward(context.Background(), "X", d("2024-06-01"), func(q *Quote, _ string) error {
		chunks++
		return nil
	})
	if err != nil {
		t.Fatalf("Backward: %v, want nil (terminal is not an error)", err)
	}
	if ft != "" {
		t.Errorf("firstTradeDate = %q, want empty (never supplied)", ft)
	}
	if chunks != 1 {
		t.Errorf("onChunk called %d times, want 1 (the terminal chunk must not be persisted)", chunks)
	}
}

// A rate-limited chunk retries with backoff and succeeds — the walker must
// not treat a transient 429 as terminal or as a hard failure. The first
// chunk's FirstTradeDate deliberately lands strictly BEFORE the second
// chunk's window, so the walk continues into a second (immediately
// successful) chunk rather than stopping after the first.
func TestBackwardRetriesRateLimitThenSucceeds(t *testing.T) {
	primary := &fakeProvider{name: "yahoo", script: []func() (*Quote, error){
		fails(ErrRateLimited),
		fails(ErrRateLimited),
		ok(&Quote{Bars: []Bar{{Date: "2024-06-01", Close: 1}}, FirstTradeDate: "2023-01-01"}),
		ok(&Quote{Bars: []Bar{{Date: "2023-01-01", Close: 1}}, FirstTradeDate: "2023-01-01"}),
	}}
	var slept []time.Duration
	bf := &Backfiller{
		Primary: primary, ChunkYears: 1, ChunkDelay: 0, MaxRetries: 3,
		Backoff: []time.Duration{time.Millisecond, 2 * time.Millisecond, 3 * time.Millisecond},
		Sleep:   func(d time.Duration) { slept = append(slept, d) },
	}
	_, err := bf.Backward(context.Background(), "X", d("2024-06-01"), func(q *Quote, _ string) error { return nil })
	if err != nil {
		t.Fatalf("Backward: %v", err)
	}
	if len(primary.calls) != 4 {
		t.Fatalf("primary called %d times, want 4 (2 retries + success for chunk 1, then chunk 2)", len(primary.calls))
	}
	if len(slept) < 2 || slept[0] != time.Millisecond || slept[1] != 2*time.Millisecond {
		t.Errorf("backoff sequence = %v, want the ladder honored in order", slept)
	}
}

// Retries exhausted on Primary falls back to Fallback for that one chunk,
// then Primary is used again for the next chunk.
func TestBackwardFallsBackAfterRetriesExhausted(t *testing.T) {
	primary := &fakeProvider{name: "yahoo", script: []func() (*Quote, error){
		fails(ErrRateLimited), fails(ErrRateLimited), fails(ErrRateLimited), // exhausts MaxRetries=2 (3 attempts total)
		ok(&Quote{Bars: []Bar{{Date: "2024-01-01", Close: 1}}, FirstTradeDate: "2023-06-01"}), // second chunk: primary recovers
	}}
	fallback := &fakeProvider{name: "tiingo", script: []func() (*Quote, error){
		ok(&Quote{Bars: []Bar{{Date: "2024-06-01", Close: 2}}}),
	}}
	bf := &Backfiller{
		Primary: primary, Fallback: fallback, ChunkYears: 1, MaxRetries: 2,
		Backoff: []time.Duration{time.Millisecond, time.Millisecond}, Sleep: noSleep,
	}

	var dates, sources []string
	_, err := bf.Backward(context.Background(), "X", d("2024-06-01"), func(q *Quote, source string) error {
		if len(q.Bars) > 0 {
			dates = append(dates, q.Bars[0].Date)
			sources = append(sources, source)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("Backward: %v", err)
	}
	if len(fallback.calls) != 1 {
		t.Fatalf("fallback called %d times, want exactly 1 (only for the exhausted chunk)", len(fallback.calls))
	}
	if len(primary.calls) != 4 {
		t.Fatalf("primary called %d times, want 4 (3 exhausted retries + 1 successful next chunk)", len(primary.calls))
	}
	if len(dates) != 2 || dates[0] != "2024-06-01" || dates[1] != "2024-01-01" {
		t.Errorf("onChunk order/content = %v, want fallback chunk first then primary's next chunk", dates)
	}
	if len(sources) != 2 || sources[0] != "tiingo" || sources[1] != "yahoo" {
		t.Errorf("onChunk source = %v, want [tiingo yahoo] — the fallback-served chunk must not be "+
			"attributed to the primary", sources)
	}
}

// A non-retryable primary failure with no fallback configured surfaces as an
// error — the walker must not silently stop as if it reached history's start.
func TestBackwardNonRetryableFailureWithNoFallback(t *testing.T) {
	boom := errors.New("marketdata: unexpected response shape")
	primary := &fakeProvider{name: "yahoo", script: []func() (*Quote, error){fails(boom)}}
	bf := &Backfiller{Primary: primary, ChunkYears: 5, Sleep: noSleep}
	_, err := bf.Backward(context.Background(), "X", d("2024-06-01"), func(q *Quote, _ string) error { return nil })
	if err == nil {
		t.Fatal("Backward: want an error, not a silent stop")
	}
}

// A crash-safe caller upserts per chunk; onChunk's own failure (e.g. a DB
// write error) must abort the walk rather than continuing to fetch chunks
// that will never be persisted.
func TestBackwardStopsIfOnChunkFails(t *testing.T) {
	primary := &fakeProvider{name: "yahoo", script: []func() (*Quote, error){
		ok(&Quote{Bars: []Bar{{Date: "2024-06-01", Close: 1}}}),
		ok(&Quote{Bars: []Bar{{Date: "2019-06-01", Close: 1}}}),
	}}
	bf := &Backfiller{Primary: primary, ChunkYears: 1, Sleep: noSleep}
	dbErr := errors.New("db: write failed")
	_, err := bf.Backward(context.Background(), "X", d("2024-06-01"), func(q *Quote, _ string) error { return dbErr })
	if !errors.Is(err, dbErr) {
		t.Fatalf("err = %v, want dbErr", err)
	}
	if len(primary.calls) != 1 {
		t.Errorf("primary called %d times, want 1 (must not fetch further chunks after onChunk fails)", len(primary.calls))
	}
}

// Volume must survive the walker untouched — it is opaque cargo here, but
// this is the seam prompt_4's acceptance criteria singles out.
func TestBackwardPassesVolumeThrough(t *testing.T) {
	primary := &fakeProvider{name: "yahoo", script: []func() (*Quote, error){
		ok(&Quote{Bars: []Bar{{Date: "2024-06-01", Close: 1, Volume: 102400}}, FirstTradeDate: "2024-06-01"}),
	}}
	bf := &Backfiller{Primary: primary, ChunkYears: 5, Sleep: noSleep}
	var gotVolume int64
	_, err := bf.Backward(context.Background(), "X", d("2024-06-01"), func(q *Quote, _ string) error {
		gotVolume = q.Bars[0].Volume
		return nil
	})
	if err != nil {
		t.Fatalf("Backward: %v", err)
	}
	if gotVolume != 102400 {
		t.Errorf("volume = %d, want 102400 (passed through unmodified)", gotVolume)
	}
}

func TestBackwardRequiresPrimary(t *testing.T) {
	bf := &Backfiller{Sleep: noSleep}
	_, err := bf.Backward(context.Background(), "X", d("2024-06-01"), func(q *Quote, _ string) error { return nil })
	if err == nil {
		t.Fatal("Backward with nil Primary must error, not panic or no-op")
	}
}

func TestBackwardRespectsContextCancellation(t *testing.T) {
	primary := &fakeProvider{name: "yahoo", script: []func() (*Quote, error){
		ok(&Quote{Bars: []Bar{{Date: "2024-06-01", Close: 1}}}),
	}}
	ctx, cancel := context.WithCancel(context.Background())
	bf := &Backfiller{Primary: primary, ChunkYears: 1, Sleep: noSleep}
	_, err := bf.Backward(ctx, "X", d("2024-06-01"), func(q *Quote, _ string) error {
		cancel() // simulate the caller's context dying mid-walk
		return nil
	})
	if err == nil {
		t.Fatal("Backward: want context.Canceled surfaced, not a silent stop")
	}
}
