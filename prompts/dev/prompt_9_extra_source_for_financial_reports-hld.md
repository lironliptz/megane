# HLD: SEC Company Facts as a Second Source for Filing Financials

Design for `prompts/dev/prompt_9_extra_source_for_financial_reports.txt`.

Triage: **HEAVY** — new network dependency, new on-disk cache, a second write path into an
artifact another package already reads, and a change to what "no financials" means. The
audit below is **measured against the live SEC API and the real corpus** (2026-08-26) and
overturns the idea file's central premise about that API.

> **Governing rule, inherited from prompt 8 and unchanged:** a wrong number of the right
> order of magnitude is worse than no number. **Withholding beats guessing.** A second
> source widens coverage; it must not widen the blast radius.

---

## 1. Objective

Fill the coverage hole prompt 8 leaves: qualifying results filings whose accession folder
holds no discoverable XBRL instance, so `FindInstance()` returns nothing and no
`financials.json` is written — even though the facts were filed and accepted.

Add SEC's Company Facts API as a **gap-fill** source writing the same
`FilingFinancials` artifact, so the timeline modal shows real figures for every results
filing SEC actually holds facts for, and an explicit, diagnosable record for the rest.

---

## 2. Context — what prompt 8 shipped

| Piece | State |
|---|---|
| `internal/edgar/financials` | XBRL instance primary + rendered-HTML corroboration, six detectors, publication gate |
| `financials.json` | 15 written for Kamada; values verified against HLD §3.2 |
| `cmd/edgar-financials` | `--cik --all --dry-run --report --max-suspect-filings` |
| Read path | `filedb.Load` → `FilingRow.Financials` → `BuildHighlights` → modal |
| Known withheld | `0001213900-24-040628` — issuer mis-tagged its own XBRL; 7 metrics withheld |

This prompt adds a source. It changes **no** schema, **no** read-path code, and **no** UI.

---

## 3. Data audit (measured, not assumed)

Live fetch: `GET https://data.sec.gov/api/xbrl/companyfacts/CIK0001567529.json` →
HTTP 200, 1,112,965 bytes, 1.1 s. Namespaces `dei`, `ifrs-full` (368 concepts), `us-gaap`
(12), `srt`. Kamada tags in **IFRS**.

### 3.1 The gap is two populations, not one

| Slice | Count |
|---|---|
| Qualifying accessions (`quarterly_results` + `annual_report`) | 66 |
| With `financials.json` today | 15 |
| **Gap** | **51** |
| Gap accessions Company Facts covers by `accn` | **8** |
| Gap accessions Company Facts does **not** cover | **43** |

The 43 are not a fetching failure. **SEC has no XBRL facts for them at all** — they are
press-release 6-Ks filed without XBRL. Thirteen of them are 2020-or-later, and several are
filed on the *same day* as the 20-F whose numbers they announce (`24-020275` alongside the
FY2023 20-F; `25-020359` alongside FY2024; `26-025924` alongside FY2025). The figures exist
— in the annual report we already extract, not in the press release.

So the honest ceiling for this prompt is **15 → 23**, matching the idea file's "~23+"
estimate. The remaining 43 need an explicit *"no facts were filed"* record, which is the
idea file's own coverage-target option 2, not a fill.

### 3.2 `-xbrl.zip` predicts SEC coverage perfectly — so `--gaps` needs no network

| | SEC has facts | SEC has none |
|---|---|---|
| **Accession has `-xbrl.zip`** | 8 | 0 |
| **No `-xbrl.zip`** | 0 | 43 |

100% precision and recall across all 51 gaps. Gap classification is therefore a **local
filesystem question**, and `--gaps` can report exactly which accessions are fillable before
a single HTTP request. This is a stronger result than the idea file assumed and it shapes
D2.

### 3.3 Fact shape — period selection is mandatory, not optional

One accession's entries span several periods at once. `0001213900-24-068559` (Q2 2024)
returns five `Revenue` facts:

```
start 2023-01-01 end 2023-06-30  val 68,153,000     ← prior-year H1
start 2023-04-01 end 2023-06-30  val 37,443,000     ← prior-year quarter
start 2023-01-01 end 2023-12-31  val 142,519,000    ← prior full year
start 2024-01-01 end 2024-06-30  val 80,208,000     ← this year to date
start 2024-04-01 end 2024-06-30  val 42,472,000     ← the quarter  ✓
```

Taking the first entry yields prior-year H1 — a plausible number, wrong period. Entries
carry `start`/`end`, so selection is the same discipline prompt 8 already applies to
contexts: duration band plus `end == period end`. Balance-sheet concepts carry `end` only.

### 3.4 The values are right — verified against prompt 8's independent extraction

Six of the eight fillable accessions report a period that a locally-extracted filing one
year later reports as its prior-year comparative. Cross-checking those:

