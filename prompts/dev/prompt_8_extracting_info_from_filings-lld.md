# LLD: Deterministic Financial Extraction from SEC Filings

Implements `prompt_8_extracting_info_from_filings-hld.md` (the contract) and the idea file
`prompt_8_extracting_info_from_filings.txt`.

Triage: **HEAVY** — new Go package (9 files), new on-disk artifact, new command, and edits
to two shipped read-path packages. The HLD's §3 data audit is measured and not repeated
here; this document is the build.

> **Governing rule (from the HLD, restated because every gate below serves it):** a wrong
> number of the right order of magnitude is worse than no number. **Withholding beats
> guessing.**

---

## 1. Scope

**In (P0):** XBRL-instance fact extraction; `FilingSummary.xml` role resolution; rendered
`R*.htm` corroboration; six anomaly detectors; two-source agreement gate; `financials.json`
writer; `cmd/edgar-financials`; `filedb` load; `companyview` highlight wiring.

**In (P1):** operating cash flow; segment lines via dimensioned contexts; derived gross
margin.

**In (P2):** `adjudicate.go` behind `EDGAR_FINANCIALS_LLM`.

**Out:** taxonomy graph traversal (presentation/definition linkbases); prose extraction as
a primary path; currency conversion; request-time extraction; any change to
`python/edgar/build_meta.py`, `meta.json`, the DB schema, or HTTP routes.

---

## 2. Package layout

```
internal/edgar/financials/
├── schema.go       FilingFinancials + JSON contract, sorted-key writer
├── elements.go     canonical key → ranked XBRL element aliases
├── instance.go     XBRL instance reader (both dialects) — PRIMARY source
├── summary.go      FilingSummary.xml → statement role → file
├── rendered.go     R*.htm corroborating reader
├── verify.go       detectors A–F, agreement gate, confidence assignment
├── extract.go      per-accession + two-pass corpus orchestration, writer
├── adjudicate.go   P2 — LLM adjudicator
└── testdata/       trimmed real fixtures (§10)
cmd/edgar-financials/main.go
```

`extract.go` is the only file the rest of the repo imports.

---

## 3. `schema.go`

Mirrors the idea file's JSON, with the HLD's per-metric confidence and source added.

```go
package financials

const SchemaVersion = 1

type FilingFinancials struct {
    Version     int             `json:"version"`
    ExtractedAt time.Time       `json:"extractedAt"`
    Accession   string          `json:"accession"`
    Currency    string          `json:"currency,omitempty"`
    Period      Period          `json:"period"`
    Statements  StatementSets   `json:"statements"`
    ParseNotes  []string        `json:"parseNotes,omitempty"`
}

type Period struct {
    EndDate  string `json:"endDate"`            // dei:DocumentPeriodEndDate
    Label    string `json:"label"`              // "Q3 2025" | "FY 2025"
    Duration string `json:"duration"`           // P3M | P9M | P1Y
    Focus    string `json:"focus"`              // dei:DocumentFiscalPeriodFocus
}

type StatementSets struct {
    Income   []Line    `json:"income,omitempty"`
    Balance  []Line    `json:"balance,omitempty"`
    CashFlow []Line    `json:"cashFlow,omitempty"`   // P1
    Segments []Segment `json:"segments,omitempty"`   // P1
}

type Line struct {
    Key        string   `json:"key"`
    Display    string   `json:"display"`
    Element    string   `json:"element"`               // ifrs-full:Revenue — audit trail
    Value      float64  `json:"value"`
    ValueFmt   string   `json:"valueFmt"`
    Unit       string   `json:"unit"`                  // USD | USD/share
    PriorValue *float64 `json:"priorValue,omitempty"`
    PriorLabel string   `json:"priorLabel,omitempty"`  // "vs Dec 31 2024" (balance only)
    YoYPct     *float64 `json:"yoyPct,omitempty"`      // income lines only — D7
    Confidence string   `json:"confidence"`            // verified|single_source|suspect
    Source     string   `json:"source"`                // xbrl_instance|rendered_html|both|llm_adjudicated
    Notes      []string `json:"notes,omitempty"`       // firing detectors, candidate values
}
```

