# LLD: SEC Company Facts as a Second Source for Filing Financials

Implements `prompt_9_extra_source_for_financial_reports-hld.md` (the contract) and the idea
file `prompt_9_extra_source_for_financial_reports.txt`.

Triage: **HEAVY** — first network dependency in the extraction path, a new on-disk cache, a
second writer into an artifact `filedb` already reads, and a redefinition of what an absent
`financials.json` means. The HLD's §3 audit is measured and not repeated; this is the build.

> **Governing rule, inherited unchanged:** a wrong number of the right order of magnitude is
> worse than no number. **Withholding beats guessing.**

---

## 1. Scope

**In:** Company Facts client + disk cache; concept→canonical mapping; period selection;
per-accession scale reconciliation; gap classification (offline); `FillGaps`; no-facts
records; three new CLI flags; `.env.example`.

**Out:** inline-XBRL parsing from `-xbrl.zip`; overriding a local extract (HLD D9 — next
prompt); segments from Company Facts; currency conversion; page-load fetching; multi-CIK
cron; any change to `filedb`, `companyview`, `static/`, the DB, or routes.

---

## 2. Contract → implementation map

| HLD decision | Where it lives |
|---|---|
| D1 gap-fill only, local wins | `FillGaps` skips any accession with a local-sourced artifact |
| D2 offline gap classification | `ClassifyGaps` — filesystem only, no client constructed |
| D3 unit filter | `factIndex.add` rejects non-USD before indexing |
| D4 period selection | `selectPeriod` + `pickEntry` |
| D5 cross-accession reconciliation | `resolveScale` (per accession) + `gradeEntry` |
| D6 magnitude fallback | `plausibleMagnitude`, used only when uncorroborated |
| D7 no-facts records | `writeNoFacts` |
| D8 cache + User-Agent | `companyfacts.Client` |

---

## 3. New files

```
internal/edgar/financials/
├── companyfacts.go         Client: fetch, User-Agent, disk cache, TTL
├── companyfacts_map.go     factIndex, concept mapping, period selection, scale, grading
├── fillgaps.go             ClassifyGaps, FillGaps, writeNoFacts
└── testdata/companyfacts/  trimmed fixture + golden outputs
```

Changed: `schema.go`, `elements.go`, `extract.go` (small), `cmd/edgar-financials/main.go`,
`.env.example`.

---

## 4. `companyfacts.go`

### 4.1 Types

```go
const (
    companyFactsURL  = "https://data.sec.gov/api/xbrl/companyfacts/CIK%s.json"
    companyFactsDir  = ".sec"
    companyFactsFile = "companyfacts.json"
    defaultCFTTL     = 7 * 24 * time.Hour
    cfTimeout        = 30 * time.Second   // measured: 1.1 MB in 1.1 s
)

var (
    ErrNoUserAgent = errors.New("financials: SEC_EDGAR_USER_AGENT is required")
    ErrRateLimited = errors.New("financials: SEC rate-limited the request")
    ErrNotFound    = errors.New("financials: no Company Facts for CIK")
)

type Client struct {
    BaseURL   string        // overridden by httptest in tests
    HTTP      *http.Client
    UserAgent string
    TTL       time.Duration
}

// CompanyFacts is the raw document.
type CompanyFacts struct {
    CIK        int    `json:"cik"`
    EntityName string `json:"entityName"`
    Facts      map[string]map[string]struct {
        Units map[string][]FactEntry `json:"units"`
    } `json:"facts"`                          // namespace -> concept -> units -> entries
}

type FactEntry struct {
    Start string  `json:"start,omitempty"`   // absent for instants
    End   string  `json:"end"`
    Val   float64 `json:"val"`
    Accn  string  `json:"accn"`
    FY    int     `json:"fy"`
    FP    string  `json:"fp"`
    Form  string  `json:"form"`
    Filed string  `json:"filed"`
}
```

Mirrors `internal/marketdata/yahoo.go`: injectable `*http.Client`, explicit `UserAgent`
field, sentinel errors, 429 handled before reading the body.

### 4.2 Fetch and cache

```go
func (c *Client) Facts(ctx context.Context, root, cik string) (*CompanyFacts, error)
```

