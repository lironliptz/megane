# HLD: Batch Peer Company Ingestion (`cmd/fetch-similar`)

Design for `prompts/dev/prompt_13_fetch_similar_companies.txt`.

Triage: **STANDARD** — new orchestrator binary + a first-time Go→Python shell-out
seam, but every ingestion step it drives already exists; no schema migration.

> **Governing rule:** the source peer list is hand-curated input — never mutated by
> automation. Machine output (resolved CIKs, step results, counts) goes only to
> `{slug}.fetch-status.json`. Skipping a peer honestly beats guessing a CIK.

---

## 1. Objective

Given a hand-curated **similar-companies JSON** file, ingest every resolvable peer to
**data parity** with the reference company named in that file: filings tree, `meta.json`,
`financials.json`, and (P1) stock prices — so downstream jobs (prompt 12 cross-company
training) have a multi-ticker corpus instead of a single-company `fileDB/`.

This prompt **ingests** an existing list. It does **not** discover peers.

---

## 2. Context — what exists today (verified in-repo)

| Step | Tool | Gap for batch use |
|------|------|-------------------|
| SEC fetch | `python/edgar/fetch_kamada.py` | `CIK` is a **module constant** — no `--cik` / `--years` flags, no `argparse`. |
| Meta build | `python/edgar/build_meta_all.py` | Already takes `--dir` / `--all` — reusable per `{root}/companies/{cik}`. |
| Financials | `cmd/edgar-financials` | Already takes `--root --cik --all --fill-gaps` — reusable; needs `SEC_EDGAR_USER_AGENT` for gap-fill. |
| Stock backfill | `companyview.Service.Timeline()` → `ensureCoverage` → `backfillFull` | Only triggered by opening a company's timeline tab today. No CLI hook; backfill helpers are unexported. |
| CIK normalize | `filedb.NormalizeCIK` | Generic — no change. |
| Ticker → CIK | — | **Does not exist** — net-new for peers with `company_id: null`. |
| Go → Python shell-out | — | **Does not exist** — this orchestrator is the first `exec.Command` seam in the Go tree. |

### 2.1 Reference corpus (example only — not hard-coded)

Measured against the repo's first curated list (`fileDB/similar/kamada.json`):

| Role | Example value | Notes |
|------|---------------|-------|
| Reference company | Kamada Ltd., CIK `0001567529`, ticker `KMDA` | 381 filings on disk; full `meta.json` / gap-filled `financials.json` |
| Peer count | 10 | 7 rows carry a `company_id`; 3 null (Kedrion, BPL, Biotest — no US SEC CIK) |
| Smoke-test peer | ADMA Biologics, CIK `0001368514`, ticker `ADMA` | First resolvable peer with a domestic filing pattern |

**Nothing in code or config should assume these values.** They are fixtures for docs,
tests, and the initial hand-authored JSON — all runtime paths take parameters (see §5).

---

## 3. Input & output contracts

### 3.1 Input — similar-companies JSON (v1)

Path supplied by `--similar` (required). Convention: `{root}/similar/{slug}.json`.

```json
{
  "company_name": "<reference display name>",
  "company_id": "<10-digit CIK>",
  "ticker": "<optional primary US ticker>",
  "similar_companies": [
    {
      "company_name": "<peer name>",
      "company_id": "<CIK or null>",
      "ticker": "<optional — used when company_id is null>",
      "relation_type": "<free text>",
      "description": "<free text>"
    }
  ]
}
```

The orchestrator reads `company_id` from the **reference** block only for status metadata
and parity comparison — it does **not** re-fetch the reference company unless explicitly
included in `similar_companies[]` or passed via a future `--include-reference` flag.

### 3.2 Output — fetch-status JSON (v1)

Derived path: same directory and basename as input, suffix `.fetch-status.json`
(e.g. `kamada.json` → `kamada.fetch-status.json`). A `--dry-run` writes
`{slug}.fetch-plan.json` instead and never touches the status file (D9).

