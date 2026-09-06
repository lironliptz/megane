# Batch Peer Company Ingestion (`cmd/fetch-similar`) — Implementation Notes (as-built)

Implements `prompt_13_fetch_similar_companies-lld.md` (contract: HLD + LLD, both in this directory).

## TL;DR

`cmd/fetch-similar` now exists: given a curated similar-companies JSON (`fileDB/similar/kamada.json`
today), it resolves each peer's SEC CIK, runs fetch → meta → financials → prices per peer, and writes
`{slug}.fetch-status.json`. `--dry-run` performs the LLD §5 costed survey instead — one request per
peer, no filing documents, no writes under `fileDB/companies/`, output to `{slug}.fetch-plan.json`.
`python/edgar/fetch_kamada.py` is parameterized (`--cik`/`--years`/`--root`/`--user-agent`) with the
zero-arg Kamada default preserved. All three LLD corrections (C1 positional `build_meta_all.py` arg,
C2 in-process financials extraction, C3 reading price status from the DB coverage row) are
implemented as written. Test level: **standard** (see below) — build clean, 32 unit tests green
(fakes/httptest/fixtures only, zero live network), full `make test` green, `go vet`/`gofmt` clean.

## What changed (file by file)

| File | Change |
|---|---|
| `python/edgar/fetch_kamada.py` | Rewrote to accept `--cik`, `--years`, `--root`, `--user-agent` via `argparse`; every function that read the old module constants (`CIK`, `CIK_INT`, `OUT`, `HEADERS`) now takes them as parameters (D2). Zero-arg invocation still fetches Kamada with a 10-year window. |
| `cmd/fetch-similar/main.go` | New. CLI flags (`-similar -root -years -steps -force -dry-run -peer-delay -fail-fast -include-reference -db-path`), orchestration loop, ticker-map load-once gate, `companyview.Service`/`db.DB` wiring for the prices step, exit codes (D8). |
| `cmd/fetch-similar/similar.go` | New. `SimilarList`/`SimilarCompany` types (company_id as `*string` to distinguish JSON `null`), `loadSimilarList` (read-only). |
| `cmd/fetch-similar/tickers.go` | New. `loadTickerMap` (SEC `company_tickers.json`, fetched once per run, only when needed), `resolveCIK` (company_id wins; ticker lookup; never guesses), `ErrNoUserAgent`. |
| `cmd/fetch-similar/status.go` | New. `FetchStatus`/`PeerStatus`/`StepResults`/`Summary` structs (LLD §3.2/§4.4), `statusPath`/`planPath` derivation, `writeJSONFile`. |
| `cmd/fetch-similar/steps.go` | New. `execRunner` (injectable subprocess seam), `defaultRunner`, `fetchNeeded`/`metaNeeded` idempotency checks, `stepFetch`/`stepMeta`/`stepFinancials`/`stepPrices`, `orchestrator.runPeer` (resolve → steps → counts → rollup). |
| `cmd/fetch-similar/submissions.go` | New. Shared `submissionsDoc` parsing (used by both the fetch-idempotency check and the dry-run survey), `fetchSubmissions` (one HTTP call), `readSubmissionsRecent` (on-disk read). |
| `cmd/fetch-similar/plan.go` | New. `PeerPlan`/`FetchPlan`/`PlanTotals`, `planPeer`, `applySubmissions` (pure — decoupled from the network call for testability), estimator constants (§5.3), `runDryRun`, stdout summary printer. |
| `Makefile` | New `fetch-similar` target: `SIMILAR=` required, `ROOT=`/`YEARS=` optional. |
| Tests | 9 test files, 32 tests total (see Tests below). |

Untouched, as the LLD specified: `python/edgar/build_meta_all.py`, `cmd/edgar-financials`,
`internal/companyview`, `internal/marketdata`. No DB migration.

## Deviations from the design

1. **Financials step reuses `financials.ExtractAll`/`FillGaps`'s own per-accession idempotency
   instead of a step-level skip gate.** The LLD's step table (§4.5) describes financials'
   idempotency as "every qualifying accession has financials.json" — but `ExtractAll` already skips
   an accession with an existing artifact unless `Force`, and does so per-accession, not per-peer.
   Gating the whole step externally would be coarser (and would duplicate the library's own
   category/no-facts logic outside the package that owns it). Reusing it is strictly more precise:
   a peer with 3 new filings and 50 already-extracted ones only extracts the 3.

