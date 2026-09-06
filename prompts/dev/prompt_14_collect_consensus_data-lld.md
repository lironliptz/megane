# LLD: Consensus & Analyst-Estimate Ingestion (`cmd/collect-consensus`)

Implements `prompt_14_collect_consensus_data-hld.md` (the contract) and the idea file
`prompt_14_collect_consensus_data.txt`.

Triage: **HEAVY** — migration v34 (first since 33), new `internal/consensus/` package, new
`internal/db/consensus.go`, new CLI, a fetch-cache tree, and an external vendor dependency
with hard daily quotas.

> **Governing rule:** a number without `source`, `consensus_date`, and `basis` is worse than
> a null — it poisons every surprise computed from it. Provenance columns are `NOT NULL`
> wherever the schema can enforce it.

---

## 1. Scope

**In (P0):** migration v34; `internal/models/consensus.go`; `internal/db/consensus.go`;
`internal/consensus/` with the `Provider` interface and Finnhub; `cmd/collect-consensus`
with `--cik`, `--dry-run`, coverage rows.
**In (P1):** FMP revenue; Nasdaq Tier 2; `--similar` batch; `--snapshot`; Tier 3 `--gap-fill`.
**In (P2):** surprise join helper; `GET /api/companies/:cik/consensus`; firm roster seed.
**Out:** licensed feeds; broker scraping; analyst person names; prompt 12 training.

---

## 2. One correction and one confirmation

**Correction — the sidecar field name is `endDate`, not `period_end`.** The HLD and idea
file both describe joining on `(period_end, duration)`. That is the *column* name; the
`financials.json` sidecar uses `period.endDate`, `period.duration`, `period.focus`, and
`period.label`. The join helper reads the sidecar's names and writes the column's names —
the LLD's `financials.json` reader must not look for `period_end`.

**Confirmation — the D3 derivation works, measured 23/23.** With
`submissions.json.fiscalYearEnd = "1231"`, deriving `period_end` from fiscal year + period
focus reproduces the actual sidecar `endDate` for **all 23** joinable periods of the fixture
company. The FY/Q4 collision D3 warns about is real and present: `2025-12-31` exists as
`P1Y` (FY 2025, EPS $0.35). A Finnhub Q4 estimate would land on the same date with `P3M` —
a *different* row only because `duration` is in the key.

---

## 3. Current state (verified 2026-09-06)

| Thing | Reality |
|---|---|
| Migration head | **33** (`stock_price_coverage`); v34 is free |
| Migration format | `{N, ` + backtick-quoted DDL + `}` appended to `versionedMigrations` in `internal/db/db.go` |
| DB helper style | `func (d *DB) Verb(ctx context.Context, …) error`, `INSERT … ON CONFLICT(...) DO UPDATE SET x = excluded.x` |
| `internal/consensus/` | does not exist |
| Analyst tables | none in code |
| API keys | `FINNHUB_API_KEY`, `FMP_API_KEY`, `ALPHA_VANTAGE_API_KEY` — all empty |
| Fixture | `fileDB/similar/kamada.json`, 10 peers, **3 with `fetch: true`** |
| Fixture company | `fiscalYearEnd: "1231"`, ticker `KMDA`, 23 joinable periods |

---

## 4. Migration v34 (HLD D1–D3)

Appended to `versionedMigrations` as four entries. Prompt 11's columns survive verbatim;
prompt 14's additions are inline — **no `ALTER`** (the table does not exist yet).

