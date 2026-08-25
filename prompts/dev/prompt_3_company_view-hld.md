# HLD: Tabbed Company View + Timeline (Phase 3)

Implements `prompts/dev/prompt_3_company_view.txt`.
Triage: **HEAVY** — first DB migration for domain data, first outbound market-data dependency,
refactor of a shipped surface, plus a curation rule that the data says must be redesigned.
Scope of this doc: contract + decisions. File-by-file changes belong in the LLD.

Builds on prompt 1 (shipped — see `prompt_1_display_basic_company_data-implementation.md`).
Relationship to prompt 2 is a **non-dependency**; see D9.

---

## 1. Objective

Turn `/companies/:cik` from one vertical scroll into a hash-routed tabbed view, and ship
**Timeline** — daily price line with filing events overlaid, weighted so earnings and major
events stand out — as the first fully designed tab. General and Filings move into tabs with no
behavior change. Prices are fetched server-side once and cached in SQLite so window changes are
instant.

---

## 2. Data audit (measured, not assumed)

Measured 2026-08-25 against `fileDB/companies/0001567529` (KAMADA LTD / KMDA), 381 filings, and
against the live market-data providers the idea file proposes.

### 2.1 The classification signals are not independent

`meta.json` exposes `category`, `tier`, `tierLabel`, and `signals.hasFinancials`. Across all 381
filings, **`tier`, `tierLabel`, and `hasFinancials` are pure functions of `category`** — zero
categories have more than one distinct `(tier, tierLabel, hasFinancials)` triple:

| category | tier | tierLabel | hasFin | count |
|---|---|---|---|---|
| `current_report` | 2 | MODERATE | false | 80 |
| `unknown` | 3 | MINOR | false | 78 |
| `quarterly_results` | 1 | MAJOR | **true** | 55 |
| `governance` | 2 | MODERATE | false | 33 |
| `earnings_preview` | 3 | MINOR | false | 30 |
| `insider_trade` | 3 | MINOR | false | 26 |
| `business_deal` | 1 | MAJOR | false | 23 |
| `insider_initial` | 3 | MINOR | false | 20 |
| `annual_report` | 1 | MAJOR | **true** | 11 |
| `investor_relations` | 3 | MINOR | false | 9 |
| `regulatory_clinical` | 1 | MAJOR | false | 8 |
| `ownership_disclosure` | 4 | ROUTINE | false | 4 |
| `corporate_action` | 2 | MODERATE | false | 3 |
| `admin_update` | 2 | MODERATE | false | 1 |

This is by construction, not coincidence: `python/edgar/build_meta.py:_classify` returns
`(tier, category, tags)` as a fixed pair at every return site, and `hasFinancials` is literally
`category in ("annual_report", "quarterly_results")` (`build_meta.py:249`) — which is exactly the
measured 55 + 11 = 66.

### 2.2 What that does to the idea file's rules

Both rule sets in the idea file combine `tier` and `category` with **OR**, as though they were
independent evidence. Because they are the same signal, the OR degenerates to "any tier-1 or
tier-2 category", and the filters barely filter:

| Rule | Matches | % of 381 | per-year min/med/max |
|---|---|---|---|
| **Special events, as written in the idea file** | **244** | 64% | 12 / 22 / 29 |
| …the same rule minus the redundant form/tag clauses | 214 | 56% | 8 / 19 / 29 |
| `category` in CORE¹ only | **100** | 26% | 5 / 9 / 13 |
| CORE + `earnings_preview` | 130 | 34% | 9 / 12 / 14 |
| `tier == 1`, excluding insider/ownership/unknown | 97 | 25% | 5 / 8 / 13 |

¹ CORE = `quarterly_results`, `annual_report`, `business_deal`, `regulatory_clinical`,
`corporate_action`.

The idea file's stated target is "the ~10–30 events that matter most" and an acceptance criterion
of "count ≪ total filings". **244 of 381 is neither.** D2 resolves this.

Note also `annual_guidance` appears in the idea file's allowlist and in the classifier
(`build_meta.py:132`) but **never fires** for this company — zero occurrences.

### 2.3 Timeline marker density

