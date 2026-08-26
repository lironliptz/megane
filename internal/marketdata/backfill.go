package marketdata

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"
)

// Chunked-backfill defaults (prompt_4_stock_data-hld.md D10). Only chunk span
// and delay are env-configurable (MARKET_DATA_CHUNK_YEARS,
// MARKET_DATA_CHUNK_DELAY, wired in cmd/server/main.go); retry count and
// backoff ladder are fixed per the HLD's own table ("(constant)").
const (
	DefaultChunkYears = 5
	DefaultChunkDelay = 400 * time.Millisecond
	DefaultMaxRetries = 3
)

// DefaultBackoff is the retry ladder for a rate-limited chunk.
var DefaultBackoff = []time.Duration{2 * time.Second, 8 * time.Second, 30 * time.Second}

// Backfiller walks a provider backward in fixed-size calendar chunks so a
// full-history backfill never requests everything in one HTTP call — the
// mandatory politeness requirement in the HLD (D10): a single giant response
// risks provider throttling.
type Backfiller struct {
	// Primary is required. Fallback is optional (nil disables it) and is
	// tried once per chunk only after Primary's retries on that chunk are
	// exhausted — not for the whole walk, so a Primary that recovers after
	// one bad chunk keeps being used for the next one.
	Primary  PriceProvider
	Fallback PriceProvider

	ChunkYears int
	ChunkDelay time.Duration
	MaxRetries int
	Backoff    []time.Duration

	// Sleep is injectable so tests run instantly. Defaults to time.Sleep.
	Sleep func(time.Duration)
}

func (b *Backfiller) chunkYears() int {
	if b.ChunkYears > 0 {
		return b.ChunkYears
	}
	return DefaultChunkYears
}

func (b *Backfiller) chunkDelay() time.Duration {
	if b.ChunkDelay > 0 {
		return b.ChunkDelay
	}
	return DefaultChunkDelay
}

func (b *Backfiller) maxRetries() int {
	if b.MaxRetries > 0 {
		return b.MaxRetries
	}
	return DefaultMaxRetries
}

func (b *Backfiller) backoff() []time.Duration {
	if len(b.Backoff) > 0 {
		return b.Backoff
	}
	return DefaultBackoff
}

func (b *Backfiller) sleep() func(time.Duration) {
	if b.Sleep != nil {
		return b.Sleep
	}
	return time.Sleep
}

// Backward fetches [some-earliest-date, to] in chunks of chunkYears() each,
// calling onChunk after every successful, non-empty fetch — so the caller can
// persist crash-safe partial progress before the next chunk is even
// requested (HLD D7 point 3). onChunk's second argument is the name of
// whichever provider actually served that chunk (Primary or Fallback), for
// the caller's stock_prices.source column — a chunk served by Fallback must
// not be recorded under Primary's name.
//
// The walk stops when, in order of preference:
//   - a chunk's start reaches the FirstTradeDate discovered from any earlier
//     chunk's Quote (the preferred, precise stop — Yahoo supplies this);
//   - a provider reports ErrNoDataForRange for a chunk (the defensive
//     fallback stop when FirstTradeDate was never available);
//   - ctx is canceled, or a chunk fails for a reason other than the above
//     (returned as err).
//
// Returns the FirstTradeDate discovered, if any (for the caller's coverage
// bookkeeping), and a non-nil error only for a genuine, non-terminal failure.
func (b *Backfiller) Backward(ctx context.Context, symbol string, to time.Time, onChunk func(q *Quote, source string) error) (string, error) {
	if b.Primary == nil {
		return "", errors.New("marketdata: Backfiller.Primary is required")
	}

	chunkYears := b.chunkYears()
	chunkDelay := b.chunkDelay()
	maxRetries := b.maxRetries()
	backoff := b.backoff()
	sleep := b.sleep()

	var firstTradeDate string
	chunkEnd := to
	for {
		if err := ctx.Err(); err != nil {
			return firstTradeDate, err
		}

		chunkStart := chunkEnd.AddDate(-chunkYears, 0, 0)

		quote, source, terminal, err := b.fetchChunkWithRetry(ctx, symbol, chunkStart, chunkEnd, maxRetries, backoff, sleep)
		if terminal {
			break // walked past the listing date: expected end of history
		}
		if err != nil {
			return firstTradeDate, err
		}

		if firstTradeDate == "" && quote.FirstTradeDate != "" {
			firstTradeDate = quote.FirstTradeDate
		}
		if len(quote.Bars) > 0 {
			if err := onChunk(quote, source); err != nil {
				return firstTradeDate, err
			}
		}

		if firstTradeDate != "" {
			if ft, perr := time.Parse("2006-01-02", firstTradeDate); perr == nil && !chunkStart.After(ft) {
				break
			}
		}

		sleep(chunkDelay)
		chunkEnd = chunkStart.AddDate(0, 0, -1)
	}
	return firstTradeDate, nil
}

// fetchChunkWithRetry fetches one chunk from Primary, retrying on
// ErrRateLimited, then falls back to Fallback (if configured) once Primary's
// retries are exhausted. terminal=true means the walk is done; callers must
// check terminal before err (err is intentionally nil in that case).
func (b *Backfiller) fetchChunkWithRetry(ctx context.Context, symbol string, from, to time.Time,
	maxRetries int, backoff []time.Duration, sleep func(time.Duration)) (quote *Quote, source string, terminal bool, err error) {

	quote, terminal, err = fetchWithBackoff(ctx, b.Primary, symbol, from, to, maxRetries, backoff, sleep)
	if terminal {
		return nil, "", true, nil
	}
	if err == nil {
		return quote, b.Primary.Name(), false, nil
	}
	if b.Fallback == nil {
		return nil, "", false, err
	}

	primaryErr := err
	slog.Warn("marketdata: primary exhausted for chunk, trying fallback",
		"symbol", symbol, "from", from.Format("2006-01-02"), "to", to.Format("2006-01-02"),
		"primary_err", primaryErr, "fallback", b.Fallback.Name())

	quote, terminal, err = fetchWithBackoff(ctx, b.Fallback, symbol, from, to, maxRetries, backoff, sleep)
	if terminal {
		return nil, "", true, nil
	}
	if err != nil {
		return nil, "", false, fmt.Errorf("primary %s: %w; fallback %s: %v",
			b.Primary.Name(), primaryErr, b.Fallback.Name(), err)
	}
	return quote, b.Fallback.Name(), false, nil
}

// fetchWithBackoff retries one provider call on ErrRateLimited only — any
// other error (including ErrNoDataForRange, surfaced via terminal) is not
// retried, since retrying a non-transient failure just delays the inevitable.
func fetchWithBackoff(ctx context.Context, p PriceProvider, symbol string, from, to time.Time,
	maxRetries int, backoff []time.Duration, sleep func(time.Duration)) (quote *Quote, terminal bool, err error) {

	var lastErr error
	for attempt := 0; ; attempt++ {
		q, err := p.DailyBars(ctx, symbol, from, to)
		if err == nil {
			return q, false, nil
		}
		if errors.Is(err, ErrNoDataForRange) {
			return nil, true, nil
		}
		lastErr = err
		if !errors.Is(err, ErrRateLimited) || attempt >= maxRetries {
			return nil, false, lastErr
		}

		wait := backoff[attempt]
		if attempt >= len(backoff) {
			wait = backoff[len(backoff)-1]
		}
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, false, ctxErr
		}
		sleep(wait)
	}
}
