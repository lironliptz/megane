# HLD: Stock Price Provider & Cache (OHLCV)

Implements `prompts/dev/prompt_4_stock_data.txt`. Feeds `prompts/dev/prompt_3_company_view.txt`
(Timeline tab) — that prompt's handler consumes this layer only; it does not call Yahoo/Tiingo.

Triage: **HEAVY** — external provider, SQLite cache, chunked backfill semantics, and an
**OHLCV wire contract** (adjusted close **and volume** on every trading day).

Scope: contract + decisions. File-by-file changes belong in the LLD. **Do not implement from
this doc until the LLD is read** — see §10 for what is already shipped vs still planned.

---

## 1. Objective

Given a company's CIK, fetch and cache **all available daily OHLCV history** (not capped at 10
years) into SQLite so the Timeline tab reads locally with no per-view external calls.

Requirements:

- **Full depth** — backfill as far back as the provider offers for the ticker; if that exceeds
  10 years, store it all. Align chart "All" window with cached coverage, not an arbitrary decade cap.
- **Chunked, polite fetching** — split history into time windows so a single giant response does
  not trigger 429s or provider throttling; pause between chunks.
- **Adjusted close + raw close** — store both when the source provides them; chart uses adjusted
  series (`adjClose ?? close` in the API).
- **Volume alongside price** — every bar carries `volume int64`; persist and expose on the Timeline
  JSON so prompt 3 can render volume bars or a secondary series. Do not drop volume to simplify
  the schema.
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
| `indicators.quote[0].volume` | Present on every bar in successful probes; typical trading days show **non-zero** volume (required for product acceptance) |
| No `User-Agent` | **HTTP 429** — UA is mandatory |
| `range=max&interval=1d` | **Do not use** — measured to silently return **monthly** bars (`dataGranularity: "1mo"`, 160 bars) instead of ~3,329 daily bars. Shipped code rejects non-`1d` granularity. |
| `period1` / `period2` chunks | Verified: straddling listing date returns bars truncated at `meta.firstTradeDate`; window fully before listing returns HTTP 400 + `chart.error` (see LLD stop condition) |

**Implication:** Yahoo works with `period1`/`period2` only. Full history requires **multiple
chunked requests**, not one `range=max` or `range=10y` blob.

### Tiingo (fallback — endpoint shape verified in LLD, not live-tested here)

Documented EOD API, free tier with API key. `adjClose` + `volume` fields match D3/D12.
Limits (req/day, symbol count) must be verified with a real key before production trust.

### Filing vs price coverage (Kamada)

| Series | Earliest on disk |
|--------|------------------|
| SEC filings (`fileDB`) | **2016-01-06** |
| Yahoo daily prices (listing) | **`meta.firstTradeDate` = 2013-05-31** (chunk probe) |

Prices may start **before or after** filings depending on listing vs SEC activity. Timeline
overlays filing events on price series — store all available prices; do not fail backfill when
filings predate first trade.

### What the audit changes in the spec

1. **Yahoo primary, Stooq removed** (bot-check).
2. **Tiingo keyed fallback** replaces Stooq.
3. **`range=` is forbidden** — chunked `period1`/`period2` windows only (D10).
4. **`User-Agent` required** on every Yahoo request; 429 → retry with backoff.
5. **Volume is a first-class field** — Yahoo `quote.volume[i]` aligned with `timestamp[i]`.

---

## 3. Architecture decisions

### D1 — Yahoo chart API is primary

Verified: works with UA, returns `adjclose` and `volume`. Every request sets a realistic
`User-Agent` (env `MARKET_DATA_USER_AGENT` or code default). Handle 429 as retryable with
exponential backoff per chunk. Assert `meta.dataGranularity == "1d"` on every response.

### D2 — Tiingo is the fallback (planned; not wired in as-built)

Real second source when Yahoo returns empty, 429 exhaustion, or parse failure. Requires
`MARKET_DATA_API_KEY` in `.env.example`. Same chunked backfill semantics as Yahoo.

### D3 — Store raw `close`, adjusted `adj_close`, and `volume`

