# LLD: Tabbed Company View + Timeline (Phase 3)

Implements `prompt_3_company_view.txt` under the contract frozen in
`prompt_3_company_view-hld.md`. Triage: **HEAVY** — two new Go packages, first domain-data
migration, first outbound dependency, and a refactor of a shipped, verified surface.

The HLD's decisions are inputs here and are not re-argued. Recap: **D1** classification keys on
`category` alone (tier/hasFinancials are functions of it) · **D2** events = CORE allowlist, 100
total / 5–13 per year · **D3** Yahoo v8 behind `PriceProvider`, Stooq rejected · **D4**
`stock_prices` as a `versionedMigrations` entry, not `migrations.sql` · **D5** reuse existing
`.tabs`/`.tab-panel` + admin's hash router · **D6** markers as a Chart.js dataset, no new plugin ·
**D7** lazy price fetch, events survive provider failure · **D8** `min-height: 60vh` ·
**D9** no dependency on prompt 2.

---

## 1. Scope

**In:** `internal/companyview` (weight/event table, timeline assembly) · `internal/marketdata`
(provider interface + Yahoo v8) · migrations v32/v33 + `internal/db/stock_prices.go` ·
`GET /api/companies/:cik/timeline` · tab shell + 5 JS modules · `company.html` restructure ·
`.timeline-*` CSS · env vars · tests.

**Out:** everything in HLD §8. Also explicitly **not** touched: `internal/filedb` (no changes to
scan, search, picker, or the existing two endpoints), `internal/pipeline`, `internal/llm`,
`prompts/`, auth.

---

## 2. Current state (verified)

| Fact | Anchor |
|---|---|
| Company routes live in one group | `internal/handlers/router.go` — `apiCompanies` group, `/search`, `/:cik`, `/:cik/filings` |
| Handler struct to extend | `internal/handlers/companies.go` — `CompanyHandler{Store filedb.CompanyStore}` |
| Sentinel→HTTP mapping helper | `respondStoreErr` in `companies.go` (400/404/500, no detail leak) |
| Query-param helper | `parseBoundedInt(c, name, def, max)` in `companies.go` |
| Migrations source of truth | `versionedMigrations` at `internal/db/db.go:48`, latest **v31** (`db.go:179`) |
| `migrations.sql` is dead | referenced by no Go file; no `go:embed` in the repo |
| DB helpers live per domain | `internal/db/crawler_stats.go`, `pipeline_llm.go` — methods on `*DB` |
| No `ON CONFLICT` upsert exists yet | first use is this phase |
| Tab CSS already present | `static/css/style.css:413-439` — `.tabs`, `.tab-btn[.active]`, `.tab-panel[.active]` |
| Hash-router precedent | `static/admin.html:767-792` — `TAB_NAMES`, hash→tab, `hashchange`, lazy load |
| Chart.js v4.4.6 bundled | `static/js/chart.umd.min.js` header |
| Page JS to split | `static/js/company-page.js` — 14 functions, 4 listener blocks (inventory in §7.2) |
| Not-found path hides cards by id | `company-page.js:init()` hides `identity-card`, `coverage-card`, `summary-card` |

---

## 3. Measurements that bind the implementation

HLD §2 holds. Five further measurements were taken for this LLD; each one changes code below.