```json
{
  "source_file": "<path to input JSON>",
  "reference": {
    "company_name": "…",
    "company_id": "…",
    "ticker": "…"
  },
  "generated_at": "<RFC3339>",
  "config": {
    "root": "<fileDB root>",
    "years": 10,
    "steps": ["fetch", "meta", "financials", "prices"],
    "force": false
  },
  "peers": [
    {
      "company_name": "…",
      "company_id": "<resolved CIK or null>",
      "ticker": "…",
      "status": "complete | partial | skipped | error",
      "steps": {
        "resolve": "ok | skipped | error",
        "submissions": "ok | skipped | error",
        "filings": "ok | skipped | error",
        "meta": "ok | skipped | error",
        "financials": "ok | skipped | error",
        "stock_prices": "ok | skipped | error"
      },
      "filing_count": 0,
      "financials_count": 0,
      "stock_bar_count": 0,
      "duration_ms": 0,
      "note": ""
    }
  ],
  "summary": {
    "total": 0,
    "complete": 0,
    "partial": 0,
    "skipped": 0,
    "error": 0
  }
}
```

Status file is rewritten wholesale each run (not merged). Resolved CIKs for previously-null
peers are recorded here only — never written back to the source JSON.

### 3.3 Output — fetch-plan JSON (dry-run only, v1)

When `--dry-run` is set, write `{slug}.fetch-plan.json` instead of `.fetch-status.json`.
The plan is a **costed survey**: high-level identity plus counts and size/time estimates for
what a real run would fetch — without downloading filing documents or writing under
`{root}/companies/`.

```json
{
  "source_file": "<path to input JSON>",
  "reference": { "company_name": "…", "company_id": "…", "ticker": "…" },
  "generated_at": "<RFC3339>",
  "config": {
    "root": "<fileDB root>",
    "years": 10,
    "dry_run": true
  },
  "peers": [
    {
      "company_name": "…",
      "company_id": "<resolved CIK or empty>",
      "ticker": "…",
      "status": "planned | skipped | error",
      "sic": "2836",
      "sic_description": "Biological Products",
      "exchanges": ["Nasdaq"],
      "filings_in_index": 504,
      "filings_in_window": 359,
      "filings_on_disk": 0,
      "filings_to_fetch": 359,
      "form_counts": { "10-K": 10, "10-Q": 40, "8-K": 85 },
      "earliest_filing": "2016-03-15",
      "latest_filing": "2026-08-01",
      "xbrl_flagged": 28,
      "est_download_bytes": 396361728,
      "est_disk_bytes": 554906419,
      "est_requests": 4309,
      "est_seconds": 519,
      "note": "xbrl_flagged is an upper bound on financials.json count"
    }
  ],
  "totals": {
    "peers": 10,
    "planned": 7,
    "skipped": 3,
    "error": 0,
    "filings_to_fetch": 1842,
    "est_download_bytes": 2100000000,
    "est_disk_bytes": 2940000000,
    "est_requests": 22000,
    "est_seconds": 3600
  }
}
```

| Mode | Network | Writes under `{root}/companies/` | Output file |
|------|---------|----------------------------------|-------------|
| **Real run** | Full fetch + meta + financials + prices | Yes | `{slug}.fetch-status.json` |
| **`--dry-run`** | 1× `submissions.json` per resolvable peer (+ ticker map once if needed) | **No** | `{slug}.fetch-plan.json` |

---

## 4. Architecture decisions

### D1 — Orchestrator is a thin Go CLI that shells out, not a Go rewrite

`cmd/fetch-similar/main.go` parses the input JSON and, per resolvable CIK, runs steps via
`os/exec`:

1. Python fetch (generalized script, §D2)
2. `python/edgar/build_meta_all.py --dir {root}/companies/{cik}`
3. `go run ./cmd/edgar-financials --root {root} --cik {cik} --all --fill-gaps`
4. (P1) Stock backfill via `companyview.Service` (§D4)

Prompt 2's `cmd/edgar fetch` does not exist yet — wrapping Python for P0 is explicit scope.
When Go fetch ships (P2), only step 1's exec target changes; the loop and status schema stay.

