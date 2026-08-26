# LLD: Stock Price Provider & Cache (OHLCV)

Implements `prompts/dev/prompt_4_stock_data-hld.md` (D1–D12). The HLD and
`prompt_4_stock_data.txt` are aligned on **volume**, Yahoo primary, Tiingo fallback, and
chunked full-history backfill.

Triage: **HEAVY** (explicit).

**Do not implement from this LLD until you read § "As-built baseline"** — most of the storage
and Yahoo path already ships under `prompt_3_company_view`; this LLD describes extensions and
the canonical target state.

## Scope

- **In:** OHLCV end-to-end (provider → SQLite → Timeline JSON), chunked backfill completion,
  Tiingo fallback, env wiring, tests.
- **Out:** Chart.js volume rendering (prompt 3 UI), third provider, cron pre-warm, FX
  conversion — unchanged from the HLD.

## As-built baseline (do not re-create)

Already shipped — extend in place:

| File | Role |
|------|------|
| `internal/marketdata/provider.go` | `Bar`, `Quote`, `PriceProvider`, sentinel errors |
| `internal/marketdata/yahoo.go` + `yahoo_test.go` | Yahoo provider; parses **volume** + adjclose; rejects non-`1d` granularity |
| `internal/db/stock_prices.go` + `stock_prices_test.go` | `UpsertStockPrices`, `StockPrices`, coverage helpers; **`Volume` on `PriceBar`/`PriceRow`** |
| `internal/db/db.go` migrations `{32, 33}` | `stock_prices` (incl. `volume`, `adj_close`) + `stock_price_coverage` |
| `internal/companyview/service.go` | Window-scoped `ensureCoverage` + `pricesFor` |
| `internal/companyview/timeline.go` | `PricePoint{ Close, AdjClose, Volume }` on wire |
| `cmd/server/main.go` | Wires `marketdata.NewFromEnv`, `companyview.NewService` |

Record: `prompt_4_stock_data-implementation.md`.

**Do not add** parallel types (`DailyBar`, `Provider.DailyRange`) or a second orchestrator that
conflicts with `companyview.Service`.

## Current state — verified audit (extends HLD §2)

1. **Chunk straddling listing date** — `period1=2013-01-01&period2=2013-12-31` for `KMDA`
   returns **149** daily bars starting at `meta.firstTradeDate` (2013-05-31).
2. **Chunk fully before listing** — `period1=2010-01-01&period2=2010-12-31` returns **HTTP 400**
   with `chart.error` — treat as terminal stop, not retryable failure.
3. **`meta.firstTradeDate`** on every successful response — preferred backfill floor (Correction
   to naive "walk until empty").
4. **`range=max&interval=1d`** — silently returns **monthly** bars; **never use**. Shipped
   `yahoo.go` enforces `dataGranularity == "1d"`.
5. **Volume on Yahoo** — `indicators.quote[0].volume[i]` aligned with `timestamp[i]`; shipped
   parser maps null → `0`; fixture test asserts `Volume == 1000` in `yahoo_test.go`.
6. **Adjusted vs raw close** — full KMDA series: 3,221/3,329 bars differ; schema stores both
   `close` and `adj_close` (not idea-file `raw_close` naming).
7. **Migration ids 32–33** — consumed by shipped tables; next migration id is **34** for any
   new schema change.
8. **Tiingo endpoint** (docs only): `GET /tiingo/daily/{ticker}/prices?startDate=&endDate=`;
   fields include `adjClose`, `close`, `volume` — auth via `Authorization: Token {key}` header
   (unverified live).

## Corrections to early drafts (keep)

1. **Chunk-walker stop:** use `meta.firstTradeDate` from the first successful chunk; still handle
   HTTP 400 + `chart.error` defensively when `firstTradeDate` is absent.
2. **Yahoo errors:** branch on `resp.StatusCode`, parse `chart.error`, separate 429 backoff from
   terminal 400.
3. **Schema:** `adj_close` column (shipped), not `raw_close`.
4. **Coverage:** dedicated `stock_price_coverage` table with status + `ShouldRetry`, not
   `StockPriceCoverage() → (earliest, latest, count)` alone.

## Remaining work — file-by-file

| File | Change |
|------|--------|
| `internal/marketdata/yahoo.go` | **Optional:** expose `FirstTradeDate` (or meta struct) from successful responses for the chunk walker; volume parsing **already shipped** |
| `internal/marketdata/tiingo.go` | **New** — `TiingoProvider` implementing `PriceProvider`; map `adjClose→Bar.AdjClose`, `close→Bar.Close`, `volume→Bar.Volume`; register in `NewFromEnv` when `MARKET_DATA_PROVIDER=tiingo` or as fallback |
| `internal/marketdata/backfill.go` | **New** — chunk walker + retry/backoff; **or** move equivalent logic into `companyview.Service` if keeping one orchestrator |
| `internal/companyview/service.go` | **Extend** `ensureCoverage` — replace single window fetch with chunked full backfill when coverage empty; delta `[latest+1, today]` when stale; wire Tiingo fallback on Yahoo exhaustion |
| `internal/db/stock_prices.go` | **No schema change** for volume; verify upsert path already writes `volume` (shipped) |
| `internal/db/db.go` | **No change** unless new columns; ids 32–33 already applied |
| `.env.example` | Add `MARKET_DATA_USER_AGENT`, `MARKET_DATA_CHUNK_YEARS`, `MARKET_DATA_CHUNK_DELAY`; document Tiingo key |

`prompt_3_company_view.txt`'s timeline handler remains the caller — no new HTTP routes in this prompt.

