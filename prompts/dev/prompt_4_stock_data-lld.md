# LLD: Stock Price Provider & Cache

Implements `prompts/dev/prompt_4_stock_data-hld.md` (D1–D11 are the contract
this LLD builds to; the HLD in turn supersedes `prompt_4_stock_data.txt` on
provider choice, history depth, and chunking — see that HLD's audit).
Triage: **HEAVY** (explicit).

## Scope

- In: `internal/marketdata/{provider.go, yahoo.go, tiingo.go, backfill.go}`,
  `internal/db/stock_prices.go`, `internal/db/db.go` migration append,
  `.env.example` additions.
- Out: prompt 3's Timeline endpoint/UI, a third provider, cron pre-warm, FX
  conversion — unchanged from the HLD.

## Current state — verified, extending the HLD's own audit

The HLD's data audit (Stooq bot-gated, Yahoo works with UA, `range=10y`
returns exactly 2,513 bars with real `adjclose`) is taken as given. Three
more things were verified before writing file-by-file changes, since HEAVY
triage means freezing the chunk-walker's exact stop condition needs real
evidence, not the HLD's provisional "until a chunk returns zero rows":

1. **Chunk request format, straddling the listing date** —
   `period1=2013-01-01&period2=2013-12-31` for `KMDA` (actual listing:
   2013-05-31) returns **HTTP 200**, 149 daily bars, correctly truncated to
   start exactly at `meta.firstTradeDate` (2013-05-31). Yahoo handles a
   partial window transparently — no special-casing needed for a chunk that
   straddles the true start of history.
2. **Chunk request fully before the listing date** —
   `period1=2010-01-01&period2=2010-12-31` returns **HTTP 400**, body
   `{"chart":{"result":null,"error":{"code":"Bad Request","description":"..."}}}`.
   This is a real, structured error response, not a 200-with-empty-array —
   the HLD's "walk backward until a chunk returns zero rows" stop condition
   is wrong as written; a naive walker would treat this as a request
   failure, not a graceful "no more history" signal, unless it explicitly
   parses `chart.error` and treats it as terminal-but-expected.
3. **`meta.firstTradeDate` is present and correct on every successful
   response**, including the most recent (`range=10y`) one. This makes the
   whole "walk until error" approach unnecessary in the common case:
   **read `firstTradeDate` off the first (most recent) chunk and stop once
   a chunk's start reaches it** — one authoritative signal instead of
   walking into a 400.
4. **Next free migration id is 32** (`internal/db/db.go`'s
   `versionedMigrations` currently ends at `{31, ...}`) — confirms the HLD's
   tentative `{32, 33}` numbering is still accurate as of this LLD.