Under the idea file's weight rules, over the whole corpus: major 97 (25%), medium 117 (31%),
minor 167 (44%). In the **default 2-year window**: 96 filings — 16 major, 18 medium, 62 minor,
about **0.19 markers per trading day**. Legible only if minor markers are muted and filterable,
which the idea file already calls for.

### 2.4 Market data providers — the recommended one does not work

| Provider | Result |
|---|---|
| **Stooq** CSV (`stooq.com/q/d/l/?s=kmda.us&i=d`) — the idea file's preferred MVP | **HTTP 200 but not CSV.** Returns a JavaScript proof-of-work browser challenge requiring `crypto.subtle` SHA-256 and a POST to `/__verify`. Unusable from a Go HTTP client. |
| **Yahoo** v8 chart (`query1.finance.yahoo.com/v8/finance/chart/KMDA`) | Works: JSON, no API key, `currency=USD`, `exchangeName=NMS`, adjclose present, 0 null closes. |

The 200-with-HTML failure mode matters: a naive CSV parse would store zero rows or garbage
**without any error surfacing**.

Yahoo has one trap of its own, also measured:

| Request | Bars | Range | Actual granularity |
|---|---|---|---|
| `range=max&interval=1d` | 160 | 2013-06-01 → 2026-08-25 | **`1mo`** — silently downgraded |
| `range=2y&interval=1d` | 501 | 2024-08-26 → 2026-08-25 | `1d` |
| `period1`/`period2` 2013→now, `interval=1d` | **3329** | 2013-05-31 → 2026-08-25 | `1d` |

Asking for "all history at daily resolution" the obvious way returns **monthly** data and a chart
that looks plausible but is wrong. The response echoes `meta.dataGranularity`, which is the
mitigation (D3).

Price history (2013-05-31) starts **before** filing coverage on disk (2016-01-06), so the "All"
window is bounded by filings, not prices.

### 2.5 Schema and asset facts

- **Migrations do not live in `internal/db/migrations.sql`.** That file is referenced by no Go
  code (no `go:embed` anywhere). The source of truth is the `versionedMigrations` slice in
  `internal/db/db.go:48`, currently at **v31** with all 31 applied in the dev DB. The idea file's
  "append to `internal/db/migrations.sql`" would be a silent no-op.
- **Tabs already exist in this codebase.** `.tabs`, `.tab-btn`, `.tab-panel[.active]` are defined
  at `static/css/style.css:413-439`, and `static/admin.html:774` has a working hash-routed
  `switchTab(name, opts)` with `hashchange` and lazy per-tab loading. The idea file proposes new
  `.company-tabs` / `.tab-panel` classes; `.tab-panel` would collide.
- **Chart.js is v4.4.6, bundled locally. `chartjs-plugin-annotation` is not present** anywhere in
  `static/js/`. The idea file's "line + annotation plugin" implies a new vendored dependency.

---

## 3. Architecture decisions

### D1 — Weight and curation key on `category` alone; tier is never a separate term

Given §2.1, a rule of the form `tier <= 2 OR category in {…}` is a bug dressed as a heuristic. All
event classification uses one server-side lookup table keyed by `category`, producing `weight`
(`major` | `medium` | `minor`) and an `isEvent` flag. `tier`/`tierLabel` are still **returned** in
the payload for display, but never participate in a filter predicate.

The table is the single place this judgment lives, so re-tuning is a one-file diff, and it is
table-testable without a filesystem. An unrecognized category (a future classifier value) defaults
to `minor` / not-an-event and is logged once — fail quiet in the UI, loud in the logs.

### D2 — Special events: CORE allowlist, ~100 total / 5–13 per year, grouped by year

Frozen from the measured options in §2.2: **`category` in CORE** — `quarterly_results`,
`annual_report`, `business_deal`, `regulatory_clinical`, `corporate_action`. That is 100 of 381
(26%), 5–13 per year, median 9.

This reconciles the idea file's two conflicting targets: "~10–30 events" is achievable **per
calendar year**, which is also how the idea file says to present them ("Group by calendar year").
It is not achievable across an 11-year corpus without dropping whole years. The tab therefore
renders year-grouped, newest year first, and the acceptance criterion is restated as *per-year
count in 5–15*, not a corpus-wide total.

