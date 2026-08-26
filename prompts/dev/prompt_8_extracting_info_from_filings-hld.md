# HLD: Deterministic Financial Extraction from SEC Filings

Design for `prompts/dev/prompt_8_extracting_info_from_filings.txt`.

Triage: **HEAVY** — new Go package, new on-disk artifact beside `meta.json`, an ingest
entry point that does not exist yet, and a change to a read path already shipped by
prompt 6. The data audit below is **measured against the real corpus** and overturns the
idea file's core mechanism.

> **Governing rule of this design:** a wrong number of the right order of magnitude,
> rendered cleanly in the modal, is the worst possible outcome — worse than no number.
> Every decision below is subordinate to that. **Withholding beats guessing.**

---

## 1. Objective

Extract real financial figures — Revenue + YoY, EPS, Net income, Cash — from each
`quarterly_results` / `annual_report` accession into a canonical `financials.json`, so
the timeline modal shows audited numbers instead of prompt 6's headline-regex growth
percentage.

Deterministic on the happy path. No network. Batch at ingest, never at page load. An LLM
is used **only to adjudicate flagged ambiguities**, never to author a number.

---

## 2. Context — what exists today

| Layer | State | Path |
|---|---|---|
| Corpus | 381 accessions, 1 CIK (Kamada `0001567529`), `meta.json` on **381/381** | `fileDB/companies/{cik}/{year}/{accession}/` |
| Ingest | **Python only** — no Go ingest exists | `python/edgar/build_meta.py` |
| Prompt 2 (`cmd/edgar` in Go) | HLD + LLD written, **not implemented** — no `internal/edgar/`, no `cmd/edgar/` | — |
| Read path | `ScanCompanyFilings` → `FilingRow` → `BuildEvents` → `Event.Highlights` | `internal/filedb/scan.go`, `internal/companyview/timeline.go:170` |
| Highlights | `BuildHighlights(category, summary)` — growth-% regex, `source: "summary_parse"` | `internal/companyview/highlights.go` |
| Modal | Already renders `highlights.metrics`; falls back to "Financial highlights unavailable." | `static/js/company/tab-timeline.js:765` |

The idea file assumes `cmd/edgar meta` exists to hang extraction off. **It does not.** → D10.

---

## 3. Data audit (measured, not assumed)

### 3.1 Addressable population is 15 accessions, not 66

| Slice | Count |
|---|---|
| Accessions total | 381 |
| `hasFinancials: true` | 66 |
| `isXBRL: true` | 35 |
| **Have `R*.htm`** | **15** |
| Have `FilingSummary.xml` | **15** (same 15) |
| **Have an XBRL instance document** | **15** (same 15) |
| `quarterly_results` with data | **8 / 55** |
| `annual_report` with data | **7 / 11** |

The 20 accessions that are `isXBRL: true` with no `R*.htm` are pre-2020 filings whose
folders hold only the primary doc and exhibits. **Structured financial data exists only
from 2020-02 onward** — ~2.4% of the corpus, but every results filing from Q3'22 forward.

### 3.2 Measured extraction result — 15/15

