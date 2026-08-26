# SEC Company Facts as a Second Source — Implementation Notes (as-built)

Implements `prompt_9_extra_source_for_financial_reports-lld.md` (and its HLD).
Test level: **standard**.

---

## TL;DR

Shipped in full. `internal/edgar/financials` gained a SEC Company Facts client, a
concept/period mapper with per-accession scale reconciliation, and a gap-fill pass.
Coverage for the reference company went from **15 → 66 artifacts**:

| Source | Count |
|---|---|
| `both` (prompt 8 local, instance + rendered) | 14 |
| `xbrl_instance` (local, mis-tagged filing — revenue still withheld) | 1 |
| `sec_companyfacts` (gap-filled) | 8 |
| `none` (explicit "no XBRL was filed" record) | 43 |
| **Total qualifying accessions** | **66 / 66** |

The governing rule held under the sharpest test available. `0001213900-23-085500` is
mis-tagged **in SEC's own feed** — the API reports its revenue as `37,934`. It publishes as
**$37.9M**, never `$37.9K`, corrected by cross-accession reconciliation with the correcting
factor and the corroborating accession recorded in the artifact.

`--gaps` classifies all 66 accessions with **zero network requests**. 16 new tests, all
green; no test performs a live request.

---

## What changed (file by file)

### New — `internal/edgar/financials/`

| File | What it does |
|---|---|
| `companyfacts.go` | `Client` (injectable `*http.Client`, explicit `UserAgent`, sentinel errors), atomic disk cache at `{root}/companies/{cik}/.sec/companyfacts.json`, TTL, `PadCIK`, `CachePath` |
| `companyfacts_map.go` | `conceptKey` (reuses `elements.go` ranks), `factIndex` (by accession **and** by period), unit filter, `selectPeriod`, `pickEntry`, `priorEnd`, `priorInstant`, `resolveScale`, `plausibleMagnitude`, `gradeEntry` |
| `fillgaps.go` | `ClassifyGaps` (offline), `FillGaps`, `fillOne`, `noFactsArtifact`, `writeArtifact` |
| `companyfacts_test.go` | 16 tests |
| `testdata/companyfacts/` | 18 KB trimmed fixture + `README.md` |

### Changed

| File | Change |
|---|---|
| `schema.go` | `SourceCompanyFacts`, `SourceNone` |
| `extract.go` | `metaJSON` gains `Form`/`FilingDate`; `Options` gains `WriteNoFacts`. `ExtractAll` untouched |
| `cmd/edgar-financials/main.go` | `--gaps`, `--companyfacts`, `--fill-gaps`, `--no-facts-records`; `printGaps`, `clientFromEnv`; gap-fill line in the summary |
| `.env.example` | `SEC_EDGAR_USER_AGENT` (no default), `SEC_COMPANYFACTS_TTL=168h` |
| `internal/companyview/timeline_dod_test.go` | Extended for the three prompt-9 markers |
| `prompt_8_..._-hld.md` | D8's "366/381 will never have a `financials.json`" revised (LLD build step 8) |

`elements.go`, `filedb`, `companyview` production code, `static/`, routes and the DB are
**unchanged**. No migration.

---

## Deviations from the design

**1. `priorInstant` added — balance comparatives aligned to prior year-end.**
The LLD said only "balance lines carry `PriorLabel` and no YoY". Implemented literally via
the shared `priorEnd` helper, gap-filled cash compared against the prior-year *quarter*
(`vs 2023-06-30`) while prompt 8's local path uses the prior year-**end** (`vs 2024-12-31`).
Two adjacent timeline events would then disagree about what "prior" means, with no way for
a reader to tell. Both instants are present in the feed, so `priorInstant` prefers the
prior fiscal year-end and falls back to ~1 year earlier. Caught by the DoD run, not by a
unit test.

**2. `FillGaps` accepts a nil client when nothing is fillable.**
The LLD has it construct a client unconditionally. Since `ClassifyGaps` is offline and
`GapNoXBRL` accessions need no facts, a corpus with only unfillable gaps now writes its
no-facts records without a client — which is also what makes `TestFillGapsNeverOverwritesLocal`
and `TestNoFactsRecordIsNotPublishable` possible without a fake server.

**3. Stale cache is preferred over a hard failure.**
Not specified. If SEC is unreachable or throttling and a cached document exists — even past
its TTL — it is used rather than failing the run. Gap-fill is a batch enrichment; aborting
because a free public service is briefly unavailable is worse than using week-old facts.
Freshness is recorded in every line's notes.

**4. `hasLocalArtifact` inspects line sources rather than testing file existence.**
The LLD's D1 says "skip when a trusted local artifact exists". Once this pass writes its own
`financials.json`, a existence check would classify an API-filled or no-facts accession as
`GapHasLocal` on the next run and freeze it. The check therefore looks for a line whose
source is `xbrl_instance`/`rendered_html`/`both`/`llm_adjudicated`, so re-runs stay correct.

