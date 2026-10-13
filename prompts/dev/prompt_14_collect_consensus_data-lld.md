# LLD: Consensus & Analyst-Estimate Ingestion (`cmd/collect-consensus`)

Implements `prompt_14_collect_consensus_data-hld.md` (the contract) and the idea file
`prompt_14_collect_consensus_data.txt`.

Triage: **HEAVY** — migration v34 (first since 33), new `internal/consensus/` package, new
`internal/db/consensus.go`, new CLI, a fetch-cache tree, and an external vendor dependency
with hard daily quotas.

> **Update 2026-09-07:** P0 from this LLD has shipped (migrations 34–37; `internal/consensus/`
> with Finnhub live, FMP/Nasdaq/gapfill stubbed; `cmd/collect-consensus` with `--dry-run`,
> `--similar`, `--snapshot`). The HLD gained a new requirement since: raw rows must be
> retained before aggregation (D1's `analyst_raw_estimates`), and quarter selection must
> prioritize pre-report proximity (D5.1). §3a/§4a/§6.5/§8a/§9a below are the delta; everything
> else in this file describes what already exists and is unchanged.

> **Governing rule:** a number without `source`, `consensus_date`, and `basis` is worse than
> a null — it poisons every surprise computed from it. Provenance columns are `NOT NULL`
> wherever the schema can enforce it.

---

## 1. Scope

**In (P0):** migration v34; `internal/models/consensus.go`; `internal/db/consensus.go`;
`internal/consensus/` with the `Provider` interface and Finnhub; `cmd/collect-consensus`
with `--cik`, `--dry-run`, coverage rows. **(shipped)** Plus, new: migration v38
`analyst_raw_estimates` + backfill (§4a), `reduce.go`'s pre-report selection (§6.5), and the
raw-first retrofit of the write path (§9a).
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

## 3a. Current state, corrected (verified 2026-09-07)

§3's table describes the pre-implementation state. As shipped: migration head is **37**
(v34 `analyst_period_consensus`, v35 join index, v36 `analyst_coverage`, v37
`analyst_snapshot` — same DDL as §4 below, unchanged). `internal/consensus/` exists
(`provider.go`, `finnhub.go` live, `fmp.go`/`nasdaq.go`/`gapfill.go` stubs returning
`ErrNoData`, `period.go`, `basis.go`). `internal/db/consensus.go` has the six functions in
§8. `cmd/collect-consensus/main.go` implements `--cik`/`--similar`/`--all-in-corpus`,
`--dry-run`, `--snapshot`, `--force`. **No `analyst_raw_estimates` table exists** — this is
net-new (§4a).

**The gap this closes:** today, `UpsertConsensusPeriods` upserts
`ON CONFLICT(cik, source, fiscal_year, fiscal_period, duration, consensus_date) DO UPDATE`,
and `finnhub.go` sets `consensus_date = period_end` when the vendor gives no as-of date
(it never does, on this endpoint). Every re-fetch of the same quarter therefore collides on
the same key and **overwrites in place** — there is no history, and "reproducible from
stored raw rows" (the new requirement) is false today. §4a/§6.5/§9a fix this without
changing `analyst_period_consensus`'s schema or its existing accessors.

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

## 4a. Migration v38 — `analyst_raw_estimates` (HLD D1 addendum)

One atomic scalar observation per row — one metric, one as-of date, one value — so a
quarter can accumulate many rows across many fetch runs instead of one row that gets
overwritten each time.

```go
{38, `CREATE TABLE IF NOT EXISTS analyst_raw_estimates (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    cik           TEXT NOT NULL,
    source        TEXT NOT NULL,               -- finnhub | fmp | nasdaq_api | ...
    firm          TEXT NOT NULL DEFAULT '',    -- '' = vendor aggregate; populated by prompt 11 later
    metric        TEXT NOT NULL,               -- eps_estimate | eps_actual_vendor | revenue_estimate |
                                                -- pt_mean | pt_high | pt_low | pt_median |
                                                -- rating_buy | rating_hold | rating_sell
    fiscal_year   TEXT NOT NULL DEFAULT '',
    fiscal_period TEXT NOT NULL DEFAULT '',    -- Q1..Q4 | FY | ''
    period_end    TEXT NOT NULL DEFAULT '',
    duration      TEXT NOT NULL DEFAULT '',
    value_num     REAL,
    value_text    TEXT NOT NULL DEFAULT '',
    currency      TEXT NOT NULL DEFAULT 'USD',
    as_of_date    TEXT NOT NULL DEFAULT '',    -- vendor as-of when known, else this fetch's date (never blank)
    source_url    TEXT NOT NULL DEFAULT '',
    fetched_at    DATETIME NOT NULL,
    created_at    DATETIME NOT NULL,
    UNIQUE(cik, source, firm, metric, fiscal_year, fiscal_period, as_of_date, value_num, value_text)
)`},
{39, `CREATE INDEX IF NOT EXISTS analyst_raw_estimates_period_idx
      ON analyst_raw_estimates(cik, source, period_end, duration, metric)`},
```

**Why value is in the unique key:** an unchanged re-fetch (same as-of, same number) is a
true no-op. A vendor that revises a historical estimate under the *same* as-of date produces
a second row rather than colliding — both observations survive; §6.5's reducer tie-breaks
on `fetched_at` when it finds more than one row for the same `as_of_date`.

**Backfill:** existing `analyst_period_consensus` rows (written before this migration)
have no raw provenance. One-time backfill inserts one `eps_estimate` (+`eps_actual_vendor`
if non-null) raw row per existing period row, `as_of_date = consensus_date`,
`source_url = 'backfill:pre-raw-era'` so it's visibly distinguishable from a real fetch.
Run as a Go function called once from the migration (same transaction, same pattern as
other data-carrying migrations in this file), not a separate script.

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

**New, for §4a:**

```go
type RawEstimate struct {
    CIK, Source, Firm                       string
    Metric                                  string // see §4a's metric vocabulary
    FiscalYear, FiscalPeriod                string
    PeriodEnd, Duration                     string
    ValueNum                                *float64
    ValueText, Currency                     string
    AsOfDate, SourceURL                     string
    FetchedAt                               time.Time
}
```

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

### 6.5 `reduce.go` — pre-report proximity selection (HLD D5.1, new)

```go
const PreReportWindow = 30 * 24 * time.Hour // announcement_date-30d .. announcement_date-1d

// SelectQuarterRaw picks the one raw row that becomes the quarter's consensus_date/value,
// per metric, from every accumulated raw row for one (cik, source, period_end, duration).
// Pure function — no DB access — so it is unit-testable without fixtures.
func SelectQuarterRaw(raws []models.RawEstimate, announcementDate string) (chosen *models.RawEstimate, note string)
```

Rule, exactly D5.1:
1. Rows with `as_of_date` in `[announcement_date-30d, announcement_date-1d]` → nearest to
   `announcement_date` wins.
2. None in-window → latest row with `as_of_date` strictly before `announcement_date`.
3. Still none → `chosen == nil`, `note = "no pre-report row"` (gap, per D3's "never write a
   guess" precedent — the caller records the gap and does not touch
   `analyst_period_consensus` for that quarter).
4. **Never** a row with `as_of_date >= announcement_date` (D4's anti-lookahead rule, restated
   for the raw layer). Post-report rows are ignored even if closer in absolute distance.
5. Tie (two rows, same `as_of_date`, different `value_num`) → higher `fetched_at` wins —
   the later fetch is presumed the vendor's corrected value.

`announcement_date` comes from the matching `financials.json`'s filing date for that
`(period_end, duration)` — the same sidecar lookup `cmd/collect-consensus` already does for
the basis check (§7), extended to also return `filingDate`.

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

**New, for §4a — same file:**

```go
func (d *DB) UpsertRawEstimates(ctx context.Context, rows []models.RawEstimate) error
func (d *DB) RawEstimatesFor(ctx context.Context, cik, source, periodEnd, duration string) ([]models.RawEstimate, error)
```

Plain insert with `ON CONFLICT(...) DO NOTHING` (not `DO UPDATE`) — an identical row really
is a no-op; a changed value is, by the unique key in §4a, a different row, not an update.

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

### 9a. Retrofit — raw-first write path (HLD D1/D5.1, new)

`provider.FetchEarnings` keeps returning `[]models.ConsensusPeriod` (no interface change).
What happens after that response comes back changes:

1. Map each returned row to 1–2 `RawEstimate` rows (`eps_estimate`, and `eps_actual_vendor`
   when the vendor also returned an actual), `as_of_date` = this fetch's date (Finnhub gives
   none). `UpsertRawEstimates` — this never overwrites an earlier fetch's observation.
