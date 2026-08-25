# LLD: Company Search & Company Overview (Phase 1)

Implements `prompt_1_display_basic_company_data.txt` under the contract frozen in
`prompt_1_display_basic_company_data-hld.md`. Triage: **HEAVY** — new package, new post-login
surface, read-model that Phase 2 replaces with SQLite.

The HLD's decisions are the inputs here and are not re-argued. One-line recap:
**D1** disk is truth, `submissions.json` is identity-only · **D2** `CompanyStore` + a fourth
`ListFilings` method · **D3** lazy in-memory caches · **D4** pages are public static HTML, auth is
client-side Bearer · **D5** `/api/companies/{search,:cik,:cik/filings}` verified conflict-free on
gin v1.9.1 · **D6** post-login default → `/companies` · **D7** sparse-field display contract ·
**D8** `escHtml` promoted to `app.js`.

---

## 1. Scope

**In:** `internal/filedb` package · 3 read endpoints · 2 static pages + 2 JS files · nav and
post-login redirect · `escHtml` de-duplication · unit tests · `FILEDB_DIR` env.

**Out:** SQLite tables/migrations · timeline · live SEC/market APIs · filing detail pages ·
LLM analysis of accessions · admin company management · full-text search over filing bodies ·
i18n wiring (`window.I18N` is vestigial — never assigned by any page; new pages use plain English
like `index.html` and `admin.html` do).

---

## 2. Current state (verified)

| Fact | Anchor |
|---|---|
| Routes registered in one func | `internal/handlers/router.go:29` `NewRouter(...)`, called once at `cmd/server/main.go:120` |
| Auth is Bearer-header only, no cookie | `internal/auth/middleware.go:22-42` |
| Response shape | `gin.H{"data": …, "error": …}` — e.g. `internal/handlers/files.go:246` |
| Env helper | `getEnv(key, default)` at `cmd/server/main.go:141` |
| JS API helper throws on `body.error` | `static/js/app.js:211-221` `apiFetch()` |
| Auth bounce | `static/js/app.js:185` `requireAuth()` → `/login` |
| Post-login redirects to `/` | `static/login.html:33` and `static/login.html:56` |
| Header nav | `static/index.html:14-17` |
| `escHtml` defined twice | `static/js/analyze.js:3-8` and inline `static/admin.html:914-916` |
| `app.js` loads before both consumers | `index.html:85-87`, `admin.html:751-753` |
| Reusable CSS | `.card` `:188`, `.table-wrap` `:234`, `.badge` `:269`, `.meta-grid/-item/-label/-value` `:558-585`, `.muted` `:182`, `.cell-muted` `:177`, `input[type=text]` `:350` |
| No `.chip` / bar-chart class exists | new CSS needed (§7) |
| Go test style | plain table tests, no testify — `internal/handlers/pipeline_status_test.go` |
| `testdata/` convention exists | `internal/datamodeling/testdata` |

Measured corpus facts (full audit in HLD §2): 381 accession folders, 100% have
`filing.json`/`meta.json`/`index.json`, `filings.recent` has 504 entries, 123 indexed filings have
no folder, disk range 2016-01-06 → 2026-08-20, `.DS_Store` present inside year folders,
`filings.files` is `[]`.

---

## 3. New package `internal/filedb`

Seven source files, so the Phase 2 seam is a file-level swap rather than a diff inside one blob.

### 3.1 `models.go`