**5. Test expectations in the DoD harness are keyed by accession, not date.**
Two filings share 2024-03-06: the 20-F annual report and the 6-K press release announcing
it. The 20-F correctly carries figures, so a date-keyed assertion for "no metrics" was
ambiguous and failed against correct behaviour. This was a defect in my test, not the code.

**Open decision taken:** `Line.Element` for API-sourced lines is set to `sec:{key}` — the
feed identifies a concept by namespace and name, but the mapper has already collapsed those
onto a canonical key, and re-deriving the original concept string would be cosmetic. The
exact fact period is recorded in `Notes` instead.

---

## Tests

**Level: standard**, and why: the SEC response *shape* is the real risk, and it is not
mocked away — the fixture is the live 1.1 MB document, trimmed. `httptest` stands in only
for the transport (status codes, header, caching). **No test makes a network request.** The
one live call was the product flow itself (`--companyfacts`), not a test.

```
CGO_ENABLED=1 go test ./internal/... ./cmd/...
```

```
ok  megane/internal/companyview        1.358s
ok  megane/internal/edgar/financials   0.470s
ok  megane/internal/filedb             0.136s
ok  megane/internal/handlers           0.097s
...all packages ok
```

`gofmt -l` clean and `go vet` clean on every file this change touched. (22 pre-existing
files elsewhere in the repo are unformatted; none were touched here.)

**16 new tests.** The ones carrying the design:

| Test | Guarantees |
|---|---|
| `TestClientRequiresUserAgent` | Refuses with `ErrNoUserAgent` **and makes zero requests** — asserted with a call counter |
| `TestClientSendsUserAgentAndCaches` | Header sent; second call served from disk; expired mtime refetches |
| `TestClientRateLimitedAndNotFound` | 429/404 → sentinels, status checked before the body is parsed |
| `TestUnitFilterExcludesILS` | The ILS `10,000,000,000` fact never enters the index — **with a `t.Fatal` guard that fails if the fixture stops carrying it**, so it cannot pass vacuously |
| `TestPeriodSelectionPicksTheQuarter` | Q2 2024 → 42,472,000, not the 68,153,000 prior-year half-year |
| `TestResolveScaleDetectsMisTaggedFiling` | factor 1000, evidence names the corroborator |
| `TestResolveScaleIsPerAccession` | All monetary metrics share one factor |
| `TestCleanFilingIsNotRescaled` | The other direction — no spurious correction |
| `TestMisTaggedFilingPublishesCorrectedValue` | Renders `$37.9M`, is publishable, records `x1000` |
| `TestEPSIsNeverScaled` | Per-share stays 0.07 |
| `TestClassifyGapsOffline` | local/fillable/no_xbrl; non-qualifying categories ignored |
| `TestFillGapsNeverOverwritesLocal` | Local artifact byte-identical after a fill run |
| `TestNoFactsRecordIsNotPublishable` | `Publishable()` empty, reason recorded |
| `TestCompanyFactsWouldResolvePrompt8Suspect` | Records the HLD D9 finding — see follow-ups |

Verified with `-v` that none of these skip.

### Definition of Done — verified

`TestTimelineDefinitionOfDone` runs the real read path (`ScanCompanyFilings` → `BuildEvents`
→ highlights) over the on-disk corpus. Actual output:

```
2024-08-14 (gap-filled): Total revenues $42.5M (+13.4% YoY) · Basic EPS $0.08 (+100.0% YoY)
                         · Net income $4.4M (+144.3% YoY) · Cash $56.5M (vs 2023-12-31: $55.6M)
2023-11-13 (mis-tagged):  Total revenues $37.9M (+17.7% YoY) · ... · Cash $52.6M (vs 2022-12-31: $34.3M)
2025-11-10 (local):       Total revenues $47.0M (+12.6% YoY) · ... unchanged by this prompt
0001213900-24-020275:     no metrics — no XBRL filed
```

The 2024-08-14 marker showed "Financial highlights unavailable." before this change.
`--gaps` reports `qualifying=66 local=15 fillable=8 no_xbrl=43 (no network used)`, matching
the HLD audit exactly.

**Not verified by execution:** the browser step. `tab-timeline.js` is unchanged and the
payload it receives is asserted above, but no browser was driven.

### Live run

```
$ SEC_EDGAR_USER_AGENT="..." go run ./cmd/edgar-financials --root ./fileDB --cik 0001567529 --fill-gaps --all
accessions=15 written=15 skipped=0 suspect_metrics=7 suspect_filings=1
gap-fill: filled=8 no_facts_records=43 skipped_local=15 withheld_metrics=0
```