2. **Prices step has no separate skip check; it relies entirely on `companyview.Service`'s own
   `ensureCoverage`.** Same reasoning as (1) — `ensureCoverage` already implements exactly the LLD's
   stated rule (skip when covered, delta-refresh when stale, full backfill only when empty).
   **Real limitation this creates:** `--force` has no effect on the prices step. `companyview.Service`
   exposes no "force a full re-walk" hook — only `Timeline()`, which is unconditionally
   idempotent-by-design. Flagged as a follow-up below rather than worked around, since adding that
   hook would mean changing `internal/companyview`, which the LLD's own "what this touches" table
   explicitly marks unchanged.

3. **C3, extended:** the LLD says to read `Timeline.PriceCoverage.Status`. Reading the actual
   `pricesFor` code (`internal/companyview/service.go`) shows it returns `nil` for `PriceCoverage`
   on the no-ticker path even though it **does** write a `no_symbol` coverage row via
   `SetStockPriceCoverage` first — so trusting the returned struct would silently mis-classify every
   no-ticker peer as "error" (`coverage nil`). The step instead calls
   `database.StockPriceCoverage(ctx, cik)` directly after `Timeline()` returns, which always sees the
   real row. This is a correction to C3 in the same spirit C3 itself was a correction to the HLD —
   noted here rather than silently changed.

4. **Peer rollup with "skipped" steps:** the LLD doesn't say whether a legitimately-skipped step
   (no ticker, nothing new to fetch) should block "complete." Implemented rollup: zero step
   **errors** → `complete`, regardless of skips; some errors with some successes → `partial`; all
   attempted steps errored → `error`. A peer with no ticker whose other three steps succeeded is
   `complete`, not `partial` — skipping isn't failing.

5. **`db-path` default fixed to match `cmd/server` exactly, not the LLD's literal env fallback.**
   The LLD's §6 code block reads `envOr("DB_PATH", "./.db/megane.db")`. Checking
   `internal/db/db.go`, `cmd/server` actually resolves the empty case via
   `db.ResolvePath(getEnv("DB_PATH", ""))`, whose real fallback constant is
   `./.db/jump-starter.db` — **not** `./.db/megane.db`. Hardcoding the LLD's literal default would
   have made this tool silently write prices into a second, invisible SQLite file whenever
   `DB_PATH` is unset, disagreeing with whatever the running server actually reads. Implemented as
   `os.Getenv("DB_PATH")` passed straight into `db.ResolvePath` at open time — byte-for-byte the
   same resolution `cmd/server` performs.

6. **Added a `-db-path` flag** beyond the LLD's flag table (§4.2), defaulting to `DB_PATH`'s value —
   an additive convenience for testing/multi-instance setups; every other flag matches the LLD
   exactly.

## Tests