### D2 — Generalize the Python fetch script via parameters, keep backward compatibility

Rename is **not** required for P0. Add `argparse` to `fetch_kamada.py`:

| Flag | Default | Env fallback |
|------|---------|--------------|
| `--cik` | `0001567529` | — (keeps zero-arg Kamada behavior) |
| `--years` | `10` | — |
| `--root` | `./fileDB` | `FILEDB_DIR` |
| `--user-agent` | script header | `SEC_EDGAR_USER_AGENT` |

Derive `OUT = {root}/companies/{normalized_cik}` from flags — remove the module-level
`CIK` / `OUT` constants as the sole source of truth.

### D3 — All paths and depths are CLI- or env-driven

| Parameter | Flag / env | Default |
|-----------|------------|---------|
| Input peer list | `--similar` | **required** |
| fileDB root | `--root` / `FILEDB_DIR` | `./fileDB` |
| Filing history depth | `--years` | `10` |
| Steps to run | `--steps` | `fetch,meta,financials,prices` |
| Overwrite existing | `--force` | `false` |
| Survey only (no documents) | `--dry-run` | `false` |
| Inter-peer sleep | `--peer-delay` | `5s` |
| SEC User-Agent | — | `SEC_EDGAR_USER_AGENT` (required for gap-fill + ticker map) |

No Kamada-specific CIK, ticker, or path literals in Go orchestrator code.

### D4 — Ticker→CIK resolution is cacheable and optional per peer

For a peer with `company_id: null`:

1. If `ticker` empty → `skipped`, note `"no company_id and no ticker"`.
2. Else fetch `https://www.sec.gov/files/company_tickers.json` **once per run** (not per peer).
3. Build in-memory `map[ticker]cik` (case-insensitive).
4. On miss → `skipped`, note `"ticker not in SEC company_tickers"`.

Implementation: `cmd/fetch-similar/tickers.go`. Cache file optional P2:
`{root}/similar/.company_tickers.cache.json` with TTL — not required for P0.

### D5 — Stock backfill (P1) reuses `companyview.Service`, not a new marketdata CLI

Construct `companyview.Service` the same way `cmd/server` does (shared `db`, `filedb`,
provider/fallback from env). Call exported `Timeline(ctx, cik, window, "")` with a window
wide enough to trigger `ensureCoverage` → full-history `backfillFull`. Same code path as
opening the Timeline tab — no export of unexported backfill helpers.

Skip the `prices` step when `submissions.json` has no resolvable ticker (record
`stock_prices: skipped`, not `error`).

### D6 — Idempotency, partial status, and rate limits

| Step | Skip when (unless `--force`) |
|------|------------------------------|
| fetch | `submissions.json` exists and `--years` window already covered |
| meta | every accession folder already has `meta.json` |
| financials | all qualifying accessions have `financials.json` (or explicit no-facts) |
| prices | `stock_price_coverage.status = ok` and span covers full history |

Per-request SEC spacing: inherit ≥120 ms from Python fetch. Orchestrator adds
`--peer-delay` (default 5 s) between peers.

A peer with filings but failed gap-fill → `partial`, not `complete` or `error`.

### D7 — Subprocess contract (first Go→Python seam)

Each exec step must capture:

- exit code
- last 4 KB of combined stdout/stderr (truncated into `note` on failure)
- wall-clock `duration_ms` per peer

On non-zero exit: mark that **step** `error`, continue to next peer unless `--fail-fast`
(optional P1 flag; default false).

### D8 — Exit code reflects batch health

| Exit | Condition |
|------|-----------|
| `0` | Every peer ended `complete`, `partial`, or `skipped`; none `error` |
| `1` | At least one peer `error` |
| `2` | CLI usage / JSON parse failure before any peer runs |

Enables `make fetch-similar` to gate downstream automation without parsing status JSON.

### D9 — `--dry-run` is a costed survey, not a plan print

A batch over ten peers is an expensive, slow, mostly-irreversible download. `--dry-run`
therefore answers *"what would this cost me?"* before committing, using only the one cheap
request the real run makes first anyway.

