# HLD: Company Search & Company Overview (Phase 1)

Implements `prompts/dev/prompt_1_display_basic_company_data.txt`.
Triage: **HEAVY** — new package, new post-login surface, read-model that Phase 2 replaces with SQLite.
**Implementation language: Go only** (store, handlers, tests; see D9). Ingestion/enrichment: prompt 2.
Scope of this doc: contract + decisions. File-by-file changes belong in the LLD.

---

## 1. Objective

Give a signed-in user a company picker as the landing surface and a company page that
renders identity, filing coverage, and filing inventory for a company read directly from
the on-disk `fileDB/` tree. The read path goes behind a `CompanyStore` interface so Phase 2
can swap the file walker for SQLite without touching handlers, JSON shapes, or UI.

---

## 2. Data audit (measured, not assumed)

Run against the only company on disk, `fileDB/companies/0001567529` (KAMADA LTD / KMDA), 2026-08-25.

| Measure | Value |
|---|---|
| Accession folders on disk | **381** (11 year folders, 2016–2026) |
| `filing.json` / `meta.json` / `index.json` present | **381 / 381 / 381** (100%) |
| Entries in `submissions.json → filings.recent` | **504** |
| `filings.files` (SEC overflow shards) | `[]` — empty, no extra fetch needed |
| Indexed filings with **no folder on disk** | **123** |
| Folders **not present** in the index | **1** |
| Date range **on disk** (`filing.json.filingDate`) | **2016-01-06 → 2026-08-20** |
| Date range **in the index** | 2013-01-24 → 2026-08-20 |
| Full walk of 381 filings (baseline measurement, warm cache) | **119 ms** |
| `filing.json`+`meta.json` actual bytes | **313 KB** (~820 B/filing) |
| Whole `fileDB/` on disk | 645 MB, 4352 files, **gitignored** (`.gitignore` + `.cursorignore`; see `.graphifyignore`) |

Distributions on disk (the numbers the UI must render):

- **byForm** — `6-K` 242, `SC 13G/A` 34, `4` 26, `3` 20, `20-F` 11, `SC 13D/A` 8, `SC 13G` 6,
  `6-K/A` 5, then a tail of 11 more forms incl. `SCHEDULE 13G/A` 4, `F-3` 2, `20-F/A` 1.
- **byCategory** — `current_report` 80, **`unknown` 78**, `quarterly_results` 55, `governance` 33,
  `earnings_preview` 30, `insider_trade` 26, `business_deal` 23, `insider_initial` 20,
  `annual_report` 11, + 5 smaller.
- **byTier** — `MINOR` 163, `MODERATE` 117, `MAJOR` 97, `ROUTINE` 4 (`tier` 1–4 ↔ label).
- `signals.hasFinancials` true on **66** filings.

### What the audit changes in the spec

1. **The idea file's example JSON is index-derived, not disk-derived.** It shows
   `byForm {"6-K": 321, "20-F": 13}` and `earliestFilingDate 2016-02-02`. Those are
   `filings.recent` numbers. The disk truth is `6-K` 242 / `20-F` 11 and earliest `2016-01-06`.
   Decision D1 resolves which one ships; the sample response in §5 uses disk numbers.
2. **Tiers are four-valued, not two.** The idea file says "MAJOR / minor counts"; the data has
   `MAJOR / MODERATE / MINOR / ROUTINE`. `byTier` is an open map, and the UI must not hardcode two chips.
   The field is `tierLabel` (the idea file's alternate spelling `metaLabel` does not exist on disk).
3. **`category` is `unknown` on 20% of filings.** Not an error state — the UI needs a defined
   rendering for it (see D7), and it must not be the top chip in a "top categories" summary.
4. **`totalFilings: 381` and the "≥300" acceptance criterion both hold** under D1.
5. `.DS_Store` files sit inside year folders and are not directories — the walker must skip
   non-directory entries rather than assume every child of `{cik}/{year}/` is an accession.

---

## 3. Architecture decisions

### D1 — Disk is the source of truth; `submissions.json` supplies identity only

`coverage` and `filingsSummary` are computed **only** from accession folders that exist on disk.
`submissions.json` supplies `identity` (name, tickers, exchanges, SIC, incorporation, FYE,
category, addresses, phone, formerNames) and nothing that is counted.

Why: the product statement on the page is "we have filings from X to Y". Reporting 504 filings
and a 2013 start date while 123 of them have no folder would make every downstream feature
(filing detail, timeline, LLM analysis) 404 on a quarter of what the page advertises.

The gap is real and worth surfacing rather than hiding: `coverage` carries an
`indexedNotOnDisk` count so the page can show "381 filings on file (123 more known to SEC, not
yet downloaded)". This also becomes the natural hook for a future backfill job.