1. `cachePath = {root}/companies/{cik}/.sec/companyfacts.json`. If present and
   `mtime` within `TTL`, decode and return — **no HTTP**.
2. Otherwise require `UserAgent`; empty → `ErrNoUserAgent`, **no request attempted**.
   SEC rejects anonymous clients, and shipping a fabricated contact would misrepresent the
   caller, so this fails fast with a message naming the env var.
3. `GET` with `User-Agent` and `Accept-Encoding: gzip`. 429 → `ErrRateLimited`;
   404 → `ErrNotFound`; non-200 → error.
4. Write the body to `cachePath` atomically (temp file + rename), then decode.

`{root}` is inside the gitignored `fileDB/`, so the cache is never committed. One fetch
serves every accession of a company — the loop in `FillGaps` calls `Facts` once.

CIK is zero-padded to 10 digits (`0001567529`).

---

## 5. `companyfacts_map.go`

### 5.1 Concept mapping reuses `elements.go`

`elements.go` stores qualified names (`ifrs-full:Revenue`); Company Facts nests namespace
and concept separately. Add one helper rather than a second dictionary:

```go
// conceptKey maps a Company Facts (namespace, concept) pair onto a canonical
// metric key, reusing the same ranked aliases the instance reader uses.
func conceptKey(ns, concept string) (key string, rank int, ok bool)
```

Rank is the alias's index within its metric, so `ifrs-full:ProfitLoss` still beats
`ifrs-full:ProfitLossFromContinuingOperations` exactly as in prompt 8. **No new alias table
and no new precedence rule.**

### 5.2 `factIndex`

```go
type factIndex struct {
    byAccession map[string]map[string][]indexedFact // accn -> key -> facts
    byPeriod    map[periodKey][]indexedFact          // (key,start,end) -> facts, all accessions
}
type periodKey struct{ Key, Start, End string }
type indexedFact struct {
    Key, Accn, Form, FP string
    Start, End          string
    Val                 float64
    Unit                string
    Rank                int
}

func buildIndex(cf *CompanyFacts) *factIndex
```

`add` applies **D3 first**: monetary keys accept only `unit == "USD"`, `eps_*` only
`"USD/shares"`. Everything else is dropped and never indexed — this is what keeps the
`ILS` fact (FY2024 revenue `10,000,000,000`) out of the pipeline entirely.

`byPeriod` is what makes D5 possible: it collects every accession's reading of the same
(metric, period) in one place.

### 5.3 Period selection (D4)

```go
func selectPeriod(idx *factIndex, accn string, meta metaJSON) (Period, bool)
```

- `FP` and `FY` come from the accession's own entries (all entries of one accession share
  them). `FP == "FY"` → `P1Y`, band 350–380 days; `Q1..Q4` → `P3M`, band 80–100. Reuses
  prompt 8's `matchesDuration`.
- Period end = the **latest** `End` among that accession's revenue entries whose duration
  is in band. One accession carries prior-year, year-to-date and full-year facts alongside
  the reporting quarter (HLD §3.3), so "latest in-band end" is what isolates the quarter
  the filing is *about*. Taking the first entry yields prior-year H1.
- `Form` is cross-checked against `meta.json`; a mismatch skips the accession with a note.

```go
func pickEntry(idx *factIndex, accn, key, end string, dur string, instant bool) (indexedFact, bool)
```

Instant keys (`cash_and_equivalents`, `total_assets`) match on `End` with no `Start`.
Duration keys match band + `End == end`. Lowest alias `Rank` wins.

Prior comparable: same band, `End` 350–380 days earlier, closest to 365.

### 5.4 Scale reconciliation (D5) — per accession, not per metric

The decisive measured fact: mis-tagging is a **property of the filing**. In `23-085500`
`Revenue`, `ProfitLoss` and `CashAndCashEquivalents` are each exactly 1/1000 of the same
period as reported by `24-097126`. So the factor is resolved once and applied filing-wide.

```go
// resolveScale determines the multiplier for one accession's monetary facts by
// comparing each metric against other accessions reporting the same period.
func resolveScale(idx *factIndex, accn string, period Period) (factor float64, ev []string, ok bool)
```

