# Deterministic Financial Extraction from SEC Filings — Implementation Notes (as-built)

Implements `prompt_8_extracting_info_from_filings-lld.md` (and its HLD).
Test level: **standard**.

---

## TL;DR

P0, P1 and P2 all shipped. `internal/edgar/financials` reads XBRL instance facts as the
primary source, corroborates them against the SEC-rendered HTML, screens both through six
anomaly detectors, and writes `financials.json` beside each accession's `meta.json`.
`cmd/edgar-financials` ran over the real corpus and produced **15/15** artifacts whose
values match HLD §3.2 exactly.

The governing rule holds end to end: **14 filings publish, 1 withholds.** The filing whose
issuer mis-tagged its own XBRL (`0001213900-24-040628`, revenue tagged `37736` with
`decimals="0"`) has all seven monetary metrics withheld rather than rendered 1000× wrong.
Its EPS still publishes, because per-share units are unaffected.

23 new tests, all green. Definition of Done verified through the real read path.

---

## What changed (file by file)

### New package — `internal/edgar/financials/` (~2,100 lines + tests)

| File | What it does |
|---|---|
| `schema.go` | `FilingFinancials`/`Line`/`Segment`, `Publishable()` as the single publication predicate, `FormatValue`, byte-stable `Write`, `Load` returning `(nil, nil)` when absent |
| `elements.go` | Canonical keys → ranked XBRL element aliases; `MetricRank`; statement membership |
| `instance.go` | Instance discovery, dual-dialect parsing, unit/context/period resolution, fact selection, segment extraction |
| `summary.go` | `FilingSummary.xml` → statement role → report file. No R-number appears in code |
| `rendered.go` | `R*.htm` reader addressed by `defref_` element name; caption scale; phantom-column drop |
| `verify.go` | Detectors A–F, tolerances, `Grade` (the two-source gate) |
| `extract.go` | `ExtractAccession`, three-pass `ExtractAll`, chain check, writer, idempotency |
| `adjudicate.go` | `Adjudicator` interface and request/result types |
| `adjudicate_llm.go` | `LLMAdjudicator` over `llm.Client`, evidence gathering, response validation |
| `testdata/` | Five trimmed real fixtures + `README.md` (2.6 MB) |

### New command — `cmd/edgar-financials/main.go`

`--root --cik --all --dry-run --report --max-suspect-filings`. `--report` prints the
extraction table and exits non-zero past the threshold, so it doubles as the acceptance
harness.

### Read path

| File | Change |
|---|---|
| `internal/filedb/models.go` | `FilingRow.Financials *financials.FilingFinancials` with `json:"-"` — the filings API payload is unchanged |
| `internal/filedb/scan.go` | One `financials.Load(dir)` in `readAccession`; a bad file warns and leaves the field nil, exactly like a bad `meta.json` |
| `internal/companyview/highlights.go` | New `HighlightsFromFinancials` (pure, filters by `Publishable`); `BuildHighlights` gains a third parameter and prefers financials, else the existing growth-% regex |
| `internal/companyview/timeline.go:170` | Passes `r.Financials` |
| `prompts/edgar_financials_adjudicate.txt` | Adjudicator prompt (written in the design phase) |
| `.env.example` | `EDGAR_FINANCIALS_LLM`, `..._MODEL=gemini-3.5-flash-lite`, `..._MAX_EVIDENCE` |

`static/js/company/tab-timeline.js` is **untouched** — it already renders `metrics[]`.
No DB migration, no route change, no DTO change.

---

## Deviations from the design

Six, each forced by measurement against the real corpus.

**1. `Σ segments = total` dropped as an invariant (LLD §7.1 detector C).**
The LLD claimed it held 14/14. That measurement was taken against *rendered R3
sub-sections*, which is a different quantity from the XBRL segment axes. Measured on the
real facts, `0001213900-23-067979` reports `ReportableSegments` 34.94M + `AllOtherSegments`
6.50M against a 37.44M total — IFRS segment disclosures include inter-segment revenue that
eliminates on consolidation, so the sum legitimately exceeds the total.
*Now:* reconciliation **selects** which axis to publish; a breakdown that does not
reconcile is dropped with a parse note, and the consolidated figure keeps the grade its own
evidence earned. Condemning consolidated revenue on this basis would have withheld correct
numbers on three filings.

