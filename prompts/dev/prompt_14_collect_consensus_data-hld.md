# HLD: Consensus & Analyst-Estimate Ingestion (`cmd/collect-consensus`)

Design for `prompts/dev/prompt_14_collect_consensus_data.txt`.

Triage: **HEAVY** — first DB migration since v33, new Go package, new external vendor
dependency with API quotas, a fetch-cache tree, and the data source that prompt 12's #1
model input depends on. Greenfield: no consensus collector exists.

> **Governing rule, inherited from prompts 8–9:** withholding beats guessing. Here it binds
> hardest on *provenance*: a number without a recorded source, as-of date, and basis is
> worse than a null, because it silently poisons every surprise computed downstream.

---

## 1. Objective

Collect earnings estimates, rating aggregates, and price-target consensus for a company
over the same span as its on-disk filing corpus; persist them point-in-time; and make
earnings surprise computable against the actuals prompts 8–9 already extract.

`model_inputs_ranked.md` §4 ranks earnings surprise #1 and shows the denominator is missing.
This prompt ships the collector that supplies it.

---

## 2. Context — what exists today (verified 2026-09-06)

| Layer | State |
|---|---|
| SEC actuals | `financials.json` with `period.endDate`, `period.duration` (`P3M`/`P1Y`), `period.focus` — 66/66 qualifying accessions for the fixture company |
| Stock prices | `stock_prices` + `stock_price_coverage` (migration 33) |
| Consensus | **none** — no table, no package, no key |
| Migration head | **33** — `stock_price_coverage` |
| `internal/consensus/` | does not exist |
| Analyst tables in code | **none** — `grep 'analyst_' internal/db/*.go` returns nothing |
| API keys | `FINNHUB_API_KEY`, `FMP_API_KEY`, `ALPHA_VANTAGE_API_KEY` — **all empty in `.env`** |
| Peer fixture | `fileDB/similar/kamada.json` — 10 peers, all carry `fetch`, **3 set `true`** |

---

## 3. The blocking finding: the schema dependency is inverted

The idea file instructs: *"reuse prompt 11 DDL — do not fork schema"*, then proposes eight
`ALTER TABLE analyst_period_consensus ADD COLUMN` statements, and finally *"ship prompt 14
P0–P1 before prompt 11 P1 tab"*.

Measured: **prompt 11 is design-only.** It has an idea file and an HLD, no LLD, no code. Its
migration v34 has never been written — the head is 33. So:

- The `ALTER TABLE` statements target a table that **does not exist**. They cannot run.
- Prompt 14 is told to ship first while depending on prompt 11 having shipped.

**Resolution (D1):** prompt 14 **owns** migration v34 and creates the tables with their
full column set inline — no `ALTER`s. Prompt 11 becomes the schema *consumer* for its
`#analysts` UI, which matches the shipping order the idea file already asks for. Prompt 11's
DDL is honoured as the starting shape, not forked — every column it names survives verbatim.

### 3.1 Two defects in the inherited DDL, both load-bearing

Prompt 11's `analyst_period_consensus` declares:

```sql
UNIQUE(cik, fiscal_year, fiscal_period, consensus_date)
```

**Defect A — `source` is not in the uniqueness key.** The idea file adds a `source` column
and states as rule #1 that vendor series must never be mixed. But with this key, a Finnhub
row and an Alpha Vantage row for the same period and as-of date **collide** — one silently
overwrites the other, producing exactly the mixed series the rule forbids. Measured
consequence: on IBM the two vendors disagree on 2 of 4 quarters (Dec-2025: 4.29 vs 4.33),
so a collision is not hypothetical, it changes the number. → D2.

**Defect B — the join key is not the primary key.** The whole prompt exists to join
consensus to `financials.json` on `(period_end, duration)`, yet those are `ALTER`-added,
nullable, and absent from the uniqueness constraint, while the natural key is
`(fiscal_year, fiscal_period)`. A row can be written with a null `period_end` and be
unjoinable. → D3.

Both are cheap to fix now and expensive after rows exist.

---

## 4. Data audit — measured, not assumed