One HTTP request, 1,112,965 bytes, cached at
`fileDB/companies/0001567529/.sec/companyfacts.json`. All eight fills graded `verified`;
the six chain cross-checks from HLD §3.4 still agree with prompt 8's independent local
extraction.

---

## How to enable / roll back

```bash
export SEC_EDGAR_USER_AGENT="megane/1.0 you@example.com"   # required; no default
go run ./cmd/edgar-financials --root ./fileDB --cik 0001567529 --gaps        # offline survey
go run ./cmd/edgar-financials --root ./fileDB --cik 0001567529 --fill-gaps   # fetch + fill
```

`--gaps` never fetches. `--fill-gaps` fetches once per CIK and reuses the cache for
`SEC_COMPANYFACTS_TTL` (default 168h). Local artifacts are never overwritten.

**Roll back:** delete the API-sourced and no-facts artifacts (those whose lines carry
`source: "sec_companyfacts"`, or whose `parseNotes` start `source: none`) plus
`fileDB/companies/*/.sec/`. Locally extracted artifacts are untouched, and every reader
treats absence as normal.

---

## Follow-ups

1. **HLD D9 — let Company Facts adjudicate prompt 8's suspect lines.** `24-040628` still
   shows `total_revenues: $37.7K / suspect`, withheld, even though
   `TestCompanyFactsWouldResolvePrompt8Suspect` proves the feed identifies it as mis-tagged
   by exactly 1000 and `25-042940` reports the correct figure. The override path is out of
   scope for v1 by design. **This should land before `EDGAR_FINANCIALS_LLM` is ever
   enabled** — it is deterministic, and it removes the adjudicator's main use case.
2. **`us-gaap` mapping remains unmeasured.** Unchanged from prompt 8; this document's
   `us-gaap` namespace holds 12 concepts, none revenue-class. A domestic filer is the test.
3. **`plausibleMagnitude` is untested against a real case.** No fill in the corpus reaches
   the uncorroborated path, so the median heuristic is covered only by construction. It is
   the weakest rule in the package.
4. **No-facts records are written for `GapNoXBRL` only within qualifying categories.** The
   other ~315 accessions are untouched, which is intended but means "artifact present" is
   not a corpus-wide invariant.
5. **A `--reconcile` mode** for when EDGAR later publishes a viewer bundle for a filing
   already gap-filled (LLD open question 1). Today the artifact stays `sec_companyfacts`
   until `--all`.

---

## Touched files

**New**
```
internal/edgar/financials/{companyfacts,companyfacts_map,fillgaps}.go
internal/edgar/financials/companyfacts_test.go
internal/edgar/financials/testdata/companyfacts/{CIK0001567529.trimmed.json,README.md}
prompts/dev/prompt_9_extra_source_for_financial_reports-{hld,lld}.md
fileDB/companies/0001567529/.sec/companyfacts.json          (cache, gitignored)
fileDB/companies/0001567529/*/*/financials.json             (51 new artifacts, gitignored)
```

**Modified**
```
internal/edgar/financials/schema.go        SourceCompanyFacts, SourceNone
internal/edgar/financials/extract.go       metaJSON fields, Options.WriteNoFacts
cmd/edgar-financials/main.go               --gaps/--companyfacts/--fill-gaps/--no-facts-records
internal/companyview/timeline_dod_test.go  prompt-9 DoD markers
.env.example                               SEC_EDGAR_USER_AGENT, SEC_COMPANYFACTS_TTL
prompts/dev/prompt_8_..._-hld.md           D8 coverage claim revised
```

### Fixture regeneration

```python
import json
cf = json.load(open('fileDB/companies/0001567529/.sec/companyfacts.json'))
KEEP_ACCN = {'0001213900-24-068559','0001213900-24-097126','0001213900-23-085500',
             '0001213900-25-042940','0001213900-24-040628'}
KEEP_CONCEPT = {'Revenue','BasicEarningsLossPerShare','ProfitLoss','CashAndCashEquivalents'}
out = {'cik': cf['cik'], 'entityName': cf['entityName'], 'facts': {}}
for ns, concepts in cf['facts'].items():
    for c, node in concepts.items():
        if c not in KEEP_CONCEPT: continue
        units = {}
        for u, es in node['units'].items():
            sel = es if u == 'ILS' else [e for e in es if e['accn'] in KEEP_ACCN]
            if sel: units[u] = sel
        if units: out['facts'].setdefault(ns, {})[c] = {'units': units}
json.dump(out, open('internal/edgar/financials/testdata/companyfacts/CIK0001567529.trimmed.json','w'), indent=1)
```

Other modified files visible in `git status` (`internal/marketdata/*`, `static/*`,
`cmd/server/main.go`, `internal/admin/*`, `internal/handlers/*`) are pre-existing work from
earlier prompts and were not touched by this implementation.