```go
type CompanySummary struct {
    CIK       string   `json:"cik"`
    Name      string   `json:"name"`
    Tickers   []string `json:"tickers"`
    Exchanges []string `json:"exchanges"`
}

type Address struct {
    City    string `json:"city,omitempty"`
    Country string `json:"country,omitempty"`
}

type Identity struct {
    CIK, Name              string   `json:"cik"` // …see §5 for the exact JSON
    Tickers, Exchanges     []string
    SIC, SICDescription    string
    StateOfIncorporation   string
    FiscalYearEnd          string
    Category               string
    HQ                     *Address `json:"hq,omitempty"`
    Phone                  string   `json:"phone,omitempty"`
    FormerNames            []string `json:"formerNames,omitempty"`
}

type Coverage struct {
    EarliestFilingDate string `json:"earliestFilingDate"`
    LatestFilingDate   string `json:"latestFilingDate"`
    YearsOnDisk        []int  `json:"yearsOnDisk"`
    TotalFilings       int    `json:"totalFilings"`
    IndexedNotOnDisk   int    `json:"indexedNotOnDisk"`
}

type FilingsSummary struct {
    ByForm             map[string]int `json:"byForm"`
    ByCategory         map[string]int `json:"byCategory"`
    ByTier             map[string]int `json:"byTier"`
    HasFinancialsCount int            `json:"hasFinancialsCount"`
}

type CompanyDetail struct {
    Identity       Identity       `json:"identity"`
    Coverage       Coverage       `json:"coverage"`
    FilingsSummary FilingsSummary `json:"filingsSummary"`
}

type FilingRow struct {
    AccessionNumber string   `json:"accessionNumber"`
    FilingDate      string   `json:"filingDate"`
    ReportDate      string   `json:"reportDate,omitempty"`
    Form            string   `json:"form"`
    Year            int      `json:"year"`
    Category        string   `json:"category"`
    Tier            int      `json:"tier"`
    TierLabel       string   `json:"tierLabel"`
    Summary         string   `json:"summary,omitempty"`
    Tags            []string `json:"tags,omitempty"`
    HasFinancials   bool     `json:"hasFinancials"`
    ExhibitCount    int      `json:"exhibitCount"`
}

type FilingFilter struct{ Year int; Form string; Limit, Offset int }
type FilingPage struct {
    Items  []FilingRow `json:"items"`
    Total  int         `json:"total"`
    Limit  int         `json:"limit"`
    Offset int         `json:"offset"`
}
```

Maps and slices are initialized non-nil before returning so JSON is `{}`/`[]`, never `null` —
the renderers in §6 then need no null guards.

### 3.2 `store.go` — interface and sentinels

```go
var (
    ErrCompanyNotFound = errors.New("filedb: company not found")
    ErrInvalidCIK      = errors.New("filedb: invalid cik")
)

type CompanyStore interface {
    ListCompanies(ctx context.Context) ([]CompanySummary, error)
    SearchCompanies(ctx context.Context, query string, limit int) ([]CompanySummary, error)
    GetCompany(ctx context.Context, cik string) (*CompanyDetail, error)
    ListFilings(ctx context.Context, cik string, f FilingFilter) (FilingPage, error)
}
```

Per HLD D2: handlers map `errors.Is(err, ErrCompanyNotFound)` → 404 and everything else → 500.
Handlers must never inspect `os.ErrNotExist` or `*fs.PathError`; that is the assertion that keeps
the SQLite swap honest, and `store_test.go` covers it.

### 3.3 `cik.go`

```go
func NormalizeCIK(s string) (string, error) // "1567529" | "0001567529" | "CIK0001567529" → "0001567529"
```

Strips a `CIK` prefix, trims space, rejects anything left that is not `[0-9]{1,10}` with
`ErrInvalidCIK`, left-pads to 10. Path traversal is excluded by construction, not by sanitizing:
nothing that fails this check ever reaches `filepath.Join`.

### 3.4 `submissions.go`

```go
type submissionsFile struct{ … }                       // json tags matching SEC shape
func parseSubmissions(path string) (*submissionsFile, error)
func (s *submissionsFile) identity() Identity
func (s *submissionsFile) indexedAccessions() map[string]struct{}   // from filings.recent
```

`identity()` maps `stateOfIncorporationDescription` → `StateOfIncorporation` (the description, not
the `L3` code) and builds `HQ` from `addresses.business` falling back to `addresses.mailing`,
using `city` + `stateOrCountryDescription`. Empty `website`/`description`/`lei` are dropped
(HLD D7). `filings.files` is measured empty and is **not** read — if a future company has shards,
`indexedAccessions()` logs a warning and ignores them; only `IndexedNotOnDisk` is affected.

### 3.5 `scan.go` — the walk, extracted for Phase 2 reuse