2. For each distinct `(period_end, duration)` touched, `RawEstimatesFor` pulls **every**
   accumulated raw row (this run's plus all prior runs'), looks up `announcement_date` from
   the matching `financials.json` filing date, and calls `SelectQuarterRaw` (§6.5).
3. `chosen == nil` → gap note, no `analyst_period_consensus` write for that quarter (same
   "never null period_end" spirit as D3, applied to a missing pre-report row instead).
4. `chosen != nil` → build one `models.ConsensusPeriod` (`consensus_date = chosen.AsOfDate`,
   `eps_estimate = chosen.ValueNum`) and call the existing `UpsertConsensusPeriods` —
   unchanged function, unchanged table.

Snapshot metrics (PT/rating) get the same raw-logging in step 1 for audit completeness, but
skip steps 2–4: `analyst_snapshot`'s `UNIQUE(cik, source, as_of)` already keeps one row per
fetch day, so there is no ambiguity for a reducer to resolve.

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
| `TestSelectQuarterRawPreReportWindow` | a row 5 days before `announcement_date` beats one 40 days before |
| `TestSelectQuarterRawNeverPostReport` | a row 1 day *after* `announcement_date` is never chosen even if it is the only row |
| `TestSelectQuarterRawFallsBackBeforeWindow` | no row inside the 30d window → latest row still strictly before `announcement_date` wins |
| `TestSelectQuarterRawNoEligibleRow` | every row is post-report → `chosen == nil`, gap note |
| `TestSelectQuarterRawTieBreak` | two rows, same `as_of_date`, different value → higher `fetched_at` wins |
| `TestRawEstimatesRefetchIsNoOp` | same (source, metric, as_of, value) upserted twice → one row |
| `TestRawEstimatesRevisionAddsRow` | same as_of_date, different value → two rows, both retained |
| `TestConsensusReflectsRawHistory` | two fetch runs (different `as_of_date`s) for one quarter → `analyst_period_consensus` ends on the run §6.5 picks, not the latest run unconditionally |
| `TestBackfillPreservesExistingRows` | migration v38 backfill: every pre-existing `analyst_period_consensus` row has ≥1 raw row tagged `backfill:pre-raw-era` afterward |

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
9. **(new)** Migration v38 (§4a) + backfill. Gate: `TestBackfillPreservesExistingRows`.
10. **(new)** `RawEstimate` model + `UpsertRawEstimates`/`RawEstimatesFor` (§8a).
11. **(new)** `reduce.go` (§6.5) against fixtures only — no DB. Gate: the five
    `TestSelectQuarterRaw*` tests, especially `TestSelectQuarterRawNeverPostReport`.