`Segment{Name, Member, Value, ValueFmt}`.

**Writer:** `encoding/json` with `SetIndent("", "  ")`; Go marshals struct fields in
declaration order, which is stable — the HLD's "sorted keys" requirement is satisfied by
fixed field order plus deterministic slice ordering (§7 step 6). `ExtractedAt` is
truncated to seconds, UTC.

**Publication predicate** — the single place this is decided:

```go
func (l Line) Publishable() bool {
    return l.Confidence == ConfidenceVerified || l.Confidence == ConfidenceSingleSource
}
```

---

## 4. `elements.go`

Canonical key → **ranked** element local-names. First hit wins; ranking matters where a
filer tags both (`ProfitLoss` and `ProfitLossFromContinuingOperations` coexist in 2/15).

| Key | Ranked aliases (IFRS, then US-GAAP) |
|---|---|
| `total_revenues` | `ifrs-full:Revenue`, `ifrs-full:RevenueFromContractsWithCustomers`, `us-gaap:Revenues`, `us-gaap:RevenueFromContractWithCustomerExcludingAssessedTax` |
| `cost_of_revenues` | `ifrs-full:CostOfSales`, `us-gaap:CostOfRevenue` |
| `gross_profit` | `ifrs-full:GrossProfit`, `us-gaap:GrossProfit` |
| `operating_income` | `ifrs-full:ProfitLossFromOperatingActivities`, `us-gaap:OperatingIncomeLoss` |
| `net_income` | `ifrs-full:ProfitLoss`, `ifrs-full:ProfitLossFromContinuingOperations`, `us-gaap:NetIncomeLoss` |
| `eps_basic` | `ifrs-full:BasicEarningsLossPerShare`, `us-gaap:EarningsPerShareBasic` |
| `eps_diluted` | `ifrs-full:DilutedEarningsLossPerShare`, `us-gaap:EarningsPerShareDiluted` |
| `cash_and_equivalents` | `ifrs-full:CashAndCashEquivalents`, `us-gaap:CashAndCashEquivalentsAtCarryingValue` |
| `total_assets` | `ifrs-full:Assets`, `us-gaap:Assets` |
| `operating_cash_flow` (P1) | `ifrs-full:CashFlowsFromUsedInOperatingActivities`, `us-gaap:NetCashProvidedByUsedInOperatingActivities` |

Matching is on `namespace-prefix:LocalName` **exact**, case-sensitive. No substring, no
fuzzy, no Levenshtein — HLD D1/§3.3 defect 3.

> **`us-gaap:*` rows are UNMEASURED.** The corpus is one IFRS foreign private issuer.
> They are seeded from the standard taxonomy and must be validated against a domestic
> filer before any claim of multi-issuer support. Tracked as the top risk (§12).

Also here: `MetricRank = [total_revenues, eps_basic, net_income, cash_and_equivalents]`,
the modal display order (idea file §"What to show on the Timeline").

---

## 5. `instance.go` — primary source

### 5.1 Discovery

```go
func FindInstance(dir string) (string, error)
```

1. Any `*_htm.xml` (inline-extracted). Else
2. Any `*.xml` whose name has no linkbase suffix (`_cal`, `_def`, `_lab`, `_pre`) and is
   not `FilingSummary.xml`.

Measured: rule 1 hits 13/15, rule 2 the remaining 2 (2020, 2021). Coverage 15/15.

### 5.2 Namespace-agnostic parsing

The two dialects differ in prefix — inline uses the **default** namespace
(`<context>`, `<unit>`), classic uses `xbrli:` (`<xbrli:context>`). Struct-tag unmarshal
would need both. Use an `xml.Decoder` token loop matching **`t.Name.Local` only**:

```go
func parseInstance(r io.Reader) (*instanceDoc, error)
```

