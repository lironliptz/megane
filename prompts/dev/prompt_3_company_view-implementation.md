# Tabbed Company View + Timeline (Phase 3) — Implementation Notes (as-built)

Implements `prompt_3_company_view-lld.md` under the contract in
`prompt_3_company_view-hld.md`. Built 2026-08-25.

---

## TL;DR

Shipped as designed: `internal/companyview` and `internal/marketdata`, migrations v32/v33 with a
price store, `GET /api/companies/:cik/timeline`, and a four-tab company view whose General and
Filings tabs are a verbatim move of the prompt-1 page.

Verified literally against the running server and the real corpus + live Yahoo. Every number the
design predicted was reproduced: **502 prices / 96 events** in the default window with weights
**{major 16, medium 18, minor 62}**, `filter=major` → **97**, `filter=financials` → **100** (the
frozen D2 CORE count), and re-fetch idempotent at **2673 rows**.

**Read follow-up 1 first:** a concurrent external edit made General the default tab while I was
working. I restored Timeline per the design and backed the edit up — if that change was
intentional, re-applying it is three lines.

`make test` still fails only on the three **pre-existing** invalid files in `generated/`.

---

## What changed (file by file)

### New — `internal/companyview/`

| File | Contents |
|---|---|
| `classify.go` | The frozen category table: `WeightFor`, `IsEvent`, `WhyItMatters`, `KnownCategories`. Unknown categories → minor/not-event, warned once per value via `sync.Map` of `sync.Once`. |
| `timeline.go` | `Window`, `Event`, `PricePoint`, `PriceCoverage`, `Timeline`; `ClampWindow` (injected `now`), `BuildEvents`, `NormalizeFilter`; `ErrBadWindow`, `ErrCompanyHasNoFilings`. |
| `service.go` | `Service` + `Config`; `Timeline()` assembly, `pricesFor` (best-effort), `ensureCoverage` (per-CIK mutex, retry floor, ±5-day widening). |

### New — `internal/marketdata/`

| File | Contents |
|---|---|
| `provider.go` | `Bar`, `Quote`, `PriceProvider`; `ErrSymbolNotFound`, `ErrBadGranularity`, `ErrBadResponse`. |
| `yahoo.go` | `YahooProvider` with the three guards (period1/period2 only, `dataGranularity == "1d"`, content-type + shape), `gmtoffset` date conversion, nullable-array handling, `NewFromEnv`. |

### Database

- `internal/db/db.go` — migrations **v32** (`stock_prices`, with `adj_close`) and **v33**
  (`stock_price_coverage`) appended to `versionedMigrations`.
- `internal/db/stock_prices.go` (new) — `UpsertStockPrices` (transactional `ON CONFLICT DO UPDATE`,
  coverage recomputed in the same tx), `StockPrices`, `StockPriceCoverage`,
  `SetStockPriceCoverage`, `PriceCoverageRow.ShouldRetry`, status constants, `PriceRetryFloor`.

### Handlers and wiring

- `internal/handlers/companies.go` — `CompanyHandler.Timeline` field, `TimelineView` handler,
  `parseISODate`; `respondStoreErr` extended with `ErrBadWindow` → 400 and
  `ErrCompanyHasNoFilings` → 404.
- `internal/handlers/router.go` — new `CompanyDeps{Store, Timeline}` struct replacing the seventh
  positional parameter; route `apiCompanies.GET("/:cik/timeline", …)`.
- `cmd/server/main.go` — provider + service construction, `MARKET_DATA_PROVIDER` read.
- `.env.example` — market-data section.

### Frontend

- `static/company.html` — rewritten: tab bar (`role="tablist"`) + `.tab-container`; the prompt-1
  cards moved into `#tab-general` / `#tab-filings` **with every element id unchanged**; new
  timeline and events panels; the "coming soon" placeholder deleted.
- `static/js/company-page.js` — **deleted**.
- `static/js/company/shell.js` — registry, hash router, ARIA + roving tabindex + arrow keys,
  header render, the 404 path, shared `chip`/`metaItem`/`sortedEntries`.
- `static/js/company/tab-general.js`, `tab-filings.js` — verbatim moves.
- `static/js/company/tab-timeline.js` — category-axis chart, three scatter marker datasets,
  snap-forward index mapping, presets, lane toggle, `resize()` on show.