Yahoo: `indicators.quote[0].close` → `close`; `indicators.adjclose[0].adjclose` → `adj_close`;
`indicators.quote[0].volume` → `volume` (0 when null). Tiingo: `close` / `adjClose` / `volume`
per LLD. `source` column records `yahoo` | `tiingo`.

**Note:** The idea file's `raw_close` column name is superseded by **`adj_close`** as a
separate column — matches shipped schema and measured divergence (3,221/3,329 bars differ on
KMDA full series).

### D4 — Storage extends `internal/db`, no new storage package

One `db` package, `internal/db/stock_prices.go` on existing `*DB`. `internal/marketdata`
is network-only — never touches SQLite.

### D5 — Migrations in `versionedMigrations` (`db.go`), not `migrations.sql`

`migrations.sql` is a stale reference copy. Append to the slice in `internal/db/db.go`.

**Shipped (migrations 32–33):**

```go
{32, `CREATE TABLE IF NOT EXISTS stock_prices (
    cik        TEXT    NOT NULL,
    trade_date TEXT    NOT NULL,
    open       REAL,
    high       REAL,
    low        REAL,
    close      REAL    NOT NULL,
    adj_close  REAL,
    volume     INTEGER,
    currency   TEXT    NOT NULL DEFAULT 'USD',
    source     TEXT    NOT NULL,
    fetched_at DATETIME NOT NULL,
    PRIMARY KEY (cik, trade_date)
)`},
{33, `CREATE TABLE IF NOT EXISTS stock_price_coverage (
    cik             TEXT PRIMARY KEY,
    symbol          TEXT NOT NULL DEFAULT '',
    earliest        TEXT,
    latest          TEXT,
    status          TEXT NOT NULL,
    note            TEXT NOT NULL DEFAULT '',
    last_attempt_at DATETIME NOT NULL
)`},
```

Methods (as-built signatures):

```go
func (d *DB) UpsertStockPrices(ctx context.Context, cik, symbol string, bars []PriceBar, currency, source string) error
func (d *DB) StockPrices(ctx context.Context, cik, from, to string) ([]PriceRow, error)
func (d *DB) StockPriceCoverage(ctx context.Context, cik string) (*PriceCoverageRow, error)
func (d *DB) SetStockPriceCoverage(ctx context.Context, cik, symbol, status, note string) error
```

`PriceBar` / `PriceRow` include **`Volume int64`**. Coverage is a **dedicated table** with
status + retry floor (`PriceRetryFloor = 6h`, `ShouldRetry`) — not a computed `MIN`/`MAX`/`count`
query alone.

### D6 — Provider package is flat; as-built API surface

Mirrors `internal/llm` multi-backend pattern. **Use these names** (shipped), not the older
`DailyBar` / `Provider.DailyRange` sketch in early drafts:

```go
type Bar struct {
    Date     string
    Open, High, Low, Close, AdjClose float64
    Volume   int64
}

type Quote struct {
    Symbol, Currency, Exchange string
    Bars []Bar
}

type PriceProvider interface {
    DailyBars(ctx context.Context, symbol string, from, to time.Time) (*Quote, error)
    Name() string
}
```

Sentinel errors: `ErrSymbolNotFound`, `ErrBadGranularity`, `ErrBadResponse`.

### D7 — Backfill: chunked full history; refresh: delta-only

**First request** for a CIK with empty `stock_prices`:

1. Resolve ticker from `identity.tickers[0]` (`filedb`).
2. Walk backward in **chunks** (D10) from today until `meta.firstTradeDate` (preferred stop) or
   a terminal `chart.error` (HTTP 400 before listing).
3. Upsert after each chunk (crash-safe partial progress).
4. Update `stock_price_coverage` with `status=ok`, `earliest`, `latest`.

**Subsequent requests:** if `latest` is within one trading day of today → DB only. Else fetch
`[latest+1, today]` in one or more small chunks and upsert delta. Never re-run full backfill
unless coverage is empty or an admin `force=1` flag is added later (out of scope).