```go
{34, `CREATE TABLE IF NOT EXISTS analyst_period_consensus (
    id                INTEGER PRIMARY KEY AUTOINCREMENT,
    cik               TEXT NOT NULL,
    source            TEXT NOT NULL,              -- finnhub | fmp | nasdaq_api | alpha_vantage
    fiscal_year       TEXT NOT NULL,              -- "2025"
    fiscal_period     TEXT NOT NULL,              -- Q1..Q4 | FY
    period_end        TEXT NOT NULL,              -- YYYY-MM-DD  (join key, D3)
    duration          TEXT NOT NULL,              -- P3M | P1Y   (join key, D3)
    consensus_date    TEXT NOT NULL,              -- as-of date of the estimate (D4)
    announcement_date TEXT,
    revenue_estimate  REAL,
    eps_estimate      REAL,
    eps_actual        REAL,
    eps_surprise      REAL,
    eps_surprise_pct  REAL,
    basis             TEXT,                       -- street | reported | unknown
    currency          TEXT NOT NULL DEFAULT 'USD',
    rating_buy_count  INTEGER NOT NULL DEFAULT 0,
    rating_hold_count INTEGER NOT NULL DEFAULT 0,
    rating_sell_count INTEGER NOT NULL DEFAULT 0,
    source_url        TEXT NOT NULL DEFAULT '',
    fetched_at        DATETIME NOT NULL,
    created_at        DATETIME NOT NULL,
    UNIQUE(cik, source, fiscal_year, fiscal_period, duration, consensus_date)
)`},
{35, `CREATE INDEX IF NOT EXISTS analyst_period_consensus_join_idx
      ON analyst_period_consensus(cik, period_end, duration, source)`},
{36, `CREATE TABLE IF NOT EXISTS analyst_coverage (
    cik              TEXT PRIMARY KEY,
    status           TEXT NOT NULL,               -- idle|fetching|ok|partial|no_data|error
    provider         TEXT NOT NULL DEFAULT '',    -- vendor owning this CIK's primary series
    last_attempt_at  TEXT,
    last_success_at  TEXT,
    next_retry_after TEXT,
    note             TEXT NOT NULL DEFAULT ''
)`},
{37, `CREATE TABLE IF NOT EXISTS analyst_snapshot (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    cik          TEXT NOT NULL,
    source       TEXT NOT NULL,
    as_of        TEXT NOT NULL,                   -- YYYY-MM-DD
    pt_mean REAL, pt_high REAL, pt_low REAL, pt_median REAL,
    analyst_count INTEGER,
    rating_buy_count INTEGER NOT NULL DEFAULT 0,
    rating_hold_count INTEGER NOT NULL DEFAULT 0,
    rating_sell_count INTEGER NOT NULL DEFAULT 0,
    currency     TEXT NOT NULL DEFAULT 'USD',
    fetched_at   DATETIME NOT NULL,
    UNIQUE(cik, source, as_of)
)`},
```

**`company_analysts` / `analyst_reports` are NOT created here.** Prompt 11 owns them (its
D1 firm-first shape is unresolved and this prompt does not need them until the P2 roster
seed). If prompt 11 ships first, these numbers shift — the collector must be written against
`versionedMigrations`' actual tail, not hardcoded to 34.

**Snapshot is its own table**, not a blob on the period row: it has no fiscal period, and
`UNIQUE(cik, source, as_of)` is what makes weekly snapshots accumulate into the revision
history D4 calls perishable.

---

## 5. `internal/models/consensus.go`

```go
type ConsensusPeriod struct {
    CIK, Ticker, Source, SourceURL string
    FiscalYear, FiscalPeriod       string   // "2025", "Q2"|"FY"
    PeriodEnd, Duration            string   // "2025-06-30", "P3M"|"P1Y"
    ConsensusDate                  string   // vendor as-of; never today's date
    AnnouncementDate               string
    EPSEstimate, EPSActual         *float64
    EPSSurprise, EPSSurprisePct    *float64
    RevenueEstimate                *float64
    Basis, Currency                string
    FetchedAt                      time.Time
}

type ConsensusSnapshot struct {
    CIK, Source, Currency  string
    AsOf                   string
    PTMean, PTHigh, PTLow, PTMedian *float64
    AnalystCount           *int
    Buy, Hold, Sell        int
    FetchedAt              time.Time
}
```

Pointers for every measured value: a vendor omitting EPS must produce `nil`, never `0.0`.
`0` is a legitimate EPS and the distinction survives into the DB as `NULL`.

---

## 6. `internal/consensus/`

### 6.1 `provider.go`

