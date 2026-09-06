# Model Inputs, Ranked — What to Feed a Price/Outlook Model

Research note, not a feature spec. Written to inform prompts 11+ and any future prediction
work. Measured against this repo on 2026-08-30, with Kamada `0001567529` (381 filings,
2016-01-06 → 2026-08-20) as the **development fixture** — one company for practice, with the
corpus scaling to as many issuers as the work needs.

---

## 0. Two caveats that outrank every input below

**A. The target must be defined first.** "Predict the stock" is not a target. Next-day
reaction to a 6-K, 90-day post-earnings drift, and 12-month total return have almost
disjoint top-fives. This note assumes **short-horizon reaction and drift around company
events** (1–90 days), because that is what the timeline is built around. State it explicitly
before building, or the feature set will be right for a question nobody asked.

**B. Point-in-time discipline is the difference between a model and a fantasy.** Consensus
estimates get restated, financials get amended, tickers get reassigned. Every input below
must be stored **as it was known on the date it was known** — exactly what prompts 8–9's
accession-keyed artifacts already do, and exactly what a naive "fetch current consensus"
call would destroy. This is the most common way a backtest lies to its author, and it is
much cheaper to design in now than to retrofit across a large corpus.

The single-company corpus is a *practice fixture*, not a constraint on the design. What
follows assumes breadth arrives; §2 is about building so that it can.

---

## 1. The ranking

Ranked by **(evidence strength × availability) ÷ engineering cost**.
"Have it" = in the corpus or DB today. **X-sec** marks inputs that only become meaningful
once the corpus holds many companies.

| # | Input | Evidence | X-sec | Have it? | Cost |
|---|---|---|---|---|---|
| 1 | **Earnings surprise** — actual vs consensus (revenue, EPS) | Strong | — | Actuals ✅ / consensus ❌ | Low once consensus exists |
| 2 | **Sector & peer classification** (SIC, filer status) — the enabler for every relative feature | Foundational | ✅ | **✅ free, on disk** | Trivial |
| 3 | **Forward guidance** — issued, raised, cut, withdrawn | Strong | — | In EX-99 prose, unextracted | Medium |
| 4 | **Price & volume history** — returns, volatility, liquidity, abnormal volume | Strong (the *label* source) | — | ✅ shipped | Done |
| 5 | **Market & sector factor returns** — to turn raw returns into *abnormal* returns | Strong | ✅ | ❌ | Low |
| 6 | **Estimate revisions** — direction, velocity, and **breadth across peers** | Strong | partly | ❌ | Medium |
| 7 | **Event type & timing** — filing category, cadence, off-cycle 6-Ks | Moderate | — | ✅ shipped | Done |
| 8 | **Balance-sheet durability** — cash, burn, runway, debt | Strong for small/mid-cap | — | ✅ extracted | Low |
| 9 | **Sector-conditional domain events** — clinical/FDA for pharma, rig counts for energy, … | Strong *within* a sector | — | ❌ | Medium per sector |
| 10 | **Short interest** | Moderate | — | ❌ | Low |
| 11 | **Segment / product mix shifts** | Moderate | — | ✅ partially | Low |
| 12 | **Insider open-market purchases** (code `P` only) | Moderate, asymmetric | — | ⚠️ see §5 | Medium |
| 13 | **Analyst rating / PT changes** | Weak–moderate | — | ❌ | Low |
| 14 | **Institutional ownership (13F)** | Weak (45-day lag) | ✅ | ❌ | Medium |
| 15 | **News sentiment** | Weak, expensive to do well | — | ❌ | High |
| 16 | **Social / retail chatter** | Noise at this market cap | — | ❌ | High |

---

## 2. What breadth unlocks — and what to build now so it works

Four things do not exist at n=1 and are the main reason to design for scale early.

### 2.1 Relative beats absolute

A +12.6% revenue surprise means nothing in isolation. Its **percentile within its sector**
in that quarter is the feature that carries signal. The same is true of margin change,
revision direction, and every ratio. Once the corpus has peers, most raw features should be
replaced by their cross-sectional rank — which requires storing the raw value per company
per period, which is what prompts 8–9 already do. **No change needed; just don't collapse
history into "latest".**

### 2.2 Peer grouping is already free — measured

`submissions.json` already carries everything needed to group companies, with no external
classification vendor:

```
sic                  2834
sicDescription       Pharmaceutical Preparations
ownerOrg             03 Life Sciences
category             Accelerated filer
fiscalYearEnd        1231
exchanges            ['Nasdaq']
```