Collect three maps in one pass:

- `contexts map[string]ctx` — `ctx{ID, Start, End, Instant string, Dimensioned bool}`.
  `Dimensioned` = the context subtree contains an `explicitMember` element (under
  `segment` **or** `scenario`; both are legal, only `segment` is observed).
- `units map[string]unit` — resolved by **`measure` content, never by unit id**: ids vary
  in case across dialects (`usd` vs `USD`, `usdPershares` vs `Shares`). A bare
  `iso4217:USD` measure → `UnitUSD`; a `divide` of `iso4217:USD` over `shares` →
  `UnitUSDPerShare`; anything else → `UnitOther` (excluded).
- `facts []fact` — `fact{Element, ContextRef, UnitRef, Decimals string, Raw string}` for
  any element whose local name matches an `elements.go` alias.

`dei:DocumentPeriodEndDate`, `dei:DocumentFiscalPeriodFocus`, `dei:DocumentFiscalYearFocus`
and `dei:DocumentType` are captured in the same pass.

### 5.3 Period selection — declared, not inferred

The filing states its own period, so no header regex and no column heuristic:

```go
func selectPeriod(d *instanceDoc) Period
```

| `Focus` | Duration | Current context predicate |
|---|---|---|
| `Q1`…`Q4` | `P3M` | undimensioned, `End == DocumentPeriodEndDate`, `End-Start` in 80…100 days |
| `FY` | `P1Y` | undimensioned, `End == DocumentPeriodEndDate`, `End-Start` in 350…380 days |

Label: `"Q3 2025"` / `"FY 2025"` from focus + `DocumentFiscalYearFocus`.

**Prior comparable** (income lines): undimensioned, same duration band, `End` in
`[cur.End − 380d, cur.End − 350d]`. If several match, the one closest to exactly −365d.

**Balance instants:** current = undimensioned, `Instant == DocumentPeriodEndDate`. Prior =
undimensioned instant at the **prior fiscal year-end** (`Dec 31, FY−1`), labelled
`"vs Dec 31 2024"`, with **no YoY** — HLD D7, because R2's second column is the prior
year-end and not the prior-year quarter.

### 5.4 Fact selection

```go
func (d *instanceDoc) Pick(key string, want ctxSelector) (*fact, []*fact)
```

For each alias of `key` in rank order, collect facts whose context satisfies `want`.
Then:

- **Discard any fact whose context is `Dimensioned`.** This is the structural kill of
  defect 3 — a segment figure can never be selected as a headline metric.
- **Discard any fact whose unit is not `UnitUSD` (monetary) or `UnitUSDPerShare` (EPS).**
  Kills the `₪ in Billions` facts by unit rather than by column position.
- Exactly one survivor → return it. Zero → try next alias. **More than one → return them
  all as candidates and let detector E flag ambiguity** (never pick arbitrarily).

Values are parsed as absolute (`40017000`), because that is how conformant filings tag
them. `Decimals` is retained verbatim for detector B.

---

## 6. `summary.go` and `rendered.go` — corroborating source

### 6.1 Role resolution (HLD D2)

```go
func StatementFiles(dir string) (map[Role]string, error)   // Role: RoleIncome|RoleBalance|RoleCashFlow
```

Parse `FilingSummary.xml`; keep `MenuCategory == "Statements"`; skip `ShortName`
containing `parenthetical` (case-insensitive); first match wins per role:

| Role | `ShortName` (lowercased) contains |
|---|---|
| `RoleBalance` | `financial position` or `balance sheet` |
| `RoleIncome` | `profit or loss` or `operations` |
| `RoleCashFlow` | `cash flow` |

R-numbers appear **nowhere** in code — only in fixture paths. Measured correct 15/15,
including `25-020360` where `R2` is *Audit Information* and hardcoding breaks.

### 6.2 Rendered read

```go
func ReadRendered(path string) (*renderedTable, error)
```

Strip `<script>`/`<style>`; walk `<tr>`/`<td>|<th>`. Per row capture:

- `Element` from the first cell's drill-down anchor: `defref_([A-Za-z0-9_\-]+)`, with `_`
  → `:` on the first separator (`defref_ifrs-full_Revenue` → `ifrs-full:Revenue`).
- `Label` — first cell text, HTML-unescaped, whitespace-collapsed.
- `Values` — remaining cells, **leading non-numeric cells dropped** (HLD D4: the
  `$ in Thousands, ₪ in Billions` caption emits a phantom empty column that shifts every
  index in `25-020360`).

Caption scale from the header cell: `\$ in (Thousands|Millions|Billions)` → ×10³/10⁶/10⁹;
**absent → ×10³ with a `parseNote`**, which is the observed-correct default for
`24-040628` and the only place scale is ever inferred. Per-share rows (unit from the
matched element) always use ×1.

Row selection uses the **element name**, not the label — so this reader shares the
instance's addressing and differs only in how the number is scaled. That is deliberate:
it isolates the scale question, which is the one thing the two sources can disagree about.

---

## 7. `verify.go` — detectors and the gate

### 7.1 Detector → defect matrix

This is the load-bearing table of the design. **No single mechanism covers every defect**,
which is why both the gate and the detectors ship in P0.

| Detector | Fires when | Catches | Measured |
|---|---|---|---|
| **A** Source disagreement | `|inst − rend| / |inst| > 0.005` (monetary) or ≠ to the cent (per-share) | wrong statement/row/column/unit | 0 firings |
| **B** Implausible tagging | monetary fact with `decimals >= 0` **and** `|value| < 1e6` | **filer mis-tagged at source** (defect 2) | fires on **`24-040628` only** — 1/15, no false positives |
| **C** Calculation invariant | `|revenue − cost − gross_profit| > 1` currency unit; and `|Σ segments − total| > 1` (P1) | segment-as-total, scale | holds **14/14** computable |
| **D** Chain break | prior-period value of filing *n* vs current value of the previous comparable filing, tolerance 0.5% | scale, wrong row | holds **15/15** |
| **E** Ambiguity | >1 undimensioned candidate fact for one key/period | future taxonomy drift | 0 firings |
| **F** Role unresolved | `FilingSummary.xml` missing or a role unmatched | wrong statement | 0 firings |

**Detector A does not catch `24-040628`** — the SEC renderer faithfully reproduces the
mis-tagged instance, so both readers return `37736` and *agree*. Agreement proves the two
readers parsed the same fact, not that the fact is right. B and D are what catch it. This
corrects an over-claim in an earlier HLD draft and is the reason the detector set is not
optional hardening.

Thresholds B and C are tuned on a **single observation each** and are the most likely
things to need revision on a second issuer (§12).

### 7.2 Gate

```go
func Grade(inst, rend *candidate, sigs []Signal) (confidence, source string, notes []string)
```

| Inputs | Confidence | Source |
|---|---|---|
| both present, agree, no signals | `verified` | `both` |
| one present, no signals | `single_source` | `xbrl_instance` \| `rendered_html` |
| any signal fired, or both present and disagree | `suspect` | — |
| neither present | *(line omitted entirely)* | — |

Every firing signal, and both candidate values when they differ, are written to
`Line.Notes` — so a withheld metric is diagnosable from `financials.json` alone without
re-running the extractor.

---

## 8. `extract.go`

```go
func ExtractAccession(dir string) (*FilingFinancials, error)   // pass 1, no chain check
func ExtractAll(root, cik string, opts Options) (*Report, error)
type Options struct{ Force bool; DryRun bool; LLM Adjudicator }
```

`ExtractAll` is **two-pass by necessity** — detector D compares across filings, so nothing
can be written until every candidate exists:

1. Enumerate `{root}/companies/{cik}/{year}/{accession}/`; skip unless
   `meta.json.category ∈ {quarterly_results, annual_report}` **and** an instance is found.