```go
func ScanCompanyFilings(root, cik string) ([]FilingRow, error)
```

Walks `{root}/companies/{cik}/{year}/{accession}/`, and for each accession reads `filing.json` +
`meta.json` into one `FilingRow`. Rules, each of which exists because the corpus demanded it:

- `os.ReadDir` entries that are **not directories are skipped** — `.DS_Store` lives in `2016/` and `2026/`.
- Year folders parse as 4-digit ints; non-numeric children of `{cik}/` are skipped (this is how
  `submissions.json` itself is skipped).
- A missing or unparseable `meta.json` is tolerated: the row keeps its `filing.json` fields and
  gets `Category: "unknown"`, `Tier: 0`, `TierLabel: ""`. Measured presence is 100%, so this path
  is defensive — but `byCategory` already has 78 real `unknown`s and they must aggregate together.
- A missing or unparseable `filing.json` **drops the row** and logs at warn — without `form` and
  `filingDate` there is nothing to render.
- `index.json` is not read in this phase (file manifest is out of scope; see idea file "do not show yet").
- Sorted newest-first by `(FilingDate, AccessionNumber)` descending, once, at scan time. Every
  consumer inherits the order, so `ListFilings` never sorts per request.

This function is the one Phase 2's importer calls; nothing else in the package walks the tree.

### 3.6 `aggregate.go`

```go
func Summarize(rows []FilingRow, indexedNotOnDisk int) (Coverage, FilingsSummary)
```

Pure over the slice. `Earliest`/`Latest` are min/max of `FilingDate` (string compare is correct for
`YYYY-MM-DD`), `YearsOnDisk` is the sorted distinct set, `TotalFilings = len(rows)`.
`ByTier` keys on `TierLabel` and is left open-ended — four values exist today
(`MAJOR/MODERATE/MINOR/ROUTINE`) and nothing may hardcode two (HLD §2.2).
Empty `rows` returns zero-value dates and empty maps, not an error.

### 3.7 `search.go`

```go
type SearchCandidate struct {
    CompanySummary
    FormerNames []string
}
func RankCompanies(cands []SearchCandidate, query string, limit int) []CompanySummary
```

Pure and exported so the ranking test needs no filesystem (HLD §8.1). Score ladder, first match wins:

| Score | Rule |
|---|---|
| 100 | any ticker equals query, case-insensitive |
| 90 | name has query as prefix, case-insensitive |
| 80 | any whitespace token of name has query as prefix |
| 70 | name contains query |
| 60 | any former name contains query |
| 50 | query is all digits and is a prefix of the CIK **after stripping leading zeros from both** |
| — | no match → excluded |

Sort by score desc, then `Name` asc, then `CIK` asc — fully deterministic, no map iteration in the
ordering path. Empty/whitespace query returns `nil` (the handler 400s before this). `limit <= 0`
or `> 20` clamps to 20.

Ranking is scored per candidate in one pass over the cached index; a 1,000-company index is a
sub-millisecond loop, so no prefix trie in this phase.

### 3.8 `filestore.go` — `FileDBStore` and the caches (HLD D3)

```go
type Options struct {
    TTL time.Duration // default 5m
}
func NewFileDBStore(root string, opts Options) *FileDBStore
```

Two caches behind one `sync.RWMutex`:

```go
type FileDBStore struct {
    root string
    ttl  time.Duration

    mu        sync.RWMutex
    companies []SearchCandidate // all companies, from each submissions.json
    compBuilt time.Time
    filings   map[string]*filingCache // keyed by normalized CIK
    building  map[string]*sync.Mutex  // per-CIK build lock
}

type filingCache struct {
    rows     []FilingRow
    coverage Coverage
    summary  FilingsSummary
    builtAt  time.Time
    dirMTime time.Time
}
```

**Freshness** = `time.Since(builtAt) < ttl` **and** the `Stat` mtime of `{root}/companies/{cik}`
is unchanged. Be honest about the limit in the code comment: a directory mtime only moves when
its direct children change, so editing a `meta.json` three levels down will **not** invalidate —
the TTL is the real backstop. That is acceptable for a static local corpus and is exactly the
constraint Phase 2's downloader removes with explicit invalidation.