| Gap accession | Period | API revenue | Local filing's prior | |
|---|---|---|---|---|
| 0001178913-19-000698 | FY 2018 | 114,469,000 | 0001213900-20-004782 | **match** |
| 0001213900-21-060996 | Q3 2021 | 23,034,000 | 0001213900-22-074381 | **match** |
| 0001213900-22-048800 | Q2 2022 | 23,590,000 | 0001213900-23-067979 | **match** |
| 0001213900-23-042535 | Q1 2023 | 30,710,000 | 0001213900-24-040628 → 30,710 | *mismatch — see §3.5* |
| 0001213900-24-068559 | Q2 2024 | 42,472,000 | 0001213900-25-075264 | **match** |
| 0001213900-24-097126 | Q3 2024 | 41,740,000 | 0001213900-25-107836 | **match** |

Five exact matches against a completely independent extraction path. The sixth "mismatch"
is prompt 8's known mis-tagged filing, where **the API is right and the local value is
wrong**.

### 3.5 The idea file's central premise fails: SEC does **not** normalize scale

The idea file says the API means "per-filing instance parse required: **No** — SEC already
normalized facts across all filings." The normalization is *structural* (facts keyed by
accession and period), **not numeric**. Company Facts faithfully reproduces whatever the
filer tagged, mis-tagging included:

```
accn 0001213900-24-040628  end 2024-03-31  val 37,736       ← thousands, as mis-tagged
accn 0001213900-23-085500  end 2023-09-30  val 37,934       ← thousands, as mis-tagged
accn 0001213900-24-068559  end 2024-06-30  val 42,472,000   ← absolute, correct
```

Two of the 21 accessions carrying revenue are tagged in thousands. One of them,
**`0001213900-23-085500`, is inside the eight this prompt would fill** — so a naive gap-fill
publishes `$37.9K` as a quarter's revenue. There is no `decimals` field in Company Facts, so
prompt 8's detector B cannot be ported as written.

A second unit trap: `Revenue` carries units `USD` **and `ILS`**, and the ILS entry reports
FY2024 revenue as `10,000,000,000`. Publishing it would render "$10.0B".

### 3.6 Cross-accession redundancy resolves both, deterministically

Because issuers restate prior periods in every later filing, the same (concept, period) is
usually reported by several accessions. Measured across `Revenue`:

- **8 periods** are reported by more than one accession with differing raw values.
- **All 8 reconcile exactly after ×1000.** Every mis-tagged period has at least one
  correctly-tagged accession reporting the same period.
- After filtering to `unit == USD`, **irreconcilable periods: 0.**

This is the API-side analogue of prompt 8's two-source gate, and it needs no second network
call — the redundancy is already inside the single cached document.

---

## 4. Architecture decisions

### D1 — Company Facts gap-fills only; local always wins

Run only for qualifying accessions with no trusted local `financials.json`. Never overwrite
a `xbrl_instance`/`rendered_html` artifact in v1.

This is not merely conservative — the API is **not a superset**. Two of the 15 locally
extracted filings (`0001213900-26-055436`, `0001213900-26-088030`, both 2026) are **absent
from Company Facts**: the aggregate feed lags recent filings. Local extraction covers
filings the API does not, and vice versa. The two sources are genuinely complementary.

### D2 — Gap classification is offline; the network is only for filling

`--gaps` reports fillable vs unfillable from the filesystem alone, using the `-xbrl.zip`
predicate (§3.2, 51/51 correct). Only `--fill-gaps` fetches. A user can see the whole
coverage picture, and the cost of closing it, before any request leaves the machine.

### D3 — Unit filter before anything else

Accept `USD` for monetary concepts and `USD/shares` for per-share. Every other unit is
**discarded, never converted** — same rule as prompt 8. Without this the ILS fact publishes
a $10B revenue (§3.5).

### D4 — Period selection by declared span, never by position

Current = entry whose duration falls in the period's band (80–100 days for a quarter,
350–380 for a year) **and** whose `end` equals the filing's period end. Prior comparable =
same band, `end` within 350–380 days earlier. Balance concepts match on `end` as an instant,
and — per prompt 8 D7 — carry **no** year-over-year percentage.

Period end comes from the entry's own `fp`/`fy`/`end`, cross-checked against `meta.json`'s
filing date. Entries whose `form` disagrees with `meta.json` are skipped.

### D5 — Cross-accession agreement is the verification gate

Prompt 8 grades a metric by agreement between the instance and the rendered HTML. The API
path has no rendered HTML, so it substitutes SEC's own redundancy:

| Situation | Grade |
|---|---|
| ≥2 accessions report this (concept, period) in USD and agree within 0.5% | `verified` |
| ≥2 report it and they reconcile only after ×1000 | adopt the **plausible-magnitude** reading, `verified`, note the correction |
| Only one accession reports it, magnitude plausible | `single_source` |
| Only one accession reports it, magnitude implausible (§3.5 band) | `suspect` → **withhold** |
| Reconciliation is ambiguous (no majority) | `suspect` → **withhold** |

Note "majority" would not decide it: the real case is a 1-vs-1 split (`23-085500` says
37,934, `24-097126` says 37,934,000 for the same quarter). Scale-down is never a valid
reading, so the rule is to adopt the reading whose magnitude is consistent with the issuer's
other periods. The LLD refines this further: mis-tagging is a **per-accession** property —
all three monetary concepts in `23-085500` are off by exactly the same factor — so the
factor is detected once per filing and applied filing-wide rather than metric by metric.

Measured: this grades all 32 metrics across the eight fillable accessions as corroborated
without withholding a correct value, and it catches `23-085500` — the one that would
otherwise publish `$37.9K`.

### D6 — Magnitude guard survives the loss of `decimals`

Company Facts has no `decimals`, so detector B degrades to its magnitude half: a quarterly
or annual revenue-class figure below $1M is implausible for an issuer whose other periods
are two orders larger. Used **only** as the fallback when cross-accession corroboration is
unavailable (D5 row 4), never as the primary test — that is what D5 is for.

### D7 — "No facts filed" is a record, not silence