**2. Detector D matches comparables by period date, not list position.**
The LLD said "the previous comparable filing". Implemented literally, this compared
list-adjacent filings — but the corpus has gaps (Q3'22, then Q2'23, then Q1'24), so almost
nothing is a year apart and the detector fired on nearly every filing (this was 25 of the
first 50 false suspects). *Now:* the prior comparable is looked up by
`endDate − 1 year ± 14 days`, and when no filing covers that period the check simply does
not run.

**3. Chain breaks no longer condemn both sides when one is already explained.**
A break says two filings disagree, not which is wrong. Because `24-040628` is mis-tagged,
the literal rule also withheld `25-042940`'s perfectly good revenue. *Now:* when detector B
has already located the fault on one side, only that side is marked.

**4. Duplicate facts are deduped before detector E.**
Inline XBRL routinely tags the same value more than once in the same context (statements
and an exhibit). The LLD treated ">1 candidate" as ambiguity, which fired on 14/15 filings.
*Now:* identical values collapse; only genuinely differing values reach detector E.

**5. Acceptance threshold counts suspect *filings*, not metrics.**
The LLD's "≤1 suspect metric corpus-wide" was an estimate and was wrong: a filer who
mis-tags does so for every monetary figure in the filing, so the one bad filing yields
7 withheld metrics. Both counts are reported; the flag is `--max-suspect-filings` (default
1, currently passing at exactly 1).

**6. No YoY across a sign flip (not in the design at all).**
Caught by the DoD run: Q1 2024 swung from $(0.04) to $0.04 a share, and the formula
rendered `-200.0% YoY` on what was a return to profitability. `yoyPct` is now emitted only
when the base period is positive; the figure still publishes on its own.

Two smaller additions, both required to run at all and neither contradicting the design:
a `CharsetReader` (the pre-2022 instances declare `US-ASCII`, which Go's decoder refuses),
and root-element sniffing in `FindInstance` (accession folders also hold `ownership.xml`
and `primary_doc.xml`, which match the filename rules but are not instances; sniffing
`<xbrl>` took the match count from 66 to exactly the right 15).

**Open decision taken:** a single-member segment "breakdown" equal to the total is not a
breakdown, so it is dropped.

---

## Tests

**Level: standard**, and why: the only third-party dependency is the Gemini adjudicator,
which is P2 and whose logic is *validation* — fully exercised with a fake. Everything
load-bearing is the real filings on local disk, so the DoD is verified against the actual
corpus rather than mocks. **No live API calls were made.**

```
CGO_ENABLED=1 go test ./internal/... ./cmd/...
```

```
ok  megane/internal/admin              2.863s
ok  megane/internal/companyview        1.379s
ok  megane/internal/edgar/financials   0.456s
ok  megane/internal/filedb             (cached)
ok  megane/internal/handlers           (cached)
...all packages ok
```

`gofmt -l` clean; `go vet` clean on every touched package.

**23 new tests.** The ones that carry the design:

| Test | Guarantees |
|---|---|
| `TestConsolidatedNotSegment` | "Total revenues" appears 3× in Q3'25 (once consolidated, twice in segments) and the consolidated figure wins |
| `TestDetectorBIsolatesMisTaggedFiling` | The mis-tagged filing is withheld **and** detector B stays silent on all four clean fixtures |
| `TestSuspectLinesAreNotRendered` | A suspect metric never reaches the modal even with a plausible value |
| `TestStatementFilesNotHardcodedRNumbers` | `fy_2024_20f` resolves balance=R3/income=R4 where hardcoding breaks |
| `TestRenderedPhantomColumn` | The mixed-currency caption's empty column does not shift values |
| `TestParseInstanceDialects` / `TestUnitResolution` | `xbrli:`-prefixed US-ASCII classic parses like default-ns inline; units resolved by measure, not id |
| `TestGradeTruthTable` | Full publication-gate truth table |
| `TestChainCheckMatchesByPeriod` | Comparables matched by date; a gap-quarter is not treated as the comparable |
| `TestAdjudicationValidation` | Unknown id, invented value, non-verbatim quote, empty quote, `"low"` → all withheld |
| `TestGatherEvidenceIsProseOnly` | The flattened table in the same exhibit is excluded from evidence |
| `TestNoYoYAcrossSignFlip` | Deviation 6 |

### Definition of Done — verified

`TestTimelineDefinitionOfDone` runs the real path (`ScanCompanyFilings` → `BuildEvents` →
highlights) over the on-disk corpus. Market data is not in this path, so there is no
external dependency. Actual output:

```
2025-11-10 metrics: [{Total revenues $47.0M (+12.6% YoY)} {Basic EPS $0.09 (+28.6% YoY)}
                     {Net income $5.3M (+37.1% YoY)} {Cash and cash equivalents $72.0M (vs 2024-12-31: $78.4M)}]
2024-05-08 metrics: [{Basic EPS $0.04}]
```

The positive case matches the DoD exactly. The negative case is the point of the design:
the mis-tagged filing shows **no revenue metric**, not `$37,736`. Governance filings assert
nil highlights, so the existing "Financial highlights unavailable." path is intact.

**Not verified by execution:** the final browser step (clicking the marker and seeing the
modal). `tab-timeline.js` is unchanged and already renders `metrics[]`, and the payload it
receives is asserted above, but no browser was driven.

### Corpus acceptance

```
$ go run ./cmd/edgar-financials --root ./fileDB --cik 0001567529 --report
accessions=15 written=15 skipped=0 suspect_metrics=7 suspect_filings=1   # exit 0
```

All 15 revenue/prior/YoY/net-income/EPS/cash values match HLD §3.2.

---

## How to enable / roll back

**Deterministic extraction (default, no key needed):**
```bash
go run ./cmd/edgar-financials --root ./fileDB --cik 0001567529 --all
```
Idempotent — re-running without `--all` skips existing files. The timeline picks the data
up on the next `FILEDB_CACHE_TTL` expiry.

**LLM adjudication (off by default):** set `EDGAR_FINANCIALS_LLM=1` in `.env`. Model is
`gemini-3.5-flash-lite`. Note the wiring seam is present but `Options.LLM` is not yet
populated from the environment in `cmd/edgar-financials` — see follow-ups.

**Roll back:** `rm fileDB/companies/*/*/*/financials.json`. Every reader treats absence as
normal and falls back to the summary regex; no schema or route depends on the artifact.

---

## Follow-ups

1. **Wire `Options.LLM` from the environment in `cmd/edgar-financials`.** The adjudicator,
   its prompt, its validation and its tests are all in place, but the command does not yet
   construct an `LLMAdjudicator` from `EDGAR_FINANCIALS_LLM*`. Deliberate: the LLD puts
   this last, and enabling it needs a live-key run to verify, which is outside the standard
   test level. Roughly ten lines plus `pipeline.LoadPrompts`.
2. **Second-issuer validation — still the largest correctness unknown.** Every `us-gaap:*`
   alias in `elements.go` is unmeasured; the corpus is one IFRS foreign private issuer.
   Detector B's threshold and the scale fallback are each tuned on a single observation.
3. **`make test` fails on `generated/*/route_snippet.go`** — three code *snippets* that are
   not valid Go, present since the initial commit and untouched here. Verified pre-existing.
   Either give them a `//go:build ignore` tag or move them out of the module.
4. **P9M year-to-date lines** are extracted but have no slot in the modal (LLD open
   question 1).
5. `q1_2024`'s cash and total assets stay withheld even after revenue is adjudicated —
   adjudication is per-metric and only revenue has a unit-explicit press-release sentence.
   Correct, but worth revisiting if more metrics should be recoverable.

---

## Touched files

**New**
```
internal/edgar/financials/{schema,elements,instance,summary,rendered,verify,extract,adjudicate,adjudicate_llm}.go
internal/edgar/financials/financials_test.go
internal/edgar/financials/testdata/{q3_2025,q1_2024,fy_2024_20f,fy_2019_20f,q3_2022}/ + README.md
cmd/edgar-financials/main.go
internal/companyview/highlights_financials_test.go
internal/companyview/timeline_dod_test.go
prompts/edgar_financials_adjudicate.txt
fileDB/companies/0001567529/*/*/financials.json   (15 generated artifacts, gitignored)
```

**Modified**
```
internal/filedb/models.go          FilingRow.Financials
internal/filedb/scan.go            financials.Load in readAccession
internal/companyview/highlights.go HighlightsFromFinancials + BuildHighlights signature
internal/companyview/timeline.go   pass r.Financials (line 170)
internal/companyview/highlights_test.go  existing calls updated for the new parameter
.env.example                       EDGAR_FINANCIALS_LLM*
```

Other modified files in `git status` (`internal/marketdata/*`, `static/*`,
`cmd/server/main.go`, `internal/admin/*`) are pre-existing work from earlier prompts and
were not touched by this implementation.