Per peer it resolves the CIK and fetches **only** `submissions.json` — one request, no
filing documents, no `meta`, no `financials`, no prices. That single file is a complete
filing index and carries far more than a list: measured on the reference company it has
`filingDate`, `form`, `size` (bytes per submission), `isXBRL` / `isInlineXBRL`, and
`accessionNumber` for every filing.

That is enough to report, per peer and as a batch total:

| Reported | Derived from |
|---|---|
| Identity — name, ticker, SIC, exchange | `submissions.json` header |
| Filings in index / **within `--years`** | `filings.recent` + date filter |
| Already on disk / **would be fetched** | diff against `{root}/companies/{cik}` |
| Form distribution | `form` counts |
| **Estimated download** | `sum(size)` over the window |
| **Estimated disk** | `sum(size) × 1.4` (measured, see below) |
| XBRL-flagged count | `isXBRL \| isInlineXBRL` — an **upper bound** on `financials.json` |
| Estimated requests / wall-clock | filings × files-per-filing ÷ the SEC rate floor |

**Calibrated against the reference corpus** (2026-09-01): the index sums to 464 MB across
504 filings while the folder occupies 647 MB, giving the **1.4× factor** — the fetch stores
both the full-submission `.txt` (which is what `size` measures) and the extracted documents.
A 10-year window is 359 filings / 378 MB indexed ≈ 530 MB on disk; a 1-year window is
73 filings / 66 MB ≈ 92 MB.

The XBRL count is deliberately labelled an upper bound: 28 filings are XBRL-flagged in the
10-year window, but only 15 carry an unpacked instance locally — the gap prompt 9 exists to
close. Reporting it as a prediction would overstate what `financials` will produce.

**A dry run never writes `{slug}.fetch-status.json`.** It writes `{slug}.fetch-plan.json`
instead, so surveying can never clobber the record of a real run.

CLI also prints a one-screen **batch summary** to stdout (peer name, filings to fetch,
estimated disk, estimated time) so operators can approve the run without opening the JSON.

---

## 5. Orchestration flow

```
--similar {path}
     │
     ▼
[ Parse JSON ] ──fail──► exit 2
     │
     ▼
[ Load ticker map ] (if any peer has company_id null + ticker)
     │
     ▼
For each peer in similar_companies[]:
     │
     ├─ resolve CIK (from company_id or ticker map)
     │      └─ unresolved → status skipped, continue
     │
     ├─ [--dry-run] GET submissions.json only → counts + size/time estimate
     │      └─ write {slug}.fetch-plan.json, skip every step below   (D9)
     │
     ├─ [fetch]   python …/fetch_kamada.py --cik {cik} --years {years} --root {root}
     ├─ [meta]    python …/build_meta_all.py --dir {root}/companies/{cik}
     ├─ [financials] go run ./cmd/edgar-financials --root {root} --cik {cik} --all --fill-gaps
     ├─ [prices]  companyview.Service.Timeline(…)   (P1)
     │
     ├─ count filings / financials / stock bars on disk + DB
     ├─ append peer row to status
     └─ sleep --peer-delay
     │
     ▼
Write {slug}.fetch-status.json
Exit per D8
```

---

## 6. What this touches

| File | Change |
|------|--------|
| `python/edgar/fetch_kamada.py` | Add `--cik`, `--years`, `--root`, `--user-agent` (D2, D3) |
| `cmd/fetch-similar/main.go` | New — CLI, loop, exec, exit codes |
| `cmd/fetch-similar/tickers.go` | New — SEC ticker map |
| `cmd/fetch-similar/status.go` | New — status JSON read/write |
| `cmd/fetch-similar/steps.go` | New — step runners + idempotency checks |
| `cmd/fetch-similar/plan.go` | New — dry-run survey: submissions fetch, counts, size/time estimates (D9) |
| `Makefile` | `fetch-similar` target: `SIMILAR=` and optional `ROOT=`, `YEARS=` |
| `python/edgar/build_meta_all.py` | No change |
| `cmd/edgar-financials` | No change |
| `internal/companyview` | No change — reused via `Service.Timeline()` (D5) |

