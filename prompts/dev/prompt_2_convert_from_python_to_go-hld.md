# HLD: EDGAR Ingestion Port — Python → Go (Phase 1)

Implements `prompts/dev/prompt_2_convert_from_python_to_go.txt`. Feeds
`prompts/dev/prompt_1_display_basic_company_data-hld.md` (D9: "All
server-side code is Go; no Python in the runtime path") — that HLD assumes
this port exists.

## 1. Objective

Replace the four `python/edgar/*.py` scripts with a Go package,
`internal/edgar/`, that becomes the canonical SEC ingestion + enrichment
layer: download filings, classify them, and write `meta.json` per accession
folder — with **byte-for-byte compatible output** against the existing
`fileDB/companies/0001567529/` corpus, so prompt-1's read layer and the data
already on disk keep working unchanged.

This is a like-for-like port, not a redesign: the goal is parity with the
Python behavior first, then a clean seam (a storage interface) for an
eventual SQLite backend — not built here.

## 2. Context — what exists today

- `python/edgar/build_meta.py` reads `filing.json` + downloaded HTML/XML,
  classifies the filing (tier/category/tags), and writes `meta.json`.
- `python/edgar/build_meta_all.py` batch-drives `build_meta` over a company
  directory, skipping folders that already have `meta.json`.
- `python/edgar/fetch_kamada.py` pulls `submissions.json` from
  `data.sec.gov` and downloads the last N years of filings.
- `python/edgar/backfill_primary_docs.py` is a one-off gap-filler for
  missing `primaryDocument` files.
- Everything lands under `fileDB/companies/{cik}/...` as JSON files — there
  is no database involved yet. Prompt-1's `internal/filedb` reads this same
  JSON directly.

The idea file (`prompt_2_convert_from_python_to_go.txt`) already specifies
the target Go structs, package layout, and CLI flags in full — this HLD
does not repeat that; it records the decisions and tradeoffs behind it and
should be read alongside the idea file, not instead of it.

## 3. Architecture decisions

### D1 — Storage is an interface (`FilingStore`); Phase 1 has one implementer

`internal/edgar` exposes `FilingStore` (Submissions/Filing/Meta/WriteMeta)
with a single `FileDBStore` implementation rooted at `FILEDB_DIR` (default
`./fileDB`). Nothing above that interface — classification, fetching, CLI —
talks to the filesystem directly. This is the one seam worth building now:
it's what lets a `SQLiteStore` replace disk storage later (Phase 2, out of
scope here) without touching classification or download logic. The seam is
an interface, not a package boundary — it doesn't need its own directory to
do its job.

### D2 — One flat package, split into files by concern, not by subpackage

`internal/edgar` is a single package: `models.go`, `classify.go`,
`extract.go`, `builder.go`, `client.go`, `store.go` (plus matching
`_test.go` files) — not the four-subpackage layout the idea file sketched
(`models/`, `meta/`, `client/`, `store/`). The Python original is ~550
lines across 4 files; splitting its Go equivalent into 4 packages was
ceremony the actual code size doesn't justify — extra import paths and
directory navigation for a single cohesive component talking to itself.
`classify.go` is the one file that earns real isolation: `build_meta.py`'s
classification logic is the idea file's "highest-value piece" and is pure
(no I/O), which is what makes the parity tests in D3 possible without a
filesystem or network fixture harness — that property comes from the
function being side-effect-free, not from living in its own package.

### D3 — Classification is ported literally, then locked by parity tests

`Classify`, `BuildSummary`, `ExtractExhibitTitles`, `ExtractBodySnippet`,
`ParseOwnership` (mapping in idea file §"Classification parity") are
translated function-for-function from the Python originals — not
reimplemented from the spec. They are locked in with table-driven tests
against ≥10 real folders under `fileDB/companies/0001567529/`, asserting
the Go output matches today's `meta.json` byte-for-byte on tier, category,
summary, and tags. This is the correctness gate for the whole port: if this
doesn't match, nothing downstream (prompt-1 UI) can trust the data.

### D4 — SEC HTTP access is centralized in one client with a hard rate floor

One `Client` (shared `http.Client`, rate limiter, 30s context-timeout) is
used by both the `fetch` and `backfill` subcommands — no ad hoc `http.Get`
calls in `cmd/edgar`. `SEC_USER_AGENT` is required at startup
(reject early, not on first request); `SEC_REQUEST_INTERVAL` defaults to
120ms, matching the Python script's 0.12s throttle. Centralizing this in
one place is what makes the rate limit reliably enforced instead of
per-caller convention.

### D5 — `submissions.json`'s parallel-array shape is modeled explicitly, not flattened