| Measurement | Value | Consequence |
|---|---|---|
| `close` vs `adjclose` divergence, full history | **3221 of 3329 bars (97%)**, up to **5.7%** | Schema needs an `adj_close` column (the idea file's has none); the chart plots adjusted close. Kamada pays dividends, so plotting raw close misstates multi-year moves. |
| Chart.js date adapter bundled? | **No** — only the throwing stub (`"This method is not implemented"`); no date-fns/luxon/moment | A `type: 'time'` x-axis throws at runtime. Must use a **category** axis with server-formatted date labels. Same class of trap as the missing annotation plugin. |
| Filing dates that are not trading days | **1 of 294** (2017-04-14, Good Friday) | Marker index mapping needs a snap rule, but it is a rare edge case — snap forward, don't drop. |
| Yahoo epoch → calendar date | bars are `09:30` exchange-local; `meta.gmtoffset` supplied | Convert using `gmtoffset`, not UTC. A no-op for US listings today, wrong for a future non-US listing. |
| Null closes / volumes over 3329 bars | **0 / 0** | Null handling is defensive, not routine — skip null bars rather than building elaborate interpolation. |
| Yahoo unknown symbol | HTTP **404**, `chart.error{code,description}`, `result: null` | Distinguishable from "no rows"; maps to a typed `ErrSymbolNotFound`. |

---

## 4. `internal/companyview` (new)

Pure logic, no I/O — testable without a filesystem, DB, or network.

### 4.1 `classify.go` — the single source of investor-relevance judgment

```go
type Weight string
const (WeightMajor Weight = "major"; WeightMedium Weight = "medium"; WeightMinor Weight = "minor")

type categoryRule struct { Weight Weight; IsEvent bool; Why string }

var categoryRules = map[string]categoryRule{ ... }   // table below

func WeightFor(category string) Weight
func IsEvent(category string) bool
func WhyItMatters(category string) string
```

The frozen table. `weight` reproduces the measured 97/117/167 split; `isEvent` reproduces the
measured 100 (HLD D2's CORE). `why` is the "Why this matters" template the events tab renders —
static strings, no LLM in the request path.

| category | weight | isEvent | count | why |
|---|---|---|---|---|
| `quarterly_results` | major | **yes** | 55 | Reported quarterly financial results |
| `business_deal` | major | **yes** | 23 | Announced a commercial agreement or transaction |
| `annual_report` | major | **yes** | 11 | Filed its annual report |
| `regulatory_clinical` | major | **yes** | 8 | Regulatory or clinical milestone |
| `annual_guidance` | major | **yes** | 0 | Issued forward guidance |
| `corporate_action` | medium | **yes** | 3 | Dividend or other corporate action |
| `current_report` | medium | no | 80 | — |
| `governance` | medium | no | 33 | — |
| `admin_update` | medium | no | 1 | — |
| `earnings_preview` | minor | no | 30 | — |
| `insider_trade` | minor | no | 26 | — |
| `insider_initial` | minor | no | 20 | — |
| `investor_relations` | minor | no | 9 | — |
| `ownership_disclosure` | minor | no | 4 | — |
| `unknown` | minor | no | 78 | — |

`annual_guidance` is in the classifier (`python/edgar/build_meta.py:132`) but has **0** occurrences
for this company — included so the table is complete against the classifier, not against one corpus.

Unknown categories (a future classifier value) return `{WeightMinor, false}` and log once via a
`sync.Once`-guarded warn keyed by category — quiet in the UI, loud in the logs.

`tier`/`tierLabel` are **never** read here. Per HLD D1 they are functions of `category`; a rule
that reads both double-counts. The LLD's test suite pins this (§9).

### 4.2 `timeline.go` — assembly and window clamping

```go
type Event struct {
    FilingDate, Form, Category, TierLabel, Summary, AccessionNumber string
    Tier   int
    Weight Weight `json:"weight"`
}
type Window struct{ From, To string }
type Timeline struct {
    Window        Window         `json:"window"`
    Ticker        string         `json:"ticker"`
    Prices        []PricePoint   `json:"prices"`
    Events        []Event        `json:"events"`
    PriceCoverage *PriceCoverage `json:"priceCoverage"`   // nil when no ticker
}

func ClampWindow(req Window, coverage filedb.Coverage, now time.Time) (Window, error)
func BuildEvents(rows []filedb.FilingRow, w Window, filter string) []Event
```

- `ClampWindow`: absent `from`/`to` → default **2 years back from `to`**; `to` defaults to
  coverage latest. Clamped to `[coverage.EarliestFilingDate, coverage.LatestFilingDate]` (filings
  bound the window, not prices — prices start 2013 but filings start 2016). `from > to` → error.
  `now` is injected so the default window is deterministic in tests.
- `BuildEvents`: filters rows to the window, maps `filter` → predicate
  (`major` = `WeightMajor`; `financials` = `IsEvent`; `all` = everything), attaches `Weight`, and
  returns newest-first. `filedb` rows already arrive newest-first, so no re-sort.

**Wire-size note:** the filter is applied server-side precisely so the default 2-year window ships
~96 events, not 381.

---

## 5. `internal/marketdata` (new)

### 5.1 `provider.go`

```go
type Bar struct {
    Date     string  // YYYY-MM-DD, exchange-local
    Open, High, Low, Close, AdjClose float64
    Volume   int64
}
type Quote struct { Symbol, Currency, Exchange string; Bars []Bar }

type PriceProvider interface {
    DailyBars(ctx context.Context, symbol string, from, to time.Time) (*Quote, error)
    Name() string
}

var (
    ErrSymbolNotFound   = errors.New("marketdata: symbol not found")
    ErrBadGranularity   = errors.New("marketdata: provider returned non-daily data")
    ErrBadResponse      = errors.New("marketdata: unexpected response shape")
)
```

### 5.2 `yahoo.go`

Endpoint: `https://query1.finance.yahoo.com/v8/finance/chart/{symbol}` with
`period1`, `period2` (epoch seconds) and `interval=1d`.

Three guards, each mapped to a measured failure (§3 and HLD §2.4):

1. **Never `range=`.** `range=max&interval=1d` silently returns `1mo` data. The client only ever
   sends `period1`/`period2`.
2. **Assert `meta.dataGranularity == "1d"`** → else `ErrBadGranularity`, batch discarded. This is
   the guard against a chart that looks right and is wrong.
3. **Assert JSON content type and `chart.result[0]` presence** → else `ErrBadResponse`. A 200
   carrying HTML (Stooq's failure mode) must never parse as "zero bars".

Response decoding, pinned to the measured shape:

```go
type yahooResp struct {
    Chart struct {
        Result []struct {
            Meta struct {
                Currency, Symbol, ExchangeName, DataGranularity string
                GMTOffset int64 `json:"gmtoffset"`
            } `json:"meta"`
            Timestamp  []int64 `json:"timestamp"`
            Indicators struct {
                Quote    []struct{ Open, High, Low, Close []*float64; Volume []*int64 } `json:"quote"`
                AdjClose []struct{ AdjClose []*float64 `json:"adjclose"` } `json:"adjclose"`
            } `json:"indicators"`
        } `json:"result"`
        Error *struct{ Code, Description string } `json:"error"`
    } `json:"chart"`
}
```

- `chart.error` non-nil (HTTP 404) → `ErrSymbolNotFound`.
- `Date` = `time.Unix(ts + meta.gmtoffset, 0).UTC().Format("2006-01-02")` — exchange-local
  calendar date, per §3.
- Nullable arrays are `*float64`/`*int64`; a bar with a nil `Close` is **skipped**, not
  zero-filled. Measured 0 nulls, so this is defensive.
- `User-Agent` is set explicitly; the default Go UA is a common block trigger.
- One `http.Client` with a 15 s timeout, injected so tests use `httptest`.

`Name()` returns `"yahoo"` and is what lands in `stock_prices.source`.

---

## 6. Database

### 6.1 Migrations — appended to `versionedMigrations` (`internal/db/db.go:180`)

One statement per version, matching the existing v30/v31 convention.

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

Two deltas from the idea file's schema, both earned:

- **`adj_close` added** — §3: raw and adjusted close differ on 97% of bars.
- **`idx_stock_prices_cik_date` dropped** — the `(cik, trade_date)` primary key already provides
  that exact index; SQLite would store a redundant second b-tree.

**v33 `stock_price_coverage` is new and not in the idea file.** Without it there is no way to
distinguish "no rows because we never fetched" from "no rows because this company has no ticker or
the provider has no data" — so every Timeline open for a tickerless company would re-hit the
provider forever. `status` ∈ `ok | no_symbol | not_found | error`, and `last_attempt_at` backs a
retry floor (default 6 h) for the failure statuses.

### 6.2 `internal/db/stock_prices.go` (new)

Methods on `*DB`, matching the `crawler_stats.go` style:

```go
func (d *DB) UpsertStockPrices(ctx context.Context, cik string, bars []marketdata.Bar, currency, source string) error
func (d *DB) StockPrices(ctx context.Context, cik, from, to string) ([]PricePoint, error)
func (d *DB) StockPriceCoverage(ctx context.Context, cik string) (*PriceCoverageRow, error)
func (d *DB) SetStockPriceCoverage(ctx context.Context, cik, symbol, status, note string) error
```

- `UpsertStockPrices` runs one transaction with a prepared
  `INSERT ... ON CONFLICT(cik, trade_date) DO UPDATE SET ...`, making re-fetch idempotent (an
  acceptance criterion). It recomputes and writes coverage min/max in the same transaction.
- `StockPrices` selects `trade_date, close, adj_close, volume` ordered by `trade_date` — OHLC is
  stored but not shipped until a candlestick view needs it.
- **Import direction:** `internal/db` importing `internal/marketdata` for the `Bar` type is the
  one cross-package coupling here. If that reads wrong at build time, `UpsertStockPrices` takes a
  local `[]PriceBar` and the caller converts — decide in step 3, note it in the report.

---

## 7. Frontend

### 7.1 `static/company.html` — restructure

Header block (name, tickers, subtitle) stays as-is. Below it:

```html
<div class="tabs" role="tablist" aria-label="Company sections">
  <button class="tab-btn active" role="tab" id="tabbtn-timeline" aria-controls="tab-timeline" aria-selected="true">Timeline</button>
  <button class="tab-btn" role="tab" id="tabbtn-general"  aria-controls="tab-general"  aria-selected="false">General</button>
  <button class="tab-btn" role="tab" id="tabbtn-filings"  aria-controls="tab-filings"  aria-selected="false">Filings</button>
  <button class="tab-btn" role="tab" id="tabbtn-events"   aria-controls="tab-events"   aria-selected="false">Special events</button>
</div>
<div class="tab-container">
  <div class="tab-panel active" role="tabpanel" id="tab-timeline" aria-labelledby="tabbtn-timeline">…</div>
  …
</div>
```

- Existing cards move **unchanged, with their ids intact** into `#tab-general` (identity, coverage,
  summary) and `#tab-filings` (filters, table, pager). Keeping ids identical is what lets the
  render functions move verbatim and keeps the not-found path working.
- The "Timeline — coming soon" placeholder card is deleted.
- Script order: `app.js` → `chart.umd.min.js` → `company/shell.js` → the four `tab-*.js`.
- `.tabs`/`.tab-btn`/`.tab-panel` are reused as-is; only `.tab-container` and `.timeline-*` are new.

### 7.2 `static/js/company-page.js` → `static/js/company/` (verbatim move)

`company-page.js` is deleted. Its 14 functions move with **no behavior change** (HLD §9 risk 5):

| Destination | Functions moved |
|---|---|
| `shell.js` | `renderHeader`, the `cik` derivation, `init`'s detail fetch + not-found path, the `/auth/me` header/logout block |
| `tab-general.js` | `chip`, `metaItem`, `sortedEntries`, `renderIdentity`, `renderCoverage`, `renderFormBars`, `renderSummary`, the `form-toggle` listener |
| `tab-filings.js` | `renderFilters`, `renderFilings`, `filingsQuery`, `loadFilings`, `PAGE_SIZE`, the pager and filter listeners |
| `tab-timeline.js` | new |
| `tab-events.js` | new (P1) |

`chip`, `metaItem`, and `sortedEntries` are needed by more than one module; they move to `shell.js`
and are exposed on the shared context rather than duplicated.

### 7.3 Module contract

Classic scripts with a global registry — consistent with the rest of `static/js/` (no ESM anywhere
in this codebase). `shell.js` defines the registry before the tab files load:

```js
window.CompanyTabs = { register(name, mod) { … } };   // mod: { init(ctx), show(), hide() }
// ctx = { cik, detail, apiFetch, escHtml, chip, metaItem, sortedEntries }
```

Shell responsibilities:
- derive `cik`, fetch `/api/companies/:cik`, handle the 404 path, render the header;
- `TAB_NAMES = ['timeline','general','filings','events']`, hash→tab on load (default `timeline`),
  `hashchange` listener, `location.hash` write on switch — the `admin.html:774` shape;
- call `init(ctx)` **lazily**, once, the first time a tab is shown; `show()`/`hide()` on every
  switch thereafter;
- ARIA: toggle `aria-selected`, roving `tabindex`, ArrowLeft/ArrowRight move focus,
  Enter/Space activate. (This is the part `admin.html` lacks.)

Panels are hidden with the existing `.tab-panel` `display:none`, never unmounted — chart state and
scroll position survive, per HLD D5.

### 7.4 `tab-timeline.js`

**Chart construction — a category x-axis, not a time axis.** §3: no date adapter is bundled, so
`type: 'time'` throws at runtime. Labels are the server's `YYYY-MM-DD` strings; Chart.js indexes
them.

- Dataset 0: `type: 'line'`, `data = prices.map(p => p.adjClose ?? p.close)`, `pointRadius: 0`,
  `tension: 0`, `spanGaps: true`.
- Datasets 1–3: `type: 'scatter'`, one per weight (`major`/`medium`/`minor`), each point
  `{x: <index into labels>, y: <that day's plotted price>}` with distinct `pointRadius` (7/5/3),
  `pointStyle`, and color. Separate datasets — not one dataset with per-point styling — so the
  lane toggle is `dataset.hidden = true/false` and the legend works for free.
- **Marker index mapping:** build `dateIndex = Map<'YYYY-MM-DD', i>` once from labels. An event
  whose `filingDate` is absent (measured: **1 of 294**, 2017-04-14 Good Friday) snaps **forward**
  to the next trading day; an event after the last bar is dropped. Snapping forward, not backward,
  keeps a filing from appearing to precede itself.
- Tooltip: `callbacks.label` returns a **plain string array** (`filingDate · category · summary`).
  Chart.js renders tooltips on canvas, so there is no HTML injection surface — but the summary is
  free text from `meta.json`, so it is truncated to ~140 chars and never passed to `innerHTML`.
  Any DOM-rendered event text (the events tab, the empty-state banner) goes through `escHtml`.
- `chart.resize()` on `show()` — Chart.js cannot measure a canvas in a `display:none` container,
  so a chart built while hidden renders at 0×0 without it.
- Controls: `1Y | 2Y | 5Y | All` presets + optional `from`/`to` date inputs; lane toggle
  `Major only | Financials | All events`. Preset changes refetch; lane toggle is client-side
  (`dataset.hidden`) with no request.
- States: no ticker → events-only chart + banner; provider error → same, with the reason; empty
  window → "No filings in this window".

### 7.5 `tab-events.js` (P1)

Reverse-chronological cards grouped by calendar year, newest year first, from
`/timeline?filter=financials` (which is `IsEvent`) — no separate endpoint, no separate fetch if
Timeline already loaded the same window. Each card: date, category chip, form, tier badge,
summary, and the `WhyItMatters(category)` line from §4.1. Clicking a card switches to `#timeline`
and highlights the matching marker.

Expected volume, measured: **5–13 per year**, 100 across the corpus.

### 7.6 CSS

Append `.tab-container { min-height: 60vh; }` (HLD D8) plus `.timeline-*` (controls row, chart
wrapper with fixed aspect, banner, event card, year heading). Tokens only. Do **not** redefine
`.tabs`, `.tab-btn`, or `.tab-panel`.

---

## 8. Handler and wiring

### `internal/handlers/companies.go`

```go
type CompanyHandler struct {
    Store    filedb.CompanyStore
    Timeline *companyview.Service   // nil-safe: nil ⇒ 503 with a clear message
}
func (h *CompanyHandler) Timeline(c *gin.Context)
```

`Timeline` reuses `filedb.NormalizeCIK` + the existing `respondStoreErr`, adding one case:
`companyview.ErrBadWindow` → 400. Provider failures are **not** errors — they populate
`priceCoverage: null` and a `note`, and the events still ship (HLD D7).

`from`/`to` are validated as `YYYY-MM-DD` by `time.Parse` before reaching the service; `filter` is
validated against the three known values, defaulting to `all` rather than 400-ing on an unknown
value.

### `internal/handlers/router.go`

One line inside the existing `apiCompanies` group — auth and the 60 s timeout come with the group:

```go
apiCompanies.GET("/:cik/timeline", companyHandler.Timeline)
```

Registering `/:cik/timeline` beside `/:cik/filings` is the same shape already proven on gin v1.9.1
in prompt 1; no new conflict risk.

### `cmd/server/main.go`

Construct the provider and the service beside the existing `companyStore`:

```go
provider := marketdata.NewFromEnv(os.Getenv("MARKET_DATA_PROVIDER"))   // default "yahoo"
timelineSvc := companyview.NewService(companyStore, database, provider, companyview.Config{
    FetchOnOpen: getEnv("STOCK_FETCH_ON_COMPANY_OPEN", "false") == "true",
})
```

`NewRouter` gains no new parameter — `timelineSvc` is set on the existing `CompanyHandler`, which
is already constructed inside `NewRouter`. **Decide in step 5:** either pass the service as an 8th
`NewRouter` parameter (consistent with how `companies` was threaded in prompt 1) or bundle it with
the store into a small `CompanyDeps` struct. The second is cleaner given `NewRouter` already takes
seven; recommend `CompanyDeps` and note it in the report.

### `.env.example`

```
# ---- Market data (phase 3 timeline) ----
MARKET_DATA_PROVIDER=yahoo        # yahoo
MARKET_DATA_API_KEY=              # unused by yahoo; reserved for keyed providers
STOCK_FETCH_ON_COMPANY_OPEN=false # eager-fetch prices when a company page opens
```

---

## 9. Tests

New table tests, plain style, no testify — matching `internal/handlers/pipeline_status_test.go`.

| File | Covers |
|---|---|
| `companyview/classify_test.go` | Every one of the 15 categories maps to its table row; **`IsEvent` count over a fixture equals CORE**; unknown category → `{minor,false}`; a guard test asserting no rule reads `tier` |
| `companyview/timeline_test.go` | `ClampWindow`: defaults, 2-year back-off, clamp to coverage, `from > to` → error, injected `now`; `BuildEvents`: filter predicates, window bounds, newest-first, weight attached |
| `marketdata/yahoo_test.go` | `httptest` fixtures: happy path decodes bars; **`dataGranularity:"1mo"` → `ErrBadGranularity`**; HTML body with 200 → `ErrBadResponse`; 404 + `chart.error` → `ErrSymbolNotFound`; nil `close` bar skipped; `gmtoffset` date conversion |
| `db/stock_prices_test.go` | Upsert inserts then updates the same `(cik, trade_date)` (idempotence); range query bounds and ordering; coverage row written in the same tx; `no_symbol` status suppresses re-fetch within the retry floor |
| `handlers/companies_test.go` (extend) | `/timeline` 400 on bad CIK and bad date, 404 unknown company, 200 with `priceCoverage: null` when the provider errors — asserting **events still present** |

The `ErrBadGranularity` test is mandatory per HLD §9.2 — it is the only thing standing between a
`range=max` regression and a chart that looks correct and is wrong.

**Regression baseline.** Prompt 1's DoD render harness is the acceptance test for the General and
Filings refactor. Re-run it against the tabbed page with two changes: activate `#general` /
`#filings` before asserting, and expect the ids at their new nesting. All 23 company-page
assertions must still pass — that is what proves §7.2 was a move and not a rewrite.

---

## 10. Build order

1. `companyview/classify.go` + test. Pure, no deps — the frozen table lands first and green.
2. `companyview/timeline.go` + test (`ClampWindow`, `BuildEvents`). Still pure.
3. `marketdata/` provider + Yahoo + `httptest` tests. No DB, no handler.
4. Migrations v32/v33 + `db/stock_prices.go` + tests. `make run` once to confirm the runner applies
   32 and 33 and records them in `schema_migrations`.
5. `companyview.Service` (DB + provider + filedb wiring), handler, router line, `main.go`,
   `.env.example`. Curl §11 steps 1–3 green — **backend fully verifiable before any UI moves**.
6. **Refactor only:** `company.html` tabs + `shell.js`/`tab-general.js`/`tab-filings.js`; delete
   `company-page.js`. Re-run the prompt 1 regression harness. No new features in this step.
7. `tab-timeline.js` + `.timeline-*` CSS. Curl §11 step 4, then the browser check.
8. P1: `tab-events.js`.
9. `make test`, then the manual pass.

Steps 1–3 need no DB, no server, and no corpus. Step 6 is deliberately isolated so a regression
there is unambiguous.

---

## 11. Manual verification

```bash
make run   # PORT=8123
TOKEN=$(curl -s -X POST localhost:8123/auth/login -H 'Content-Type: application/json' \
  -d "{\"email\":\"$ADMIN_EMAIL\",\"password\":\"$ADMIN_PASSWORD\"}" | jq -r .data.token)
A="Authorization: Bearer $TOKEN"

# 1. auth (expect 401)
curl -s -o /dev/null -w '%{http_code}\n' localhost:8123/api/companies/0001567529/timeline

# 2. migrations applied
sqlite3 .db/megane.db "SELECT version FROM schema_migrations WHERE version IN (32,33);"

# 3. default window: events present, weights computed server-side
curl -s -H "$A" localhost:8123/api/companies/0001567529/timeline \
  | jq '{from:.data.window.from, to:.data.window.to, ticker:.data.ticker,
         prices:(.data.prices|length), events:(.data.events|length),
         weights:(.data.events|group_by(.weight)|map({(.[0].weight):length})|add)}'
# expect ~501 prices, ~96 events, weights ≈ {major:16, medium:18, minor:62}

# 4. idempotent re-fetch — row count must not change
sqlite3 .db/megane.db "SELECT COUNT(*), MIN(trade_date), MAX(trade_date) FROM stock_prices;"
curl -s -o /dev/null -H "$A" 'localhost:8123/api/companies/0001567529/timeline?from=2016-01-06&to=2026-08-20'
sqlite3 .db/megane.db "SELECT COUNT(*), MIN(trade_date), MAX(trade_date) FROM stock_prices;"

# 5. filters and clamping
curl -s -H "$A" 'localhost:8123/api/companies/0001567529/timeline?filter=financials' | jq '.data.events|length'   # ~ CORE subset
curl -s -H "$A" 'localhost:8123/api/companies/0001567529/timeline?from=1990-01-01' | jq '.data.window.from'       # clamps to 2016-01-06
curl -s -o /dev/null -w '%{http_code}\n' -H "$A" 'localhost:8123/api/companies/0001567529/timeline?from=zzz'      # 400

# 6. adjusted close is what was stored
sqlite3 .db/megane.db "SELECT trade_date, close, adj_close FROM stock_prices WHERE cik='0001567529' ORDER BY trade_date LIMIT 3;"
```

Browser: open `/companies/0001567529` → lands on Timeline → price line renders with markers →
hover a large marker shows a quarterly-results summary → switch to General and Filings, confirm
they match prompt 1 and the page height does not jump → reload `#filings` and land on Filings.

---

## 12. Risks specific to implementation

1. **The refactor is the likeliest regression** (HLD §9.5). Contained by making step 6 a pure move
   with the prompt 1 harness as the gate, and by keeping every element id unchanged.
2. **`internal/db` importing `internal/marketdata`** (§6.2) may read as the wrong dependency
   direction. Fallback is a local `PriceBar` type in `db` with conversion at the call site; decide
   at step 4, not at design time.
3. **Chart built while hidden renders 0×0** unless `resize()` runs on `show()`. Easy to miss
   because it only reproduces when Timeline is not the initial tab.
4. **`filedb` cache TTL vs price freshness.** Filing rows come from the 5-minute `filedb` cache,
   prices from SQLite. A filing added mid-session can appear on the timeline up to 5 minutes after
   it appears elsewhere. Acceptable; noted so it is not diagnosed as a timeline bug.
5. **Yahoo rate limiting is unmeasured.** One company was probed a handful of times. Behavior under
   a real backfill of many companies is unknown — the per-CIK serialization and the v33 retry floor
   are the containment, but the first multi-company backfill should be watched.

---

## 13. Open questions

1. **`All` preset lower bound.** Clamped to filing coverage (2016-01-06), so ~2.5 years of
   available price history (2013-05-31 →) is never shown. Deliberate — the page is about filings —
   but if the chart should show pre-coverage price context, that is a one-line change to
   `ClampWindow` and a product call.
2. **Whether `earnings_preview` joins the events allowlist.** Excluded per HLD D2 (it announces an
   event already shown), which costs 30 filings and would move per-year counts from 5–13 to 9–14.
   Revisit once the tab is on screen.

---

## 14. Done when

A signed-in user opens `/companies/0001567529`, lands on **Timeline**, and sees KMDA's adjusted
daily close for the last two years as a line with filing markers on it — quarterly results and
other MAJOR-weight events visibly heavier than routine 6-Ks — and hovering a large marker shows its
date, category, and summary. `Major only` hides the ~62 minor markers. **General** and **Filings**
show exactly what prompt 1 shipped, with no page-height jump, and `#filings` survives a reload.
`sqlite3 .db/megane.db "SELECT COUNT(*) FROM stock_prices"` is non-zero and unchanged after a
second identical request. `make test` passes, including the `1mo`-rejection test.