| Accession | Form | Revenue | Prior | YoY | Net income | Basic EPS | Cash |
|---|---|---|---|---|---|---|---|
| 0001213900-20-004782 | 20-F | $127.2M | $114.5M | +11.1% | $22.3M | 0.55 | $42.7M |
| 0001213900-21-011255 | 20-F | $133.2M | $127.2M | +4.8% | $17.1M | 0.39 | $70.2M |
| 0001213900-22-012308 | 20-F | $103.6M | $133.2M | −22.2% | −$2.2M | −0.05 | $18.6M |
| 0001213900-22-074381 | 6-K | $32.2M | $23.0M | +39.9% | $0.5M | 0.01 | $31.3M |
| 0001213900-23-020114 | 20-F | $129.3M | $103.6M | +24.8% | −$2.3M | −0.05 | $34.3M |
| 0001213900-23-067979 | 6-K | $37.4M | $23.6M | +58.7% | $1.8M | 0.04 | $21.8M |
| 0001213900-24-020272 | 20-F | $142.5M | $129.3M | +10.2% | $8.3M | 0.17 | $55.6M |
| 0001213900-24-040628 | 6-K | $37.7M | $30.7M | +22.9% | $2.4M | 0.04 | $48.2M |
| 0001213900-25-020360 | 20-F | $161.0M | $142.5M | +12.9% | $14.5M | 0.25 | $78.4M |
| 0001213900-25-042940 | 6-K | $44.0M | $37.7M | +16.6% | $4.0M | 0.07 | $76.2M |
| 0001213900-25-075264 | 6-K | $44.8M | $42.5M | +5.4% | $7.4M | 0.13 | $66.0M |
| 0001213900-25-107836 | 6-K | $47.0M | $41.7M | +12.6% | $5.3M | 0.09 | $72.0M |
| 0001213900-26-025934 | 20-F | $180.5M | $161.0M | +12.1% | $20.2M | 0.35 | $75.5M |
| 0001213900-26-055436 | 6-K | $45.2M | $44.0M | +2.8% | $4.1M | 0.07 | $32.9M |
| 0001213900-26-088030 | 6-K | $54.9M | $44.8M | +22.7% | $9.3M | 0.16 | $29.5M |

Two independent proofs: Q3'25 matches the idea file's hand-checked ground truth exactly
($47.0M vs $41.7M, NI $5.3M, EPS $0.09, cash $72.0M); and **chain validation** holds —
each filing's prior-period column equals the previous comparable filing's current column
across both series (127.2→133.2→103.6→129.3→142.5→161.0→180.5 annual;
25-042940 current $44.0M = 26-055436 prior $44.0M). A scale or row error breaks the
chain. It does not break.

**But arriving at this table required patching four separate silent-failure modes**, and
that is the finding that matters more than the table.

### 3.3 Why label-scraping is the wrong mechanism

The idea file's algorithm — match English row labels in `R*.htm`, infer scale from the
caption, index columns positionally — produced these failures on a 15-filing,
single-issuer corpus:

| # | Failure | Measured | Why it is dangerous |
|---|---|---|---|
| 1 | Hardcoded `R2`/`R3`/`R5` wrong: `25-020360` has `R2`=*Audit Information*, `R3`=balance, `R4`=income; cash flow is `R6` in **4/15** | 4/15 | Parses the wrong statement entirely |
| 2 | Caption scale `$ in Thousands` **absent** in `24-040628` while values are still thousands | 1/15 | **1000× error**, renders plausibly |
| 3 | Bare alias `revenues` prefix-matched `Revenues from proprietary products` → a **segment** figure returned as consolidated revenue (FY2019 read $97.7M instead of $127.2M) | ≥3/15 | Right magnitude, wrong number — invisible |
| 4 | Alias `basic earnings (loss) per share` never matches the real `Basic net earnings (loss) per share (in Dollars per share)` | 4/15 | Metric silently dropped |

Plus two the idea file does not mention: a **phantom empty column** from a mixed
`$ in Thousands, ₪ in Billions` caption that shifts positional indexing; and **segments
living inside R3**, not R20.

Defect 3 is the archetype of your concern: a plausible number, correct order of
magnitude, silently wrong. Patching each defect individually would leave the *class*
intact — the next issuer, or the next label rewording, reopens it. These are not four
bugs. They are four symptoms of one root cause: **`R*.htm` is a rendering, not a record.**

### 3.4 The record itself is present, and is authoritative

Every one of the 15 accessions ships the XBRL instance document — `*_htm.xml` (inline,
13 filings) or the classic `kmda-YYYYMMDD.xml` (2 pre-2022 filings). **Coverage 15/15.**

Each fact in it carries what the HTML only implies:

```xml
<ifrs-full:Revenue contextRef="c0" unitRef="usd" decimals="-3">40017000</ifrs-full:Revenue>
<ifrs-full:BasicEarningsLossPerShare contextRef="c0" unitRef="usdPershares" decimals="2">0.07</...>
```

And the rendered HTML corroborates it — every value row's first cell carries the same
element name in its drill-down anchor (`defref_ifrs-full_Revenue`), with segment blocks
opening on an `...Axis` row.

Mapped against the four defects:

| Defect | Resolved by | How |
|---|---|---|
| 1 — wrong statement | *n/a* | Facts are not organised by report file |
| 2 — scale | `unitRef` + absolute values | Values are already absolute (`40017000`); no caption inference, no per-share special-casing |
| 3 — segment as total | **context dimensions** | Consolidated = context with **no** `explicitMember`. Segment facts are dimensioned and structurally excluded |
| 4 — EPS label | **element name** | `ifrs-full_BasicEarningsLossPerShare` — closed taxonomy, language-independent |

Column selection (3M vs 9M vs 12M) likewise stops being a header-regex guess: the
context's period start/end **is** the duration. Non-USD is `unitRef`, not a caption scan
— the `₪ in Billions` facts carry `decimals="-9"` and a NIS unit, and are excluded by
unit, not by column position.

Element names need a small ranked alias set (`ifrs-full_ProfitLoss`, and
`ifrs-full_ProfitLossFromContinuingOperations` in 2/15; `us-gaap_*` equivalents for
domestic filers), but that set is **taxonomy-defined and closed**, unlike free-text
English labels.

### 3.5 Anomalies are detectable — measured, not hoped for

Your premise — *if we can tell we're in an edge case, we can route it* — holds. Six
signals, all deterministic:

| Signal | Catches | Measured on corpus |
|---|---|---|
| **A. Two-source disagreement** — instance value vs rendered value | scale, wrong row, wrong column | see below |
| **B. Implausible `decimals`/magnitude** — `decimals="0"` on a monetary fact with max < $1M | defect 2 | flags **exactly `24-040628`**; 14/15 clean — 1 flag, 0 false positives |
| **C. Calculation-linkbase invariant** — `MetaLinks.json` gives the filer's own `parentTag`/`weight`; check `revenue − cost = gross profit`, `Σ segments = total` | defect 3 | holds **14/14** where computable |
| **D. Cross-filing chain** — this filing's prior period vs the previous filing's current | scale, wrong row | holds **15/15** |
| **E. Element absent / multiple undimensioned candidates** | defect 4, ambiguity | 0 today |
| **F. Statement role unresolved in `FilingSummary.xml`** | defect 1 | 0 today |

Signal **B** is worth dwelling on because it is the one real trap in the corpus.
`24-040628`'s instance tags Revenue as `37736` with `decimals="0"` and `unitRef="usd"` —
i.e. the filer declared thirty-seven *thousand* dollars. Every other filing tags
`decimals="-3"` with absolute values (`40017000`). **The authoritative source is itself
wrong in 1/15.** So "read the XBRL" is necessary but not sufficient — which is precisely
why the two-source gate (D3) and the adjudication path (D6) exist rather than blind trust
in the instance.

And a **third independent source** exists exactly where it is needed: the EX-99 press
release for that filing states in plain English *"Net income was $2.4 million, or $0.04
per share, in the first quarter of 2024"* — unit-explicit, confirming the thousands
reading. Prose is the one input an LLM reads better than a parser. That is the concrete
basis for D6.

---

## 4. Architecture decisions

### D1 — The XBRL instance is the primary source; `R*.htm` is demoted to corroboration

Read facts from the instance document by **element name + context**, not labels from a
rendering. Discovery: `*_htm.xml`, else `{ticker}-{yyyymmdd}.xml` excluding the
`_cal`/`_def`/`_lab`/`_pre` linkbase suffixes. Coverage 15/15.

Selection rules, all structural:

- **Consolidated** = context carries **no** `explicitMember`. Dimensioned facts are
  segments and are never eligible for a headline metric. *(kills defect 3 at the root)*
- **Period** = context `startDate`/`endDate` duration → `P3M` / `P9M` / `P1Y`; instants
  for balance-sheet items. *(kills positional column guessing)*
- **Unit** = `unitRef`; non-USD facts are excluded, not converted. *(kills the ₪ trap)*
- **Metric** = ranked element alias list per canonical key. *(kills defect 4)*

This is **not** full XBRL taxonomy parsing — no presentation or calculation graph
traversal is required to select a fact. That remains out of scope (§6).