## Volume — parse and persist paths

### Yahoo (`yahoo.go` — shipped)

```
for i, ts := range timestamp:
    bar.Close  = quote.close[i]      // raw
    bar.AdjClose = adjclose[i]       // when present
    bar.Volume = quote.volume[i] ?? 0
    bar.Date = exchange-local YYYY-MM-DD from ts + gmtOffset
```

Skip bar when `close` is null (halted day). Do not skip when only volume is null (store 0).

### Tiingo (`tiingo.go` — planned)

Map JSON array elements: `date`, `open/high/low/close`, `adjClose`, `volume` → `Bar`.
Use adjusted fields for chart canonical series same as Yahoo path.

### DB upsert (`stock_prices.go` — shipped)

```sql
INSERT INTO stock_prices (..., close, adj_close, volume, ...)
ON CONFLICT(cik, trade_date) DO UPDATE SET
  ..., volume = excluded.volume, ...
```

### Timeline wire (`timeline.go` — shipped)

```go
PricePoint{ Date, Close, AdjClose, Volume int64 `json:"volume,omitempty"` }
```

Prompt 3 chart should consume `volume` for a lower-band bar series (separate task).

## Chunk walker — backfill loop (planned; not shipped)

Implement on `companyview.Service` or `marketdata/backfill.go`:

```
today := now().UTC().Date()
chunkEnd := today
firstTradeDate := zero   // from first successful chunk meta

loop:
    chunkStart := chunkEnd - ChunkYears (5y)
    quote, err := provider.DailyBars(ctx, symbol, chunkStart, chunkEnd)

    if err is rate-limited:
        backoff (2s -> 8s -> 30s), retry same chunk, up to 3 attempts
        on exhaustion: try Tiingo fallback for this chunk, else abort with partial coverage

    if err is terminal (HTTP 400 / chart.error before firstTradeDate known):
        break loop

    if firstTradeDate.IsZero() && meta.FirstTradeDate != zero:
        firstTradeDate = meta.FirstTradeDate

    UpsertStockPrices(cik, quote.Bars)   // includes Volume on each bar
    sleep(ChunkDelay)

    if !firstTradeDate.IsZero() && chunkStart <= firstTradeDate:
        break loop
    chunkEnd = chunkStart - 1 day
```

`RefreshIfStale` (delta): single bounded fetch `[coverage.latest+1, today]` — no walking.

**As-built today:** `ensureCoverage` performs **one** `DailyBars(from, to)` for the Timeline
window (+5/−1 day pad), not this loop. Completing the loop is the main prompt 4 implementation gap.

## Provider interface (canonical — use as-built)

```go
type Bar struct {
    Date string
    Open, High, Low, Close, AdjClose float64
    Volume int64
}

type PriceProvider interface {
    DailyBars(ctx context.Context, symbol string, from, to time.Time) (*Quote, error)
    Name() string
}
```

Optional extension for chunk walker:

```go
type MetaProvider interface {
    PriceProvider
    // DailyBarsWithMeta returns quote + firstTradeDate when available
}
```

Prefer returning meta on `Quote` if the interface change is acceptable.

## Tests

### Already shipped (keep green)

- `yahoo_test.go` — volume + adjclose mapping, granularity rejection, HTML content-type guard
- `stock_prices_test.go` — upsert idempotency, coverage row updates

### Add when implementing remaining work

- `yahoo_test.go` — fixture with **systematically missing volume** (assert 0 + optional log hook)
- `backfill_test.go` or `companyview/service_test.go` — chunk walker against fake
  `PriceProvider`: stops at `firstTradeDate` without extra call past it; per-chunk upsert order;
  fallback-on-exhausted-retry; **assert volume passed through upsert**
- `tiingo_test.go` — URL/param construction + field mapping only (mark **unverified live**)
- Integration smoke: `EnsureCoverage` for `KMDA` / `0001567529` — row count spans
  2013-05-31 → today; spot-check non-zero `volume` on a recent trading day; timeline curl shows
  `"volume"` in JSON

## Build order (remaining implementation)

1. Confirm as-built tests green (`go test ./internal/marketdata/... ./internal/db/... ./internal/companyview/...`)
2. `.env.example` — chunk + UA env vars
3. Extend Yahoo response handling if `firstTradeDate` needed on `Quote` / meta return path
4. Chunk walker in `companyview/service.go` or `marketdata/backfill.go` + unit tests
5. `tiingo.go` + structural tests + `NewFromEnv` wiring
6. Delta refresh path when `coverage.latest` stale
7. Smoke: timeline curl for `0001567529` — verify `volume` + coverage span ≥ listing history
8. Update `prompt_4_stock_data-implementation.md`

## Risks

- **Tiingo unverified live** — auth header assumption may 401; test before production fallback
- **`firstTradeDate` wrong after relisting** — HTTP 400 defensive branch remains safety net
- **Partial chunk walk crash** — outer `earliest`/`latest` on coverage table cannot detect mid-series
  holes; acceptable MVP; future `force` refresh
- **429 backoff budget unmeasured** under sustained chunked traffic
- **Volume UI gap** — data may be present while chart shows price-only until prompt 3 UI work

## Done when

Same as HLD §8, with volume explicitly checked:

- Yahoo (and Tiingo fallback test) return bars with **non-zero volume** on typical KMDA days
- Chunked backfill stores full listing history with **volume column populated**
- Timeline JSON includes **`volume`** on each price point in range
- No duplicate provider/orchestrator types; as-built API extended, not replaced
- `make test` passes