SEC's `filings.recent` is parallel arrays (`accessionNumber[i]`,
`form[i]`, `filingDate[i]`, ... all indexed together), not an array of
objects. `RecentFilings` keeps that shape as-is and exposes `.At(i)` /
`.Rows()` to zip it into `FilingRecord` on demand, rather than
restructuring at decode time. This keeps `Submissions` a faithful,
round-trippable mirror of the SEC response (acceptance criterion: "JSON
structs round-trip ... without data loss") instead of a lossy projection.

### D6 — `index.json`'s object-or-array ambiguity is handled once, at the model boundary

SEC's directory index (`index.json`) returns `directory.item` as either a
single object or an array depending on file count. `IndexItem` gets a
custom `UnmarshalJSON` so every caller downstream sees a normalized
`[]IndexItem` — this ambiguity is a known Python pain point
(`fetch_kamada.py` has to special-case it); solving it once in the model
avoids every call site re-deriving the same check.

### D7 — `Meta`'s JSON tags are pinned to the existing on-disk schema, not redesigned

Field names/types in `Meta` and `MetaSignals` (idea file §4) match what's
already written under `fileDB/companies/0001567529/*/meta.json` exactly,
including the 14 fixed category strings and the 1–4 tier scale. This is
what "byte-for-byte compatible" buys: prompt-1's UI and any already-shipped
`meta.json` files need zero migration.

### D8 — One `cmd/edgar` binary with subcommands, not three binaries

`cmd/edgar meta`, `cmd/edgar fetch --cik=... --years=N`, `cmd/edgar
backfill --cik=...` — one binary, dispatched on `os.Args[1]`, instead of
three separate `cmd/edgar-meta`/`cmd/edgar-fetch`/`cmd/edgar-backfill`
binaries. This also cuts against overdesign: the repo's only existing
binary is `cmd/server`, so three new binaries would be new precedent for
no functional reason, and one Makefile target replaces two. `main.go`
still stays thin — it parses flags, calls one or two `internal/edgar`
functions, and formats output. The specific behaviors in idea file
§"CLI behavior" (skip-if-exists, `--all`, exit-1-on-error, idempotent
downloads) are implemented in `internal/edgar`, not in `main.go`, so the
batch logic is independently testable without invoking the binary.

### D9 — Port order is fixed: classify first, network last

`build_meta` (+ parity tests) → `build_meta_all` → `fetch` → `backfill`.
Classification is both the highest-value and highest-risk piece (silent
drift would corrupt every downstream `meta.json`); it's built and locked
with tests before any network code exists, so parity is verified against
already-downloaded fixtures, not against live SEC responses.

### D10 — Python scripts stay in the repo until sign-off

`python/edgar/` is not deleted as part of this work. A README note is
added there pointing at the Go equivalents. Deletion is an explicit,
separate follow-up gated on the acceptance criteria passing — not
implied by "Go port done."

## 4. What this touches

| Area | New | Notes |
|---|---|---|
| `internal/edgar/` | yes | one package: `models.go`, `classify.go`, `extract.go`, `builder.go`, `client.go`, `store.go` (+ tests) |
| `cmd/edgar/main.go` | yes | one binary; `meta`/`fetch`/`backfill` subcommands |
| `internal/filedb` (prompt-1) | consumer, not modified here | imports `internal/edgar` (struct shapes + store read functions); must not duplicate structs |
| `.env.example` | append | `SEC_USER_AGENT` (required), `SEC_REQUEST_INTERVAL` (optional) |
| `Makefile` | append | `edgar` target wrapping `go run ./cmd/edgar` |
| `python/edgar/` | untouched (README note only) | not deleted this phase |

## 5. Contract with prompt-1

`internal/filedb` depends on `internal/edgar`'s exported struct shapes
(`Submissions`/`FilingRecord`/`Meta`) and its store read functions — it
must not define its own copies. This is the interface boundary between
the two prompts: as long as `Meta`'s JSON shape (D7) doesn't change,
prompt-1's UI work is decoupled from how this port proceeds internally.

## 6. Phase 2 migration path (not built here)

`FilingStore` (D1) is the swap point. A future `SQLiteStore` implements the
same four methods against tables instead of `fileDB/`; nothing in
classification, the HTTP client, or the CLI layer needs to change.
Migrating existing on-disk JSON into SQLite is explicitly out of scope for
this prompt.

## 7. Out of scope

- SQLite migrations/tables for companies, filings, meta
- LLM pipeline integration
- Prompt-1 UI itself (separate prompt; it consumes these models)
- Deleting `python/edgar/` (follow-up, after parity sign-off)

## 8. Risks

- **Classification drift**: a subtle mistranslation from Python changes
  tier/category for filings silently. Mitigated by D3's parity tests, but
  those only cover the ≥10 sampled folders — a long-tail form type could
  still slip through.
- **SEC rate limiting / blocking**: `fetch`/`backfill` are the first code
  paths to hit `data.sec.gov` live; a bug in the rate limiter (D4) risks a
  User-Agent ban, which would also affect any other tooling sharing that
  UA.
- **`index.json` shape surprises**: the object-vs-array normalization (D6)
  is based on today's observed SEC responses; a future SEC response shape
  change could resurface the same class of bug the Python code already hit.
- **Parallel-array indexing bugs**: `RecentFilings.At(i)` assumes every
  slice in `filings.recent` is the same length and aligned; a malformed or
  partial SEC response would silently misalign fields rather than error.

## 9. Done when

`make edgar-meta` run against `fileDB/companies/0001567529` reproduces
today's `meta.json` files exactly (tier, category, summary, tags) for every
existing accession folder, `go test ./internal/edgar/...` and `make test`
both pass, and prompt-1's `internal/filedb` reads the Go-produced JSON with
no changes on its side.

## 10. Deliverables

- `internal/edgar/` package (models, classification, client, store) with tests
- `cmd/edgar` binary (`meta`/`fetch`/`backfill` subcommands)
- `SEC_USER_AGENT` / `SEC_REQUEST_INTERVAL` in `.env.example`
- `edgar` Makefile target
- `python/edgar/README.md` pointing to the Go commands