For every monetary metric of the accession that has at least one *other* accession
reporting the same `periodKey`:

1. Let `own` be this accession's value and `others` the rest.
2. For each other value `o`, compute the ratio `o/own` and snap it to the nearest power of
   1000 within 0.5%. A ratio that is not 1, 1000 or 1/1000 → **irreconcilable**.
3. Collect the votes.

| Votes | Outcome |
|---|---|
| all ratios 1 | `factor = 1`, corroborated |
| all ratios 1000 | `factor = 1000`, corroborated, evidence recorded |
| mixed 1 and 1000, or any irreconcilable | `ok = false` → **withhold the whole accession** |
| no metric has a corroborator | `ok = false` → fall through to D6 |

Scale-down is never adopted: a ratio of 1/1000 would mean the corroborator is the
mis-tagged one, and `resolveScale` is always invoked from the perspective of the accession
being filled, so that case appears as `factor = 1` from the other side.

**Measured:** across the eight fillable accessions this resolves all 32 metrics; seven
accessions get `factor = 1` and `23-085500` gets `factor = 1000` with `24-097126` as
evidence. `eps_*` is **excluded from scaling entirely** — per-share values are never
mis-scaled by thousands, and they must agree to the cent or the metric is withheld.

### 5.5 Magnitude fallback (D6)

```go
func plausibleMagnitude(idx *factIndex, key string, val float64) bool
```

Used **only** when `resolveScale` found no corroborator. Rather than a fixed floor —
which would false-positive on genuinely small figures, and the corpus has a real
`ProfitLoss` of `3,000` for a half-year — it is self-calibrating: compare against the
**median absolute value of the same metric across all accessions in the document**. A value
smaller than `median/100` is implausible and the metric is withheld.

This is issuer-independent by construction. It is also the weakest rule in the package and
is deliberately confined to the uncorroborated path.

### 5.6 Grading

```go
func gradeEntry(f indexedFact, corroborators int, scaled bool) (confidence, source string, notes []string)
```

| Situation | Confidence |
|---|---|
| ≥1 corroborating accession, ratios consistent | `verified` |
| scaled by 1000 with evidence | `verified` + note naming the corroborating accession and factor |
| no corroborator, magnitude plausible | `single_source` |
| no corroborator, magnitude implausible | `suspect` (withheld) |
| mixed/irreconcilable ratios | `suspect` (withheld) |

`Source` is `SourceCompanyFacts` throughout. `Line.Notes` always records
`fetchedAt`, the concept used, and the corroborating accessions — so a published API figure
is as auditable as a locally extracted one.

---

## 6. `fillgaps.go`

### 6.1 Offline classification (D2)

```go
type GapKind int
const (
    GapFillable   GapKind = iota // has {accession}-xbrl.zip
    GapNoXBRL                    // no zip: SEC will have no facts either
    GapHasLocal                  // already extracted
)

type Gap struct {
    Dir, Accession, FilingDate, Form, Category string
    Kind GapKind
}

func ClassifyGaps(root, cik string) ([]Gap, error)
```

Walks qualifying accessions (`quarterly_results`, `annual_report`) and classifies by two
`os.Stat` calls: `financials.json` present → `GapHasLocal`; else any `*-xbrl.zip` →
`GapFillable`; else `GapNoXBRL`. **No client, no network.**

Measured on the corpus: 15 `GapHasLocal`, 8 `GapFillable`, 43 `GapNoXBRL`, and the zip
predicate agreed with the API on 51/51.

### 6.2 Fill

```go
func FillGaps(ctx context.Context, root, cik string, c *Client, opts Options) (*FillReport, error)
```

1. `ClassifyGaps`. If nothing is `GapFillable` and `opts.WriteNoFacts` is false, return
   without constructing a request.
2. `c.Facts(ctx, root, cik)` — once.
3. `buildIndex`.
4. For each `GapFillable`: `selectPeriod` → `resolveScale` → per metric `pickEntry`,
   apply factor, `gradeEntry`, build `Line`.
   - YoY from the prior comparable, **income lines only**, and — reusing prompt 8's rule —
     only when the base is positive.
   - Balance lines carry `PriorLabel` and no YoY.
   - An accession whose `selectPeriod` fails, or whose `resolveScale` is irreconcilable,
     is written as a no-facts record with the reason rather than skipped silently.