5. **Tiingo's documented endpoint** (from `tiingo.com/documentation/end-of-day`,
   docs only — no key available to live-test, per the HLD's flagged risk):
   `GET https://api.tiingo.com/tiingo/daily/{ticker}/prices?startDate=&endDate=&token=`.
   Response fields: `date, open/high/low/close` (raw) and `adjOpen/adjHigh/
   adjLow/adjClose` (adjusted) — confirms the HLD's D3 assumption
   (`adjClose` field name) is correct. `divCash`/`splitFactor` also present
   but unused here. Auth placement (header vs. `token` query param) is not
   settled by the docs page fetched; implement via `Authorization: Token
   {key}` header (Tiingo's documented standard elsewhere) with the query
   param as a documented fallback if the header approach 401s in practice —
   **this remains unverified against a live key**, matching the HLD's Risk.

## Corrections to the HLD

1. **Chunk-walker stop condition**: use `meta.firstTradeDate` from the
   first successful (most recent) chunk, not "walk until empty/error."
   Still handle a `chart.error` response defensively (e.g. `firstTradeDate`
   absent or zero, or the floor computed from it is somehow still wrong) —
   treat HTTP 400 + a parseable `chart.error` body as a normal stop
   signal, not a retryable failure, and do not spend a retry budget on it.
2. **Yahoo errors are HTTP-status-coded, not 200-with-null** — the walker
   and the plain single-request path both need `resp.StatusCode != 200` as
   a first-class branch (parse `chart.error.description` into the returned
   Go error), separate from the 429 backoff branch (D1).

## File-by-file changes

| File | Change |
|---|---|
| `internal/marketdata/provider.go` | `DailyBar{Date, Open, High, Low, Close, RawClose *float64, Volume int64, Source string}`; `Provider` interface — `DailyRange(ctx, ticker, from, to string) ([]DailyBar, error)`; shared HTTP client construction (timeout, UA header) |
| `internal/marketdata/yahoo.go` | `YahooProvider` — builds `period1`/`period2` from `from`/`to`; parses `chart.result[0]` (`meta.firstTradeDate`, `indicators.quote[0]`, `indicators.adjclose[0].adjclose`, `timestamp`) into `[]DailyBar` (`Close` = adjclose, `RawClose` = quote.close); on HTTP 400 parses `chart.error` and returns a typed `ErrNoData` (see Correction 1); on HTTP 429 returns a typed `ErrRateLimited` for the backfill loop's backoff (D1) |
| `internal/marketdata/tiingo.go` | `TiingoProvider` — `GET /tiingo/daily/{ticker}/prices?startDate=&endDate=`, `Authorization: Token {key}` header (Correction, verified endpoint shape); maps `adjClose→Close`, `close→RawClose` |
| `internal/marketdata/backfill.go` | `Service{Primary, Fallback Provider; DB *db.DB; ChunkYears int; ChunkDelay time.Duration}`; `EnsureCoverage(ctx, cik, ticker)` — chunk walker (see below); `RefreshIfStale(ctx, cik, ticker)` — delta fetch when `StockPriceCoverage(cik).latest` is more than one trading day old |
| `internal/db/stock_prices.go` | `StockPrice` struct (mirrors table columns); `(d *DB) UpsertStockPrices(cik string, rows []StockPrice) error` (batched `INSERT ... ON CONFLICT(cik, trade_date) DO UPDATE`); `(d *DB) StockPrices(cik, from, to string) ([]StockPrice, error)`; `(d *DB) StockPriceCoverage(cik string) (earliest, latest string, count int, err error)` |
| `internal/db/db.go` | append to `versionedMigrations`: `{32, CREATE TABLE stock_prices ...}` (columns per HLD D5, including `raw_close`), `{33, CREATE INDEX idx_stock_prices_cik_date ...}` — ids confirmed free (Current state, point 4) |
| `.env.example` | new section: `MARKET_DATA_PROVIDER=yahoo`, `MARKET_DATA_API_KEY=` (Tiingo), `MARKET_DATA_USER_AGENT=` (default provided in code, overridable), `MARKET_DATA_CHUNK_YEARS=5`, `MARKET_DATA_CHUNK_DELAY=400ms` |

`prompt_3_company_view.txt`'s handler is the only planned caller of
`Service.EnsureCoverage`/`RefreshIfStale` — not built in this prompt.

## Chunk walker — backfill loop (`EnsureCoverage`)

```
today := now().UTC().Date()
chunkEnd := today
firstTradeDate := nil   // learned from the first successful chunk

loop:
    chunkStart := chunkEnd - ChunkYears (5y)
    bars, meta, err := Primary.DailyRange(ctx, ticker, chunkStart, chunkEnd)

    if err is ErrRateLimited:
        backoff (2s -> 8s -> 30s), retry same chunk, up to 3 attempts
        on exhaustion: try Fallback for this chunk, else abort with partial coverage recorded

    if err is ErrNoData (HTTP 400 / chart.error):
        break loop   # normal stop condition, not a failure

    if firstTradeDate == nil and meta.FirstTradeDate != zero:
        firstTradeDate = meta.FirstTradeDate   # Correction: known floor, no need to walk into a 400

    UpsertStockPrices(cik, bars)   # per-chunk upsert — crash-safe partial progress (HLD D10)
    sleep(ChunkDelay)              # politeness

    if firstTradeDate != nil and chunkStart <= firstTradeDate:
        break loop   # reached the known floor — stop before wasting a call past it
    chunkEnd = chunkStart
```

`RefreshIfStale` is the same shape with a single bounded chunk
`[coverage.latest+1, today]` — no walking, since the floor is already known
(existing coverage).

## Tests

- `internal/marketdata/yahoo_test.go` — `httptest.Server` fixtures for: (a)
  the verified straddling-window response (149 bars, truncated at
  `firstTradeDate`), (b) the verified HTTP 400/`chart.error` response
  (asserts `ErrNoData`, not a generic error), (c) a 429 response (asserts
  `ErrRateLimited`). All three fixtures are the *actual* verified response
  bodies from this LLD's audit, not hand-invented JSON.
- `internal/marketdata/backfill_test.go` — chunk walker against a fake
  `Provider` (no network): asserts it stops at `firstTradeDate` without an
  extra call past it (Correction 1), asserts per-chunk upsert order, asserts
  fallback-on-exhausted-retry behavior.
- `internal/db/stock_prices_test.go` — `UpsertStockPrices` idempotency
  (re-run same rows, no duplicate/no error); `StockPriceCoverage` on empty
  vs. populated table.
- `internal/marketdata/tiingo_test.go` — endpoint URL/param construction
  and response-field mapping only (per Current state point 5, no live key
  available); explicitly marked as **unverified against the real API** in a
  test comment, matching the HLD's open risk.

## Build order

1. `internal/db/stock_prices.go` + migration entries (32, 33) + tests —
   storage first, no network dependency
2. `internal/marketdata/provider.go` — types + interface only
3. `internal/marketdata/yahoo.go` + `yahoo_test.go` against the three
   verified fixtures — the only provider with real live verification
4. `internal/marketdata/backfill.go` + `backfill_test.go` (fake `Provider`,
   no network) — chunk walker logic, including the `firstTradeDate` stop
   condition
5. `internal/marketdata/tiingo.go` + `tiingo_test.go` (structural only)
6. `.env.example`
7. Manual smoke: run `EnsureCoverage` against real Yahoo for `KMDA` /
   `0001567529`, confirm `stock_prices` row count and `firstTradeDate`-based
   stop match this LLD's audit (149-in-2013, ~2,513-in-last-10y as a
   sanity cross-check)