Every source below was tested live during `model_inputs_ranked.md` §4 (2026-08-31).

| Source | Verified result |
|---|---|
| **Alpha Vantage** `EARNINGS` | **Tested, HTTP 200** — 122 quarters for IBM with `reportedEPS`, `estimatedEPS`, `surprise` |
| **Nasdaq** `earnings-surprise` | **Tested, HTTP 200** — EPS actual + consensus, **exactly 4 quarters**, no key |
| **Nasdaq** `revenue` | **Tested** — `"Data not available"`. Not a revenue source |
| **Finnhub** `/stock/earnings` | Fields confirmed from the official Go client's `EarningResult`: `actual`, `estimate`, `surprise`, `surprisePercent`, `period`, `year`, `quarter` — **EPS only, no revenue** |
| **FMP** `analyst-estimates` | Documented: revenue + EPS, historical + forward. **250 calls/day** free |
| **Yahoo** `quoteSummary` | **Tested — HTTP 429** from both hosts. Not usable without cookie/crumb |

### 4.1 Consequences the ladder must absorb

**Revenue consensus is the binding constraint.** Finnhub free is EPS-only; Nasdaq has none;
Yahoo is blocked. **FMP is the single free route to revenue estimates**, at 250 calls/day —
so a revenue backfill is quota-bound, not latency-bound. The `--metrics` flag exists mainly
to keep revenue off the default path.

**Consensus is vendor-defined, not measured** (`model_inputs_ranked.md` §4). Vendors differ
on contributor panel, staleness rule, basis normalization (GAAP vs street), mean-vs-median,
and they **revise history** retroactively. This is why D2's per-source key and the
`basis` flag are not bookkeeping niceties.

**The basis check has a measured false-positive mode.** Comparing Nasdaq actuals to our
extracted EPS for the fixture company: 3 of 4 quarters matched exactly; the 4th "mismatch"
was the 20-F's **full-year** EPS (0.35) compared against a **Q4** consensus (0.09) — a
manufactured 289% surprise caused purely by joining on period end alone. This is the direct
evidence for D3.

---

## 5. Architecture decisions

### D1 — Prompt 14 owns migration v34; no `ALTER`s

One append-only migration after 33 creating `company_analysts`, `analyst_reports`,
`analyst_period_consensus`, `analyst_coverage` — prompt 11's four tables, with prompt 14's
additional columns folded into the `CREATE`. Prompt 11's LLD then consumes this schema
rather than redefining it. If prompt 11 ships its own v34 first, prompt 14 becomes v35 with
the extra columns as `ALTER`s; the LLD must state which branch it is implementing.

### D2 — `source` is part of the uniqueness key, and one vendor owns a series

```sql
UNIQUE(cik, source, fiscal_year, fiscal_period, duration, consensus_date)
```

Two vendors may coexist in the table — that is useful for cross-checking — but never in one
exported series. Reads are `WHERE source = ?`, and `analyst_coverage.provider` records which
vendor owns the company's primary series. Mixing is prevented by the query layer, not by
hoping the writer behaves.

### D3 — The join key is `(period_end, duration)`, and both are `NOT NULL`

A consensus row that cannot be joined to an actual is useless for this prompt's purpose, so
the schema forbids it. When a vendor supplies only `fiscal_year`/`fiscal_period`, the
collector derives `period_end` from `submissions.json`'s `fiscalYearEnd` plus the period
focus, and **refuses to write the row** if it cannot — recording a gap instead. `duration`
is `P3M` for Q1–Q4 and `P1Y` for FY, matching `financials.json` exactly.

### D4 — Point-in-time or it does not ship

