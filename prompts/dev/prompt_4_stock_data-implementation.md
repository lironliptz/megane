# Stock Price Provider & Cache — Implementation Notes (as-built)

## TL;DR

**No code was written for this prompt.** `prompt_4_stock_data`'s entire
scope — a market-data provider, a `stock_prices` cache table, and a
backfill/refresh orchestrator — was already built and shipped under
`prompt_3_company_view`'s own HLD/LLD before this `/implement-ld` run
started. That implementation is more rigorous than `prompt_4-lld.md`'s plan
in real, measurable ways (see below). Implementing `prompt_4-lld.md` as
written would have meant creating a second, conflicting set of types in an
already-working, already-tested package. This note exists so a future
reader of `prompt_4_stock_data-{hld,lld}.md` isn't misled into thinking the
feature is unbuilt or that those exact type names/signatures are what
shipped.

## What actually exists (built via `prompt_3_company_view-{hld,lld}.md`)

| File | Role |
|---|---|
| `internal/marketdata/provider.go` | `Bar`, `Quote`, `PriceProvider` interface (`DailyBars(ctx, symbol, from, to time.Time) (*Quote, error)`, `Name() string`); sentinel errors `ErrSymbolNotFound`, `ErrBadGranularity`, `ErrBadResponse` |
| `internal/marketdata/yahoo.go` + `yahoo_test.go` | `YahooProvider`; `NewFromEnv(name)` (Yahoo only today) |
| `internal/db/stock_prices.go` + `stock_prices_test.go` | `UpsertStockPrices`, `StockPrices`, `StockPriceCoverage`/`SetStockPriceCoverage`, `PriceCoverageRow.ShouldRetry` |
| `internal/db/db.go` migrations `{32, 33}` | `stock_prices` + `stock_price_coverage` tables (ids match what `prompt_4-lld.md` predicted, coincidentally) |
| `internal/companyview/{service.go, timeline.go, classify.go}` | Orchestrator (this prompt's planned `backfill.go`/`Service`) + prompt 3's own timeline/event-weighting logic |
| `cmd/server/main.go` | Wires `filedb.NewFileDBStore`, `marketdata.NewFromEnv`, `companyview.NewService` together at startup |

All of it passes: `go test ./internal/marketdata/... ./internal/db/...
./internal/companyview/...` → `ok`, `ok`, `ok` (verified at the start of
this `/implement-ld` run, before deciding not to touch it).

## Where the shipped design is stronger than `prompt_4-lld.md`

- **`range=max&interval=1d` bug**: measured to silently return **monthly**
  bars (`dataGranularity: "1mo"`, 160 bars) instead of the real 3,329 daily
  bars — a request that looks right and is wrong. `prompt_4`'s own audit
  never tested `range=max`, only `range=10y` and explicit `period1`/`period2`
  windows, so this bug was never caught in this prompt's design docs. The
  shipped `yahoo.go` never uses `range=`, only `period1`/`period2`, and
  asserts `meta.dataGranularity == "1d"` on every response — see
  `TestDailyBarsRejectsNonDailyGranularity`.
- **Adjusted-close divergence measured across the full series**, not a
  single day: 3,221/3,329 bars differ between `close` and `adj_close`, up
  to 5.7% — `prompt_4-hld.md`'s audit only compared one sample day (4.37 vs
  4.12). The shipped schema stores both columns rather than collapsing to
  one, which is more defensible given how pervasive the divergence is.
- **`stock_price_coverage` table with a status + retry floor**
  (`PriceStatusOK`/`NoSymbol`/`NotFound`/`Error`, `PriceRetryFloor = 6h`,
  `ShouldRetry`) — handles "this company has no ticker" or "the provider
  failed" as a first-class, persisted state so a tickerless company isn't
  re-hit against Yahoo on every page open. `prompt_4-lld.md`'s coverage
  design (`StockPriceCoverage` as a computed `MIN`/`MAX`/`count` query) had
  no equivalent — a failed/tickerless fetch would have been retried on
  every single visit.
- **200-with-HTML guard via `Content-Type` check** — belt-and-suspenders
  against exactly the kind of bot-wall response this prompt's audit found
  Stooq returning; not something `prompt_4`'s design called for explicitly,
  since it only ever tested Yahoo returning real JSON.

## Deviations from `prompt_4-lld.md`

Every point below is a "the doc's plan was superseded by already-shipped,
better-verified code," not a change made in this run:

1. **No Tiingo fallback was built** — `NewFromEnv` only recognizes
   `"yahoo"`; `MARKET_DATA_API_KEY` exists in `.env.example` but is unwired
   ("reserved for keyed providers"). `prompt_4-hld.md` D2 called for Tiingo
   specifically as the Stooq replacement. Given how the `stock_price_coverage`
   retry-floor already protects against a flaky/failing Yahoo without
   hammering it, and per the user's explicit choice not to build anything in
   this run, this stays a documented gap, not a bug — see Follow-ups.
2. **Type names/signatures differ entirely** from `prompt_4-lld.md`'s plan
   (`Bar`/`Quote`/`PriceProvider.DailyBars` vs. the doc's
   `DailyBar`/`Provider.DailyRange`) — cosmetic, but means `prompt_4-lld.md`
   is not a usable reference for the real API surface; use the files listed
   above instead.
3. **Coverage design differs structurally** — a dedicated table with status/
   retry-floor vs. the doc's computed-range approach (see above). The
   shipped version is the one to build against going forward.

## Tests

No new tests were written (no new code). Confirmed passing, unchanged:

```
go test ./internal/marketdata/... ./internal/db/... ./internal/companyview/...
ok  	megane/internal/marketdata
ok  	megane/internal/db
ok  	megane/internal/companyview
```

## How to enable / roll back

N/A — nothing was changed by this run. The feature is already live behind
`MARKET_DATA_PROVIDER=yahoo` (default) and `STOCK_FETCH_ON_COMPANY_OPEN`
in `.env.example`, wired in `cmd/server/main.go`.

## Follow-ups

- **Retire or rewrite `prompt_4_stock_data-{hld,lld}.md`** so they don't
  keep describing an API surface that was never built. This note is a
  patch, not a fix — the underlying docs still show `DailyBar`/`DailyRange`.
- **Tiingo fallback** remains a real, undecided piece of scope if Yahoo's
  reliability becomes a problem in practice — build it against
  `internal/marketdata`'s actual `PriceProvider` interface, not
  `prompt_4-lld.md`'s.
- **No `prompt_3_company_view-implementation.md` exists yet**, despite that
  prompt's code being the one actually shipped and wired into `main.go` —
  worth generating via `/implement-ld` against `prompt_3_company_view-lld.md`
  so prompt 3 has its own as-built record, separate from this note.

## Touched files

None.
