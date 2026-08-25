# LLD: EDGAR Ingestion Port — Python → Go (Phase 1)

Implements `prompts/dev/prompt_2_convert_from_python_to_go.txt` per
`prompt_2_convert_from_python_to_go-hld.md` (D1–D10 are the contract this
LLD builds to). Triage: **STANDARD**.

## Scope

- In: `internal/edgar/` (one package, files split by concern —
  `models.go`, `classify.go`, `extract.go`, `builder.go`, `client.go`,
  `store.go`), `cmd/edgar/main.go` (one binary, `meta`/`fetch`/`backfill`
  subcommands), `.env.example` + `Makefile` additions, `python/edgar/README.md`.
- Out: SQLite, LLM pipeline integration, prompt-1 UI, deleting `python/edgar/`
  (unchanged from the HLD).

Revised from the first pass of this LLD, which sketched 4 subpackages
(`models/`, `meta/`, `client/`, `store/`) and 3 binaries — more package/
build ceremony than ~550 lines of original Python logic justifies. See
HLD D2/D8 for the rationale; `classify.go` is the one file kept isolated
because it's pure logic and the highest-risk piece to get wrong (D9/D3).

## Current state — verified against the real corpus and real scripts

Read all four `python/edgar/*.py` files and the actual on-disk data under
`fileDB/companies/0001567529/` (not just the idea file's paraphrase) before
writing this. Two corrections came out of that, below.

- `submissions.json`: 504 filings in `filings.recent`. **`recent` has exactly
  16 keys, stable across all 504 entries**: `accessionNumber, filingDate,
  reportDate, acceptanceDateTime, act, form, fileNumber, filmNumber, items,
  core_type, size, isXBRL, isInlineXBRL, isXBRLNumeric, primaryDocument,
  primaryDocDescription`.
- 381 accession folders exist on disk (of 504 known filings — the other 123
  are the `indexedNotOnDisk` gap `filedb.Coverage` already models). Sampled
  all 381 `filing.json` files: same 16 keys, no extras, no missing keys.
- `download_filing_files()` in `fetch_kamada.py` writes `filing.json` as
  `json.dump(filing, ...)` where `filing = {key: recent[key][i] for key in
  recent}` — **the full row, verbatim**, not a curated subset.
- `reportDate` is `""` (empty string, not absent) for filings with no report
  period — confirmed in real data. `build_meta.py` converts `"" → None` only
  when writing `meta.json` (`filing.get("reportDate") or None`); `filing.json`
  itself keeps the raw `""`.
- `index.json`: `directory.item` is an array when an accession has multiple
  files (verified sample: 4 items) — Python special-cases the single-file
  case (`isinstance(items, dict)`) but no on-disk sample here exercises it;
  treat that branch as untested-but-real per the Python behavior.
- `meta.json` real sample matches the idea file's `Meta` struct field-for-field
  exactly (`accession, form, filingDate, reportDate, tier, tierLabel,
  category, summary, signals{isXBRL,hasFinancials,fileCount,exhibitCount},
  tags`) — no correction needed there.
- `internal/filedb` (prompt-1) already exists: `models.go` (`CompanySummary`,
  `Identity`, `Coverage`, `FilingsSummary`, `CompanyDetail`, `FilingRow`,
  `FilingFilter`, `FilingPage`), `cik.go` (`NormalizeCIK`), `store.go`
  (`CompanyStore` interface, `ErrCompanyNotFound`/`ErrInvalidCIK`). These are
  a **separate, higher-level read-model** (aggregated, UI-shaped) — no name
  or shape collision with this port's `FilingStore`/raw JSON mirrors.
  `FileDBStore` (prompt-1's `CompanyStore` implementer) does not exist yet;
  it is the future caller of `internal/edgar`, not built here.

## Corrections to the HLD / idea-file spec

1. **`FilingRecord` needs all 16 verified `filings.recent` columns, not the
   8 sketched in the idea file** (idea file omits `acceptanceDateTime, act,
   fileNumber, filmNumber, items, core_type, size, isXBRLNumeric`). Omitting
   them breaks the acceptance criterion "JSON structs round-trip ... without
   data loss" the moment a real `filing.json` is unmarshaled and
   re-marshaled. `RecentFilings` (parallel arrays in `submissions.json`) gets
   the same 16 fields, one slice per key. Since the set has been verified
   stable across the whole real corpus, hardcode it — but decode through a
   small `map[string]json.RawMessage` first and keep any unrecognized key
   in an `Extra map[string]json.RawMessage` field (marshaled back inline via
   custom `MarshalJSON`), so a future SEC schema addition degrades to
   "preserved but untyped" instead of silently dropped.
2. **`_parse_ownership` is not regex — it's namespace-agnostic tag matching**
   via `ElementTree.iter()` + `elem.tag.endswith(name)`. The idea file's
   "encoding/xml ... or targeted regex" undersells this. Go's `xml.Decoder`
   already splits `Name.Local` from `Name.Space`, so the port is actually
   *simpler* than Python here: walk tokens with `Decoder.Token()`, match
   `StartElement.Name.Local == "rptOwnerName"` etc. (no endswith heuristic
   needed) — not a struct-tag `Unmarshal`, because the five fields Python
   pulls come from different, inconsistent ownership-form schemas
   (Form 3/4/5 variants) and a fixed struct would miss variants the same
   way a rigid Go type would.
3. **Rate limiting scope**: Python's `time.sleep(0.12)` is only inside
   `download()` — `get_json()` (the `submissions.json` and `index.json`
   fetches) is unthrottled in the original. Decision: the Go `client.Client`
   throttles *every* SEC request uniformly, not just file downloads. This is
   a deliberate strengthening (fewer, more predictable request bursts), not
   a parity requirement — call it out in code comments so it isn't mistaken
   for a missed port detail.

## File-by-file changes

| File | Change |
|---|---|
| `internal/edgar/models.go` | `Submissions`, `FilingsBlock`, `RecentFilings` (16 named `[]string`/`[]int`/`[]*string` fields per verified key list + `Extra`), `.At(i) FilingRecord`, `.Rows()`, `.Len()`; `FilingRecord` (same 16 fields as one `RecentFilings` row — filing.json is a verbatim row copy, see Current state); `IndexManifest`/`IndexDirectory`/`IndexItem` (`Size string` — confirmed mixed `""`/numeric-string in real data) with custom `UnmarshalJSON` on `IndexDirectory` normalizing `item` object→`[]IndexItem`; `Meta`/`MetaSignals`/tier consts (matches real `meta.json` as-is) |
| `internal/edgar/classify.go` | `Classify(form, titles, body, isXBRL, ownership)` + `classify6K(...)` — literal port of `_classify`/`_classify_6k`; same branch order (form-code checks first, then the 6-K keyword cascade) |
| `internal/edgar/extract.go` | `ExtractExhibitTitles(dir)` — scan `*.htm`/`*.html` (skip `-index.html`), read first 2000 bytes, regex `<TYPE>([^\n\r<]+)` for doc type, keep only `EX-99*`, then regex `<DESCRIPTION>([^<\n]+)` per matched file; `ExtractBodySnippet(dir, filing)` — primaryDocument first, else sorted `.htm`/`.html`, strip tags + unescape + collapse whitespace, require >80 chars, lowercase, 2000-char cap; `ParseOwnership(dir)` — see Correction 2 |
| `internal/edgar/builder.go` | `BuildMeta(dir) (Meta, error)` orchestrates classify/extract exactly like `build_meta()`; `fileCount` = count of directory entries (`os.ReadDir`, files only), `exhibitCount = len(titles)`; `BuildSummary(form, category, titles, body, ownership)` — literal port of `_build_summary`, same fallback order (titles → ownership → form-specific → category-specific → bare form) — kept next to `BuildMeta` since it's only ever called from there |
| `internal/edgar/client.go` | `Client{httpClient *http.Client, limiter}`; `GetJSON(ctx, url, v)`, `Download(ctx, url) ([]byte, error)`; `SubmissionsURL(cik)` → `https://data.sec.gov/submissions/CIK{cik}.json`; `ArchiveBaseURL(cik, accession)` → `https://www.sec.gov/Archives/edgar/data/{cikInt}/{accessionNoDashes}` (both verified against `fetch_kamada.py`); reject construction if `SEC_USER_AGENT` empty; every call gated by the limiter (Correction 3); 30s per-request context timeout |
| `internal/edgar/store.go` | `FilingStore` interface + `FileDBStore` implementation; `fileDB/companies/{cik}/{year}/{accession}/...` path convention, root from `FILEDB_DIR` env (default `./fileDB`); loaders for `Submissions`/`Filing`/`Meta`/`Index`; `WriteMeta` + `filing.json`/`index.json` writers matching Python's `json.dump(..., indent=2)` (2-space indent, trailing newline — verified in real files) |
| `cmd/edgar/main.go` | one binary, subcommand dispatch on `os.Args[1]` (`meta`/`fetch`/`backfill`); `meta` mirrors `build_meta_all.py` (default dir `fileDB/companies/0001567529`, `--all` flag, `filepath.WalkDir` for `filing.json`, print format `"{filingDate} {form:16} tier={tier} {accession}"`, exit 1 iff any error); `fetch` mirrors `fetch_kamada.py` with `--cik`/`--years` flags (Python hardcodes both — the one behavioral upgrade the idea file already asks for); `backfill` mirrors `find_real_gaps()` exactly, including the XSL-viewer skip rule (`primary` contains `/` or ends `.xml`) |
| `.env.example` | new `---- EDGAR ingestion ----` section: `SEC_USER_AGENT=` (required, comment: no personal email in source), `SEC_REQUEST_INTERVAL=120ms` (optional) |
| `Makefile` | `edgar` target wrapping `go run ./cmd/edgar` (same `CGO_ENABLED=1` pattern as `run`/`build`) |
| `python/edgar/README.md` | new — points at the Go commands; scripts stay until parity sign-off (HLD D10) |

`internal/filedb` is not touched by this prompt — `FileDBStore` (prompt-1's,
distinct from `internal/edgar`'s own `FileDBStore`) importing
`internal/edgar` is the next prompt's work, not this one's.

## Tests

- `internal/edgar/models_test.go` — round-trip: unmarshal a real
  `submissions.json`/`filing.json`/`index.json` fixture, re-marshal, byte-diff
  (this is what guards Correction 1 — it fails immediately if a field is
  dropped).
- `internal/edgar/classify_test.go` — table-driven; seed with concrete
  verified cases, e.g. `(form="6-K", filingDate="2022-11-16")` →
  `tier=3, category="earnings_preview"` (real accession
  `0001213900-22-072976`, confirmed above).
- `internal/edgar/builder_test.go` — golden test: `BuildMeta()` against
  ≥10 real folders under `fileDB/companies/0001567529/` (381 available),
  byte-diff against the existing `meta.json` in each.
- `internal/edgar/client_test.go` — `httptest.Server`-based: rate limiter
  enforces the configured interval; construction fails with no
  `SEC_USER_AGENT`.
- `cmd/edgar` — exercised via the underlying `internal/edgar` functions
  (not by shelling out to the built binary).

## Build order

1. `models.go` + round-trip tests against real fixtures
2. `classify.go` + `classify_test.go` — pure logic, no I/O, fastest
   feedback loop, and the highest-risk piece (HLD D9/D3)
3. `extract.go`, `builder.go` + golden parity tests
4. `cmd/edgar meta` — first working subcommand, no network required yet
5. `client.go` + tests
6. `store.go` (read + write)
7. `cmd/edgar fetch`
8. `cmd/edgar backfill`
9. `.env.example`, `Makefile`, `python/edgar/README.md`

## Risks

- Classification drift on a form/category combination not present in the
  10+ sampled test folders (long-tail forms beyond 6-K/20-F/13G/3/4).
- `ParseOwnership`'s Form 3/4/5 field variants (Correction 2) — Go's
  token-walk needs to be checked against at least one real ownership XML
  from this corpus, not just the Python source, before trusting the port.
- SEC rate limiting / User-Agent ban if the limiter (Correction 3) has a
  concurrency bug — `edgar-fetch`/`edgar-backfill` are the first Go code
  paths to hit `data.sec.gov`/`www.sec.gov` live.
- `RecentFilings.Extra` (Correction 1) is untested against a real schema
  change by construction — it can only be validated once SEC actually adds
  a 17th field.
- `index.json`'s single-item object case is unverified against real data
  (no on-disk sample exercises it) — port the Python behavior as specified,
  but flag it as unverified in a code comment.

## Done when

Same as the HLD: `make edgar-meta` against `fileDB/companies/0001567529`
reproduces all 381 existing `meta.json` files byte-for-byte, `go test
./internal/edgar/...` and `make test` pass, and `internal/filedb` can read
the Go-produced JSON with no changes on its side.