### D2 — `internal/filedb` with `CompanyStore`, plus a fourth method

The interface in the idea file covers three of the four things the endpoints need — the paginated
filings endpoint has no method. The interface ships as:

```go
type CompanyStore interface {
    ListCompanies(ctx context.Context) ([]CompanySummary, error)
    SearchCompanies(ctx context.Context, query string, limit int) ([]CompanySummary, error)
    GetCompany(ctx context.Context, cik string) (*CompanyDetail, error)
    ListFilings(ctx context.Context, cik string, f FilingFilter) (FilingPage, error)
}
```

`FilingFilter{Year int, Form string, Limit, Offset int}` → `FilingPage{Items []FilingRow, Total int}`.
Total is returned so the UI can page without a second call. Phase 1 impl: `FileDBStore`.
Phase 2: `SQLiteStore` — same signatures, handlers and JSON untouched.

Errors cross the boundary as sentinel values (`filedb.ErrCompanyNotFound`), not as `os.ErrNotExist`
or `*fs.PathError`; the handler maps sentinel → 404 and everything else → 500. If handlers
inspect filesystem errors, the SQLite swap breaks the 404 path.

### D3 — Read-through in-memory index, built lazily per company

A full walk is ~120 ms for one company (measured on the Kamada corpus). Even in Go it is
per-request work that grows linearly with company count, and the picker's search endpoint would
walk every company on every keystroke. That does not survive the second company, let alone the
`<200 ms` budget.

Decision: `FileDBStore` keeps two caches.

- **Company index** (all companies): `cik → {name, tickers, exchanges, formerNames}` parsed from
  each `submissions.json`. This is what `SearchCompanies` queries. Built on first use.
- **Per-company filing index**: the projected filing rows for one CIK, built on first `GetCompany`
  or `ListFilings` for that CIK, and reused for both.

Invalidation in Phase 1 is deliberately dumb: a TTL (default 5 min, env-tunable) plus a check of
the company directory's mtime. `fileDB/` is a static local corpus in this phase; a cron-driven
downloader is Phase 2's problem and will invalidate explicitly.

Memory ceiling, from the measured 820 B/filing of raw JSON: a projected row (accession, date,
form, category, tier, summary, tags, 2 flags) is ~200 B. 1,000 companies × 400 filings ≈ **80 MB**.
Acceptable for this phase; it is also the number that justifies Phase 2 rather than a bigger cache.
The LLD should state the cap at which the process refuses to preload and falls back to per-request walks.

### D4 — Pages are public static HTML; auth is client-side, as everywhere else in this app

`auth.AuthRequired()` (`internal/auth/middleware.go:22`) reads the JWT from the `Authorization:
Bearer` header only. There is no cookie and no session middleware. A browser navigation to
`/companies/0001567529` therefore **cannot** be authenticated server-side without inventing a
cookie auth path, which is out of scope for a phase-1 read feature.

Decision: follow the existing pattern exactly — `/companies` and `/companies/:cik` serve static
HTML unauthenticated (as `/` and `/admin` already do), the page calls `requireAuth()` on load,
and every `/api/companies/*` call carries the Bearer header via `apiFetch()`. The acceptance
criterion "all endpoints require auth; unauthenticated requests get 401" is about the **API**, and
is satisfied by the `auth.AuthRequired()` group. The LLD should say this out loud so the criterion
is not read as "the HTML page must 401".

### D5 — Route shape (verified, not assumed)

Probed against the pinned `gin-gonic/gin v1.9.1`: registering `/api/companies/search` beside
`/api/companies/:cik` and `/api/companies/:cik/filings`, and `/companies` (StaticFile) beside
`/companies/:cik` (GET), registers without panic and routes correctly to each. The static-sibling /
wildcard conflict that bites older httprouter versions does not apply here, so the clean path
shape ships — no `?cik=` fallback needed.

CIK is normalized at the handler edge: accept `1567529` or `0001567529`, left-pad to 10 digits,
reject anything non-numeric with 400 before it reaches the store. This keeps path traversal out of
`filepath.Join` by construction rather than by sanitizing.

### D6 — Post-login default becomes the picker

Three call sites redirect to `/` today: `static/login.html:33`, `static/login.html:56`, and the
`requireAuth()` bounce in `static/js/app.js:185`. The picker becomes the post-login destination by
pointing the two login redirects at `/companies` and adding a "Companies" nav link to the header in
`static/index.html`. `/` stays the upload/analyze page — nothing about the pipeline moves.