**Thundering herd:** the first request for a cold CIK takes the per-CIK `building` mutex so N
concurrent requests do one walk, not N. Implemented with a small keyed-mutex helper — no new
dependency (no `golang.org/x/sync`).

`GetCompany` and `ListFilings` share one `filingCache`, so a page load that hits both endpoints
walks once. `GetCompany` additionally reads `submissions.json` for `Identity` and
`IndexedNotOnDisk = len(indexed) - |indexed ∩ onDisk|`.

`ListCompanies` / `SearchCompanies` build the company index by reading only each
`{cik}/submissions.json` — never the filing tree — so search stays O(companies), not O(filings).

---

## 4. Handlers — `internal/handlers/companies.go` (new)

```go
type CompanyHandler struct{ Store filedb.CompanyStore }
func (h *CompanyHandler) Search(c *gin.Context)
func (h *CompanyHandler) Get(c *gin.Context)
func (h *CompanyHandler) Filings(c *gin.Context)
```

| Handler | Params | Validation | Codes |
|---|---|---|---|
| `Search` | `q`, `limit` | `q` trimmed non-empty else 400; `limit` default/cap 20 | 200, 400, 500 |
| `Get` | `:cik` | `NormalizeCIK` → 400 on `ErrInvalidCIK` | 200, 400, 404, 500 |
| `Filings` | `:cik`, `year`, `form`, `limit`, `offset` | CIK as above; `limit` default 25 cap 100; `offset` ≥ 0; bad ints → 400 | 200, 400, 404, 500 |

Every response is `gin.H{"data": …, "error": …}` per `files.go:246`. Errors are mapped in one
helper so the sentinel rule of §3.2 lives in exactly one place:

```go
func respondStoreErr(c *gin.Context, err error) // ErrInvalidCIK→400, ErrCompanyNotFound→404, else slog.Error + 500
```

Internal error text is never echoed to the client — 500s return a fixed `"internal error"` string
and the detail goes to `slog`, matching `files.go:240`.

---

## 5. Wiring

### `internal/handlers/router.go`

`NewRouter` takes a seventh parameter `companies filedb.CompanyStore`. It has exactly one call
site (`cmd/server/main.go:120`) and no test constructs it — verified by grep — so the signature
change is contained.

Register a new group after the `apiFiles` block (`router.go:56-66`), before `apiAdmin`:

```go
companyHandler := &CompanyHandler{Store: companies}
apiCompanies := r.Group("/api/companies", auth.AuthRequired(), requestTimeout(defaultRequestTimeout))
{
    apiCompanies.GET("/search", companyHandler.Search)
    apiCompanies.GET("/:cik", companyHandler.Get)
    apiCompanies.GET("/:cik/filings", companyHandler.Filings)
}
```

Page routes go beside the existing static files (`router.go:41-44`):

```go
r.StaticFile("/companies", "./static/companies.html")
r.GET("/companies/:cik", func(c *gin.Context) { c.File("./static/company.html") })
```

Both shapes were probed against the pinned `gin-gonic/gin v1.9.1` (HLD D5): `/search` beside
`/:cik`, and static `/companies` beside `/companies/:cik`, register without panic and route to the
right handler. No `?cik=` fallback is needed.

### `cmd/server/main.go`

Beside the other `getEnv` reads, before the `NewRouter` call at `:120`:

```go
fileDBDir := getEnv("FILEDB_DIR", "./fileDB")
companyStore := filedb.NewFileDBStore(fileDBDir, filedb.Options{TTL: filedb.ParseTTL(os.Getenv("FILEDB_CACHE_TTL"))})
```

`ParseTTL` returns 5m on empty or unparseable input. The store is **not** warmed at startup —
construction must not walk 645 MB or stat anything, so a missing `fileDB/` directory does not
block boot; it surfaces as an empty search result and a 404, and is logged once on first use.

### `.env.example`

```
# ---- Company file database (phase 1 read model) ----
FILEDB_DIR=./fileDB
FILEDB_CACHE_TTL=5m
```

