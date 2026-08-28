# HLD: Sell-Side Analyst Coverage & Research Tracking

Design for `prompts/dev/prompt_11_company_analysts.txt`.

Triage: **HEAVY** — new SQLite tables (first migration since v33), a new Go package, a new
parallel on-disk corpus, a fifth company tab, a new batch CLI, and an external data
dependency. Greenfield: no analyst code exists today.

The audit below is **measured** — against the repo, the corpus, and the actual response
shapes of the providers the idea file names — and it overturns the central assumption of
the spec.

> **Governing rule, inherited from prompts 8–9:** withholding beats guessing. Here it
> applies to *identity* as much as to numbers: a coverage row that names the wrong person,
> or implies a roster we cannot actually obtain, is worse than an honest firm-level row.

---

## 1. Objective

Let a user researching a company see **who covers it**, **what they say** (rating, price
target), and **how that changes over time**, stored durably enough to feed later extraction
and modelling.

Ship storage and a curation path first; treat automated discovery as a best-effort seed,
not a precondition.

---

## 2. Context — what exists today

| Layer | State | Path |
|---|---|---|
| Company identity | `submissions.json`; Kamada carries ticker **KMDA** (Nasdaq) | `fileDB/companies/{cik}/` |
| Filing corpus | `meta.json`, `financials.json` (prompts 8–9) | `{year}/{accession}/` |
| Timeline | Two marker bands on a hidden `yEvents` axis: financial 0.93–0.98, general 0.82–0.90 (prompt 10, **shipped**) | `internal/companyview/` |
| DB | Latest migration **33** (`stock_price_coverage`); `crawler_run_stats` at 7 | `internal/db/db.go` |
| Company tabs | `general`, `filings`, `events`, `timeline` | `static/js/company/shell.js` |
| Company API | `/api/companies/:cik{,/filings,/timeline}`, auth-gated group | `internal/handlers/router.go:86` |
| Analyst code | **none** | — |

The idea file's "append migration v34+" is correct: 33 is the current head.

---

## 3. Data audit (measured, not assumed)

### 3.1 The central finding: individual analyst names are not obtainable from the free tiers

The spec's Tier 2 is meant to "seed roster + latest rating/PT". Measured against the actual
documented response shapes:

| Provider / endpoint | Returns | Individual **person** name? |
|---|---|---|
| Finnhub `recommendation-trends` | `strongBuy, buy, hold, sell, strongSell, period, symbol` | **No** — aggregate counts |
| Finnhub `price-target` | `targetHigh, targetLow, targetMean, targetMedian, numberAnalysts, lastUpdated` | **No** — `numberAnalysts` is a *count* |
| Finnhub `upgrade-downgrade` | `gradeTime, fromGrade, toGrade, company, action, symbol` | **No** — `company` is the **firm** ("JP Morgan", "DA Davidson", "Bank of America") |
| FMP `grades` | `gradingCompany, newGrade, previousGrade, action, priceWhenPosted` | **No** — `gradingCompany` is the **firm** ("J.P. Morgan") |

Finnhub's field is even *documented* as "Company/analyst who did the upgrade/downgrade",
which invites exactly this mistake — but every published sample value is a broker-dealer.

So the achievable ladder is:

| Wanted | Free/public tier | Reality |
|---|---|---|
| Rating distribution, consensus PT, analyst **count** | ✅ | Genuinely good |
| **Firm** names + rating actions over time | ✅ | Via upgrade/downgrade + grades |
| **Individual analyst** names | ❌ | Licensed (Tier 3) or manual only |
| **Report PDFs** | ❌ | Licensed or manual upload only |

**This breaks the proposed schema.** `company_analysts` declares
`analyst_name TEXT NOT NULL` with `analyst_key = lower(firm)|lower(name)`. Nothing in Tier 2
can populate it. Implemented as written, automated discovery would either insert rows with
a fabricated or placeholder person, or insert nothing at all — and a tab called "Analysts"
would sit empty while the data we *can* get (consensus, firm actions) goes unused. → D1, D2.

### 3.2 Tier 2's SEC option yields nothing in this corpus

The spec suggests scanning `fileDB` for research attached as EX-99 exhibits. Measured:

| | Count |
|---|---|
| PDFs anywhere in the corpus | 7 |
| …that are sell-side research | **0** |
| EX-99 exhibits | 166 `.jpg`, 82 `.htm`, 0 `.pdf` |

All seven PDFs are named `filename1.pdf` and sit under placeholder accessions
(`0000000000-*`, `9999999997-*`) — SEC correspondence and no-action letters, not research.
The EX-99 exhibits are press releases and investor-deck images. **Drop this source.** → D3.

### 3.3 Corpus scale

One company (`0001567529`), ticker on file. Everything below is designed for one company
and must not assume a populated multi-company roster exists to test against.

### 3.4 Timeline lane space is already committed

Prompt 10 shipped two bands on the hidden `yEvents` axis (0.82–0.90 and 0.93–0.98, after F3
raised the general lane). A third analyst band has roughly 0.03 of usable gap between them
and would crowd a chart that is already dense. → D8.

---

## 4. Architecture decisions

### D1 — The coverage entity is the **firm**; the analyst person is an optional attribute

Coverage is stored per `(cik, firm)`, with `analyst_name` **nullable**. A row with no
person is not a degraded row — it is the normal, honest result of automated discovery.

```sql
company_analysts(
  cik, firm NOT NULL, analyst_name NULL, analyst_key NOT NULL,
  status, discovered_via, first_seen_at, last_seen_at,
  UNIQUE(cik, analyst_key)
)
```

`analyst_key` = `slug(firm)` when no person is known, `slug(firm)|slug(person)` when one is.
When a person is later supplied for a firm-only row (manual entry, or a licensed feed), the
existing row is **upgraded in place** — its key is rewritten rather than a second row
inserted. The LLD must specify that upgrade as an explicit `UPDATE … WHERE analyst_key =
slug(firm)`, because the naive upsert produces a duplicate firm.

This also settles the spec's "duplicate analysts" pitfall for v1: identity is
firm-scoped, so an analyst moving firms correctly appears as coverage ending at one firm and
starting at another. A global person ID stays P4.

### D2 — Reframe P1 from "roster of people" to "consensus + firm coverage"

What Tier 2 delivers well is the aggregate, and it is genuinely useful: rating distribution,
consensus price target, analyst count, and dated firm-level rating actions. That is the P1
product, and the UI must say what it is.

The tab leads with a **consensus panel** (N analysts covering · rating split · PT
mean/median/range, each with an as-of date and the provider named), then a **firm coverage
table** (firm, latest action, latest rating, PT if known, first/last seen). Individual
names appear only where a human supplied them.

One line of UI copy carries the honesty: *"Firm-level coverage from {provider}. Individual
analyst names require a licensed feed or manual entry."* Without it the tab silently
implies we could not find the people, rather than that this source tier never has them.

### D3 — Three sources for v1, and the SEC scan is not one of them

| Tier | Source | Gives | Status |
|---|---|---|---|
| 1 | Manual / admin + CSV-JSON import + PDF upload | Everything, including person names and documents | **P0** |
| 2 | Finnhub or FMP (env-gated, one implementation) | Consensus + firm actions | **P1** |
| 3 | Refinitiv / FactSet / Bloomberg | Person names + full PDFs | Interface only |
| 4 | Aggregator scraping | — | **Off. Not built.** |

§3.2 removes the SEC EX-99 scan. Tier 4 stays unbuilt rather than "default off": a
per-site ToS review is a prerequisite this design does not perform, and shipping dormant
scraper code invites someone to flip the flag without it.

Pick **one** Tier 2 provider for P1 behind the `Provider` interface. Both return the same
shape of thing; implementing two is duplicated work before either is validated.

### D4 — Documents are a manual path in v1; do not promise PDFs

No free tier returns research PDFs (§3.1). The on-disk layout the spec proposes is right,
but in v1 it is populated **only** by admin upload or import — reachable, private, and
legally clean. The batch fetcher (spec P2) has nothing to fetch until a Tier 3 contract or
a URL list exists, so it is scoped to *import*, not *crawl*.

```
fileDB/companies/{cik}/analyst-reports/{report_id}/
  report.json      metadata mirror (firm, person?, date, rating, PT, sha256, source)
  document.pdf     or document.html — manual/import only in v1
  analysis.json    P3
```