5. For each `GapNoXBRL` (when `opts.WriteNoFacts`): `writeNoFacts`.
6. Never touch a `GapHasLocal` accession (D1).

```go
func writeNoFacts(dir string, meta metaJSON, reason string) error
```

Writes a `FilingFinancials` with `Source: SourceNone`, empty statements, and `ParseNotes`
naming the reason. `Publishable()` returns nothing, so `BuildHighlights` falls through to
the summary regex exactly as today.

### 6.3 `extract.go` changes

`ExtractAll` is untouched. `FillGaps` is a separate entry point the command calls **after**
it, so the local path keeps its existing two-pass semantics and cannot regress.

`Options` gains `WriteNoFacts bool`.

---

## 7. `schema.go` / `elements.go` changes

```go
const (
    SourceCompanyFacts = "sec_companyfacts"
    SourceNone         = "none"
)
```

`elements.go`: no new aliases. `conceptKey` (§5.1) derives the Company Facts view from the
existing qualified names by splitting on `:`.

The `us-gaap` aliases already present remain **unmeasured** — the corpus is one IFRS
issuer, and the document's `us-gaap` namespace holds only 12 concepts, none of them
revenue-class. Unchanged risk from prompt 8, restated in §11.

---

## 8. `cmd/edgar-financials`

| Flag | Behaviour |
|---|---|
| `--gaps` | Print the D2 classification table and exit. **Never fetches.** |
| `--companyfacts` | Fetch (or refresh) the cache for the CIK; print `fetchedAt`, size, accession count |
| `--fill-gaps` | Run `FillGaps` after local extraction; implies `--companyfacts` |
| `--no-facts-records` | Also write D7 records for `GapNoXBRL` (default true with `--fill-gaps`) |

Env: `SEC_EDGAR_USER_AGENT` (required for any fetch), `SEC_COMPANYFACTS_TTL` (default
`168h`). `--report` gains a `source` column so local and API rows are distinguishable, and
its coverage line reports `local=N api=M none=K`.

Ordering in `main`: `ExtractAll` → `FillGaps` → `printReport`. Local always runs first, so
a fill can only ever touch accessions local extraction declined.

---

## 9. Fixtures

`testdata/companyfacts/CIK0001567529.trimmed.json` — the live document reduced to four
concepts (`Revenue`, `BasicEarningsLossPerShare`, `ProfitLoss`,
`CashAndCashEquivalents`) and only the accessions the tests need. Must retain:

| Kept | Why |
|---|---|
| `0001213900-24-068559` | clean fill; the Definition-of-Done accession |
| `0001213900-24-097126` | clean fill **and** the corroborator for `23-085500` |
| `0001213900-23-085500` | mis-tagged ×1000; the D5 regression |
| `0001213900-25-042940` | corroborator proving `24-040628` is mis-tagged (HLD D9 groundwork) |
| the `ILS` `Revenue` entry | D3 unit-filter regression |
| multi-period entries of `24-068559` | D4 regression (prior-year H1 trap) |

`testdata/companyfacts/README.md` records the trimming command, as prompt 8's fixtures do.
**No live network in any test.**

---

## 10. Tests