2. `ExtractAccession` each → in-memory candidates.
3. Sort by `Period.EndDate`; run detector D across comparable pairs (same `Focus` kind:
   quarterly against quarterly, FY against FY); downgrade both sides of any break to
   `suspect`.
4. P2 only: hand `suspect` lines to the adjudicator (§9).
5. Drop lines that are neither publishable nor informative-as-suspect — i.e. keep suspects
   (with notes) so the withholding is auditable, drop nothing silently.
6. Order lines by `MetricRank`, segments by descending value — deterministic output.
7. Write `financials.json` unless `DryRun`. **Idempotency:** skip when the file exists and
   `!Force`; `--all` sets `Force`.

Gate 1 keeps the writer off 366/381 accessions with no cost beyond a `stat`.

**No migration, no backfill.** `financials.json` is additive; absence is the normal case
(HLD D8) and every reader treats `(nil, nil)` as valid. Re-running with `--all` is the
only "backfill" and is safe by construction — the writer is a pure function of the
accession folder plus the chain neighbours.

---

## 9. `adjudicate.go` (P2)

```go
type Adjudicator interface {
    Adjudicate(ctx context.Context, req AdjudicationRequest) (*AdjudicationResult, error)
}
```

Implements HLD D6 steps 2–7 exactly; prompt is
`prompts/edgar_financials_adjudicate.txt`, loaded via the existing
`pipeline.LoadPrompts` map (key `edgar_financials_adjudicate`).

- **Evidence (step 3):** scan `*ex99*.htm` in the accession dir; strip tags; split into
  sentences; keep those containing a metric keyword **and** an explicit-unit pattern
  (`\$\s?[\d.,]+\s*(million|billion)` or `per share`); **reject any candidate sentence
  with ≥3 bare numbers in sequence** — that is a flattened table and reintroduces the very
  ambiguity under dispute. Cap at `EDGAR_FINANCIALS_LLM_MAX_EVIDENCE` (4000).
- **No evidence → no API call**, metric stays withheld.
- **Call:** `llm.Client.Complete` with `Model: gemini-3.5-flash-lite`,
  `GeminiResponseSchema` set for API-enforced JSON, temperature 0. Concurrency rides the
  existing `MAX_CONCURRENT_LLM` limiter.
- **Validation (step 6):** `choice` ∈ supplied ids ∪ `{"none"}`; `value` equals that
  candidate's value **exactly**; `evidenceQuote` is a verbatim substring of what was sent.
  Any violation → treat as `"none"`. This makes a fabricated number unpublishable rather
  than merely unlikely.
- **Apply (step 7):** accept only when `choice != "none"` **and** `confidence != "low"` →
  `verified` / `llm_adjudicated`, quote + model id into `Notes`.

Wired only when `EDGAR_FINANCIALS_LLM=1`; otherwise `Options.LLM` is nil and step 4 of
§8 is skipped entirely.

---

## 10. Fixtures — `testdata/`

Trimmed real files (statements only, notes/details stripped), **not** whole accession
folders. Each fixture directory carries `FilingSummary.xml`, the instance, and the two
`R*.htm` it names.

| Fixture | Why it is mandatory |
|---|---|
| `q3_2025/` (`25-107836`) | Happy path; the §8 smoke target; segments inside R3 |
| `q1_2024/` (`24-040628`) | **Detector B** — `decimals="0"`, no caption scale; also the adjudication fixture (its EX-99 revenue sentence) |
| `fy_2024_20f/` (`25-020360`) | `R2`=*Audit Information* (role resolution), phantom NIS column, `₪ in Billions` unit exclusion |
| `fy_2019_20f/` (`20-004782`) | **Classic `xbrli:`-prefixed instance**, `Shares`/`USD` unit ids — the other dialect |
| `q3_2022/` (`22-074381`) | `ProfitLossFromContinuingOperations` alias rank |

Instance files are large; trim to the `context`/`unit` blocks actually referenced plus the
matched facts, and record the trimming command in `testdata/README.md` so fixtures are
reproducible.