Every row carries `consensus_date` (the vendor's as-of), `source`, `source_url`,
`fetched_at`, and `basis`. Surprise selection picks the consensus row **immediately
preceding** `announcement_date` — never the latest row. Historical EPS surprise is
retrofittable in one call per symbol; forward estimates are perishable and only a recurring
`--snapshot` preserves them. The LLD must ship `--snapshot` in P1, not defer it.

### D5 — Surprise is computed within one vendor; SEC actuals are a basis *check*

`surprise = vendor_actual − vendor_estimate`. The SEC-extracted actual is compared to the
vendor actual only to classify `basis` (`reported` when they agree within 5%, `street`
otherwise) and to log divergence. A `SEC_actual − vendor_estimate` figure is never written
without an explicit `basis` flag and downstream opt-in, because street EPS excludes items
GAAP includes and the difference is not a surprise.

### D6 — The source ladder is strictly ordered and never merges

Tier 1 vendor APIs → Tier 2 keyless public endpoints → Tier 3 allowlisted fetch, and a
lower tier only fills fields the higher tier left null. Tier 2 rows carry
`source: "nasdaq_api"` and form their own series (D2). **Nasdaq's 4-quarter depth makes it a
sanity check, not a backfill** — the idea file's own framing, confirmed by measurement.

### D7 — Tier 3 caches raw bytes before parsing, or does not write

Allowlist only (`nasdaq.com`, `api.nasdaq.com`, `sec.gov`, `data.sec.gov`,
`finance.yahoo.com`, issuer IR domains from `submissions.json`). Every fetch is written to
`consensus-cache/{sha256}.{ext}` with a `fetch.json` sidecar recording url, timestamp,
status, content-type — **before** parsing. Deterministic parsers first; an LLM runs only
over already-cached bytes and only when rules fail, emitting `confidence: low`. A number
without a cached source document is never written. Tier 3 is off unless `--gap-fill`.

### D8 — No vendor call on the request path

The CLI refreshes; the P2 API reads SQLite only. Same rule that made the price path safe
(prompt 4) and the extraction path safe (prompts 8–9). `analyst_coverage` mirrors
`stock_price_coverage`: status, provider, attempt/success timestamps, retry floor, and a
`CONSENSUS_MIN_FETCH_INTERVAL` (default 24h) that `--force` overrides.

### D9 — `--dry-run` is a quota survey

Quotas here are hard and daily (FMP 250/day), so a dry run must answer *"what will this
spend?"* before it spends it: CIKs in scope, calls per vendor per metric, periods already in
DB versus expected, and the FMP-cap warning when the batch exceeds it. Writes
`{slug}.consensus-plan.json` beside the `--similar` input — never `consensus.json`, never a
SQLite row. Mirrors prompt 13's `.fetch-plan.json` convention.

### D10 — No key, no silent no-op

All three API keys are empty today. A run whose vendor key is unset must **fail that CIK
loudly** with `analyst_coverage.status = error` and a note naming the env var — never record
`no_data`, which would be indistinguishable from a company genuinely lacking coverage.

---

## 6. What this touches

| Area | Change |
|---|---|
| `internal/db/db.go` | **+1 migration (v34)** — four tables (D1) |
| `internal/db/consensus.go` | **new** — upsert/query, source-scoped reads |
| `internal/models/consensus.go` | **new** — `ConsensusPeriod`, `ConsensusSnapshot` DTOs |
| `internal/consensus/` | **new** — `provider.go`, `finnhub.go`, `fmp.go`, `nasdaq.go`, `gapfill.go` |
| `cmd/collect-consensus/` | **new** — CLI orchestrator |
| `fileDB/companies/{cik}/consensus.json`, `consensus-cache/` | **new artifacts** (gitignored) |
| `.env.example` | vendor keys, `CONSENSUS_PROVIDER`, `CONSENSUS_MIN_FETCH_INTERVAL`, `CONSENSUS_GAP_FILL` |
| `Makefile` | `collect-consensus` target |
| `internal/handlers/router.go` | P2 — one GET in the existing `/api/companies/:cik` group |

Nothing in prompts 1–13 changes. `financials.json` is read-only to this prompt.

---

## 7. Out of scope

Licensed feeds (interface hook only); ToS-prohibited broker scraping; individual analyst
person names (not on free tiers — prompt 11 D1); PDF ingestion; page-load vendor calls;
mixing vendor slugs in one exported series; prompt 12's model training.

---

## 8. Risks

| Risk | Severity | Mitigation |
|---|---|---|
| **Lookahead leakage** — today's consensus used for a 2023 filing | **High** | D4: `consensus_date` mandatory, select the row preceding `announcement_date`; never "latest" |
| **Vendor collision overwrites a series** (defect A) | **High** | D2: `source` in the uniqueness key |
| **Period-end-only join** manufactures a 289% surprise — measured | **High** | D3: `(period_end, duration)` both `NOT NULL` |
| **Basis mismatch** — GAAP actual vs street estimate | **High** | D5: surprise within one vendor; SEC actual is a check, `basis` recorded |
| Revenue consensus quota-bound (FMP 250/day) | Medium | D9 dry-run warns; `--metrics` keeps revenue off the default path |
| Vendors revise history retroactively | Medium | `fetched_at` per row makes drift detectable; cannot be prevented locally |
| No API key configured today | Medium | D10 fails loudly; P0 is unrunnable until a free key is added |
| Tier 3 parse drift on unversioned HTML | Low | D7 caches raw bytes, so a reparse never needs a refetch |

---

## 9. Done when

`go run ./cmd/collect-consensus --cik 0001567529 --ticker KMDA --snapshot` populates
`analyst_period_consensus` with ≥4 EPS rows carrying `source`, `consensus_date`,
`period_end`, `duration`, and `basis`; writes `consensus.json`; and sets
`analyst_coverage.status = ok`.

Then the join that justifies the prompt: for the **2025-09-30 `P3M`** period, the collector
reports a surprise against the `$0.09` basic EPS in
`fileDB/companies/0001567529/2025/0001213900-25-107836/financials.json` — matched on
`(period_end, duration)`, with `basis` recorded.

And three negative cases:

- A vendor row that cannot be resolved to a `period_end` is **not written**; it appears as a
  gap with a reason (D3).
- With `FINNHUB_API_KEY` unset, the run exits non-zero with `status = error` naming the
  variable — **not** `no_data` (D10).
- `--dry-run` on `kamada.json` lists only the **3 peers with `fetch: true`**, prints the
  per-vendor call plan, writes `kamada.consensus-plan.json`, and touches no DB row.

---

## 10. Deliverables & phasing

| Priority | Deliverable |
|---|---|
| **P0** | Migration v34 (D1–D3); `internal/models/consensus.go`; `internal/db/consensus.go` |
| **P0** | `internal/consensus/` + Finnhub provider (EPS, ratings, PT); `cmd/collect-consensus` with `--cik`, `--dry-run`, coverage row |
| **P1** | FMP revenue; Nasdaq Tier 2 sanity check; `--similar` batch; **`--snapshot`** (perishable — D4) |
| **P1** | Tier 3 `--gap-fill` with cache + allowlist (D7) |
| **P2** | Surprise join helper + basis-check log; `GET /api/companies/:cik/consensus` (cache-only); firm roster seed |

Ship P0–P1 before prompt 11's `#analysts` tab, per the idea file — which D1 makes possible
by moving schema ownership here.

### Open questions (2)

1. **Which vendor is primary.** Finnhub has the deepest free EPS history; Alpha Vantage is
   deeper still (122 quarters measured) but has a very low daily cap. The choice should
   follow whichever licence permits displaying consensus to authenticated users — a licence
   read, not an engineering decision. Default `finnhub` until settled.
2. **If prompt 11 ships v34 first**, prompt 14 becomes v35 with `ALTER`s and must still add
   `source` to the uniqueness key — which SQLite cannot do via `ALTER`, requiring a table
   rebuild. Strong argument for prompt 14 owning v34 (D1).

---

## References

- `prompts/dev/model_inputs_ranked.md` §4 — measured source table, vendor-disagreement
  evidence, and the surprise rules this HLD implements
- `prompt_11_company_analysts-hld.md` — D6 four-table v34; schema consumer after D1
- `prompt_9_extra_source_for_financial_reports-hld.md` — provider + cache + coverage-row
  pattern this mirrors
- `prompt_13_fetch_similar_companies-hld.md` — `.fetch-plan.json` dry-run convention (D9)