For the 43 accessions SEC has no facts for, write a `financials.json` carrying the period,
`source: "none"`, empty statements, and a `parseNotes` entry naming the reason ("no XBRL in
accession folder; SEC Company Facts has no entries for this accession"). `Publishable()`
returns nothing, so `BuildHighlights` falls through to the summary regex exactly as today
and the modal is unchanged.

The point is diagnosability: a maintainer asking "why does this results filing show no
figures?" gets an answer from the artifact instead of an absence. This directly implements
the idea file's coverage-target option 2, and it means **prompt 8's HLD line that
"366/381 accessions will never have a `financials.json`" should be revised** — for
qualifying categories, absence will mean "not yet examined", which is now a bug rather than
a fact.

### D8 — One cached document per CIK; SEC's fair-access rules are load-bearing

Cache at `fileDB/companies/{cik}/.sec/companyfacts.json` with a sidecar `fetchedAt`;
refresh after `SEC_COMPANYFACTS_TTL` (default 7 days) or on `--all`. One fetch serves every
accession of that company — never fetch per accession.

`User-Agent` is **mandatory** on `data.sec.gov`; SEC rejects requests without a real
contact. Add `SEC_EDGAR_USER_AGENT` to `.env.example` with no default that impersonates
anyone, and fail fast with a clear message when it is unset rather than sending a
placeholder. *(The audit above used a neutral non-personal UA. A deployment must set a real
contact address; do not reuse an end user's personal email for this.)*

### D9 — Company Facts can retire the LLM adjudicator for the mis-tagging class — recommended P1

Prompt 8 withholds `24-040628` and reaches for an LLM (its D6) to adjudicate. The API
resolves the same dispute **deterministically**: `5-042940` reports that exact period as
`37,736,000`, so cross-accession reconciliation (D5) fixes it with no model, no prompt, and
no network beyond the cached document.

The idea file scopes "override when local exists but fails verification" out of v1, and
that ordering is right for shipping — but the finding should be recorded now: a
deterministic corroborator beats an LLM adjudicator for this class, and the adjudicator's
already-small addressable population shrinks further once D5 exists. **Recommend a P1
follow-up** to let Company Facts adjudicate `suspect` local lines, ahead of enabling
`EDGAR_FINANCIALS_LLM`.

---

## 5. What this touches

| Area | New / Changed | Notes |
|---|---|---|
| `internal/edgar/financials/companyfacts.go` | **new** | HTTP client, cache, JSON parse |
| `internal/edgar/financials/companyfacts_map.go` | **new** | concept → canonical key, D4 selection, D5 grading |
| `internal/edgar/financials/schema.go` | changed | `SourceCompanyFacts`, `SourceNone` enum values |
| `internal/edgar/financials/elements.go` | changed | element aliases gain bare local names (`Revenue`), reusing existing rank order |
| `internal/edgar/financials/extract.go` | changed | `FillGaps` after `ExtractAll`; skip when a trusted artifact exists |
| `cmd/edgar-financials/main.go` | changed | `--gaps`, `--companyfacts`, `--fill-gaps`, `SEC_EDGAR_USER_AGENT` |
| `.env.example` | changed | `SEC_EDGAR_USER_AGENT`, `SEC_COMPANYFACTS_TTL` |
| `internal/filedb`, `internal/companyview`, `static/*` | **unchanged** | same artifact, same reader, same modal |

No DB migration. No route change. No schema change to `meta.json`.

**Backfill:** `--fill-gaps` over an existing corpus. Idempotent by the same rule as prompt 8
— an existing `financials.json` is never overwritten without `--all`, and local-sourced
artifacts are never overwritten at all in v1.

---

## 6. Out of scope

- Inline-XBRL parsing from `{accession}-xbrl.zip` (the idea file defers it; §3.2 shows the
  zip's *presence* is all we need for classification, and its *contents* would only
  duplicate what the API already returns).
- Overriding a local extract with API data (D9 — recommended next, not this).
- Segment breakdowns from Company Facts.
- Currency conversion; fetching at page load; multi-CIK cron.
- LLM as a primary number source.

---

## 7. Risks

| Risk | Severity | Mitigation |
|---|---|---|
| **Mis-tagged facts published 1000× wrong** — the API does not normalize scale (§3.5), and one of the eight fillable accessions is affected | **High** | D5 cross-accession reconciliation (measured: resolves 8/8, 0 irreconcilable), D6 magnitude fallback, withhold when ambiguous |
| Non-USD facts (ILS revenue of 10,000,000,000) | **High** | D3 unit filter applied before selection |
| Wrong period selected — one accession carries 3M, YTD, prior-year and full-year facts | **High** | D4 duration band + `end` match; never positional |
| Single-issuer audit again — one IFRS foreign private issuer; `us-gaap` mapping unmeasured | **High** | Inherited from prompt 8 and unresolved; concept aliases seeded, not validated |
| SEC blocks the client (missing/abusive `User-Agent`, rate limit) | Medium | D8: mandatory env var, fail fast, one fetch per CIK, disk cache, 7-day TTL |
| API lags recent filings (2 of 15 local filings absent) | Medium | D1 local-first; gap-fill is additive and absence is normal |
| Coverage still ends at 23/66 and reads as failure | Medium | D7 explicit no-facts records make the remaining 43 self-explaining |
| Cache goes stale and hides a restatement | Low | TTL + `--all`; `fetchedAt` recorded in the artifact's notes |

---

## 8. Done when

Open `/company/0001567529` → Timeline tab → click the **2024-08-14** marker (`0001213900-24-068559`,
Q2 2024 — a filing that shows "Financial highlights unavailable." today): the modal shows
**Revenue $42.5M (+13.4% YoY)**, **Basic EPS $0.08**, **Net income $4.4M**, **Cash $56.5M**,
from a `financials.json` whose lines carry `source: "sec_companyfacts"`.

And the two negative cases, which matter as much:

- The **2023-11-13** marker (`0001213900-23-085500`) — mis-tagged in SEC's own feed —
  shows revenue reconciled to **$37.9M**, never `$37.9K`; its notes record the ×1000
  correction and the corroborating accession. If reconciliation had been unavailable, it
  shows no revenue at all.
- The **2024-03-06** marker (`0001213900-24-020275`, a press release with no XBRL anywhere)
  still shows "Financial highlights unavailable.", and its `financials.json` says why:
  `source: "none"`, no facts filed.

`go run ./cmd/edgar-financials --root ./fileDB --cik 0001567529 --gaps` reports 8 fillable
and 43 unfillable **without network access**; after `--fill-gaps`, `--report` shows
**23** artifacts with publishable revenue.

---

## 9. Deliverables

1. `prompt_9_extra_source_for_financial_reports-lld.md` — client, cache layout, concept
   mapping, D4/D5 algorithms with thresholds, fixture plan.
2. `companyfacts.go` + `companyfacts_map.go` with table tests.
3. Trimmed `testdata/companyfacts_CIK0001567529.json` covering `24-068559` (clean),
   `23-085500` (mis-tagged, reconcilable), and the ILS entry. **No live network in CI** —
   `httptest` for the client, fixture JSON for the mapper.
4. `--gaps` / `--companyfacts` / `--fill-gaps` on `cmd/edgar-financials`.
5. `.env.example` entries; fail-fast when `SEC_EDGAR_USER_AGENT` is unset.
6. Revision of prompt 8 HLD's "366/381 will never have `financials.json`" (D7).

### Build order

1. `companyfacts.go` — fetch, `User-Agent`, cache, TTL. `httptest` tests only.
2. Concept mapping + D3 unit filter + D4 period selection; fixture tests.
3. **D5 cross-accession grading. Gate: `23-085500` reconciles to $37.9M or is withheld —
   never published as $37.9K.**
4. `FillGaps` + `--gaps` (offline classification first, asserted 8/43 against the corpus).
5. Wire `--fill-gaps`; run live; verify the six chain cross-checks in §3.4 still hold.
6. D7 no-facts records for the 43.
7. Manual smoke: all three §8 markers.
8. Only then consider D9 (Company Facts adjudicating prompt 8's suspect lines).