## Risks

- **Tiingo remains unverified against a live key** (Current state point 5,
  HLD Risk) — the auth-header assumption could be wrong; first real
  integration test should happen before trusting the fallback path in
  production, not just before merging.
- **`meta.firstTradeDate` could be wrong/stale for some tickers** (e.g. a
  relisting after a gap) — the HTTP 400 defensive branch (Correction 1)
  is the safety net if the floor optimization ever undershoots.
- **Chunk upsert crash mid-walk** leaves partial coverage with a "hole"
  between the last completed chunk and `firstTradeDate` — `StockPriceCoverage`
  as designed (single earliest/latest/count) can't detect an internal gap,
  only the outer bounds. Acceptable for MVP (matches HLD scope) but worth
  flagging: a future `RefreshIfStale` won't backfill a mid-series hole,
  only extend the tail.
- **429 backoff budget (3 retries, 2s→8s→30s) is a guess**, not measured —
  only a handful of requests were made in this audit, not enough to
  characterize Yahoo's real rate ceiling under sustained chunked traffic.

## Done when

Same as the HLD: given CIK `0001567529` / ticker `KMDA`, first backfill
populates `stock_prices` using ≥2 Yahoo chunks and stops at the verified
`firstTradeDate` (2013-05-31) without hitting a 400; a second call makes no
external request; `go test ./internal/marketdata/... ./internal/db/...` and
`make test` pass.