### D2 — `FilingSummary.xml` still resolves statement roles, for the corroborating read

R-numbers never appear in extractor code. Take `MenuCategory == "Statements"`, drop
`ShortName` containing `parenthetical`, map by phrase: `financial position`|`balance
sheet` → balance; `profit or loss`|`operations` → income; `cash flow` → cashflow.
Correct on 15/15 including the outlier that breaks hardcoding.

### D3 — Two-source agreement gate: nothing publishes on one reading alone

Every metric is read **twice, independently**: once from the instance (D1), once from the
rendered statement (D2 + element name via the `defref_` anchor). Then:

| Both sources | Outcome | `confidence` | Published? |
|---|---|---|---|
| Agree within tolerance | accept | `verified` | **yes** |
| Only one source has it | accept, note it | `single_source` | **yes** |
| Disagree, or any signal A–F fires | **suspect** → D6 | `suspect` | **no**, unless adjudicated |
| Neither has it | absent | — | no |

Tolerance: relative 0.5% for monetary values, exact to the cent for per-share. A
scale error is a 1000× disagreement — it can never pass this gate silently.

**The gate alone is not sufficient, and the corpus proves it.** On `24-040628` the two
sources *agree* — the renderer faithfully reproduces the mis-tagged instance, so both read
`37736`. Agreement therefore establishes that the two readers parsed the same fact, not
that the fact is right. Defect 2 is caught by detectors **B** and **D** (§3.5), not by
this gate. The gate covers wrong-statement, wrong-row, wrong-column and wrong-unit errors;
the detectors cover a filer who mis-tagged at source. Both layers are required — see the
detector→defect matrix in the LLD.

### D4 — Confidence is a gate, not a label

`confidence` exists to decide publication, not to decorate output. The read path
(`HighlightsFromFinancials`) filters to `verified` and `single_source` and **must not**
render `suspect`. A metric that cannot be trusted is absent from the modal, and the
existing "Financial highlights unavailable." path handles it. This is the mechanism that
enforces the governing rule at the top of this document.

### D5 — Ship all six anomaly detectors in P0, not as a later hardening pass

Signals A–F (§3.5) are cheap, deterministic, and each maps to a measured defect. B, C and
D in particular are near-free and independently catch the whole silent-wrong-number
class:

- **C** (`Σ segments = total`) is the direct structural refutation of defect 3.
- **D** (cross-filing chain) requires extraction to run corpus-wide before writing, so
  `Extract` is a **two-pass batch**: extract all candidates, then chain-validate, then
  write. This ordering is a P0 requirement, not an optimisation.

Detectors are what make D6's trigger meaningful. Without them the LLM has no way to know
it is needed — which is exactly why the idea file's "zero metrics extracted" trigger was
useless.

### D6 — LLM adjudication on `suspect` metrics only, as a bounded chooser

**This reverses the previous draft's D10.** That draft argued the fallback population was
zero — but measured the idea file's trigger ("deterministic returned nothing"), which is
indeed never true. The right trigger is *detected ambiguity*, whose measured population
today is **1/15**, and which is exactly the case that would otherwise publish a
1000×-wrong number.

**Model: `gemini-3.5-flash-lite`** (`EDGAR_FINANCIALS_LLM_MODEL`). Weakest sufficient
tier, and deliberately so — the task is reduced to picking one of N supplied numbers
against one quoted passage. There is no extraction, no arithmetic, no long-context
reasoning, so a lite model is not a compromise here; the constrained task is what makes
it adequate. Note the id must be `gemini-<major>…`: the dotted form
`gemini.3.5-flash-lite` fails `ValidateGeminiModelID` (`internal/llm/gemini.go:60`) and
would abort client construction. Verified accepted by the existing validator.

Prompt: `prompts/edgar_financials_adjudicate.txt`, loaded verbatim per `CLAUDE.md`'s
prompt-driven-logic rule.

#### Workflow