### D7 — Display contract for sparse fields

The audit shows real sparsity, so the shape of "missing" is part of the contract, not a UI detail:

- `category: "unknown"` renders as a muted "Uncategorized" chip and is excluded from any
  "top categories" summary while still counting in `byCategory`.
- Absent optional identity fields (`formerNames` is `[]`, `website`/`description`/`lei` are empty
  on the sample company) are omitted from the identity card, not rendered as "—" rows.
- `byForm` has a 19-form tail; the summary shows the top N by count with a "+N more" affordance
  rather than 19 bars.

### D8 — `escHtml` moves to `app.js`

It is currently defined twice — `static/js/analyze.js:3` and inline in `static/admin.html:914` —
and the new picker and company page would make it four. It moves to `static/js/app.js` beside the
other shared helpers, and the two existing definitions are deleted. This is the one refactor of
existing code this phase makes; it is in scope because the idea file mandates `escHtml()` on all
dynamic text and a third copy is not defensible.

### D9 — All server-side code is Go; no Python in the runtime path

This feature ships entirely in Go under `internal/filedb/` and `internal/handlers/`. The legacy
`python/edgar/` scripts are reference material only — they are **not** invoked by the server,
tests, or Makefile targets for this phase.

JSON on disk (`submissions.json`, `filing.json`, `meta.json`) is the contract between layers.
`internal/filedb` reads those files; ingestion and `meta.json` enrichment belong in the Go EDGAR
port (`prompts/dev/prompt_2_convert_from_python_to_go.txt` → `internal/edgar/`). Reuse
`internal/edgar/models` for shared struct shapes where the port lands first; do not duplicate
types or call Python from Go via `exec`.

Populating a fresh clone: `go run ./cmd/edgar-fetch` (or equivalent Makefile target), not
`python/edgar/fetch_kamada.py`. Unit and integration tests use committed fixtures under
`internal/filedb/testdata/` — never shell out to Python.

---

## 4. What this touches

| Area | Nature of change |
|---|---|
| `internal/filedb/` (new) | Store interface, `FileDBStore`, search ranking, aggregation, caches — **Go only** |
| `internal/edgar/models/` | Shared JSON structs for submissions / filing / meta (from prompt 2; consumed, not redefined) |
| `internal/handlers/` | New `CompanyHandler` (3 endpoints) + registration in `router.go` |
| `cmd/server/main.go` | Construct the store, read `FILEDB_DIR` (default `./fileDB`), pass to `NewRouter` |
| `static/` | `companies.html`, `company.html`, `js/companies.js`, `js/company-page.js` |
| `static/js/app.js` | `escHtml` promoted here |
| `static/index.html`, `static/login.html` | Nav link, post-login redirect |
| `static/css/style.css` | Chips/summary bars only if existing `card`/`table-wrap`/`badge` do not cover it |
| `.env.example` | `FILEDB_DIR`, cache TTL |

No changes to: pipeline, LLM client, prompts, `internal/models`, DB schema, migrations, admin.

---

## 5. API contract

All under `/api`, all behind `auth.AuthRequired()`, all responding `{"data": …, "error": …}`.

| Method | Path | 200 payload | Errors |
|---|---|---|---|
| GET | `/api/companies/search?q=&limit=` | `[{cik, name, tickers, exchanges}]`, ≤20 | 400 empty `q` |
| GET | `/api/companies/:cik` | `{identity, coverage, filingsSummary}` | 400 bad CIK, 404 unknown |
| GET | `/api/companies/:cik/filings?year=&form=&limit=&offset=` | `{items, total, limit, offset}` | 400, 404 |

Detail response, with the **disk-derived** values this company actually produces:

```json
{
  "identity": {
    "cik": "0001567529", "name": "KAMADA LTD",
    "tickers": ["KMDA"], "exchanges": ["Nasdaq"],
    "sic": "2834", "sicDescription": "Pharmaceutical Preparations",
    "stateOfIncorporation": "Israel", "fiscalYearEnd": "1231",
    "category": "Accelerated filer",
    "hq": {"city": "REHOVOT", "country": "Israel"}, "phone": "97289406472",
    "formerNames": []
  },
  "coverage": {
    "earliestFilingDate": "2016-01-06", "latestFilingDate": "2026-08-20",
    "yearsOnDisk": [2016, 2017, 2018, 2019, 2020, 2021, 2022, 2023, 2024, 2025, 2026],
    "totalFilings": 381, "indexedNotOnDisk": 123
  },
  "filingsSummary": {
    "byForm": {"6-K": 242, "SC 13G/A": 34, "4": 26, "3": 20, "20-F": 11},
    "byCategory": {"current_report": 80, "unknown": 78, "quarterly_results": 55},
    "byTier": {"MAJOR": 97, "MODERATE": 117, "MINOR": 163, "ROUTINE": 4},
    "hasFinancialsCount": 66
  }
}
```