**Level: standard** — the LLD's own Triage line says STANDARD, and its own §7 test table already
separates fully-automated unit tests (fakes/httptest/fixtures, no network) from one explicitly
manual item ("a live multi-peer SEC fetch. Verified manually per §8" — the LLD's words, not mine).
That's exactly what "standard" calls for here: extend tests per the doc's own Tests section with no
live third-party calls, and verify the DoD the cheapest reliable way locally. A full live batch
against real SEC endpoints was not run in this pass.

32 tests across 9 files, all real logic exercised — no test is a placeholder:

| Test file | Covers |
|---|---|
| `similar_test.go` | Parses the real `fileDB/similar/kamada.json` fixture; asserts the 7/2/1 CIK-resolution split; missing-file and empty-peers error paths. |
| `tickers_test.go` | `resolveCIK`'s four paths (company_id wins, null+ticker hits map, null+no-ticker skipped, unknown ticker skipped); ticker-map zero-padding via `httptest`; the UA guard makes **zero** HTTP requests when unset; `needsTickerMap` gating. |
| `status_test.go` | Path derivation (status vs. plan never collide); a full marshal→unmarshal round trip on the real schema; summary rollup arithmetic. |
| `steps_test.go` | Subprocess runner (fake — no real `exec`), tail-truncation of long output; fetch/meta idempotency against real temp-dir fixtures (no submissions.json → needed; all on disk → skip; a missing accession folder → needed again); price-status→step-result mapping (C3); **financials step run for real** against `internal/edgar/financials.ExtractAll` (no mock) — an empty corpus is `ok`, a missing directory is `error`, not a panic; full `runPeer` rollup (skipped-when-unresolved, complete-when-clean, error-when-nothing-succeeded). |
| `plan_test.go` | The dry-run survey end-to-end via `httptest` (fixture `submissions.json`, no live SEC): filing-count/form/XBRL/window/on-disk logic against a hand-built fixture, the disk/requests/seconds estimator formulas, and the three D9 guarantees proven directly — plan lists every peer including skips, a dry run never touches a pre-existing status file, the source JSON is byte-identical after a run. |
| `main_test.go` | CLI exit codes (`-similar` missing / malformed JSON → 2); `--steps` parsing and ordering. |

```
$ CGO_ENABLED=1 go test ./cmd/fetch-similar/... -v   # 32/32 PASS, 0.06s
$ CGO_ENABLED=1 make test                             # whole repo, all packages ok
$ go vet ./cmd/fetch-similar/...                      # clean
$ gofmt -l ./cmd/fetch-similar/                       # clean
$ python3 -m py_compile python/edgar/fetch_kamada.py  # clean
$ python3 python/edgar/fetch_kamada.py --help          # argparse output verified, zero-arg default intact
```

**DoD verification at the standard level:** the design's "Done when" (LLD §9) centers on
`make fetch-similar SIMILAR=./fileDB/similar/kamada.json YEARS=1` against **live** SEC — out of
scope for standard (no live third-party calls). What *was* verified locally, matching the DoD's
substance without the network dependency:

- `TestDryRunPlansAllPeers` runs the actual dry-run code path (parse → resolve → fetch submissions →
  estimate → write plan → exit code) against an `httptest` fixture standing in for `data.sec.gov`,
  and asserts the resolved peer is `planned` while the unresolvable one is `skipped` — the same
  shape the real "Done when" describes for the 7/3 Kamada split.
- `TestDryRunNeverWritesStatus` / `TestSourceJSONNeverWritten` prove the negative-case DoD line
  ("running the same command again... downloads nothing") holds for the survey path: a pre-seeded
  status file survives byte-for-byte, and `root/companies/` is never created.
- The real `financials.ExtractAll` and `companyview.Service` integration points are exercised
  directly (steps_test.go), not mocked — the only thing genuinely unverified end-to-end is the live
  HTTP round trip to `data.sec.gov`/`www.sec.gov` and the two Python subprocess invocations against
  a real network, which the design itself scoped as a manual step.

## How to enable / roll back

**Enable:** `make fetch-similar SIMILAR=./fileDB/similar/kamada.json` (add `YEARS=1` for a cheap
smoke run; set `SEC_EDGAR_USER_AGENT` in `.env` first — required for gap-fill and any ticker
resolution). Nothing is wired into the server or any existing route; this is a standalone CLI.

**Roll back:** delete `cmd/fetch-similar/`, revert `python/edgar/fetch_kamada.py` and the
`Makefile` hunk. No migration, no schema change, no other package touched.

## Follow-ups

- `--force` is a no-op on the prices step (deviation 2) — would need a new exported hook on
  `companyview.Service` to force a full re-walk; out of scope here since the LLD marks
  `internal/companyview` unchanged.
- A live smoke run (LLD §8, e.g. `--years 1` against ADMA) has not been performed in this session —
  worth doing once `SEC_EDGAR_USER_AGENT` is confirmed set, before the first real multi-peer batch.
- LLD §11's open question (ticker-map disk cache) is deliberately deferred to P2, unchanged from the
  design.

## Touched files

```
 M Makefile
 M python/edgar/fetch_kamada.py
?? cmd/fetch-similar/main.go
?? cmd/fetch-similar/similar.go
?? cmd/fetch-similar/tickers.go
?? cmd/fetch-similar/status.go
?? cmd/fetch-similar/steps.go
?? cmd/fetch-similar/submissions.go
?? cmd/fetch-similar/plan.go
?? cmd/fetch-similar/*_test.go   (9 test files)
```