| # | Step | Detail |
|---|---|---|
| 1 | **Gate** | Only metrics marked `suspect` by D3/D5 enter. `verified` and `single_source` never call the LLM. Skipped entirely when `EDGAR_FINANCIALS_LLM=0` (default) — the metric is simply withheld |
| 2 | **Assemble candidates** | Each deterministic reading becomes `{id, value, unitBasis, source}` — e.g. `A` = instance `37736` (`as_tagged_usd`), `B` = rendered × 1000 = `37736000` (`assumed_thousands`) |
| 3 | **Gather evidence** | Grep the accession's EX-99 exhibits for prose sentences containing the metric's keywords **and** an explicit-unit pattern (`$N million`, `$N per share`). Reject candidate passages with ≥3 bare numbers in sequence — those are flattened tables and reintroduce the ambiguity. Truncate to `EDGAR_FINANCIALS_LLM_MAX_EVIDENCE` (4000 chars) |
| 4 | **Abort if no evidence** | No qualifying passage → withhold, no API call. The model must never be asked to choose without a source |
| 5 | **Call** | One request per disputed metric, `temperature: 0`, structured JSON response schema, concurrency bounded by the existing `MAX_CONCURRENT_LLM` |
| 6 | **Validate** | Response must parse; `choice` must be a supplied id or `"none"`; `value` must equal that candidate's value **byte-for-byte**; `evidenceQuote` must appear verbatim in the evidence sent. Any violation → treat as `"none"` |
| 7 | **Apply** | `choice != "none"` **and** `confidence != "low"` → metric becomes `confidence: "verified"`, `source: "llm_adjudicated"`, with the quote and model id in `parseNotes`. Otherwise the metric stays withheld |
| 8 | **Log** | accession, metric, candidates, choice, confidence, model, token counts — one line per adjudication, for audit |

#### Response contract

```json
{
  "choice": "B",
  "value": 37736000,
  "evidenceQuote": "Total revenues were $37.7 million in the first quarter of 2024, a 23% increase from the prior year period.",
  "reasoning": "Press release states Q1 2024 total revenues in millions, matching candidate B's unit basis.",
  "confidence": "high"
}
```

#### Worked example — `0001213900-24-040628`

Signal B fires on `total_revenues`: the instance tags Revenue `37736` with
`decimals="0"`, while the rendered table shows `37,736` under a caption with no scale
hint. Candidates are `A` = 37736 and `B` = 37736000 — a 1000× spread, both plausible on
their face.

Step 3 pulls the matching sentence from EX-99: *"Total revenues were $37.7 million in the
first quarter of 2024, a 23% increase from the prior year period."* Metric, period and
unit are all explicit, so prompt rule 2 is satisfied. The model returns `choice: "B"`.
Step 6 confirms the quote appears verbatim in what was sent and that `value` equals
candidate B exactly; the metric publishes as `llm_adjudicated`. With the flag off, it is
withheld — §8's negative case.

Note the same exhibit also contains a **table** dump reading `Total revenues 37,736
30,710 142,519` — unscaled and unit-less, i.e. the very ambiguity under dispute. That is
why step 3 sends prose sentences only: including that block would hand the model the
ambiguity instead of the resolution.

#### Guardrails

1. **Never authors a value** — multiple choice over supplied candidates, with a mandatory
   `"none"`. Step 6's byte-for-byte check makes a hallucinated number structurally
   impossible to publish.
2. **Never overwrites `verified`** — only `suspect` is eligible.
3. **Never sees raw tables** — prose passages only, which is the one input a small model
   reads more reliably than a parser.
4. **`"none"` is a success** — the prompt says so explicitly, so the model is not pushed
   toward a guess by helpfulness.
5. **Deterministic path ships first.** P0 acceptance is **≤1 suspect metric across the 15
   filings without the LLM**, that one withheld cleanly. The adjudicator is P2 and cannot
   mask a deterministic regression: if the suspect count rises, that is a parser bug to
   fix, not adjudication volume to absorb.

### D7 — Balance-sheet metrics carry no YoY

R2's columns are `Sep. 30 2025 | Dec. 31 2024 | Sep. 30 2024`; the instant contexts
confirm column 1 is the prior **year-end**, not the prior-year quarter. Cash emits
`value` + `priorValue` labelled `vs Dec 31 2024` and **omits** `yoyPct`. Only duration
(income-statement) facts get YoY.

