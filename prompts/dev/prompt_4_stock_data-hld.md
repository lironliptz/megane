# HLD: Stock Price Provider & Cache

Implements `prompts/dev/prompt_4_stock_data.txt` (this HLD **supersedes** the idea file on
provider choice, history depth, and chunking). Feeds `prompts/dev/prompt_3_company_view.txt`
(Timeline tab) — that prompt's handler consumes this layer only; it does not call Yahoo/Tiingo.

Triage: **HEAVY** — new package, new table, external dependency with unverified reliability;
live audit below changed the original Stooq-first design.

Scope: contract + decisions. File-by-file changes belong in the LLD.

---

## 1. Objective

Given a company's CIK, fetch and cache **all available daily price history** (not capped at 10
years) into SQLite so the Timeline tab reads locally with no per-view external calls.

Requirements:

- **Full depth** — backfill as far back as the provider offers for the ticker; if that exceeds
  10 years, store it all. Align chart "All" window with cached coverage, not an arbitrary decade cap.
- **Chunked, polite fetching** — split history into time windows so a single giant response does
  not trigger 429s or provider throttling; pause between chunks.
- **Adjusted close** — store split/dividend-adjusted prices as the canonical `close`.
- **Idempotent cache** — `(cik, trade_date)` primary key; delta refresh for new trading days only.
- **Free primary path where possible** — Yahoo (no key); Tiingo keyed fallback (see audit).

---

## 2. Data audit (measured, not assumed)

Live-tested 2026-08-25 against `KMDA` (Kamada Ltd., CIK `0001567529`, NASDAQ listing in corpus).

### Stooq — `GET https://stooq.com/q/d/l/?s=kmda.us&i=d`

Returns HTML with a JavaScript SHA-256 proof-of-work gate, not CSV. Plain `net/http` cannot
pass it. **Stooq is not usable** as a simple HTTP client source — dropped from this design.

### Yahoo — `GET https://query1.finance.yahoo.com/v8/finance/chart/KMDA?...`

With a realistic `User-Agent`: JSON, no bot wall.

| Probe | Result |
|-------|--------|
| `range=10y&interval=1d` | **2,513** daily bars, 2016-08-25 → 2026-08-25 — confirms `adjclose` exists and differs from raw close (e.g. 4.37 vs 4.12) |
| No `User-Agent` | **HTTP 429** — UA is mandatory |
| `range=max&interval=1d` | Not live-tested in this audit — treat as **one possible chunk strategy**, not the default single call (see D10) |

**Implication:** Yahoo works but `range=10y` **is** a 10-year cap. Full history requires
**multiple ranged requests** (`period1` / `period2` unix bounds), not one `range=max` blob unless
the LLD proves `max` is stable at scale.

### Tiingo (fallback — not live-tested here)

Documented EOD API, free tier with API key. Limits (req/day, symbol count) must be verified
with a real key before production trust. Added as D2 fallback because Stooq failed and Yahoo
is unofficial.

### Filing vs price coverage (Kamada)

| Series | Earliest on disk |
|--------|------------------|
| SEC filings (`fileDB`) | **2016-01-06** |
| Yahoo `range=10y` prices | **2016-08-25** |

Prices may start **after** filings (IPO/listing lag). Timeline overlays filing events on price
series — store all available prices; do not fail backfill when filings predate first trade.

### What the audit changes in the spec

1. **Yahoo primary, Stooq removed** (bot-check).
2. **Tiingo keyed fallback** replaces Stooq — reverses idea file's "no keyed provider speculatively".
3. **`range=10y` is not the backfill model** — chunked `period1`/`period2` windows instead (D10).
4. **`User-Agent` required** on every Yahoo request; 429 → retry with backoff, not opaque 500.

---

## 3. Architecture decisions

### D1 — Yahoo chart API is primary

Verified: works with UA, returns `adjclose`. Every request sets `MARKET_DATA_USER_AGENT` (env,
default a descriptive app string). Handle 429 as retryable with exponential backoff per chunk.

### D2 — Tiingo is the fallback

Real second source when Yahoo returns empty, 429 exhaustion, or parse failure. Requires
`MARKET_DATA_API_KEY` in `.env.example`. Same chunked backfill semantics as Yahoo.

### D3 — Adjusted close is the stored `close`; raw OHLC kept when available

Yahoo: `indicators.adjclose[0].adjclose` → `close`; `quote[0].close` → optional `raw_close`
column (see schema). Tiingo: `adjClose` → `close`. `source` column records `yahoo` | `tiingo`.