```go
const (
    SourceFinnhub      = "finnhub"
    SourceFMP          = "fmp"
    SourceNasdaq       = "nasdaq_api"
    SourceAlphaVantage = "alpha_vantage"
)

var (
    ErrNoAPIKey   = errors.New("consensus: vendor API key not configured")
    ErrRateLimit  = errors.New("consensus: vendor rate-limited the request")
    ErrNoData     = errors.New("consensus: vendor has no data for this symbol")
)

type Provider interface {
    Slug() string
    FetchEarnings(ctx context.Context, ticker string, from, to time.Time) ([]models.ConsensusPeriod, error)
    FetchSnapshot(ctx context.Context, ticker string) (*models.ConsensusSnapshot, error)
}
```

Constructors take the key explicitly and return `ErrNoAPIKey` on empty **before** any
request — same fail-fast shape as `financials.Client` (prompt 9 D8). HLD D10: the caller
maps `ErrNoAPIKey` to `status = error` naming the env var, never `no_data`.

### 6.2 `finnhub.go` — the P0 provider

`GET /api/v1/stock/earnings?symbol={t}&token={k}` returns objects with
`actual`, `estimate`, `surprise`, `surprisePercent`, `period`, `year`, `quarter`
(confirmed from the vendor's official Go client `EarningResult`).

Normalization, in order:

1. `quarter` 1–4 → `fiscal_period` `Q1..Q4`, `duration = P3M`. `quarter == 0` (annual, when
   present) → `FY`, `P1Y`.
2. **`period_end` is derived, never taken from `period`** — see §6.4. The vendor's `period`
   field is a report-ish date, not a fiscal period end, and using it would break the join.
3. `estimate`/`actual`/`surprise` → pointers; missing keys stay `nil`.
4. `consensus_date`: Finnhub does not return an estimate as-of date on this endpoint. Set it
   to the **announcement date** when known, else `period_end`, and record
   `basis = "unknown"` until §7's check runs. **Never today's date** — that would make every
   historical row look freshly-estimated and defeat D4.
5. `Currency = "USD"`; `Source = SourceFinnhub`.

Snapshot uses `/stock/price-target` and `/stock/recommendation`, merged into one
`ConsensusSnapshot` with `as_of = today`.

### 6.3 `fmp.go`, `nasdaq.go`, `gapfill.go` (P1)

- **FMP** — the only free revenue-estimate route. Hard cap **250 calls/day**: the client
  keeps a per-run counter, and on exhaustion returns `ErrRateLimit` so the CIK ends
  `partial` with a note, never a silent truncation.
- **Nasdaq** — `api.nasdaq.com/api/company/{sym}/earnings-surprise`, no key, requires a
  browser-like `User-Agent`. Returns **exactly 4 quarters** (measured), so it is a sanity
  check, not a backfill. Writes with `source = nasdaq_api` — a separate series by D2.
  `/revenue` returns `"Data not available"` (measured) and is not called.
- **gapfill** — allowlist, `consensus-cache/{sha256}.{ext}` + `.fetch.json` sidecar written
  **before** parsing (HLD D7). Deterministic parse first; LLM over cached bytes only on
  failure, emitting `confidence: low`. No cached document ⇒ no write.

### 6.4 `period.go` — the D3 derivation

```go
func DerivePeriodEnd(fiscalYearEnd, fiscalYear, fiscalPeriod string) (end, duration string, ok bool)
```

`fiscalYearEnd` is `submissions.json`'s `"MMDD"` (fixture: `"1231"`). For a December
year-end: `Q1→03-31`, `Q2→06-30`, `Q3→09-30`, `Q4→12-31` (`P3M`), `FY→12-31` (`P1Y`).
Non-December year-ends shift the quarter boundaries accordingly.

**Measured: 23/23 correct** against the fixture company's sidecars. `ok == false` when the
year-end is missing or unparseable — and per D3 the row is then **dropped with a gap
record**, never written with a null `period_end`.

---

## 7. Basis check (HLD D5)

```go
func ClassifyBasis(vendorActual float64, sidecarEPS *float64) (basis string, diverged bool)
```

Compare the vendor's `eps_actual` to the `eps_basic` line of the `financials.json` matched
on `(period_end, duration)`, restricted to `Publishable()` lines (prompts 8–9 already gate
those). Within 5% relative → `reported`; outside → `street`, and log at warn with both
values. No sidecar for the period → `unknown`.

This never rewrites the vendor number — surprise stays `vendor_actual − vendor_estimate`.
The measured precedent: for the fixture company, vendor actuals matched the extracted EPS on
3 of 4 comparable quarters; the 4th was the FY/Q4 period collision, which §6.4's `duration`
now prevents from ever being compared.

---

## 8. `internal/db/consensus.go`

```go
func (d *DB) UpsertConsensusPeriods(ctx context.Context, rows []models.ConsensusPeriod) error
func (d *DB) UpsertConsensusSnapshot(ctx context.Context, s models.ConsensusSnapshot) error
func (d *DB) ConsensusPeriods(ctx context.Context, cik, source string) ([]models.ConsensusPeriod, error)
func (d *DB) ConsensusBefore(ctx context.Context, cik, source, periodEnd, duration, before string) (*models.ConsensusPeriod, error)
func (d *DB) SetAnalystCoverage(ctx context.Context, cik, status, provider, note string) error
func (d *DB) AnalystCoverage(ctx context.Context, cik string) (*AnalystCoverageRow, error)
```

`ON CONFLICT(cik, source, fiscal_year, fiscal_period, duration, consensus_date) DO UPDATE SET`
over the value columns, mirroring `UpsertStockPrices`. Batched in one transaction per CIK.

**`ConsensusBefore` is the anti-lookahead primitive** and the only accessor the surprise
join may use: `... AND consensus_date < ? ORDER BY consensus_date DESC LIMIT 1`. Every read
is `source`-scoped (D2) — there is deliberately no "all sources" query.

---

## 9. `cmd/collect-consensus`

Flags per the idea file. Resolution order for the ticker: `--ticker` → `submissions.json`
`tickers[0]` → error (never guess).

Per CIK: resolve → window (intersect `--from`/`--to` with the on-disk filing span) → Tier 1
→ Tier 2 for still-null recent quarters → Tier 3 when `--gap-fill` → basis check → upsert →
coverage → sidecar.

**Coverage status:** `ok` (every requested metric present) · `partial` (some null, or FMP
cap hit) · `no_data` (vendor returned an empty series) · `error` (missing key, HTTP failure,
or unparseable). Exit `0` when all CIKs are `ok`/`no_data`, `1` on any `error`, `2` on usage.

**Idempotency:** skip when `last_success_at` is within `CONSENSUS_MIN_FETCH_INTERVAL`
(default 24h) unless `--force`.

**`--dry-run` (HLD D9):** writes `{slug}.consensus-plan.json` beside the `--similar` input
and nothing else — no DB row, no `consensus.json`. Reports per CIK: ticker, window, periods
already in DB, periods expected, planned calls per vendor per metric, and an explicit
**FMP-cap warning** when planned FMP calls exceed the remaining daily budget. With
`--similar`, only peers with `fetch: true` are planned (3 of 10 in the fixture).

---

## 10. Tests

No live vendor call in `make test`. Fixtures under `internal/consensus/testdata/`.

| Test | Asserts |
|---|---|
| `TestFinnhubNormalize` | fixture JSON → periods; `quarter` → `fiscal_period`+`duration`; missing estimate stays `nil`, not `0` |
| `TestDerivePeriodEnd` | table-driven incl. non-December year-ends; `ok == false` on a bad `fiscalYearEnd` |
| `TestDerivePeriodEndMatchesCorpus` | replays the fixture company's 23 sidecar periods, expects 23/23 (skipped when `fileDB` absent) |
| `TestFYQ4Distinct` | `2025-12-31/P1Y` and `2025-12-31/P3M` are two rows, not one |
| `TestUpsertIsIdempotent` | same batch twice → same row count, values updated not duplicated |
| `TestSourcesDoNotCollide` | finnhub + nasdaq rows for one period coexist; `ConsensusPeriods` returns only the requested source |
| `TestConsensusBeforeExcludesLookahead` | a row dated after `announcement_date` is never returned |
| `TestClassifyBasis` | within 5% → `reported`; outside → `street`; no sidecar → `unknown` |
| `TestNoKeyIsErrorNotNoData` | empty key → `ErrNoAPIKey`, coverage `error`, **and zero HTTP requests** (httptest counter) |
| `TestRowWithoutPeriodEndIsDropped` | underivable period → gap record, no row written |
| `TestDryRunWritesPlanOnly` | plan file exists; DB row count unchanged; `consensus.json` absent |
| `TestDryRunHonoursFetchFlag` | fixture → 3 planned peers, not 10 |

---

## 11. Build order

1. Migration v34 + `internal/db/consensus.go` + upsert/idempotency tests.
2. `models/consensus.go`, `provider.go`, **`period.go`**. **Gate: `TestDerivePeriodEndMatchesCorpus` 23/23.**
3. `finnhub.go` against a checked-in fixture. Gate: normalization + no-key tests.
4. `cmd/collect-consensus` — `--cik`, coverage, sidecar. Gate: `TestNoKeyIsErrorNotNoData`.
5. `--dry-run` + `--similar`. Gate: 3-of-10 planned.
6. Basis check + surprise join helper (§7).
7. P1: FMP, Nasdaq, `--snapshot`, `--gap-fill`.
8. P2: API handler, firm roster seed.

Steps 1–6 are P0 and need one free Finnhub key to smoke-test; everything before step 4 runs
offline against fixtures.

---

## 12. Done when

With `FINNHUB_API_KEY` set:

```bash
CGO_ENABLED=1 go run ./cmd/collect-consensus --cik 0001567529 --ticker KMDA --snapshot
```

writes ≥4 `analyst_period_consensus` rows with non-null `source`, `period_end`, `duration`,
`consensus_date`, `basis`; an `analyst_snapshot` row for today; `consensus.json`; and
`analyst_coverage.status = ok`.

The join that justifies the prompt: for **`2025-09-30` / `P3M`**, the surprise helper pairs
the vendor estimate with the `$0.09` basic EPS in
`fileDB/companies/0001567529/2025/0001213900-25-107836/financials.json` and records `basis`.

Three negative cases:

- **`2025-12-31`** resolves to the FY row (`P1Y`, EPS `$0.35`) and never to a Q4 estimate.
- With the key unset, exit is non-zero and coverage is `error` naming `FINNHUB_API_KEY` —
  not `no_data`.
- `--dry-run --similar ./fileDB/similar/kamada.json` plans **3** peers, writes
  `kamada.consensus-plan.json`, and leaves the DB untouched.

---

## 13. Risks

| Risk | Handling |
|---|---|
| Finnhub gives no estimate as-of date | §6.2 rule 4 — never stamp today; `basis: unknown` until §7 runs. Weakest point in the design; a vendor that supplies a real `consensus_date` should be preferred if one is licensed |
| FMP 250/day silently truncates a batch | Per-run counter → `ErrRateLimit` → `partial` + note; dry-run warns before spending |
| Migration numbers shift if prompt 11 ships first | Write against the actual tail of `versionedMigrations`; HLD open question 2 |
| Vendors revise history retroactively | `fetched_at` per row makes drift detectable; not preventable locally |
| Tier 3 HTML parse drift | Raw bytes cached, so a reparse never needs a refetch |
| Non-December fiscal year-ends unmeasured | Derivation is 23/23 on one December filer only; the table-driven test covers other year-ends by construction, not by observation |

---

## 14. Open question (1)

**Whether to store a second vendor for cross-checking.** D2 makes coexistence safe, and the
IBM measurement (two vendors disagreeing on 2 of 4 quarters) argues a second series is
genuine signal about consensus reliability. But it doubles quota use and no consumer reads
it yet. Recommend: allow it in the schema (done), don't collect it in P0.