---

## 6. Frontend

### `static/companies.html` (new)

Same shell as `index.html`: `app-header` with logo, nav (`Companies` active, `Analyze`,
`Admin` hidden by default), `header-right` with email + Sign out. One `.card` holding an
`<input type="search" id="company-search">` (styled by the existing `input[type=text]` rule at
`style.css:350`), a `<div id="search-results">`, and a `<div id="search-state" class="muted">` for
the empty/loading/error line. Loads `app.js` then `companies.js`.

### `static/js/companies.js` (new)

- `requireAuth()` on load, mirroring `analyze.js:1`.
- 250 ms debounce; each keystroke aborts the previous `fetch` via `AbortController` so a slow
  response cannot overwrite a newer one.
- `apiFetch('/api/companies/search?q=' + encodeURIComponent(q))`.
- Renders rows as `name` · ticker badges (reusing `.badge`) · CIK, all through `escHtml`.
- Keyboard: `ArrowDown`/`ArrowUp` move an `.active` row, `Enter` navigates, `Escape` clears.
  Rows are also click targets; navigation is `location.href = '/companies/' + cik`.
- Three explicit states, per idea file: `Searching…`, `No companies match "…"`, and the idle hint.

### `static/company.html` (new)

Sections in the idea file's order: header (name, ticker badges, CIK, exchange) · identity `.card`
using `.meta-grid`/`.meta-item` · coverage `.card` with the sentence and year chips · filings
summary `.card` · recent filings `.card` with `.table-wrap` + table · a final `.card` reading
"Timeline view — coming soon" with one line naming what it will correlate. Loads `app.js` then
`company-page.js`.

### `static/js/company-page.js` (new)

- `requireAuth()`, then CIK from `location.pathname.split('/').pop()`.
- Two calls in parallel: `/api/companies/{cik}` and `/api/companies/{cik}/filings?limit=25&offset=0`.
- Identity card omits absent fields entirely rather than rendering `—` (HLD D7); `formerNames`
  is `[]` on the sample company and must not produce an empty row.
- Form breakdown renders the **top 8** by count as proportional bars with a "+N more" toggle —
  the corpus has 19 distinct forms (HLD D7). Category chips exclude `unknown` from any "top
  categories" line while `byCategory.unknown` still shows in the full list as a muted
  "Uncategorized" chip.
- Tier chips iterate `byTier` keys as returned; nothing hardcodes MAJOR/MINOR.
- If `coverage.indexedNotOnDisk > 0`, the coverage card appends
  "· N more known to SEC, not yet downloaded" — this is the D1 gap made visible.
- Filings table paginates with Prev/Next over `limit`/`offset` against `total`; the first render
  fetches 25 rows, never 381. Optional `year`/`form` selects are populated from
  `coverage.yearsOnDisk` and `filingsSummary.byForm`.
- All dynamic text through `escHtml` — filing `summary` values are free text from `meta.json`
  and are the highest-risk injection surface on the page.

### Existing-file edits

| File | Line | Edit |
|---|---|---|
| `static/js/app.js` | after `formatFileSize` (`:52`) | add `escHtml` — the `analyze.js` version verbatim (it null-guards and `String()`-coerces; the `admin.html` one does `(str \|\| '')` and throws on numbers) |
| `static/js/analyze.js` | `3-8` | delete the local `escHtml`; `app.js` loads first (`index.html:85-87`) |
| `static/admin.html` | `914-916` | delete the inline `escHtml`; `app.js` loads at `:751`, before the inline script at `:753` |
| `static/index.html` | `15` | add `<a href="/companies">Companies</a>` before `Analyze` |
| `static/login.html` | `33`, `56` | `'/'` → `'/companies'` (both sites) |
| `static/js/app.js` | `185` | unchanged — `requireAuth()` still bounces to `/login` |

`static/css/style.css` gains one appended section: `.chip` / `.chip-muted` (year, category, tier
chips), `.bar-row`/`.bar-track`/`.bar-fill` (form breakdown), `.search-results` /
`.search-result-row` / `.search-result-row.active`, and `.company-title`. Tokens only — no new
colors, no `:root` changes. No RTL rules: these pages are English-only, like `index.html`.

