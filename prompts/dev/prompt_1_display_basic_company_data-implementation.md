# Company Search & Company Overview (Phase 1) — Implementation Notes (as-built)

Implements `prompt_1_display_basic_company_data-lld.md` under the contract in
`prompt_1_display_basic_company_data-hld.md`. Built 2026-08-25.

---

## TL;DR

Shipped as designed: a new `internal/filedb` read model, three authenticated endpoints, a company
picker and a company page, and `escHtml` consolidated into `app.js`. Post-login now lands on
`/companies`.

The Definition of Done was verified literally against the running server and the real 645 MB
corpus — not just built and unit-tested. Every number the HLD's data audit predicted is reproduced
by the shipped code: **381** filings, **2016-01-06 → 2026-08-20**, **123** indexed-not-on-disk,
**6-K 242**, **20-F 11**, four tier labels, `unknown` 78, `hasFinancials` 66.

Nine deviations, all recorded below. The notable ones: the `.DS_Store` test fixture had to be
injected at runtime because `.gitignore` would have silently deleted it from the repo, and
`make test` cannot go green because three **pre-existing** files in `generated/` are not valid Go.

---

## What changed (file by file)

### New package — `internal/filedb/` (8 source files, 6 test files)

| File | Contents |
|---|---|
| `models.go` | `CompanySummary`, `Identity`, `Address`, `Coverage`, `FilingsSummary`, `CompanyDetail`, `FilingRow`, `FilingFilter`, `FilingPage`. Maps/slices initialized non-nil so JSON is `{}`/`[]`, never `null`. |
| `store.go` | `CompanyStore` interface (4 methods) + `ErrCompanyNotFound`, `ErrInvalidCIK`. |
| `cik.go` | `NormalizeCIK` (accepts `1567529`, `0001567529`, `CIK0001567529`), `trimCIKZeros`. |
| `submissions.go` | `parseSubmissions`, `identity()`, `formerNameStrings()`, `indexedAccessions()`. |
| `scan.go` | `ScanCompanyFilings` — the single tree walk, extracted for the Phase 2 importer. |
| `aggregate.go` | `Summarize(rows, indexedNotOnDisk)` — pure; `CategoryUnknown` constant. |
| `search.go` | `SearchCandidate`, `RankCompanies`, the 6-rule score ladder, `MaxSearchResults`. |
| `filestore.go` | `FileDBStore`, `NewFileDBStore`, `Options`, `ParseTTL`, both caches, per-CIK build lock. |

`var _ CompanyStore = (*FileDBStore)(nil)` pins the Phase 2 seam at compile time.

### Handlers

- **`internal/handlers/companies.go`** (new) — `CompanyHandler` with `Search`, `Get`, `Filings`;
  `parseBoundedInt` for query-param validation; `respondStoreErr` as the single sentinel→HTTP map.
  500s return a fixed `"internal error"`; detail goes to `slog`.
- **`internal/handlers/router.go`** — `NewRouter` gained a 7th parameter `companies
  filedb.CompanyStore`; new `/api/companies` group behind `auth.AuthRequired()`; two page routes
  (`/companies` static, `/companies/:cik` → `c.File`).

### Wiring

- **`cmd/server/main.go`** — reads `FILEDB_DIR` (default `./fileDB`) and `FILEDB_CACHE_TTL`,
  constructs the store, passes it to `NewRouter`. Construction does no I/O, so a missing corpus
  does not block boot.
- **`.env.example`** — new "Company file database (phase 1 read model)" section.

### Frontend

- **`static/companies.html`** + **`static/js/companies.js`** (new) — picker; 250 ms debounce,
  `AbortController` per keystroke, ArrowUp/Down/Enter/Escape, three explicit states.
- **`static/company.html`** + **`static/js/company-page.js`** (new) — header, identity card,
  coverage card, form bars + category/tier chips, paginated filings table with year/form filters,
  timeline placeholder.
- **`static/css/style.css`** — appended one section: `.chip*`, `.bar-*`, `.search-*`,
  `.company-*`, `.pager*`, `.filings-filters`, `.placeholder-panel`. Tokens only; no `:root` change.
- **`static/js/app.js`** — `escHtml` added (the `analyze.js` implementation).
- **`static/js/analyze.js`**, **`static/admin.html`** — local `escHtml` copies deleted.
- **`static/index.html`** — "Companies" nav link.
- **`static/login.html`** — both post-login redirects `/` → `/companies`.