**As-built gap:** `companyview.Service.ensureCoverage` currently fetches **only the requested
Timeline window** (with a small date pad), not the full chunked history walk. Completing D7/D10
is remaining prompt 4 work — see §10.

### D8 — Ticker resolution stays in the caller

`internal/marketdata` accepts `symbol string` only. CIK → ticker is `filedb` / handler job.

### D9 — No scheduled/background refresh in this phase

Lazy refresh on Timeline read (prompt 3). Optional eager fetch via `STOCK_FETCH_ON_COMPANY_OPEN`
(as-built). Cron pre-warm is future work.

### D10 — Chunked requests (mandatory politeness)

Hard requirement: **do not request full history in one HTTP call.**

| Parameter | Default | Env |
|-----------|---------|-----|
| Chunk calendar span | **5 years** | `MARKET_DATA_CHUNK_YEARS=5` |
| Pause between chunks | **400 ms** | `MARKET_DATA_CHUNK_DELAY=400ms` |
| Max retries per chunk | **3** | (constant) |
| Backoff on 429 | 2s → 8s → 30s | exponential |

**Yahoo chunk:** `GET .../chart/{ticker}?period1={unix}&period2={unix}&interval=1d`

Walk backward: start at today; read `firstTradeDate` from the first successful chunk; stop when
`chunkStart <= firstTradeDate`. Treat HTTP 400 + parseable `chart.error` as terminal (not retry).

**Tiingo chunk:** same window strategy using `startDate` / `endDate` query params.

Unit-test the chunk walker with a fake `PriceProvider` — no network.

### D11 — Orchestrator owns backfill loop; handlers stay thin

**Target:** `internal/marketdata/backfill.go` or equivalent methods on `companyview.Service`.

**As-built:** orchestration lives in `internal/companyview/service.go` (`ensureCoverage`,
`pricesFor`, per-CIK fetch mutex). Prompt 3's timeline handler calls `companyview.Service`;
that service calls `PriceProvider.DailyBars` and `db.UpsertStockPrices`.

### D12 — Volume is required on fetch, store, and Timeline JSON

| Layer | Rule |
|-------|------|
| Provider | Map Yahoo `quote.volume[i]` / Tiingo `volume`; use `0` only when the source sends null for that bar |
| SQLite | `volume INTEGER` on every row; upsert on conflict |
| Timeline API | Each `prices[]` element includes `"volume": N` when cached (see §5). `omitempty` on zero is acceptable only when the value is genuinely zero, not when volume was never fetched |
| UI | Volume bars / secondary axis — **prompt 3 / 5**, out of scope here, but blocked if this layer omits volume |

**Acceptance spot-check:** For `KMDA`, cached rows show non-zero volume on typical trading days;
timeline JSON includes volume on the same dates as `close` / `adjClose`.

---

## 4. What this touches

| Area | Status | Notes |
|------|--------|-------|
| `internal/marketdata/` | **partial** | Yahoo shipped; Tiingo + chunk walker planned |
| `internal/db/stock_prices.go` | **shipped** | OHLCV + coverage table |
| `internal/companyview/service.go` | **partial** | Window-scoped fetch; full chunk backfill planned |
| `internal/companyview/timeline.go` | **shipped** | `PricePoint.Volume` on wire |
| `.env.example` | **partial** | `MARKET_DATA_PROVIDER`; chunk envs planned |
| `prompt_4_stock_data.txt` | **canonical idea** | Aligned with this HLD on volume + Yahoo/Tiingo |
| `prompt_3_company_view.txt` | consumer | Timeline handler; chart renders volume visually |

---

## 5. Contract with prompt 3

`GET /api/companies/:cik/timeline?from=&to=` (prompt 3):

1. Ensure price coverage for the requested window (may trigger provider fetch).
2. `StockPrices(cik, from, to)` — serve chart window from SQLite only.
3. Response includes OHLCV points and coverage metadata.

Example (wire shape):

```json
"prices": [
  { "date": "2024-01-02", "close": 4.12, "adjClose": 4.37, "volume": 102400 }
],
"priceCoverage": {
  "earliest": "2013-05-31",
  "latest": "2026-08-25",
  "source": "yahoo",
  "status": "ok"
}
```