| Test | Asserts |
|---|---|
| `TestClientRequiresUserAgent` | empty UA → `ErrNoUserAgent`, and **no request is made** (httptest counter stays 0) |
| `TestClientCachesAndHonoursTTL` | first call hits httptest, second reads disk; expired mtime refetches |
| `TestClientRateLimited` | 429 → `ErrRateLimited` before body read |
| `TestUnitFilterExcludesILS` | the `10,000,000,000` ILS entry never enters the index |
| `TestPeriodSelectionPicksTheQuarter` | `24-068559` → 42,472,000, **not** 68,153,000 (prior-year H1) |
| `TestResolveScaleDetectsMisTaggedFiling` | `23-085500` → factor 1000, evidence names `24-097126` |
| `TestResolveScaleIsPerAccession` | all three monetary metrics of `23-085500` get the same factor |
| `TestMisTaggedFilingPublishesCorrectedValue` | revenue renders `$37.9M`, **never** `$37.9K` |
| `TestIrreconcilableWithholds` | synthetic mixed ratios → whole accession suspect |
| `TestUncorroboratedUsesMedianMagnitude` | synthetic lone implausible value → withheld; lone plausible → `single_source` |
| `TestEPSNeverScaled` | a per-share value is never multiplied by 1000 |
| `TestClassifyGapsOffline` | 15/8/43 on the corpus (skipped when absent); asserts no `Client` is constructed |
| `TestFillGapsSkipsLocal` | an accession with a local artifact is untouched byte-for-byte |
| `TestNoFactsRecord` | `GapNoXBRL` → `Source: "none"`, `Publishable()` empty, reason in notes |
| `TestGoldenFillOutputs` | golden `financials.json` for `24-068559` and `23-085500` |

Corpus-level (skipped without `fileDB`): after `FillGaps`, the six §3.4 chain cross-checks
still hold against prompt 8's local values.

---

## 11. Build order

1. `companyfacts.go` + client tests (httptest only). Gate: UA required, cache works.
2. `factIndex` + `conceptKey` + D3 unit filter. Gate: ILS excluded.
3. `selectPeriod`/`pickEntry`. Gate: `24-068559` yields the quarter, not H1.
4. **`resolveScale` + `gradeEntry`. Gate: `23-085500` publishes `$37.9M` or is withheld —
   never `$37.9K`.** This is the gate the whole prompt exists for.
5. `ClassifyGaps` + `--gaps`. Gate: 15/8/43 offline, zero requests.
6. `FillGaps` + `--fill-gaps`; run live; re-verify the §3.4 cross-checks.
7. `writeNoFacts` for the 43.
8. `--report` source column; `.env.example`; revise prompt 8 HLD's "366/381" line.

---

## 12. Definition of Done

Open `/company/0001567529` → Timeline → click **2024-08-14** (`0001213900-24-068559`, which
shows "Financial highlights unavailable." today): **Revenue $42.5M (+13.4% YoY)**, **Basic
EPS $0.08**, **Net income $4.4M**, **Cash $56.5M**, from a `financials.json` whose lines
carry `source: "sec_companyfacts"`.

Negative cases, equally required:

- **2023-11-13** (`23-085500`, mis-tagged in SEC's own feed) shows **$37.9M**, never
  `$37.9K`, with notes naming the ×1000 correction and `24-097126` as corroborator.
- **2024-03-06** (`24-020275`, no XBRL anywhere) still shows "Financial highlights
  unavailable.", and its artifact says why: `source: "none"`.

`--gaps` reports 8 fillable / 43 unfillable **with no network**; after `--fill-gaps`,
`--report` shows **23** artifacts with publishable revenue.

---

## 13. Risks and open questions

| Risk | Severity | Handling |
|---|---|---|
| SEC does not normalize scale; 1 of 8 fills is mis-tagged | **High** | D5 per-accession reconciliation (measured 32/32), withhold when mixed |
| `us-gaap` concept mapping unmeasured | **High** | Inherited from prompt 8; unresolved until a domestic filer is loaded |
| D6 median heuristic is the weakest rule | Medium | Confined to the uncorroborated path; no fill in today's corpus relies on it |
| Cache hides a restatement | Medium | 7-day TTL, `--all` forces, `fetchedAt` in notes |
| SEC blocks the client | Medium | Fail fast on missing UA, one fetch per CIK, 429 sentinel |
| Prompt 8's HLD claim about coverage becomes stale | Low | Build step 8 revises it |

**Open questions (2, neither blocking):**

1. **Should a fill be re-verified when local extraction later becomes possible?** If EDGAR
   generates the viewer bundle for a filing we already filled, the artifact stays
   `sec_companyfacts` until `--all`. Acceptable for v1; a `--reconcile` mode would close it.
2. **HLD D9 — Company Facts adjudicating prompt 8's suspect local lines.** The fixture
   deliberately keeps `25-042940` so the groundwork is in place, but the override path is
   out of scope here and should land before `EDGAR_FINANCIALS_LLM` is ever enabled.