12. **(new)** Retrofit §9a into `cmd/collect-consensus`. Gate: `TestConsensusReflectsRawHistory`
    — run the CLI twice against two different fixture responses for the same quarter and
    confirm the final row matches §6.5's pick, not "whichever ran last."

Steps 1–6 are P0 and need one free Finnhub key to smoke-test; everything before step 4 runs
offline against fixtures. Steps 9–11 (new) are pure/offline; step 12 needs a live key only
to confirm end-to-end, same as step 3.

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

**Two more, from D1/D5.1 (new):** the same run above also writes to `analyst_raw_estimates`
**before** touching `analyst_period_consensus`, and re-running it a second time with a
different fixture response for `2025-09-30`/`P3M` (simulating a revised Finnhub estimate)
never lets a post-report-dated raw row win — the consensus row only changes if the new raw
row is itself within the pre-report window and nearer to `announcement_date` than what was
already chosen.

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
| Finnhub's endpoint has no real as-of date, so every raw row's `as_of_date` is *this fetch's* date, not the vendor's | §6.5 still orders correctly across repeated fetches (each run's date is real), but a single run can never place its own row inside a *past* pre-report window — multi-run history has to accumulate over real time before D5.1 has more than one candidate to choose from |
| Backfilled raw rows (`backfill:pre-raw-era`) carry `as_of_date = consensus_date`, which was itself `period_end` under the old scheme, not a genuine estimate date | Flagged via `source_url`, never silently indistinguishable from a real observation; §6.5 still applies D4's anti-lookahead check against it like any other row |

---

## 14. Open question (1)

**Whether to store a second vendor for cross-checking.** D2 makes coexistence safe, and the
IBM measurement (two vendors disagreeing on 2 of 4 quarters) argues a second series is
genuine signal about consensus reliability. But it doubles quota use and no consumer reads
it yet. Recommend: allow it in the schema (done), don't collect it in P0.