### D4 — Storage extends `internal/db`, no new storage package

One `db` package, `internal/db/stock_prices.go` on existing `*DB`. `internal/marketdata`
is network-only — never touches SQLite.

### D5 — Migration in `versionedMigrations` (`db.go`), not `migrations.sql`

`migrations.sql` is a stale reference copy. Append to the slice in `internal/db/db.go`.

```go
{32, `CREATE TABLE IF NOT EXISTS stock_prices (
    cik          TEXT     NOT NULL,
    trade_date   TEXT     NOT NULL,
    open         REAL,
    high         REAL,
    low          REAL,
    close        REAL     NOT NULL,
    raw_close    REAL,
    volume       INTEGER,
    currency     TEXT     NOT NULL DEFAULT 'USD',
    source       TEXT     NOT NULL,
    fetched_at   DATETIME NOT NULL,
    PRIMARY KEY (cik, trade_date)
)`},
{33, `CREATE INDEX IF NOT EXISTS idx_stock_prices_cik_date ON stock_prices(cik, trade_date)`},
```

Methods:

```go
func (d *DB) UpsertStockPrices(cik string, rows []StockPrice) error
func (d *DB) StockPrices(cik string, from, to string) ([]StockPrice, error)
func (d *DB) StockPriceCoverage(cik string) (earliest, latest string, count int, err error)
```

### D6 — Provider package is flat: `provider.go`, `yahoo.go`, `tiingo.go`

Mirrors `internal/llm` multi-backend pattern. No subpackages.

```go
type DailyBar struct {
    Date string
    Open, High, Low, Close float64
    RawClose *float64
    Volume   int64
    Source   string
}

type Provider interface {
    // DailyRange returns daily bars for [from, to] inclusive (YYYY-MM-DD), oldest first.
    DailyRange(ctx context.Context, ticker, from, to string) ([]DailyBar, error)
}
```

`DailyRange` replaces idea file's monolithic `DailyHistory` — chunking lives in the orchestrator.

### D7 — Backfill: chunked full history; refresh: delta-only

**First request** for a CIK with empty `stock_prices`:

1. Resolve ticker from `identity.tickers[0]` (`filedb`).
2. Compute **target end** = today (UTC date).
3. Compute **target start** = walk backward in chunks until the provider returns no rows —
   **no 10-year floor or ceiling**. Optional optimization: stop when chunk end is before
   `coverage.earliestFilingDate − 1 year` if caller passes filing bounds (Timeline can pass this
   to avoid pre-IPO noise; default orchestrator still fetches full provider history).
4. Fetch in **chunks** (D10), upsert after each chunk (crash-safe partial progress).
5. Record `StockPriceCoverage` when done.

**Subsequent requests:** if `latest` is within one trading day of today → DB only. Else fetch
`[latest+1, today]` in one or more small chunks and upsert delta. Never re-run full backfill
unless coverage is empty or an admin `force=1` flag is added later (out of scope).

### D8 — Ticker resolution stays in the caller

`internal/marketdata` accepts `ticker string` only. CIK → ticker is `filedb` / handler job.

### D9 — No scheduled/background refresh in this phase

Lazy refresh on Timeline read (prompt 3). Cron pre-warm is future work.

### D10 — Chunked requests (mandatory politeness)

Hard requirement from product: **do not request full history in one HTTP call.**

| Parameter | Default | Env |
|-----------|---------|-----|
| Chunk calendar span | **5 years** | `MARKET_DATA_CHUNK_YEARS=5` |
| Pause between chunks | **400 ms** | `MARKET_DATA_CHUNK_DELAY=400ms` |
| Max retries per chunk | **3** | (constant) |
| Backoff on 429 | 2s → 8s → 30s | exponential |

**Yahoo chunk:** `GET .../chart/{ticker}?period1={unix}&period2={unix}&interval=1d`

Walk backward: start at today, each chunk covers `[chunkEnd − 5y, chunkEnd]` until a chunk
returns zero rows or `period2` reaches a sane floor (e.g. 1970-01-01). Merge overlapping bars
by date; upsert idempotently.

**Tiingo chunk:** same window strategy using documented `startDate` / `endDate` query params.

**Why not one `range=max` call:** unbounded payload, higher 429 risk, no incremental progress
if the connection drops mid-response. Chunked upsert means a failed chunk 3 of 5 leaves
chunks 1–2 already in SQLite.