This mirrors prompts 8–9 exactly: sidecar JSON beside the artifact, SQLite for the queryable
metadata, `fileDB` gitignored.

### D5 — Coverage bookkeeping mirrors `stock_price_coverage`; never fetch on page load

`analyst_coverage(cik PRIMARY KEY, status, provider, last_attempt_at, last_success_at,
next_retry_after, note)` — the same shape as migration 33, and the same rule that made the
price path safe: a tab open reads the DB and nothing else. Refresh is `cmd/analyst-reports`
only, recorded in the existing `crawler_run_stats` (migration 7) under tool name
`analyst-reports`.

A company with no ticker returns an empty roster with `note: "no ticker on file"`, matching
the timeline's existing behaviour.

### D6 — Migration v34, append-only, three tables

One migration adding `company_analysts`, `analyst_reports`, `analyst_coverage`, appended to
`versionedMigrations` after 33. Existing entries are never edited — the repo's standing
rule. Naming is `analyst_*` throughout so nothing collides with `filedb.Coverage`, which
means *filing date span* and is a genuinely confusable term (the spec flags this; it is
worth honouring in every identifier).

### D7 — API follows the existing company group verbatim

| Method | Path | Notes |
|---|---|---|
| GET | `/api/companies/:cik/analysts` | Consensus + firm rows |
| GET | `/api/companies/:cik/analyst-reports` | Paginated, `from`/`to`/`limit`/`offset` |
| GET | `/api/companies/:cik/analyst-reports/:reportId` | Metadata + document link |
| POST | `/api/admin/companies/:cik/analyst-reports` | Admin upload (P0) |

Registered in the existing auth-gated `apiCompanies` group (`router.go:86`), CIK through
`filedb.NormalizeCIK`, `{"data": …, "error": null}` envelope. Document download is a
**separate authenticated route** — never a static path (D9).

### D8 — Defer the timeline overlay; prefer a dedicated pane if it ships

The `yEvents` axis already carries two bands with ~0.03 of gap (§3.4). Adding a third is the
lowest-value, highest-risk part of this prompt: it touches the chart prompt 10 just
stabilised, for data that the `#analysts` tab already displays better.

If it ships, it is **opt-in** (`?include=analysts`, default payload unchanged) with a
distinct marker shape, and prompt 10's F1 "dedicated financial-metrics pane" is the better
precedent than a third band. **Recommend P2-or-later, after the tab has proven the data.**

### D9 — Research is private by default

Sell-side research is licensed material. Documents are served only through an authenticated
route, never a public static path, and never a CDN. `report.json` records `source` and, when
API-sourced, the provider — so a later licence question can be answered per artifact.
Redistribution limits on Tier 2 data (both providers restrict it) mean the consensus panel
shows the provider name and is not exported wholesale. Admin upload is trusted-admin-only;
virus scanning is out of scope and stated as such.

---

## 5. What this touches

| Area | New / Changed |
|---|---|
| `internal/db/db.go` | **+1 migration (v34)** |
| `internal/db/analysts.go` | **new** — query helpers |
| `internal/analysts/` | **new** — `provider.go`, `manual.go`, `<tier2>.go`, `ingest.go`, `roster.go`, `schema.go` |
| `internal/handlers/companies.go`, `router.go` | 3 GET routes + 1 admin POST |
| `cmd/analyst-reports/` | **new** — `import`, `refresh`, `--gaps` |
| `static/company.html`, `shell.js`, `tab-analysts.js`, `style.css` | fifth tab |
| `fileDB/companies/{cik}/analyst-reports/` | **new artifact tree** (gitignored) |
| `.env.example` | provider key + TTL, off by default |
| `internal/companyview/` | **unchanged** in P0–P1 (D8) |

Nothing in prompts 8–10 changes. No existing table is altered.

---

## 6. Out of scope

- Tier 4 scraping (not built, not flagged-off).
- SEC EX-99 research scanning (§3.2 — no such documents exist here).
- Automated PDF crawling (D4 — nothing to crawl in v1).
- Insider / 13F ownership, news sentiment without named attribution.
- P3 LLM extraction and P3 prediction export — designed for, not built.
- Global cross-firm person identity (P4).

---