SIC gives the peer set, `ownerOrg` gives a coarser sector, `fiscalYearEnd` is required to
align periods across companies (comparing a June-FY company to a December-FY one on
"FY2025" is a real and easy mistake), and `category` (filer status) drives filing deadlines.
This is why classification ranks **#2**: it costs nothing and every relative feature depends
on it.

### 2.3 Abnormal return, not raw return

With many companies the label should be the **excess** return over market and sector on the
event window. A 6-K that "moved the stock +4%" on a day the sector rose 4% carries no
information. This needs a market index and a sector aggregate — the latter computable from
the corpus itself once enough peers exist, so it is cheap. Skipping this makes every label
partly a proxy for beta.

### 2.4 Survivorship is the trap that only appears at scale

If the universe is built from *today's* listed companies, every delisted, acquired, and
bankrupt issuer is silently excluded, and the backtest inherits a strong upward bias. EDGAR
retains filings for companies that no longer trade, so the fix is available: **build the
fetch list from historical constituents, not current ones**, and keep companies in the
corpus after they stop trading. This is invisible at n=1 and expensive to correct after the
corpus is large.

---

## 3. Why the top of the list looks like that

### #1 Earnings surprise — and why it reframes prompt 11

The most robustly documented equity anomaly is **post-earnings-announcement drift**: prices
keep moving in the direction of a surprise for weeks after the print. Surprise is a
*difference*, so it needs two terms:

- **Actual** — already extracted, verified, point-in-time, accession-keyed (prompts 8–9).
- **Consensus** — **not held anywhere in this system.**

That is the gap, and it means prompt 11's consensus panel is not a nice-to-have tab: **it is
the missing denominator of the highest-value input.** Finnhub/FMP consensus is the cheapest
path to it — a far stronger argument for that prompt's Tier 2 than "show the user a rating
split."

The retrofit question splits in two, and an earlier draft of this note got it wrong by
treating both halves the same:

- **Historical EPS surprise is retrofittable.** Vendors return, per past quarter, the
  estimate *as it stood at that announcement* alongside the actual. One call per company
  recovers the whole history — no prior snapshotting required. This is the #1 feature, and
  it is cheaper than previously stated.