---

## 7. Tests

`make test` = `go test ./...`; plain table tests, no testify, matching
`internal/handlers/pipeline_status_test.go`.

Fixture tree at `internal/filedb/testdata/companies/` — three synthetic companies, ~6 filings
total, committed. It deliberately contains the corpus's real hazards: a `.DS_Store` inside a year
folder, one accession whose `meta.json` is absent, one whose `category` is `"unknown"`, and a
`submissions.json` whose `filings.recent` lists two accessions with no folder on disk. Tests read
this, never the 645 MB real tree.

| File | Covers |
|---|---|
| `cik_test.go` | `NormalizeCIK`: padding, `CIK` prefix, non-numeric → `ErrInvalidCIK`, 11 digits → error, traversal strings (`../..`) rejected |
| `search_test.go` | `RankCompanies`: exact ticker beats name prefix; token prefix beats substring; former-name hit; zero-stripped CIK prefix; tie-break by name; limit clamp; empty query → nil |
| `aggregate_test.go` | `Summarize`: min/max dates, distinct sorted years, `byForm`/`byCategory`/`byTier` counts, `unknown` aggregates as one key, four tier labels survive, empty input |
| `scan_test.go` | `ScanCompanyFilings`: `.DS_Store` skipped, missing `meta.json` → `category:"unknown"` and row kept, missing `filing.json` → row dropped, newest-first order |
| `store_test.go` | `GetCompany` shape; unknown CIK → `errors.Is(err, ErrCompanyNotFound)`; `IndexedNotOnDisk` equals the fixture's 2; `ListFilings` offset/limit/`Total` and `year`+`form` filters; second call served from cache (walk counter) |
| `companies_handler_test.go` (in `handlers`) | `httptest` + a stub `CompanyStore`: 400 on empty `q`, 400 on bad CIK, 404 when the stub returns `ErrCompanyNotFound`, 500 body carries no internal error text, `limit` clamping |

One build-tagged test, `//go:build corpus`, asserts against the real tree and is excluded from
`make test`: total **381**, earliest **2016-01-06**, latest **2026-08-20**, `byForm["6-K"] == 242`,
`byForm["20-F"] == 11`, `indexedNotOnDisk == 123`, tier labels exactly
`{MAJOR:97, MODERATE:117, MINOR:163, ROUTINE:4}`. Run with `go test -tags corpus ./internal/filedb/`.
This is the regression net for D1 — if someone reverts to index-derived counts, it fails loudly.

---

## 8. Build order

1. `internal/filedb`: `models.go`, `cik.go` + `cik_test.go`, `search.go` + `search_test.go`,
   `aggregate.go` + `aggregate_test.go`. All pure, no filesystem — green before anything touches disk.
2. `testdata/` fixture tree, then `scan.go` + `scan_test.go`.
3. `submissions.go`, `store.go`, `filestore.go` + `store_test.go`. Package complete and green.
4. `handlers/companies.go` + `companies_handler_test.go` against a stub store — no real fileDB needed.
5. `router.go` registration, `main.go` wiring, `.env.example`. `make build` passes; curl §9 steps 1-4.
6. `escHtml` move (D8) — `app.js` add, then both deletions. Verify `/` and `/admin` still render;
   this is the one step that can break existing pages.
7. `static/companies.html` + `js/companies.js`, plus the CSS section. Search works end to end.
8. `static/company.html` + `js/company-page.js`. Full page renders.
9. Nav link + the two login redirects (D6) — last, so the landing page only moves once the
   destination is real.
10. `make test`, then the `-tags corpus` run, then the manual pass in §9.

Steps 1-4 need no `fileDB/` at all, which keeps the package testable on a machine without the corpus.

---

## 9. Manual verification