## 7. Risks

| Risk | Severity | Mitigation |
|---|---|---|
| **Tab promises a roster the data cannot deliver** — the spec's framing implies named people | **High** | D1 firm-first schema, D2 reframed panel + explicit UI copy |
| A person name is inferred from a firm-level row and attributed to a real individual | **High** | `analyst_name` populated only from human input; never derived. Misattributing a rating to a named person is a reputational harm to a third party |
| Licence breach redistributing Tier 2 or licensed research | **High** | D9 auth-only documents, provider recorded per artifact, no bulk export |
| Third timeline lane destabilises the chart prompt 10 just shipped | Medium | D8 defers it; opt-in and distinct marker if it lands |
| Provider rate limits / page-load fetching | Medium | D5 coverage row + batch-only refresh, mirroring the price path |
| Single-company corpus — no multi-company roster to test against | Medium | Fixtures for P0; live provider behind a key, fixture-recorded for CI |
| `coverage` overloaded (filing span vs analyst coverage) | Low | D6 `analyst_*` naming throughout |

---

## 8. Done when

Open `/company/0001567529#analysts`. The tab shows a **consensus panel** — "N analysts
covering · Buy/Hold/Sell split · PT mean, median, range", each labelled with its as-of date
and the provider — above a **firm coverage table** listing the firms with rating actions on
record, newest first.

Two negative cases carry the design:

- A firm discovered automatically shows **no analyst name** and the tab explains why, rather
  than showing a blank column that reads as missing data.
- A company with no ticker on file shows "no ticker on file", the same as the timeline —
  not an empty table.

And the curation path: an admin uploads a research PDF with firm, analyst name, date, rating
and price target; it appears in the reports table, its document opens through an
authenticated route, and `fileDB/companies/{cik}/analyst-reports/{id}/report.json` records
its SHA-256.

---

## 9. Deliverables & phasing

1. `prompt_11_company_analysts-lld.md` — exact v34 SQL, `Provider` interface, the
   firm-only→person upgrade path (D1), API JSON shapes, tab wireframe, fixture plan.
2. **P0** — migration, `internal/db/analysts.go`, manual import + admin POST, on-disk
   artifact + SHA-256 dedupe.
3. **P1** — three GET endpoints, `#analysts` tab (consensus panel + firm table), one Tier 2
   provider behind the interface, coverage row + batch refresh.
4. **P2** — `cmd/analyst-reports` import/refresh, `crawler_run_stats` wiring.
5. **P3+** — `analysis.json` extraction (prompt/struct/UI three-surface sync), consensus
   snapshot export, timeline overlay if D8's bar is cleared.

Ship P0 → P1 before any extraction, per the spec's own instruction.

### Open questions (2)

1. **Which Tier 2 provider.** Finnhub and FMP are equivalent for this purpose; the choice
   should follow whichever licence permits displaying consensus to authenticated users.
   Needs a licence read, not an engineering decision.
2. **Whether firm-level-only coverage clears the product bar.** If the intent was
   specifically *named people*, then Tier 1 manual curation is the only path and Tier 2
   should be dropped from P1 rather than shipped as a partial answer. Worth settling before
   the LLD.

---

## References

- Finnhub upgrade/downgrade — [finnhub.io/docs/api/upgrade-downgrade](https://finnhub.io/docs/api/upgrade-downgrade), [finnhub-go model](https://github.com/Finnhub-Stock-API/finnhub-go/blob/master/model_upgrade_downgrade.go)
- Finnhub recommendation trends — [finnhub.io/docs/api/recommendation-trends](https://finnhub.io/docs/api/recommendation-trends)
- FMP stock grades — [site.financialmodelingprep.com/developer/docs/stable/grades](https://site.financialmodelingprep.com/developer/docs/stable/grades)
- FMP upgrades & downgrades — [upgrades-and-downgrades-api](https://site.financialmodelingprep.com/developer/docs/upgrades-and-downgrades-api/?direct=true)
- Prior art in this repo: `prompt_8_extracting_info_from_filings-hld.md` (sidecar + gate),
  `prompt_9_extra_source_for_financial_reports-hld.md` (provider + cache + coverage row),
  `prompt_10_timeline_events_financial-hld.md` (marker lanes)