### D8 — Absence is normal; `summary_parse` stays

Most accessions will never have a `financials.json` from *this* path. `BuildHighlights` becomes a
two-source resolver: financials when present and trusted, else the existing growth-%
regex, else `nil`. Prompt 6's `highlights.go` is **extended, not replaced**.

> **Revised by prompt 9.** This section originally read "366/381 accessions will never have
> a `financials.json`", which conflated "local XBRL is absent" with "no figures exist".
> Prompt 9 adds SEC Company Facts as a gap-fill source and writes explicit no-facts records
> for the remainder, so for the two qualifying categories every accession now carries an
> artifact: locally extracted, gap-filled, or an explicit record that no source holds facts
> for it.

### D9 — `BuildHighlights` stays pure; IO moves outward

Keep prompt 6 D6's side-effect-free, table-tested property:

```go
func HighlightsFromFinancials(f *FilingFinancials) *EventHighlight  // pure, filters by confidence
func BuildHighlights(category, summary string, f *FilingFinancials) *EventHighlight
```

`filedb` gains `LoadFinancials(...) (*FilingFinancials, error)` returning `(nil, nil)`
when absent; `FilingRow` gains an optional `Financials`. Loaded once during
`ScanCompanyFilings` (≤15 small reads per company, inside a walk that already stats every
accession) — not per request, not inside `companyview`.

### D10 — Ship a standalone command; do not block on prompt 2

Prompt 2's Go ingest does not exist. Ship `internal/edgar/financials/` (pure library) plus
`cmd/edgar-financials/` (batch runner, `--cik`, `--all`, `--dry-run`, `--report`). The
seam is `financials.ExtractAll(root, cik)` — two-pass per D5. When prompt 2 lands
`cmd/edgar meta` it calls that function and the standalone command retires.
`python/edgar/build_meta.py` is untouched.

### D11 — Segments come from dimensioned contexts

Segment lines are facts whose context carries an `explicitMember` on the products/services
axis — the same facts D1 excludes from headline metrics, now selected deliberately.
`R20` is never read. **P1**, gated behind P0.

---

## 5. What this touches

| Area | New / Changed | Notes |
|---|---|---|
| `internal/edgar/financials/` | **new** — `schema.go`, `instance.go` (D1), `summary.go` (D2), `rendered.go`, `elements.go`, `verify.go` (D5), `extract.go`, `adjudicate.go` (D6) | Pure; no network, no DB |
| `internal/edgar/financials/testdata/` | **new** — instance + `FilingSummary.xml` + `R*.htm` snippets | Must include `24-040628` (mis-tagged `decimals`) and `25-020360` (phantom column, R-shift, NIS facts) |
| `cmd/edgar-financials/` | **new** | D10 |
| `fileDB/.../financials.json` | **new artifact** | ≤15 files today |
| `internal/filedb/models.go`, `scan.go` | changed | `LoadFinancials`, optional `FilingRow.Financials` |
| `internal/companyview/highlights.go` | changed | D8/D9 — extend, keep pure, filter by confidence |
| `internal/companyview/timeline.go:170` | changed | pass `r.Financials` |
| `static/js/company/tab-timeline.js` | **unchanged** | already renders `metrics[]` |
| `prompts/edgar_financials_adjudicate.txt` | **new** | Adjudicator prompt (D6), loaded verbatim |
| `.env.example` | changed | `EDGAR_FINANCIALS_LLM`, `EDGAR_FINANCIALS_LLM_MODEL=gemini-3.5-flash-lite`, `EDGAR_FINANCIALS_LLM_MAX_EVIDENCE` |

No DB migration. No new endpoint. No change to `meta.json`.

---

## 6. Out of scope

- XBRL **taxonomy graph** traversal — presentation/definition linkbases. Only facts,
  contexts, units, and `MetaLinks.json` calculation weights (for signal C) are read.
- Prose extraction as a *primary* path — the only route to the 366 accessions with no
  structured data. LLM prose reading here is confined to adjudicating a flagged metric.
- Currency conversion. `currency` is recorded; non-USD facts are excluded.
- Extraction at request time, multi-company aggregation, chart callouts beyond the modal.
- Touching `python/edgar/build_meta.py`.

