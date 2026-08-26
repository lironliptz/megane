# Stock Price Provider & Cache (OHLCV) — Implementation Notes (as-built)

Implements the remaining work in `prompt_4_stock_data-lld.md` (§"Remaining work — file-by-file",
§"Build order") under the contract in `prompt_4_stock_data-hld.md` (D1–D12). Built 2026-08-26 on
top of the already-shipped baseline (Yahoo provider, `stock_prices`/`stock_price_coverage`
tables, window-scoped `ensureCoverage`) that both docs' "As-built baseline" sections describe.

---

## TL;DR

Completed every item the LLD's "Not shipped" table (HLD §10) listed: **chunked full-history
backfill** (D7/D10), **Tiingo fallback** (D2), and the **chunk env vars** in `.env.example`. Yahoo
now exposes `meta.firstTradeDate` and distinguishes a rate limit (HTTP 429, retryable) from the
terminal "no data before listing" response (HTTP 400 + `chart.error`, measured live and now
handled as the walker's expected stop condition instead of a generic error).

Verified against **live Yahoo**, not just fakes: an isolated verification server (its own DB copy,
port `:8198`; the user's `:8123` server was never touched) ran the real chunked backfill for CIK
`0001567529`/KMDA. Measured result — **3 real Yahoo HTTP chunks**, full history from the true
listing date **2013-05-31** to today, **3,327 rows**, non-zero volume on real trading days, a
second identical request in **0.042s vs the first request's 1.8s** (zero external HTTP), and a
forced delta scenario that appended exactly **7 new rows** in **0.44s** — a single bounded fetch,
not a re-walk. Full numbers in §"Tests — Definition of Done" below.

`make test` — the same three **pre-existing** invalid `generated/*/route_snippet.go` failures
every prior report in this repo has recorded; every real Go package passes, including 46 new
tests across three packages.

---

## What changed (file by file)

| File | Change |
|---|---|
| `internal/marketdata/provider.go` | Added `Quote.FirstTradeDate`; new sentinels `ErrRateLimited`, `ErrNoDataForRange` |
| `internal/marketdata/yahoo.go` | Parses `meta.firstTradeDate`; `YahooProvider.UserAgent` field (env-configurable, was a hardcoded const); HTTP 429 detected on status code alone (before any JSON parse — a 429 body can be plain text); HTTP 400 + `chart.error` mapped to `ErrNoDataForRange`; `NewFromEnv`/new `NewFallbackFromEnv` read `MARKET_DATA_USER_AGENT`/`MARKET_DATA_API_KEY` |
| `internal/marketdata/yahoo_test.go` | +8 tests: `FirstTradeDate` parsing (present/absent), UA default/override, terminal-400, 429 (plain-text and JSON bodies) |
| `internal/marketdata/tiingo.go` (new) | `TiingoProvider` implementing `PriceProvider`; `/tiingo/daily/{ticker}/prices` with `Authorization: Token {key}`; maps `close/open/high/low/volume/adjClose`; fails fast on a missing key instead of making a request Tiingo would reject |
| `internal/marketdata/tiingo_test.go` (new) | 10 structural tests (URL/param shape, auth header, field mapping, 404/401/429, empty-array terminal) — explicitly marked unverified-live, no Tiingo key in this repo |
| `internal/marketdata/backfill.go` (new) | `Backfiller.Backward` — chunked walker: retry-with-backoff on `ErrRateLimited`, per-chunk fallback to a secondary provider, stop on `FirstTradeDate` or `ErrNoDataForRange`, `onChunk(quote, source)` callback for crash-safe per-chunk persistence |
| `internal/marketdata/backfill_test.go` (new) | 10 tests against a fake `PriceProvider`, no network: stop conditions, retry/backoff ladder, fallback attribution, context cancellation, onChunk-failure abort, volume pass-through |
| `internal/companyview/service.go` | `Config` gains `BackfillTimeout`, `ChunkYears`, `ChunkDelay`; `Service` gains an optional `fallback` provider; `ensureCoverage` now branches full-backfill (empty coverage) vs. delta-refresh (stale coverage) instead of always doing one window-scoped fetch; new `refreshDelta`, `backfillFull`, `upsertQuote`, `hasCoverage` |
| `internal/companyview/service_test.go` (new) | 7 tests against a fake `PriceProvider` + a real temp SQLite DB: full-backfill-ignores-requested-window, delta-fetch-is-bounded, no-op-when-covered, retry-floor, partial-backfill-stays-OK, fallback-wiring |
| `internal/handlers/timeline_test.go` | Updated the one `companyview.NewService` call site for the new `fallback` parameter |
| `cmd/server/main.go` | Wires `marketdata.NewFallbackFromEnv()`; reads `MARKET_DATA_CHUNK_YEARS`/`MARKET_DATA_CHUNK_DELAY` via new `getEnvInt`/`getEnvDuration` helpers |
| `.env.example` | Documents `MARKET_DATA_API_KEY` (Tiingo), `MARKET_DATA_USER_AGENT`, `MARKET_DATA_CHUNK_YEARS`, `MARKET_DATA_CHUNK_DELAY` |

No DB schema change — the LLD's own "As-built baseline" table already had `volume`/`adj_close`
shipped (migrations 32–33); this work is provider + orchestration only.

---

## Deviations from the design

**1. `backfillFull` does NOT downgrade coverage status to `error` after a partial success.**
The LLD's pseudocode doesn't spell out what happens to the coverage row when the walk gets some
chunks then hits a hard failure. `UpsertStockPrices` already recomputes `earliest`/`latest` from
whatever landed and sets `status=ok` on every successful chunk; if `backfillFull` then overwrote
that to `status=error` on the walk's eventual failure, the next request would see `hasCoverage()
== false` and re-walk the **entire** history from today again — including re-hitting the exact
chunk that just failed — rather than resuming. Left the partial `ok` row alone instead (only a
walk that got **zero** bars records a failure, so `ShouldRetry`'s floor still protects a
genuinely broken symbol). This is the HLD's own accepted risk (§7: "outer earliest/latest cannot
detect mid-series holes; acceptable MVP; future force refresh") — I chose the reading of it that
doesn't make a partial success worse than a total one. Covered by
`TestEnsureCoveragePartialBackfillStaysOK`.

**2. `Backfiller.Backward`'s `onChunk` callback carries the serving provider's name**
(`func(q *Quote, source string) error`), not just the `Quote` as the LLD's pseudocode shows. A
chunk served by the fallback must be recorded as `source="tiingo"` in `stock_prices`, not
attributed to the primary — the LLD didn't address this because its pseudocode has no fallback
branch inside the loop body. Verified by `TestBackwardFallsBackAfterRetriesExhausted`'s explicit
`sources = [tiingo, yahoo]` assertion.

**3. `MARKET_DATA_API_KEY` is reused as the Tiingo key**, not a new env var. The LLD says "document
Tiingo key" without naming one; `.env.example` already had `MARKET_DATA_API_KEY` marked "reserved
for keyed providers" from the as-built baseline. Using it avoids a redundant second key variable.

**4. `NewYahooProvider`'s signature changed** from `(client *http.Client)` to
`(client *http.Client, userAgent string)` — required to make `MARKET_DATA_USER_AGENT`
configurable per D1 ("every request sets a realistic User-Agent... env or code default"), which
wasn't wired at all in the as-built baseline. One existing test call site updated.

**5. `BackfillTimeout` is a new `Config` field** (default 5 minutes), separate from the existing
`FetchTimeout` (20s, still used for the single-call delta path). The LLD doesn't mention this
knob, but `FetchTimeout` bounding an entire multi-chunk walk — each chunk with up to 3 retries and
backoff to 30s — would time out a legitimate backfill partway through. Not env-configurable (the
HLD's D10 table only lists chunk span/delay as tunable); a code-level default only.

**6. Retry count and backoff ladder are Backfiller-internal constants**, not threaded through
`companyview.Config` — this matches the HLD's D10 table marking them "(constant)" while only
chunk years/delay get env vars. `DefaultMaxRetries=3`, `DefaultBackoff=[2s,8s,30s]` live in
`marketdata.backfill.go`.

No other deviations — provider interface (`Bar`/`Quote`/`PriceProvider`), storage functions, and
the Timeline JSON contract are unchanged, per the LLD's explicit "do not add parallel types"
instruction.

---

## Tests

```
go build ./cmd/... ./internal/...                         # clean
go vet   ./cmd/... ./internal/...                          # clean
gofmt -l <all touched .go files>                            # clean
go test ./internal/marketdata/...                           # ok — 33 tests (was 12)
go test ./internal/companyview/...                           # ok — +7 new (service_test.go)
go test ./internal/handlers/... ./internal/db/...             # ok — unaffected, still green
make test                                                    # FAILS only on the 3 pre-existing generated/ files
```

### Definition of Done — verified live, against real Yahoo

Verification server on `:8198`, its own copy of the DB (the user's `:8123` server never touched).
KMDA's existing cached rows were deliberately cleared in **that copy only** so the backfill ran
from empty coverage — the same starting state a brand-new company hits.

| DoD element (HLD §8) | Measured evidence |
|---|---|
| Yahoo fetches with non-zero volume on typical days | `2026-08-03..07`: volumes 26400/65200/92700/42600/17700 |
| Chunked backfill uses ≥2 chunks for >5y history | **3** distinct `fetched_at` timestamps (real HTTP round-trips, ~600ms apart matching the 400ms `MARKET_DATA_CHUNK_DELAY`) |
| Stops at `firstTradeDate`, pre-listing 400 not a hard failure | `priceCoverage.earliest = 2013-05-31` — the true KMDA listing date (Yahoo's own `meta.firstTradeDate`), reached without an error |
| `stock_prices` holds all bars, listing → today | **3,327 rows**, `2013-05-31` → `2026-08-25` |
| Second request same window: zero external HTTP | Request 1: **1.819s**. Request 2 (identical window): **0.042s**, byte-identical response |
| Delta refresh adds only dates after `coverage.latest` | Forced `latest` back to `2026-08-14`; next request took **0.438s** (one bounded call, not a 3-chunk walk) and appended exactly **7** rows, `2026-08-17..2026-08-25` — the real trading-day gap |
| Timeline API returns `volume` on each price point | Confirmed in every response above (`"volume": N` on each `prices[]` element) |
| Tiingo fallback path tested | Structural only (no live key in this repo, exactly as the LLD anticipated) — 10 tests in `tiingo_test.go` plus `TestEnsureCoverageUsesFallbackOnPrimaryExhaustion`/`TestBackwardFallsBackAfterRetriesExhausted` proving the wiring end-to-end with a fake provider |
| `go test .../marketdata ... .../db ... .../companyview` + `make test` | All pass except the 3 pre-existing `generated/` failures |
| `.env.example` documents provider/key/UA/chunk settings | Done (see table above) |

Also measured, beyond the checklist: requesting **today's** date (2026-08-26, before market
close/report) correctly returned `ErrNoDataForRange` from real Yahoo and `refreshDelta` treated it
as "nothing new yet," not an error — confirmed directly against Yahoo outside the app too
(`curl` on that exact date range returns the same `chart.error` shape).

The isolated DB copy and built verification binary were deleted after testing; nothing was
committed to the real `.db/megane.db`.

---

## How to enable / roll back

Live once rebuilt — no migration, no new endpoint. `MARKET_DATA_PROVIDER=yahoo` (default) needs no
key; setting `MARKET_DATA_API_KEY` to a real Tiingo token activates the fallback automatically
(no separate "enable fallback" flag). `MARKET_DATA_CHUNK_YEARS`/`MARKET_DATA_CHUNK_DELAY` are
optional — omitting them uses the HLD's D10 defaults (5 years / 400ms).

**Roll back:** revert `internal/marketdata/{provider,yahoo}.go` and delete `backfill.go`,
`tiingo.go`, and their tests; revert `internal/companyview/service.go` and delete
`service_test.go`; revert `cmd/server/main.go` and `.env.example`; revert the one call-site fix in
`internal/handlers/timeline_test.go`. Existing cached `stock_prices` rows remain valid either way
— this is a fetch-path change, not a schema or wire-format change.

---

## Follow-ups

1. **Tiingo remains unverified against a live key** — the LLD flagged this explicitly and it's
   still true. The auth-rejection branch (401/403) and the empty-array terminal condition are
   both best-effort mappings from Tiingo's published docs, not confirmed against real responses.
2. **Mid-series holes from a partial backfill are still undetected** (HLD §7, accepted MVP risk,
   reaffirmed by Deviation #1) — a `force=1` full re-walk is future work, not attempted here.
3. **Chart.js volume rendering** (HLD §6 out-of-scope, prompt 3/5 UI territory) — data has been
   available on the wire since the as-built baseline; still nothing renders it.
4. **`FetchOnOpen` config field is still dead code** — noticed while reading `service.go` (never
   read anywhere, not introduced by this work, not touched here; out of this LLD's scope).
5. **The three pre-existing `generated/*/route_snippet.go` files** still break `go test ./...` —
   carried forward from every prior implementation report in this repo.

---

## Touched files

**New (5):** `internal/marketdata/backfill.go`, `internal/marketdata/backfill_test.go`,
`internal/marketdata/tiingo.go`, `internal/marketdata/tiingo_test.go`,
`internal/companyview/service_test.go`

**Modified (8):** `internal/marketdata/provider.go`, `internal/marketdata/yahoo.go`,
`internal/marketdata/yahoo_test.go`, `internal/companyview/service.go`,
`internal/handlers/timeline_test.go`, `cmd/server/main.go`, `.env.example`

No commit was created.