- `static/js/company/tab-events.js` — year-grouped curated event cards.
- `static/css/style.css` — `.tab-container`, `.timeline-*`, `.event-*` appended. `.tabs`,
  `.tab-btn`, `.tab-panel` reused untouched.

---

## Deviations from the design

**1. `internal/db` does not import `internal/marketdata`.** The LLD left this open (§6.2). Chose the
documented fallback: `db.PriceBar` is a local type and `companyview.Service` converts. Keeps the
low-level DB package free of provider concepts.

**2. `NewRouter` took a `CompanyDeps` struct rather than an eighth parameter.** The LLD recommended
this and left it to step 5; taken. `NewRouter`'s prompt-1 `companies filedb.CompanyStore` parameter
became `companies CompanyDeps`.

**3. `tab-events.js` detects curated events via the `why` field**, not a separate flag. The server
only emits `why` for `IsEvent` categories, so `!!e.why` is equivalent and needed no payload
addition.

**4. Timeline's "financials" lane reuses `filter=financials`** from one fetch and filters
client-side on `why`, so switching lanes issues no request — as the LLD required for the lane toggle.

**5. Two `ClampWindow` behaviors the LLD did not spell out.** A window entirely outside coverage
returns `ErrBadWindow` (400) rather than an empty clamp; a company with no filings returns
`ErrCompanyHasNoFilings` (404). Both are tested.

**6. Initial page load does not write the hash.** `switchTab(tabFromHash(), {skipHash: true})` on
boot, matching `admin.html:774`. An explicit tab click does write it. This keeps a bare
`/companies/:cik` URL clean while `#filings` still survives a reload.

**7. Restored the tab order/default after a concurrent external edit** — see follow-up 1. Not a
design deviation; a revert to what the design specifies.

---

## Tests

```
go test ./cmd/... ./internal/...        # all packages ok (companyview, marketdata, db, handlers new)
go test -tags corpus ./internal/filedb/ # ok — prompt 1's regression net still green
go vet ./internal/companyview/ ./internal/marketdata/ ./internal/db/ ./internal/handlers/ ./cmd/...   # clean
gofmt -l <files I authored>             # clean
make test                               # FAILS — pre-existing generated/ snippets only (see follow-up 2)
```

**New Go tests:** `companyview/classify_test.go` (all 15 categories; the measured 97/117/167 split
and the 100-event CORE count reproduced from a corpus fixture; **an AST guard asserting no
classification rule reads `Tier`/`TierLabel`/`HasFinancials`**, per D1),
`companyview/timeline_test.go` (11 `ClampWindow` cases, filter predicates, window bounds, weight
and why attachment), `marketdata/yahoo_test.go` (happy path; **`1mo` → `ErrBadGranularity`**;
HTML-with-200 → `ErrBadResponse`; 404 → `ErrSymbolNotFound`; null-close skip; a UTC+12 case proving
`gmtoffset` is applied; a test asserting the query never contains `range=`),
`db/stock_prices_test.go` (v32/v33 applied, upsert idempotence, range/order, coverage in the same
tx, retry floor), `handlers/timeline_test.go` (**events still served when the provider fails**,
null coverage without a ticker, server-side weights, filters, clamping, 400/404/503).

### Definition of Done — verified literally

Server built and run against the real corpus and live Yahoo. **Port 8123 was already held by the
user's own dev server, so verification ran on 8199 with its own DB; that process was left
untouched.**

| DoD element | Result |
|---|---|
| Unauthenticated `/timeline` | **401** |
| Migrations applied | v32, v33 present in `schema_migrations` |
| Default window | 2024-08-20 → 2026-08-20 (2y back from latest filing) |
| Prices / events | **502 / 96** — design predicted ~501 / ~96 |
| Weights | **{major 16, medium 18, minor 62}** — design predicted exactly this |
| `filter=major` / `financials` / `all` | **97 / 100 / 381** — 100 is the frozen D2 CORE count |
| Adjusted close stored and differs | `2016-01-04 close=4.2200 adj_close=3.9818` |
| Idempotent re-fetch | 505 → 2673 → **2673** rows |
| Clamping | `from=1990-01-01` → `2016-01-06` |
| Bad input | bad cik/from/to/inverted → **400**; unknown cik → **404**; nil service → **503** |
| Cold vs warm | **0.55 s** (real fetch) vs **0.002 s** |