---

## 7. Risks

| Risk | Severity | Mitigation |
|---|---|---|
| Single-issuer corpus — every rule tuned to one IFRS foreign private issuer; `us-gaap_*` element aliases are **unmeasured** | **High** | D1's mechanism (element+context) is taxonomy-agnostic where labels were not; a second CIK is the real test and remains unvalidated. Flag explicitly before generalising |
| The authoritative source is itself wrong (`24-040628`) | **High** | D3 two-source gate + signal B + D6 adjudication; measured to isolate it 1/1 with no false positives |
| Silent wrong-magnitude value reaches the modal | **High** | D3 gate, D4 publication filter, signals B/C/D — three independent nets before render |
| LLM hallucinates an adjudication | Medium | D6 step 6 (byte-for-byte value match + verbatim quote check) makes an invented number unpublishable; multiple-choice only; never overwrites `verified`; flag-gated off by default |
| Adjudication becomes a crutch that hides parser regressions | Medium | D6 guardrail 5 — suspect count is a tracked P0 acceptance metric, measured with the LLM **off** |
| Chain validation needs corpus-wide ordering | Medium | D5 — `ExtractAll` is two-pass by design; single-accession extraction yields `single_source` at best |
| Feature looks broken because ~96% of markers show no financials | Medium | D8 fallback preserved; set expectations — 2020+ results filings only |
| `financials.json` drifts from a re-extracted corpus | Low | Idempotent write, `--all` to force, sorted keys, `--report` diffs against §3.2 |

---

## 8. Done when

Open `/company/0001567529` → Timeline tab → click the **2025-11-10** 6-K marker: the modal
shows **Revenue $47.0M (+12.6% YoY)**, **Basic EPS $0.09**, **Net income $5.3M**,
**Cash $72.0M**, sourced from that accession's `financials.json` with per-metric
`confidence: "verified"`.

And the negative case, which matters equally: with `EDGAR_FINANCIALS_LLM` unset, the
**2024-05-08** 6-K (`24-040628`, the mis-tagged filing) shows **no revenue metric at
all** rather than `$37,736` — its `financials.json` records the metric as `suspect` with
both candidate readings and the firing signal in `parseNotes`. A `governance` 6-K still
shows "Financial highlights unavailable."

---

## 9. Deliverables

1. `prompt_8_extracting_info_from_filings-lld.md` — file-by-file plan, element alias
   tables, context-selection rules, detector thresholds, fixture list.
2. `prompts/edgar_financials_adjudicate.txt` — **written**; adjudicator prompt per D6.
2. `internal/edgar/financials/` + table tests covering **all six** measured failure modes.
3. `cmd/edgar-financials`, idempotent, with `--report`.
4. `financials.json` for 15/15 accessions; values matching §3.2; **≤1 suspect metric
   corpus-wide, withheld cleanly**.
5. `BuildHighlights` two-source resolver + confidence filter + tests for every path.
6. `go test ./internal/edgar/... ./internal/companyview/... ./internal/filedb/...` green.

### Build order

1. `schema.go` + `elements.go` (canonical keys → ranked element aliases) + unit tests.
2. `instance.go` — fact/context/unit reader; undimensioned + duration selection (D1).
3. `summary.go` (D2) + `rendered.go` — corroborating read via `defref_` anchors.
4. `verify.go` — signals A–F; assert B flags only `24-040628`, C holds 14/14, D holds 15/15.
5. `extract.go` — two-pass `ExtractAll`, agreement gate (D3), writer.
6. `cmd/edgar-financials`; run corpus-wide; diff `--report` against §3.2.
7. `filedb.LoadFinancials` → `HighlightsFromFinancials` (confidence filter) → timeline.
8. Manual smoke: both cases in §8, including the withheld one.
9. **Only then** P1: cash flow, segments via dimensioned contexts (D11), gross margin.
10. **Only then** P2: `adjudicate.go` — evidence grep, candidate assembly, response
    validation (D6 steps 2–7) — behind `EDGAR_FINANCIALS_LLM`, model
    `gemini-3.5-flash-lite`.