Deliberately excluded, with reasons worth recording:

- `current_report` (80) — the classifier's fallback bucket (`build_meta.py:158,160`, "no rule
  matched"), not a positive signal. Including it is what pushes the count past 200.
- `governance` (33) — AGM/proxy mechanics; real, but not the investor narrative this tab is for.
- `earnings_preview` (30) — genuinely investor-relevant ("will announce results on…"), but it is
  the *announcement of* an event the tab already shows. Listed as the first candidate to add if
  the tab reads too sparse; adding it moves the count to 130 / 9–14 per year.
- `unknown` (78) — 20% of the corpus and by definition uninformative.

### D3 — Yahoo v8 behind a `PriceProvider` interface; Stooq rejected on evidence

MVP provider is Yahoo v8 chart, selected because §2.4 shows the idea file's preferred Stooq
cannot be fetched server-side at all. It sits behind a narrow interface so the choice is reversible:

```go
type PriceProvider interface {
    DailyBars(ctx context.Context, symbol string, from, to time.Time) ([]Bar, error)
}
```

Three non-negotiable behaviors, each traceable to a measured failure mode:

1. **Always request `period1`/`period2` with `interval=1d`.** Never `range=max` (§2.4).
2. **Validate `meta.dataGranularity == "1d"` on every response** and reject the batch otherwise.
   This is the guard against silently storing monthly bars.
3. **Validate content type and shape before parsing.** A 200 carrying HTML is a provider failure,
   not an empty result set — it must surface as an error, never as "no prices".

Yahoo's v8 endpoint is undocumented and unversioned; that is a real risk (§8), and the interface
plus these guards are the containment. `MARKET_DATA_PROVIDER` selects the implementation;
`.env.example` documents it.

### D4 — `stock_prices` ships as migration **v32** in `versionedMigrations`

Not in `migrations.sql`, which is dead (§2.5). Schema as the idea file specifies, with `cik` and
`trade_date` as the composite primary key so re-fetch is an idempotent `INSERT … ON CONFLICT DO
UPDATE`. The stated PK already covers the idea file's proposed index, so
`idx_stock_prices_cik_date` is redundant and is dropped.

Sizing, measured: Kamada's full daily history is **3329 rows**. At ~100 B/row that is ~330 KB per
company — the idea file's estimate holds. This is a third table alongside prompt 1's planned
`companies` and `filings`, not a competing store.

Currency is stored per row from the provider (`meta.currency`), because foreign private issuers
are exactly the population this product targets and a hardcoded USD default would be wrong for a
Tel Aviv or London listing later.

### D5 — The tab shell reuses the existing tab pattern rather than inventing one

`.tabs` / `.tab-btn` / `.tab-panel[.active]` already exist and already implement
"hide with `display:none`, keep the DOM mounted" — exactly what the idea file asks for. The
company view reuses those classes and follows `admin.html:767-792`'s hash-router shape
(`TAB_NAMES`, hash→tab on load, `hashchange`, lazy per-tab load).

Two additions the admin pattern lacks and this page needs: ARIA (`role="tablist|tab|tabpanel"`,
`aria-selected`, arrow-key roving focus) and the no-jump container. The idea file's proposed
`.company-tabs` class is dropped; `.tab-panel` would have collided with the existing definition.

Chart state is preserved across tab switches precisely because panels are hidden, not unmounted —
a Chart.js instance in a `display:none` container survives, and Timeline must call `chart.resize()`
on show since Chart.js cannot measure a hidden canvas.

### D6 — Event markers use a Chart.js dataset, not a new plugin dependency

`chartjs-plugin-annotation` is not vendored (§2.5) and adding it means a new ~40 KB third-party
asset. Markers are instead a **second scatter dataset** on the same chart: x = filing date, y =
that day's close (so markers sit on the price line), with `pointRadius`/`pointStyle`/color driven
by `weight`. This needs no new library, gives per-point tooltips for free, and lets the
`Major only | Financials | All` toggle simply swap the dataset's point set.

If a future need genuinely requires vertical spanning lines across the full plot height, vendoring
the annotation plugin is the escape hatch — but it is not required to ship P0.

### D7 — Prices are fetched lazily on first Timeline open, and the fetch never blocks the page