Unit-test the chunk walker with a fake `Provider` that returns deterministic slices — no network.

### D11 — Orchestrator owns backfill loop; handlers stay thin

New file `internal/marketdata/backfill.go` (or methods on a small `Service` struct):

```go
type Service struct {
    Primary   Provider
    Fallback  Provider
    DB        *db.DB
    ChunkYears int
    ChunkDelay time.Duration
}

func (s *Service) EnsureCoverage(ctx context.Context, cik, ticker string) error
func (s *Service) RefreshIfStale(ctx context.Context, cik, ticker string) error
```

Prompt 3's timeline handler calls `EnsureCoverage` / `RefreshIfStale`, then
`db.StockPrices(cik, from, to)` for the UI window.

---

## 4. What this touches

| Area | New | Notes |
|------|-----|-------|
| `internal/marketdata/` | yes | `provider.go`, `yahoo.go`, `tiingo.go`, `backfill.go` |
| `internal/db/stock_prices.go` | yes | Upsert, range read, coverage |
| `internal/db/db.go` | append | migrations v32–33 (numbers tentative — LLD picks next free id) |
| `.env.example` | append | `MARKET_DATA_PROVIDER`, `MARKET_DATA_API_KEY`, `MARKET_DATA_USER_AGENT`, chunk envs |
| `prompt_4_stock_data.txt` | stale | Still says Stooq + 10y; implement from **this HLD** |
| `prompt_3_company_view.txt` | consumer | Timeline handler; chart "All" = min/max of **cached** prices + filings |

---

## 5. Contract with prompt 3

`GET /api/companies/:cik/timeline?from=&to=` (prompt 3):

1. `RefreshIfStale(ctx, cik, ticker)` — may run chunked backfill on first visit.
2. `StockPrices(cik, from, to)` — serve chart window from SQLite only.
3. Response `priceCoverage.earliest/latest` reflects **DB**, not the requested `from`/`to`.

Timeline presets (`1Y`, `2Y`, `5Y`, `All`) clamp to cached coverage. **"All" must span the
full stored series** — which may be >10 years when the provider supplied it.

Prompt 3 does not define `stock_prices` schema or call external APIs.

---

## 6. Out of scope

- Timeline Chart.js UI (prompt 3)
- Intraday / real-time quotes, options, indices
- Third provider beyond Yahoo + Tiingo
- Cron pre-warm (D9)
- FX conversion
- Stooq / headless-browser / hashcash solvers
- Re-fetching entire history on corporate-action drift (see Risks)

---

## 7. Risks

| Risk | Mitigation |
|------|------------|
| Yahoo unofficial — crumb/cookie/429 changes | Tiingo fallback; configurable primary; chunked + backoff |
| Tiingo limits unverified | Live-test with key before relying on fallback |
| `adjclose` drift on old rows | Accept for MVP; optional future `force` refresh |
| Single ticker validated (`KMDA`) | Table tests + mocked providers; document OTC gaps |
| Chunk overlap duplicates | Upsert on `(cik, trade_date)` PK |
| Long backfill on first Timeline visit | Return 202 + poll, or block UI with progress — **LLD decision** (prompt 3 UX) |

---

## 8. Done when

Given CIK `0001567529` / ticker `KMDA`:

- [ ] First Timeline-triggered backfill uses **≥2 Yahoo chunks** (mock or integration) when
      history spans >5 years — not a single `range=10y` call.
- [ ] `stock_prices` holds **all bars returned** across chunks (~2,500+ for KMDA today; grows
      if earlier chunks discover pre-2016 data).
- [ ] Second request same day: **zero** external HTTP; `StockPrices` serves from SQLite.
- [ ] Delta refresh adds only dates after `coverage.latest`.
- [ ] `go test ./internal/marketdata/... ./internal/db/...` passes; `make test` passes.
- [ ] `.env.example` documents provider, API key, UA, chunk settings.

---

## 9. Deliverables

- `internal/marketdata/` — Yahoo + Tiingo providers, chunk walker, backfill orchestrator, tests
- `internal/db/stock_prices.go` + `versionedMigrations` entries
- Env vars in `.env.example`
- `prompt_4_stock_data-lld.md` — file-by-file build order, chunk walker pseudocode, 429 handling
- This audit preserved so the LLD does not re-litigate Stooq

**Supersedes idea file on:** Stooq removal, Yahoo primary, Tiingo fallback, **unlimited history
depth**, **chunked fetch**, dedicated `stock_prices` table as the only local store for prices.