---

## 11. Tests

**`internal/edgar/financials/`**

| Test | Asserts |
|---|---|
| `TestFindInstance` | inline and classic dialects both discovered; linkbases excluded |
| `TestParseInstanceDialects` | `xbrli:`-prefixed and default-ns parse to identical structs |
| `TestUnitResolution` | `usd`/`USD` → USD; `usdPershares` → USD/share; NIS → Other |
| `TestSelectPeriod` | Q3→P3M/2025-09-30; FY→P1Y; prior-comparable −365d; balance instant + prior year-end |
| `TestPickExcludesDimensioned` | table-driven, **golden defect 3**: the `Proprietary Products` Revenue fact is never selected as `total_revenues` |
| `TestStatementFiles` | `25-020360` → income `R4`, balance `R3`; parentheticals skipped |
| `TestRenderedPhantomColumn` | `25-020360` values align despite the empty leading cell |
| `TestRenderedScaleFallback` | missing caption → ×10³ + note; per-share rows → ×1 |
| `TestDetectorB` | fires on `q1_2024`, **silent on the other four fixtures** |
| `TestDetectorC` | `revenue − cost = gross_profit` on all fixtures |
| `TestDetectorD` | synthetic chain with one injected 1000× break → both sides `suspect` |
| `TestGrade` | full truth table of §7.2 |
| `TestExtractGolden` | each fixture → golden `financials.json`, byte-identical |
| `TestIdempotency` | second run without `Force` writes nothing; with `Force` reproduces bytes |
| `TestAdjudicateValidation` | (P2) wrong value, non-verbatim quote, unknown id, `"low"` → all withheld |

**`internal/filedb/`** — `TestLoadFinancialsAbsent` returns `(nil, nil)`;
`TestScanAttachesFinancials` on the corpus fixture.

**`internal/companyview/`** — `TestBuildHighlightsPrefersFinancials`;
`TestBuildHighlightsFallsBackToSummary`; **`TestSuspectLinesAreNotRendered`** (the §8
negative case, the most important test in the change).

Corpus-level assertion, run by `cmd/edgar-financials --report` and asserted in CI if the
corpus is present: extracted values match HLD §3.2 for 15/15, and **suspect count ≤ 1**
with the LLM off.

---

## 12. Read-path changes

### `internal/filedb`

```go
func LoadFinancials(accessionDir string) (*financials.FilingFinancials, error) // (nil,nil) when absent
```

- `models.go`: `FilingRow` gains `Financials *financials.FilingFinancials \`json:"-"\``
  — **`json:"-"`**, because `FilingRow` is serialized to the filings API and this payload
  belongs only to the timeline event.
- `scan.go`: one call inside `readAccession(dir, year)` (`internal/filedb/scan.go:105`),
  which already holds the accession dir. Failure is tolerated exactly like a bad
  `meta.json`: log at warn, leave the field nil, keep the row.
- Cost: ≤15 small reads per company inside a walk that already stats every accession;
  cached by `FileDBStore` under `FILEDB_CACHE_TTL` like everything else.

### `internal/companyview`

`highlights.go` — extend, never replace (HLD D8/D9, prompt 6 D6 purity):

```go
func HighlightsFromFinancials(f *financials.FilingFinancials) *EventHighlight
func BuildHighlights(category, summary string, f *financials.FilingFinancials) *EventHighlight
```

`HighlightsFromFinancials` filters `Line.Publishable()`, emits in `MetricRank` order, and
formats: revenue `"$47.0M (+12.6% YoY)"`, EPS `"$0.09"`, net income `"$5.3M"`, cash
`"$72.0M"`. Returns nil when nothing publishable survives — so a fully-suspect filing
falls through to the summary regex, then to nil.

`BuildHighlights` tries financials first, else the existing growth-% path unchanged.
`Source` becomes `"financials"` or stays `"summary_parse"`.