**UI verified by executing the real modules** (`shell.js` → `tab-general` → `tab-filings` →
`tab-timeline` → `tab-events`, in the page's script order) against the live API under a DOM shim
that emulates browser `DOMContentLoaded` timing and `location.hash` normalization:
**45 assertions, passing for every start hash** (`none`, `#timeline`, `#filings`, `#general`,
`#events`, `#bogus`, `#TIMELINE`).

Those 45 include **all 22 prompt-1 regression assertions** (identity, coverage 2016-01-06 →
2026-08-20, 381 total, the 123 gap, 11 year chips, 6-K 242 / 20-F 11, top-8 + "+11 more", four tier
chips, 25 table rows, "1–25 of 381", filter option counts) — which is the evidence that General and
Filings were moved, not rewritten. Plus: one panel active at a time, ARIA/roving tabindex, hash
written on explicit switch but not on load, chart built with a **category** x-axis and ISO labels,
three weight datasets, markers carrying event payloads and sitting exactly on the price line, and
the events tab showing **100 curated events grouped by year** with why-text and no insider trades.

Regression check after the refactor: `/`, `/admin`, `/login`, `/companies`, `/companies/:cik` all
200; `company-page.js` now 404; all five module files 200; server log clean.

---

## How to enable / roll back

Live once rebuilt. Optional env:

```
MARKET_DATA_PROVIDER=yahoo         # only implementation today; unknown values fall back to yahoo
MARKET_DATA_API_KEY=               # unused by yahoo
STOCK_FETCH_ON_COMPANY_OPEN=false  # eager backfill on company open
```

**Roll back** by reverting the touched files. `stock_prices`/`stock_price_coverage` can be dropped;
nothing else reads them and Timeline degrades to events-only. No data migration to undo — both
tables start empty and fill lazily. To restore the pre-tab page, revert `static/company.html` and
restore `static/js/company-page.js` from git; the two prompt-1 endpoints are untouched.

Note: starting the server applies v32/v33 to whatever `DB_PATH` points at. They are additive
`CREATE TABLE IF NOT EXISTS`, and this already happened once against `.db/megane.db`.

---

## Follow-ups

1. **A concurrent external edit changed the default tab while I was working — please confirm which
   you want.** At 22:56 `static/js/company/shell.js` and `static/company.html` were modified
   (not by me) to reorder the tabs so **General** is first and default. That contradicts the idea
   file ("default `#timeline` once Timeline ships") and the DoD ("lands on Timeline"), so I restored
   Timeline-first. The edit is backed up at
   `…/scratchpad/{shell.js,company.html}.external-edit`. To re-apply intentionally: set
   `TAB_NAMES = ['general','timeline','filings','events']` and `DEFAULT_TAB = 'general'` in
   `shell.js:14-15`, and move the `active`/`aria-selected="true"` to the General button and panel in
   `company.html`. Everything else works unchanged either way.
2. **`generated/*/route_snippet.go` still breaks `go test ./...`** — three files that are code
   snippets, not valid Go, present at `HEAD`. Carried over from the prompt-1 report; renaming them
   to `.go.txt` is the one-line fix.
3. **Yahoo rate limiting is unmeasured** at multi-company scale (LLD §12.5). The per-CIK
   serialization and the v33 retry floor are the containment; watch the first real backfill.
4. **`All` preset is clamped to filing coverage**, so ~2.5 years of available price history
   (2013-05-31 →) is never shown. Open question 1 from the LLD; a one-line change in `ClampWindow`.
5. **`earnings_preview` remains excluded** from curated events (open question 2). Adding it moves
   per-year counts from 5–13 to 9–14 and total from 100 to 130 — one row in `categoryRules`.
6. **`internal/db/db.go` and `internal/handlers/router.go` remain `gofmt`-dirty** from pre-existing
   struct-tag/map alignment unrelated to this work; left alone to keep the diff honest.

---

## Touched files

**New (14):** `internal/companyview/{classify,timeline,service}.go` +
`{classify,timeline}_test.go` · `internal/marketdata/{provider,yahoo}.go` + `yahoo_test.go` ·
`internal/db/stock_prices.go` + `stock_prices_test.go` · `internal/handlers/timeline_test.go` ·
`static/js/company/{shell,tab-general,tab-filings,tab-timeline,tab-events}.js`

**Modified (6):** `internal/db/db.go` · `internal/handlers/companies.go` ·
`internal/handlers/router.go` · `cmd/server/main.go` · `.env.example` · `static/company.html` ·
`static/css/style.css`

**Deleted (1):** `static/js/company-page.js`

No commit was created.