```bash
make run   # PORT=8123 per .env

TOKEN=$(curl -s -X POST localhost:8123/auth/login \
  -H 'Content-Type: application/json' \
  -d '{"email":"'"$ADMIN_EMAIL"'","password":"'"$ADMIN_PASSWORD"'"}' | jq -r .data.token)

# 1. auth is enforced (expect 401)
curl -s -o /dev/null -w '%{http_code}\n' localhost:8123/api/companies/search?q=kamada

# 2. all three query forms find the one company
for q in kamada KMDA 1567529; do
  curl -s -H "Authorization: Bearer $TOKEN" "localhost:8123/api/companies/search?q=$q" | jq -c '.data[0]'
done

# 3. detail matches the disk audit, not the index
curl -s -H "Authorization: Bearer $TOKEN" localhost:8123/api/companies/0001567529 \
  | jq '{total:.data.coverage.totalFilings, from:.data.coverage.earliestFilingDate,
         to:.data.coverage.latestFilingDate, gap:.data.coverage.indexedNotOnDisk,
         sixK:.data.filingsSummary.byForm["6-K"], twentyF:.data.filingsSummary.byForm["20-F"]}'
# expect: 381, 2016-01-06, 2026-08-20, 123, 242, 11   ← NOT 504/2013-01-24/321/13

# 4. pagination returns 25 of 381
curl -s -H "Authorization: Bearer $TOKEN" \
  'localhost:8123/api/companies/0001567529/filings?limit=25&offset=0' \
  | jq '{got:(.data.items|length), total:.data.total}'

# 5. bad input
curl -s -o /dev/null -w '%{http_code}\n' -H "Authorization: Bearer $TOKEN" localhost:8123/api/companies/abc      # 400
curl -s -o /dev/null -w '%{http_code}\n' -H "Authorization: Bearer $TOKEN" localhost:8123/api/companies/9999999999 # 404
```

Then in a browser: sign in → land on `/companies` → type `kmda` → arrow-down + Enter →
`/companies/0001567529` renders identity, coverage, breakdown, and a 25-row table.

---

## 10. Risks specific to implementation

1. **`NewRouter` signature change** — one call site, but it is the integration point every future
   sprout-app touches. Add the parameter last in the list and keep the group registration adjacent
   to `apiFiles` so the diff reads as an addition.
2. **`escHtml` move (step 6)** is the only edit to working pages. Both consumers load `app.js`
   first (verified `index.html:85-87`, `admin.html:751-753`), so the risk is a missed third copy —
   grep `function escHtml` after the change and expect exactly one hit.
3. **Directory-mtime invalidation is coarse** (§3.8). Documented in-code; the TTL is the backstop.
   It will be wrong the moment a downloader writes concurrently — that is Phase 2's design point,
   not a bug to fix here.
4. **Single-company corpus.** Search ranking is exercised only by synthetic fixtures. The ladder in
   §3.7 is a guess about relevance that no real multi-company data has tested yet; expect to revisit
   it when the second company lands.
5. **`/companies/:cik` serves HTML for any string**, including a CIK that does not exist — the page
   then shows a client-side "company not found" state from the API's 404. That is intended, but it
   means the URL is not a validity signal and the page must handle the 404 rather than render blanks.

---

## 11. Open questions

1. **`fileDB/` is tracked by git** — 645 MB, 4352 files, absent from `.gitignore` (present only in
   `.graphifyignore`). Carried forward from HLD §8.2 and deliberately not acted on here: if it is
   unintentional it wants its own change, and if it is intentional the `-tags corpus` test in §7 is
   the thing that depends on it.
2. **`ListCompanies` has no endpoint.** It is on the interface per the idea file and is used by
   `SearchCompanies` internally. Leave it exported for Phase 2 rather than adding a `/api/companies`
   list route nothing calls.

---

## 12. Done when

A signed-in user lands on `/companies`, types `kmda` (or `Kamada`, or `1567529`), clicks the single
result, and `/companies/0001567529` shows **KAMADA LTD · KMDA · Nasdaq**, "filings from 2016-01-06
to 2026-08-20 · 381 filings · 123 more known to SEC, not yet downloaded", a form breakdown led by
**6-K 242** and **20-F 11**, four tier chips, and a filings table whose first page renders 25 of 381
rows. `make test` passes; `go test -tags corpus ./internal/filedb/` passes against the real corpus.