**Call sites — the complete list.** `BuildHighlights` has exactly one caller:
`timeline.go:170` inside `BuildEvents`, which changes to pass `r.Financials`. `BuildEvents`
itself is called only from `service.go:99` (`Service.Timeline`). No other package
references either symbol; no handler, route, or DTO changes.

**UI:** none. `static/js/company/tab-timeline.js:765` already renders `metrics[]` and
already shows "Financial highlights unavailable." when absent.

---

## 13. `cmd/edgar-financials`

```
edgar-financials --root ./fileDB --cik 0001567529 [--all] [--dry-run] [--report]
```

`--report` prints the §3.2 table plus per-metric confidence and every firing detector,
and exits non-zero if suspects exceed the threshold — the acceptance harness for build
step 6 and the guard against adjudication masking a parser regression (HLD D6 guardrail 5).

`--cik` empty → every company under `root`. Reads `EDGAR_FINANCIALS_LLM*` from the
environment; with the flag unset no LLM client is constructed at all.

---

## 14. Build order

1. `schema.go` + `elements.go` + `Publishable()`; unit tests.
2. `instance.go` — discovery, dual-dialect parse, units, period selection, `Pick`.
   Gate: `TestPickExcludesDimensioned` green (defect 3 dead).
3. `summary.go` + `rendered.go`. Gate: role resolution and phantom column green.
4. `verify.go` — detectors A–F + `Grade`. **Gate: B fires on `q1_2024` and nothing else;
   C 14/14; D 15/15.**
5. `extract.go` — two-pass, gate, writer, idempotency. Gate: golden files.
6. `cmd/edgar-financials`; run corpus; `--report` matches HLD §3.2, suspects ≤ 1.
7. `filedb.LoadFinancials` → `HighlightsFromFinancials` → `timeline.go:170`.
8. Manual smoke — **both** §15 cases, the withheld one included.
9. P1: cash flow, segments via dimensioned contexts, gross margin.
10. P2: `adjudicate.go` behind `EDGAR_FINANCIALS_LLM`.

Steps 1–8 are P0 and independently shippable; nothing before step 10 constructs an LLM
client.

---

## 15. Done when

Open `/company/0001567529` → Timeline tab → click the **2025-11-10** marker: the modal
shows **Revenue $47.0M (+12.6% YoY)**, **Basic EPS $0.09**, **Net income $5.3M**, **Cash
$72.0M**, from that accession's `financials.json` with every line `confidence: "verified"`.

And, with `EDGAR_FINANCIALS_LLM` unset, the **2024-05-08** marker (`24-040628`) shows
**no revenue metric at all** — not `$37,736` — with its `financials.json` recording the
line as `suspect`, both candidate values, and `detector_B` in `notes`. A `governance` 6-K
still shows "Financial highlights unavailable."

---

## 16. Risks and open questions

| Risk | Severity | Handling |
|---|---|---|
| `us-gaap:*` aliases (§4) are **unmeasured** — one IFRS issuer in the corpus | **High** | Ship as seeded-but-unvalidated; do not claim domestic-filer support until a second CIK is loaded |
| Detector B/C thresholds tuned on one observation each | **High** | Thresholds are named constants with the measurement in a comment; revisit on second issuer |
| A filer mis-tags *and* the chain is unavailable (first filing of a company) | Medium | Detector D cannot fire; line degrades to `single_source` at best, and B still guards magnitude |
| `scenario`-based dimensions instead of `segment` | Medium | Both are treated as dimensioned; unobserved but handled |
| Fixture instances are large; trimming could hide a parse bug | Low | `testdata/README.md` records the trim command; `--report` runs against the untrimmed corpus |

**Open questions (2, both deferred by design, neither blocking P0):**

1. **`P9M` year-to-date lines** — quarterly filings carry them and the modal has no slot.
   P0 extracts the `P3M` column only; revisit if the modal grows a YTD row.
2. **Second-issuer validation** — which CIK, and when. This is the single largest
   correctness unknown in the design and it cannot be closed with the corpus on disk.