Nothing in prompts 1–12 changes. No DB migration.

---

## 7. Out of scope

- Automated peer discovery (LLM, industry screens, FactSet).
- Analyst coverage fetch (prompt 11) and prediction training (prompt 12).
- Go fetch replacement (prompt 2) — P2 swap of exec target only.
- DB schema changes.
- `POST /api/admin/fetch-similar` — P3 optional admin trigger.

---

## 8. Risks

| Risk | Severity | Mitigation |
|------|----------|------------|
| Hard-coded Kamada CIK in Python fetch blocks batch | **High** | D2 — parameterize before orchestrator ships |
| Missing `SEC_EDGAR_USER_AGENT` | **High** | Fail financials / ticker-map steps fast; mark step `error`, not whole process |
| Foreign-primary ADR filers (different form mix) | Medium | Ingest anyway; low `financials_count` vs reference is expected — status stays `complete` or `partial`, not `error` |
| First Go→Python subprocess seam | Medium | D7 — capture exit code + stderr snippet in status |
| Partial run reported as complete | Medium | D6 — explicit `partial` when any requested step failed or was skipped |
| Burst SEC rate limits on 10-peer batch | Low | `--peer-delay` + inherited 120 ms per request |

---

## 9. Done when

Given **any** valid similar-companies JSON (not only the Kamada list):

1. `go run ./cmd/fetch-similar --similar {path} [--dry-run]` parses all peers and resolves CIKs.
2. A full run creates `{slug}.fetch-status.json` with one row per peer and a `summary` block.
3. At least one resolvable peer has `{root}/companies/{cik}/submissions.json` and ≥1 accession folder.
4. Resolvable peers have `meta.json` on downloaded accessions; `edgar-financials --fill-gaps` has run when UA is set.
5. Company search finds ingested peers by name or ticker without manual DB edits.

A **dry run** on the same list makes exactly one request per resolvable peer, writes
`{slug}.fetch-plan.json` with per-peer counts and a batch total, downloads no filing
documents, and leaves any existing `.fetch-status.json` untouched.

**Example acceptance** (using the bundled fixture list):

```bash
make fetch-similar SIMILAR=./fileDB/similar/kamada.json YEARS=1
```

→ ADMA folder exists; status shows 3 `skipped` (no SEC CIK); exit 0.

---

## 10. Deliverables & phasing

| Priority | Deliverable |
|----------|-------------|
| **P0** | Parameterized `fetch_kamada.py` (D2, D3) |
| **P0** | `cmd/fetch-similar` — parse, resolve, fetch, meta, financials, status (D1, D4, D6–D8) |
| **P0** | `--dry-run` costed survey + `{slug}.fetch-plan.json` (D9) — ships before the first real batch |
| **P1** | Stock backfill step via `companyview.Service` (D5) |
| **P1** | `make fetch-similar` with `SIMILAR`, `ROOT`, `YEARS` variables |
| **P1** | Unit tests: JSON parse, ticker map fixture, dry-run plan |
| **P2** | Swap fetch step to `cmd/edgar fetch` when prompt 2 ships |
| **P3** | Optional ticker-map disk cache; `POST /api/admin/fetch-similar` |

Ship P0 (+ P1 stock backfill) before prompt 12 cross-company training.

### Open questions (1)

1. **Include reference company in batch?** Default: fetch peers only. If the reference
   CIK is missing from disk, add `--include-reference` to run the same pipeline on
   `reference.company_id` — useful for fresh clones. Defer to LLD unless needed for P0.

---

## References

- Input fixture: `fileDB/similar/kamada.json`
- Prior art: `prompt_2_convert_from_python_to_go-hld.md` (fetch port plan),
  `prompt_9_extra_source_for_financial_reports-hld.md` (batch CLI + coverage row),
  `prompt_12_suggest_prediction_mechanism-hld.md` (multi-company training consumer)
- SEC ticker map: [sec.gov/files/company_tickers.json](https://www.sec.gov/files/company_tickers.json)