---

## Deviations from the design

**1. `.DS_Store` fixture is injected at test time, not committed.** *(the one that mattered)*
The LLD called for a committed fixture containing `.DS_Store` inside a year folder. `.gitignore:54`
ignores `.DS_Store`, so it would have vanished on a fresh clone and the "skip non-directory
entries" test would have quietly stopped testing anything while still passing. Added
`internal/filedb/fixture_test.go`: `fixtureRoot(t)` copies `testdata/` into `t.TempDir()` and
writes the `.DS_Store` files there. Every filesystem test uses it.

**2. `make test` cannot pass — pre-existing breakage, not caused by this work.**
`generated/appraisal/route_snippet.go`, `generated/bank_account_confirmation/route_snippet.go`, and
`generated/payment_order/route_snippet.go` are code *snippets* beginning with `pipeline.Route{`,
not valid Go files. Verified present in that state at `HEAD` (`git show
HEAD:generated/appraisal/route_snippet.go`). `go test ./...` fails at setup on those three
packages. All real packages pass; see Tests below. Follow-up suggested.

**3. Numeric queries still match names by prefix.** A test asserting digit queries match only CIKs
failed against `3M COMPANY`. The code is right and the assertion was wrong — the idea file's name
rules carry no digit exclusion, and `3` → `3M COMPANY` is a desirable name-prefix hit. Test
rewritten as `TestRankCompaniesNumericQuery`, which pins the real rule: name prefix (90) outranks
CIK prefix (50).

**4. `openCompany()` instead of `open()` in `companies.js`.** A top-level `function open()` in a
classic script overrides `window.open` for the whole page. Renamed.

**5. Both new pages log out when `/auth/me` fails.** The LLD treated the header email as cosmetic
with a swallowed error. `analyze.js:14-23` instead calls `logout()`, which is the real
stale-token enforcement for a client-authenticated page. Adopted that pattern — strictly stronger
auth behavior, and consistent with the existing page.

**6. `escHtml(0)` now returns `"0"` instead of `""`.** The promoted `analyze.js` implementation
null-guards and `String()`-coerces; the deleted `admin.html` one used `(str || '')`, which
returned `""` for `0` and threw on numbers. Behavior change is a fix; no caller depended on it.

**7. `Identity` written with per-field JSON tags.** The LLD's struct sketch had two fields sharing
one malformed tag line — a doc shorthand, not a spec. Written out properly.

**8. `parseBoundedInt` covers `year` too.** The LLD specified bad-int→400 for `limit`/`offset`;
`year` uses the same helper, so `?year=abc` is a 400 rather than a silently-ignored filter.

**9. `FilingPage` echoes `Limit`/`Offset` back to the client.** Specified in the LLD's JSON table;
noting it because the interface sketch in §3.2 of the LLD omitted them.

---

## Tests

```
go test ./cmd/... ./internal/...          # all packages: ok  (internal/filedb, internal/handlers new)
go test -race -tags corpus ./internal/filedb/   # ok — no data races in the cache paths
go test -tags corpus ./internal/filedb/   # ok — real 645MB corpus assertions
make test                                  # FAILS — pre-existing generated/ snippets only (deviation 2)
gofmt -l <all files touched>              # clean, except router.go's pre-existing health-map alignment
go vet ./internal/filedb/ ./internal/handlers/ ./cmd/...   # clean
```

**New tests:** `cik_test.go` (incl. traversal strings), `search_test.go` (score ladder, tie-break,
limit clamp), `aggregate_test.go` (four tier labels, `unknown` folding, non-nil empties),
`scan_test.go` (`.DS_Store`, missing `meta.json` survives, missing `filing.json` drops,
newest-first), `store_test.go` (disk-vs-index, pagination, filters, cache reuse and expiry,
missing corpus), `internal/handlers/companies_test.go` (stub store: 400/404/500, limit clamping,
no internal detail in 500 bodies, `data:[]` not `null`).

`corpus_test.go` is `//go:build corpus`, excluded from `make test`, and **skips** rather than fails
when `fileDB/` is absent — which turned out to matter, see Follow-ups.

### Definition of Done — verified literally

Server built and run on `:8123` against the real corpus; interaction driven end to end.