Rules:

- **`volume`** populated from DB for every bar when provider supplied it.
- **`priceCoverage.earliest/latest`** reflect **cached DB** span, not the requested `from`/`to`.
- Provider failures → degraded coverage note; events still returned (prompt 3 D7).
- Chart uses **`adjClose ?? close`** for the price line and **`volume`** for a companion series.

Timeline presets (`1Y`, `2Y`, `5Y`, `All`, custom range) clamp to cached coverage. **"All"**
must span the full stored series — which may be >10 years when chunked backfill is complete.

Prompt 3 does not define `stock_prices` schema or call external APIs.

---

## 6. Out of scope

- Timeline Chart.js volume rendering (prompt 3 UI)
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
| Yahoo unofficial — crumb/cookie/429 changes | Tiingo fallback; chunked + backoff; coverage retry floor |
| Tiingo limits unverified | Live-test with key before relying on fallback |
| `adjclose` drift on old rows | Accept for MVP; optional future `force` refresh |
| Single ticker validated (`KMDA`) | Table tests + mocked providers; document OTC gaps |
| Chunk overlap duplicates | Upsert on `(cik, trade_date)` PK |
| Long backfill on first Timeline visit | Return 202 + poll, or block UI with progress — prompt 3 UX |
| Volume systematically absent from provider | Log once per fetch; store 0; surface in coverage note |

---

## 8. Done when

Given CIK `0001567529` / ticker `KMDA`:

- [ ] Yahoo fetches daily bars with **non-zero volume** on typical trading days (fixture + smoke)
- [ ] Chunked backfill uses **≥2 Yahoo chunks** when history spans >5 years — not a single window fetch
- [ ] Backfill stops at **`firstTradeDate`** without treating pre-listing HTTP 400 as a hard failure
- [ ] `stock_prices` holds **all bars returned** across chunks (listing date → today)
- [ ] Second request same day: **zero** external HTTP when coverage already spans the window
- [ ] Delta refresh adds only dates after `coverage.latest`
- [ ] Timeline API returns **`volume`** on each `prices[]` element for cached data
- [ ] Tiingo fallback path tested (mock or live with key)
- [ ] `go test ./internal/marketdata/... ./internal/db/... ./internal/companyview/...` passes; `make test` passes
- [ ] `.env.example` documents provider, API key, UA, chunk settings

---

## 9. Deliverables

- `internal/marketdata/` — Yahoo (+ Tiingo) providers, chunk walker, tests including volume fixtures
- `internal/db/stock_prices.go` + `versionedMigrations` entries (shipped)
- `internal/companyview/service.go` — full chunked orchestration (extend as-built)
- Env vars in `.env.example`
- `prompt_4_stock_data-lld.md` — file-by-file build order, chunk walker pseudocode, volume parse paths
- `prompt_4_stock_data-implementation.md` — as-built record (update when implementing)

**Canonical on:** Yahoo primary, Stooq removal, Tiingo fallback, unlimited history depth,
chunked fetch, **OHLCV storage and Timeline wire contract**.

---

## 10. Implementation status (as-built vs this HLD)

See `prompt_4_stock_data-implementation.md` for detail. Summary:

| Item | Status |
|------|--------|
| `stock_prices` + `volume` column | **Shipped** (migration 32) |
| `stock_price_coverage` + retry floor | **Shipped** (migration 33) |
| Yahoo `period1`/`period2`, granularity guard, volume parse | **Shipped** |
| `PricePoint.Volume` on Timeline JSON | **Shipped** |
| Window-scoped fetch in `companyview.Service` | **Shipped** (partial D7) |
| Chunked full-history backfill (D10) | **Not shipped** |
| Tiingo fallback (D2) | **Not shipped** |
| Chunk env vars in `.env.example` | **Not shipped** |
| Chart volume bars (UI) | **Not shipped** (prompt 3) |

Future implementation should extend the as-built API (`Bar`/`Quote`/`PriceProvider`) rather
than introducing parallel `DailyBar`/`Provider` types.