Sequence: client opens `#timeline` → `GET /api/companies/:cik/timeline?from&to` → handler reads
`stock_prices` for the window → on a gap, calls the provider for the missing span, upserts, and
returns. Subsequent window changes are DB-only.

The handler returns events **even when prices are unavailable** — no ticker, provider down,
provider returning HTML. Timeline degrades to an events-only chart with an explicit banner rather
than an empty panel, because the events half depends on nothing external. `priceCoverage` in the
payload is what the UI uses to say which half is missing.

Outbound fetches are bounded by a per-request timeout well under the 60 s route timeout and are
serialized per CIK, so a slow provider cannot pile up goroutines. `STOCK_FETCH_ON_COMPANY_OPEN`
stays default-off: eager fetching on every company open would hit the provider for users who never
open Timeline.

### D8 — No layout jump via a fixed-height panel container

`min-height: 60vh` on the shared panel container, per the idea file's second option. Measuring the
tallest panel is fragile (the Filings table's height depends on data) and requires a layout pass
per switch. 60vh is one CSS line, is data-independent, and the Filings table already scrolls
inside `.table-wrap`.

### D9 — This phase does not depend on prompt 2

The idea file says events consume "`meta.json` categories from prompt 2". Prompt 2 is **designed
but not built** — `prompt_2_convert_from_python_to_go-{hld,lld}.md` exist, no implementation doc.

No dependency actually exists: the categories are already on disk and already served by prompt 1's
`filedb` (`FilingRow.Category`). Prompt 2's D3 ports classification *literally* and locks it with
parity tests, and its D7 pins `Meta`'s JSON tags to the existing on-disk schema — so the values in
§2.1 are stable across that port by design. Prompt 3 reads what prompt 1 already exposes; the two
can land in either order.

---

## 4. What this touches

| Area | Nature of change |
|---|---|
| `internal/db/db.go` | migration **v32** `stock_prices`; price upsert + range-query helpers |
| `internal/marketdata/` (new) | `PriceProvider` interface, Yahoo v8 impl, `Bar`, granularity/shape guards |
| `internal/companyview/` (new) | category→`weight`/`isEvent` table, timeline assembly, window clamping |
| `internal/handlers/companies.go` | `GET /api/companies/:cik/timeline` (+ `/events` only if not client-filtered) |
| `internal/handlers/router.go` | one route registration inside the existing `apiCompanies` group |
| `cmd/server/main.go` | construct provider + price store, pass to the handler |
| `static/company.html` | tab bar + panels; existing cards re-homed, placeholder card deleted |
| `static/js/company/` (new) | `shell.js`, `tab-general.js`, `tab-filings.js`, `tab-timeline.js`, `tab-events.js` |
| `static/js/company-page.js` | deleted — its functions move verbatim into the tab modules |
| `static/css/style.css` | `.timeline-*` only; tab classes reused as-is |
| `.env.example` | `MARKET_DATA_PROVIDER`, `MARKET_DATA_API_KEY`, `STOCK_FETCH_ON_COMPANY_OPEN` |

Unchanged: `internal/filedb` (no search/picker/scan changes), pipeline, LLM, prompts, auth.

---

## 5. API contract

New, inside the existing `apiCompanies` group and therefore already behind `auth.AuthRequired()`:

| Method | Path | 200 payload | Errors |
|---|---|---|---|
| GET | `/api/companies/:cik/timeline?from=&to=&filter=` | `{window, ticker, prices[], events[], priceCoverage}` | 400 bad CIK/date, 404 unknown company |

- `from`/`to` are `YYYY-MM-DD`, clamped server-side to filing coverage; absent → default 2-year window.
- `filter` ∈ `major|financials|all`, default `all`. Server-side so the wire payload stays small.
- `events[].weight` is computed server-side per D1 — the chart never re-derives it.
- `prices[]` carries `{date, close, volume}`; OHLC is stored but not shipped until a candlestick view needs it.
- `priceCoverage: {earliest, latest, gaps[]}` — `null` when the company has no ticker, which the UI
  renders as an explicit banner rather than an empty chart.

Payload size, default 2-year window: ~501 price points + 96 events ≈ 30–40 KB uncompressed.

---

## 6. Migration and backfill

- **v32** appended to `versionedMigrations`; append-only, never edit v1–v31. `CREATE TABLE IF NOT
  EXISTS` keeps it re-runnable, and the migration runner already records the version.
- **No backfill.** `stock_prices` starts empty and fills lazily per D7. There is no historical data
  to migrate — this is a new capability, not a reshaping of existing rows.
- **Rollback** is dropping the table; nothing else reads it, and Timeline degrades to events-only.
- Prompt 1's Phase 2 (`companies`, `filings` in SQLite) is unaffected: `stock_prices` shares only
  the `cik` string, with no FK, so the two can land in either order.

---

## 7. Ship order

**P0** — tab shell + hash routing (D5, D8) · General and Filings refactored with zero behavior
change · v32 migration · `marketdata` + Yahoo provider (D3) · `/timeline` endpoint · Timeline tab.
**P1** — Special events tab on the D2 rule.
**P2+** — Ownership, Financials, Documents tabs stay hidden until built, per the idea file.

The General/Filings refactor lands before Timeline: it is pure motion with a shipped, verified
baseline to diff against, and doing it first means Timeline is built inside the final shell.

---

## 8. Out of scope

Intraday/real-time quotes · options · indices · peer comparison · filing body viewer · LLM event
significance · WebSocket streaming · admin event editing · `#filings?accession=` deep links ·
replacing `fileDB` with SQLite (still prompt 1 Phase 2) · candlestick/OHLC rendering.

---

## 9. Risks

1. **Yahoo v8 is undocumented and unversioned.** It can change shape or start rate-limiting without
   notice, and its ToS for programmatic access is grey. Contained by the `PriceProvider` interface
   (D3) and by Timeline degrading to events-only (D7). If it becomes untenable, a keyed provider
   (Tiingo, Alpha Vantage, Polygon) is a one-implementation swap — but that trades free for a
   credential, which is a product decision, not a technical one.
2. **Silent granularity downgrade** (§2.4) is the highest-consequence bug available here: it
   produces a chart that looks right and is wrong. The `dataGranularity` assertion in D3 is
   mandatory, and the LLD should require a test that feeds a `1mo` fixture and asserts rejection.
3. **The curation rule is tuned on one company.** D2's CORE allowlist yields 5–13/year for Kamada;
   a company with different filing habits could read sparse or noisy. The lookup table (D1) is one
   file, and `earnings_preview` is the pre-identified first dial to turn.
4. **Marker density** at 0.19/trading-day in the default window (§2.3) means the `All events` lane
   will look busy. Mitigated by muted minor styling and the filter toggle; if it still reads
   poorly, the default lane should be `Major only` rather than `All`.
5. **Refactor regression.** General and Filings are shipped, verified behavior (prompt 1's DoD).
   Moving them into modules risks silently breaking pagination, filters, or the company-not-found
   path that hides `identity-card`/`coverage-card`/`summary-card` by id. The LLD must treat prompt
   1's render assertions as the regression baseline and re-run them against the tabbed page.

---

## 10. Done when

A signed-in user opens `/companies/0001567529`, lands on **Timeline**, and sees KMDA's daily close
for the last two years with filing markers on the line — quarterly results and other MAJOR-category
events visibly heavier than routine 6-Ks — and hovering a marker shows its date, category, and
summary. Switching to **General** and **Filings** shows exactly what prompt 1 shipped, with no
change in page height; the URL reads `#general` / `#filings` and reloading it returns to that tab.
`make test` passes, including tests for the category→weight table and for rejecting a non-daily
provider response.

---

## 11. Deliverables

- `prompt_3_company_view-lld.md` — file-by-file changes, build order, the D2 rule as a frozen table
- `internal/marketdata/` — interface, Yahoo v8 impl, guards, tests
- `internal/companyview/` — weight/event table, timeline assembly, clamping, tests
- `internal/db` — v32 migration + price store helpers + tests
- `/api/companies/:cik/timeline` handler + tests
- `static/js/company/{shell,tab-general,tab-filings,tab-timeline,tab-events}.js`, `company.html`,
  `.timeline-*` CSS
- Regression evidence that General and Filings still match prompt 1's verified behavior