| DoD element | Result |
|---|---|
| Unauthenticated API → 401 | all three endpoints: **401** |
| Search finds `Kamada`, `kamada`, `KMDA`, `kmda`, `kam`, `1567529`, `0001567529` | all → KAMADA LTD / KMDA / 0001567529 |
| Header: name · ticker · exchange | KAMADA LTD · KMDA · Nasdaq |
| Coverage dates | 2016-01-06 → 2026-08-20 |
| Total filings | **381** |
| Gap surfaced | 123 more known to SEC, not yet downloaded |
| Form breakdown | 6-K **242**, 20-F **11**, 19 distinct forms, top-8 + "+11 more" toggle |
| Tier chips | MAJOR 97, MODERATE 117, MINOR 163, ROUTINE 4 |
| Category | `unknown` 78 rendered as muted "Uncategorized", never leading |
| Table paginates | 25 rows of 381; page 2 offset 25 correct; prev disabled, next enabled |
| Error codes | `abc`→400, 11 digits→400, `9999999999`→404, `year=abc`→400, no `q`→400 |
| Page routes | `/companies` 200, `/companies/0001567529` 200 |

The picker and company page were verified by **executing the real page JS** (`companies.js`,
`company-page.js`) against the live API under a DOM shim: 23 render assertions on the company page,
11 picker assertions (including ArrowDown→Enter navigating to `/companies/0001567529`, Escape
clearing, and the no-match state) across all three query forms, plus 7 assertions on the
company-not-found path. All passed.

**Latency** (budget was <200 ms): cold detail **128 ms** (the full 381-filing walk), warm **0.5 ms**,
search **0.9 ms**. This is the concrete justification for the D3 cache — per-request walking was
already marginal at one company.

**Regression check after the `escHtml` move:** `/`, `/admin`, `/login` all still 200; exactly one
`function escHtml` remains in `static/`.

---

## How to enable / roll back

Nothing to enable — the feature is live once the binary is rebuilt. Optional env:

```
FILEDB_DIR=./fileDB        # default; a missing directory yields empty search + 404s, not a boot failure
FILEDB_CACHE_TTL=5m        # default; unparseable values fall back to 5m with a warning
```

**Roll back** by reverting the touched files listed below. The only edits to previously-working
behavior are the `escHtml` consolidation (3 files) and the two `login.html` redirects; reverting
`static/login.html` alone restores `/` as the post-login landing page while leaving the rest intact.
No DB migration, no schema change, no pipeline change — nothing to undo server-side.

---

## Follow-ups

1. **`generated/*/route_snippet.go` breaks `go test ./...`** (deviation 2). One-line fix is renaming
   them to `route_snippet.go.txt` so they stay readable as snippets without being compiled. Left
   alone as out of scope — it is unrelated to this feature and is a codegen-output decision.
2. **`fileDB/` was untracked from git mid-session by something outside this work** — the index now
   holds 4352 staged deletions plus `.gitignore` rules (`fileDB/*`, `!fileDB/.gitkeep`) and a
   `fileDB/.gitkeep`. The files remain on disk and the app is unaffected. This resolves open
   question 1 from the HLD/LLD, but it means the `-tags corpus` test now depends on a corpus no
   longer in the repo; it skips cleanly rather than failing. Worth a `make fetch-corpus` target or
   a README line so the assertions stay runnable.
3. **Search ranking is still validated only against synthetic fixtures** — one real company cannot
   exercise the ladder. Revisit when a second company lands.
4. **Cache invalidation is coarse** — a company directory's mtime does not move when a nested
   `meta.json` changes; the TTL is the backstop. Documented in `filestore.go`. Phase 2's downloader
   should invalidate explicitly.
5. **`ListCompanies` has no endpoint** — interface-only, per the LLD, awaiting Phase 2.

---

## Touched files

**New (16):** `internal/filedb/{models,store,cik,submissions,scan,aggregate,search,filestore}.go` ·
`internal/filedb/{cik,search,aggregate,scan,store,corpus,fixture}_test.go` ·
`internal/filedb/testdata/companies/**` (3 companies, 6 accessions) ·
`internal/handlers/companies.go` · `internal/handlers/companies_test.go` ·
`static/companies.html` · `static/company.html` · `static/js/companies.js` ·
`static/js/company-page.js`

**Modified (8):** `cmd/server/main.go` · `internal/handlers/router.go` · `.env.example` ·
`static/css/style.css` · `static/js/app.js` · `static/js/analyze.js` · `static/admin.html` ·
`static/index.html` · `static/login.html`

No commit was created.