Search ranking is a pure, exported, table-testable function over the cached index —
exact ticker → name prefix → name substring → CIK prefix, ties broken by name — so the
ranking test does not need a filesystem.

---

## 6. Phase 2 migration path (documented now, built later)

The interface is the seam, and D1/D3 are the two decisions that make the swap mechanical:

- Two tables (`companies`, `filings`) mirroring `CompanySummary` and the projected filing row.
- A one-shot **Go** importer walks `fileDB/` once and writes rows — the same walk `FileDBStore` already
  performs, extracted as a reusable scanner package rather than rewritten.
- `byForm` / `byCategory` / `byTier` become `GROUP BY` queries; the in-memory caches in D3 are
  deleted, not ported.
- Cutover is a constructor swap in `cmd/server/main.go`. Handlers, JSON, and both pages are untouched.
  If that turns out to be false, D2 was violated somewhere and the LLD's tests should be the ones to catch it.

---

## 7. Out of scope

SQLite migration · timeline / price correlation · live SEC or market-data APIs · filing detail
pages and LLM analysis of accession folders · admin company management · full-text search over
filing bodies · multi-company comparison. The timeline note in the idea file (use `filingDate` +
`meta.category` as events, highlight `tier` MAJOR) is preserved here as the input to that prompt.

---

## 8. Risks

1. **Single-company corpus.** Every ranking and aggregation decision is validated against one
   company. Search ranking in particular is untested at N>1. Mitigation: ranking is a pure function
   with synthetic-fixture table tests, not a live-corpus test.
2. **`fileDB/` is local-only.** It is gitignored and excluded from Cursor/Claude indexes; clones start
   empty and must run the Go fetch CLI (prompt 2) to populate. Docs and tests must not assume the
   Kamada corpus is present on every machine — use `testdata/` fixtures.
3. **Cache staleness** is invisible to the user in Phase 1. A TTL plus mtime check is the mitigation;
   it is wrong the moment a downloader writes into `fileDB/` concurrently, which is Phase 2's design point.
4. **`byCategory` quality.** 20% `unknown` means category chips are a weaker navigation signal than
   the idea file assumes. Form type is the reliable axis; the LLD should lead the summary with it.
5. **Test fixtures.** Tests that read the real 645 MB tree are slow and machine-dependent. A small
   committed fixture tree (3 companies, ~6 filings) under `internal/filedb/testdata/` is the unit of
   truth; one build-tagged Go test (`//go:build integration`) may optionally assert against a locally
   populated `fileDB/` — never Python scripts.
6. **Prompt 2 ordering.** If `internal/edgar/models` is not merged yet, `filedb` may define minimal
   read structs locally, but they must be deleted in favor of the shared package as soon as the EDGAR
   port lands — one JSON shape, one set of Go types.

---

## 9. Done when

A signed-in user lands on `/companies`, types `kmda` (or `Kamada`, or `1567529`), clicks the single
result, and `/companies/0001567529` shows **KAMADA LTD · KMDA · Nasdaq**, "filings from 2016-01-06
to 2026-08-20", **381** total, a form breakdown led by **6-K 242** and **20-F 11**, and a filings
table whose first page renders without fetching all 381 rows. `make test` passes with new unit
tests for search ranking and the aggregation helpers.

---

## 10. Deliverables

- `internal/filedb/` — interface, `FileDBStore`, ranking, aggregation, caches, `testdata/` (Go)
- `internal/handlers/companies.go` + `router.go` registration
- `cmd/server/main.go` wiring and `FILEDB_DIR` env
- `static/companies.html`, `static/company.html`, `static/js/companies.js`, `static/js/company-page.js`
- `escHtml` in `app.js`; duplicates removed from `analyze.js` and `admin.html`
- Nav link + post-login redirect change
- Unit tests (Go): search ranking, CIK normalization, coverage/summary aggregation, disk-vs-index reconciliation
- `prompt_1_display_basic_company_data-lld.md` — file-by-file changes and build order

**Language:** server, store, handlers, tests, and corpus fetch/enrichment are **Go only**.
`python/edgar/` is not part of this deliverable.