- **Forward estimates and revision velocity (#6) are not.** Those endpoints return only
  *today's* view. Every week without a snapshot is a week of revision history that cannot
  be recovered later. This is the genuinely perishable half, and the reason to start
  snapshotting before the corpus grows.

Both halves still need an `as_of` date stored per row — see §4 for why that matters more
than it first appears.

### #3 Guidance — often the bigger mover than the print

Forward guidance ("raising full-year revenue guidance to $158–162 million") frequently moves
the stock more than the quarter itself. Measured: that exact sentence sits in an EX-99
exhibit in this corpus, unextracted — the same exhibits prompt 9's adjudicator already pulls
unit-explicit prose from. Guidance issued / raised / cut / withdrawn is a small extraction
with high value, and an LLM is genuinely good at it.

### #9 Sector-conditional domain events — reranked

In the previous version this sat at #5 as "clinical/regulatory", because the fixture company
is a plasma-derived pharma business where a Phase 3 readout can reprice the equity more than
a decade of earnings. That is still true **for pharma**, and ClinicalTrials.gov and openFDA
are free and structured.

But in a multi-sector corpus it is one of a family of sector-specific feeds, each valuable
only within its sector. It moves to #9 as a *class*, while staying near the top for any
healthcare-heavy universe. Which sector to build first should follow the corpus composition
— if the universe is mostly pharma, build it early.

---

## 4. Consensus — where it comes from, and why it is not objective

**Sourcing, measured.** Tested 2026-08-31:

| Source | Gives | Depth | Cost | Status |
|---|---|---|---|---|
| **Finnhub** `/stock/earnings` | EPS `actual`, `estimate`, `surprise`, `surprisePercent` | Deep history | Free key, per-minute limit | Fields confirmed from the official Go client's `EarningResult` model |
| **FMP** `analyst-estimates` / earnings | **Revenue + EPS** estimates | Historical + forward | Free key, **250 calls/day** | Documented |
| **Alpha Vantage** `EARNINGS` | EPS `reportedEPS`, `estimatedEPS`, `surprise` | 122 quarters for IBM | Free key, very low daily cap | **Tested live**, HTTP 200 |
| **Nasdaq** `/api/company/{sym}/earnings-surprise` | EPS actual + consensus | **4 quarters only** | **No key** | **Tested live**, HTTP 200 |
| Nasdaq `/revenue` | — | — | — | **Tested: "Data not available"** |

**Revenue consensus is the binding gap.** Finnhub's free earnings endpoint is EPS-only and
Nasdaq has no revenue; FMP is the only free route to revenue estimates, and its 250/day cap
is what decides feasibility. A watchlist of ~200 names fits in a day; a 1,000-name universe
needs roughly 17 minutes on Finnhub for EPS but **four days** on FMP for revenue — which is
where a paid tier stops being optional.

**Consensus is a vendor construct, not a measured quantity.** Same company, same quarters,
two vendors (IBM, 2026-08-31):

| Quarter | Actual (both agree) | Alpha Vantage est. | Nasdaq est. |
|---|---|---|---|
| 2026-06-30 | 2.93 | 2.93 | 2.93 |
| 2026-03-31 | 1.91 | 1.81 | 1.81 |
| **2025-12-31** | 4.52 | **4.29** | **4.33** |
| **2025-09-30** | 2.65 | **2.45** | **2.44** |

The **actuals agree exactly; the consensus disagrees in half the quarters.** On Dec-2025
the implied surprise is 0.23 against 0.19 — a ~20% relative difference in the modelled
feature, arising purely from vendor choice.

It cannot be otherwise. Consensus is a statistic over a population each vendor defines
differently: the **contributor panel** (which firms submit), the **staleness rule** (how
recently an estimate must have been revised to count), **basis normalization** (vendors
restate contributors onto a house definition of EPS — GAAP vs street, stock comp,
intangible amortization, one-offs), **mean vs median and outlier trimming**, **as-of
timing** (consensus drifts daily), **share count and FX** (diluted assumptions, ADR ratios,
reporting currency — live issues for an Israeli IFRS filer), and **fiscal alignment**.

And vendors **revise history**: I/B/E/S analyst records have been documented as altered
retroactively (Ljungqvist, Malloy & Marston, *Rewriting History*, Journal of Finance 2009).
So "historical consensus" pulled today may not equal what was on screen then — a direct
threat to §0 caveat B that no amount of local discipline can fix.

### The rules this implies

1. **Surprise = vendor actual − vendor estimate.** Both sides on one basis, internally
   consistent. This is the number to model.
2. **The SEC-extracted actual is a basis check, not the numerator.** This corrects an
   earlier draft. IBM's `reportedEPS` of 2.93 is *street* EPS, not GAAP; computing
   `our GAAP actual − vendor street estimate` manufactures a surprise out of a basis
   mismatch. Use the comparison to *classify* the vendor: if vendor actual ≈ our extracted
   actual, the consensus is on a reported basis; if it diverges, the vendor is on street
   basis. Either is usable — mixing them is not.
3. **Match on (period end **and** duration).** Measured on the fixture company: joining on
   period end alone compared the 20-F's full-year EPS of 0.35 against a Q4 consensus of
   0.09, manufacturing a ~289% surprise from nothing. `financials.json` already carries
   `period.duration` (`P3M`/`P1Y`), so this costs nothing to get right.
4. **One vendor per feature, never mixed.** The cross-vendor spread above is noise that
   would sit on top of an already-weak signal.
5. **Prefer scaled surprise (SUE-style)** — surprise divided by its own historical standard
   deviation for that company. A systematic vendor basis offset largely cancels, making the
   feature robust to exactly this ambiguity.
6. **Store `vendor` and `as_of` on every row**, so a later vendor switch is detectable
   rather than silently corrupting a backtest.

---

## 5. Insider transactions — the measured reality

The evidence is real but **narrower than the folklore**: open-market insider *purchases*
(Form 4 code **P**) carry modest predictive power; *sales* are mostly uninformative
(diversification, tax, 10b5-1 scheduling); grants and vesting carry essentially none.

Measured on the fixture company:

| | |
|---|---|
| Form 4 filings | 26 (all parse cleanly) |
| Form 3 filings | 20 |
| Transactions extracted | 227 |
| Transaction codes present | **`A` 118, `D` 109** |
| Code **P** (open-market purchase) | **0** |
| Code **S** (open-market sale) | **0** |
| Transactions carrying a price | 218 of 227 |
| Insider history depth | **2026-03-13 → 2026-08-06 (≈5 months)** vs 10.6 years of filings |

1. **Every insider event currently on disk is compensation mechanics** — awards and
   dispositions to the issuer, not conviction trades. Feeding these to a model adds noise.
   An implementation must filter to `P`/`S` and treat `A`/`D`/`F`/`M` as equity-comp events
   (useful for dilution accounting, not direction).
2. **The history is 5 months deep** because the EDGAR fetch only pulled recent Forms 3/4 —
   the filings exist upstream and were never requested. `submissions.json` even flags
   `insiderTransactionForIssuerExists`, so the backfill target is knowable per company.

Worth building — free, structured XML, already half-parsed, and the `ownership.xml` schema
is stable — but **#12, not top-five**, and with the code filter stated up front. Across many
companies the `P` population will be thin but non-zero, which is exactly the case where
breadth converts a useless single-name feature into a usable one.

---

## 6. Scaling constraints — measured, and they bind

| Constraint | Measured | Implication |
|---|---|---|
| **SEC fair-access limit** | **10 requests/second**, enforced **per IP across all EDGAR domains** (`www`, `data`, `efts`), regardless of how many machines you run | The fetcher must be globally rate-limited, not per-worker. Exceeding it throttles the IP until the rate stays down for 10 minutes |
| **Disk per company** | **647 MB**, 4,422 files | ~650 GB per 1,000 companies as currently fetched |
| **…of which `.txt`** | **386 MB (60%)** | The full-submission `{accession}.txt` files duplicate documents already stored individually. Dropping them cuts the corpus ~60% |
| **…of which `.jpg`** | **78 MB (12%)** | Investor-deck images; no modelling value |
| **Company Facts** | 1.1 MB, one fetch per CIK | Already correct — cached per company, not per accession (prompt 9) |
| **Price rows** | 2,673 per symbol (~10.6y daily) | Trivial; ~2.7M rows at 1,000 companies |

Pruning `.txt` and `.jpg` takes a company from 647 MB to roughly **180 MB** — 650 GB down to
180 GB at a thousand names. Worth deciding **before** a large fetch, since re-fetching to
prune is far more expensive than not storing it.

---

## 7. What I would build next, in order

1. **Consensus** (prompt 11 Tier 2) — the highest-leverage single addition. Two jobs, not
   one: a **backfill** of historical EPS surprise (retrofittable, one call per company) and
   a **recurring snapshot** of forward estimates (perishable — start it first). Pick one
   vendor and record it per row (§4).
2. **Corpus expansion** — with the fetch list built from *historical* constituents (§2.4)
   and `.txt`/`.jpg` pruned (§6). Both decisions are cheap now and costly later.
3. **Guidance extraction from EX-99** — reuses prompt 9's prose-passage machinery.
4. **Peer-relative feature layer** — SIC grouping is already on disk; convert absolute
   metrics to sector-relative ranks, with `fiscalYearEnd` alignment.
5. **Abnormal-return labels** — market/sector adjustment on event windows.
6. **Form 4 parser with `P`/`S` filtering + backfill.**
7. **Clinical/FDA calendar** — early if the universe stays healthcare-weighted.

Deliberately *not* near-term: news sentiment, social, 13F — expensive per unit of signal,
and two of the three are laggy by construction.

---

## 8. Honest limits

- Published anomalies decay after publication; PEAD and insider-purchase effects are weaker
  now than in the studies that established them.
- The #1 feature rests on a number that is **defined, not measured** (§4). Results will
  shift with vendor choice, and a vendor's retroactive edits to history are outside your
  control. Report which vendor a result came from.
- Every input here is weak individually. Combining weak signals across a broad cross-section
  is what works — which is the real argument for corpus breadth, over and above having more
  rows.
- Backtest results on a corpus you assembled yourself will be optimistic. Reserve a holdout
  period from the start and do not look at it.
- None of this is investment advice. The defensible product is a research aid — a way to
  read filings and spot deviations faster — not a trading signal.

---

## References

- Consensus sourcing — [Finnhub `EarningResult` model](https://github.com/Finnhub-Stock-API/finnhub-go/blob/master/model_earning_result.go), [FMP Financial Estimates](https://site.financialmodelingprep.com/developer/docs/stable/financial-estimates), [FMP limits](https://site.financialmodelingprep.com/faqs)
- Vendors revising history — Ljungqvist, Malloy & Marston, "Rewriting History", *Journal of Finance* 64(4), 2009
- SEC fair access / rate limits — [Accessing EDGAR Data](https://www.sec.gov/search-filings/edgar-search-assistance/accessing-edgar-data), [new rate control limits](https://www.sec.gov/filergroup/announcements-old/new-rate-control-limits)
- Prior art in this repo: `prompt_8_extracting_info_from_filings-hld.md` (point-in-time
  sidecar artifacts), `prompt_9_extra_source_for_financial_reports-hld.md` (provider cache +
  coverage row), `prompt_11_company_analysts-hld.md` (consensus source tiers)
