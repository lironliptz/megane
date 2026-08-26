# Financial-Results Lane on the Timeline Chart — Implementation Notes (as-built)

Implements `prompt_10_timeline_events_financial-lld.md` under the contract in
`prompt_10_timeline_events_financial-hld.md`. Both design docs were written in this same
session (the idea file had no HLD/LLD yet; its own "Doc chain" section asked for both before
implementing). Test level: **heavy**, at the user's explicit request.

---

## TL;DR

P0 and P1 both shipped. Financial reports (`quarterly_results`/`annual_report`) now render as
squares on a dedicated top lane, excluded from the general lane's major/medium/minor
datasets entirely (no duplicates). A new `afterDatasetsDraw` canvas plugin paints a short
revenue/EPS/net-income callout above financial markers, collision-aware, with a >12-marker
downgrade to revenue-only.

Verified against the **real Kamada corpus** on an isolated server (`:8197`, its own DB copy,
the user's `:8123` server never touched): all 66 real financial events partitioned correctly
with zero leakage into the general lane, the real `2025-11-10` event produced the exact
callout text the idea file's own example showed (`Rev $47.0M +13%`), and the real corpus
naturally exceeded the 12-marker downgrade threshold (23 financial events with highlights at
the `All` preset) — so the downgrade path was exercised with real data, not a fabricated
count. **One real bug was caught by this** (see Deviations) and fixed before the harness went
green.

31/31 harness assertions pass. No Go files touched — pure frontend (`tab-timeline.js`,
`style.css`).

---

## What changed (file by file)

| File | Change |
|---|---|
| `static/css/style.css` | Two new `:root` tokens: `--chart-financial-q` (`#3b82f6`), `--chart-financial-a` (`#1e3a8a`) |
| `static/js/company/tab-timeline.js` | `isFinancialReport()`; replaced the single `EVENT_LANE_*`/`eventLaneY()` with two independent bands (`GENERAL_LANE_*` bottom, `FINANCIAL_LANE_*` top) via a generalized `laneY()`; partitioned `visible` into `financialEvents`/`generalEvents` before dataset construction; new financial-lane dataset build (one per cadence, square markers, `financialLane: true` tag); new `financialCalloutPlugin` (`calloutFor`, `shortCallout`) registered via `Chart.register` |

Not touched: `internal/companyview/*`, any Go file, any migration, `static/company.html`
(legend is Chart.js-generated per the HLD's D3 — confirmed correct in the harness, no manual
key needed).

---

## Deviations from the design

**1. Fixed a real bug the LLD's own pseudocode had: the YoY sign was silently dropped for
positive percentages.** `shortCallout`'s original code was `text += ' ' + Math.round(parseFloat(pct[1])) + '%'`
— `Math.round()` on a positive parsed float (e.g. `12.6` from `"+12.6%"`) returns a plain
positive number, and JS never prints a `+` prefix on string-concatenation of a positive
number. So `"(+12.6% YoY)"` rendered as `"Rev $47.0M 13%"`, not the idea file's own example
`"Rev $47.0M +13%"` — the sign silently vanished for every positive figure (negative figures
were fine, since `-35` already prints its own minus sign). The harness caught this
immediately against the real `2025-11-10` value and four other real callouts in the sample
window (`+33%`, `+11%`, `+11%`, `+5%`). Fixed by explicitly re-adding `+` when the rounded
value is `>= 0`. This is exactly the kind of bug a hand-picked LLD-pseudocode example can
hide (if the author's own worked example happens to use round numbers) but real corpus data
exposes immediately — the reason `heavy` verification was worth doing here even though there
is no live third-party API in this feature.

**2. Clarified the P1 "priority table" as a waterfall, not simultaneous metrics.** The idea
file's wording ("EPS: ... if revenue callout fits"; "Net income: ... if still no overlap") is
ambiguous between "try EPS only if revenue is absent" and "show both when there's room." The
HLD (D6) and this LLD both read it as a waterfall for the **single** MVP callout line (revenue
→ EPS → net income, first available), reserving "more than one line" for P2 (design-only, not
built). Documented in the LLD before implementation, not discovered as a deviation during
coding — flagged here because it's a real interpretation call the idea file left open.

**3. "Major financial events only" (the idea file's other suggested >12 downgrade) was
rejected, not partially implemented.** Every `quarterly_results`/`annual_report` event is
already `WeightMajor` in `classify.go`'s `categoryRules`, so filtering the financial lane to
"major only" is a no-op — there is no non-major financial event to exclude. Revenue-only is
the only downgrade that actually reduces the label count. This was decided in the LLD (§3.6)
and confirmed by the harness: the real corpus's 23-with-highlights window downgraded to
exactly the revenue-only subset, with zero EPS/NI callouts surviving.

No other deviations — the two-lane Y-band split, the dataset partition mechanism, the
`financialLane` tag, and the plugin architecture all match the LLD as written.

---

## Tests

**Level: heavy**, per the user's explicit `/implement-ld ... heavy` request. Named
justification (per the command's own "can you name the specific thing a mock would miss"
bar): a hand-authored fixture for the callout formatter could easily reuse the same YoY-sign
bug in both the code and the fixture's expected value (exactly what happened in the LLD's own
pseudocode — see Deviation 1) and pass anyway. Only real corpus values, independently known
from prompt 8/9's own audits (`2025-11-10` → `$47.0M`, `+12.6% YoY`), could catch that
mismatch. The >12-marker downgrade path also needed a real count exceeding 12, which the
real corpus happens to supply (23) — a synthetic test would have had to fabricate that count
anyway, so using the real one costs nothing extra and proves more.

```
node --check static/js/company/tab-timeline.js     # clean, both before and after the fix
```

No Go changes, so no `go build`/`go test`/`make test` were affected by this work — not run
again beyond confirming `git status` shows no Go files touched.

### Definition of Done — verified against the live corpus

Isolated verification server on `:8197`, its own copy of `.db/megane.db` (the user's `:8123`
server never touched), stopped and its temp DB/binary deleted after testing. A 31-assertion Node harness (`vm` + fake DOM + mocked `Chart`, throwaway, not
committed) loaded the real `app.js` + `tab-timeline.js`, called the real `CompanyTabs`-
registered module against the live API for CIK `0001567529` at the `All` preset (66 real
financial events in view), and drove the real dataset-partition logic, the real
`financialCalloutPlugin.afterDatasetsDraw`, and the real delegated filter/click handlers.

| DoD element | Evidence |
|---|---|
| Squares on a dedicated top lane, others on the bottom | Financial Y range measured `0.965–0.980`; general Y range measured `0.030–0.130` — bands strictly disjoint, financial always above general |
| No duplicates | Zero financial-category points found in any general (WEIGHTS) dataset; all 66 real financial events land in exactly one of the two financial datasets |
| Quarterly = blue, annual = dark blue, square markers | `qDs.backgroundColor === '#3b82f6'`, `aDs.backgroundColor === '#1e3a8a'`, both `pointStyle === 'rect'` |
| Legend entries | Datasets carry `label: 'Quarterly report'` / `'Annual report'` — Chart.js's existing legend renders them automatically, no markup change needed |
| Prompt-7 filter gates both lanes | Unchecking `6-K\|quarterly_results` dropped the quarterly dataset from 55 points to 0; the 11 annual-report points were unaffected |
| `2025-11-10` shows a revenue callout | Exact text `Rev $47.0M +13%`, matching prompt 8/9's own audited figures ($47.0M revenue, +12.6% YoY rounded) |
| >12 downgrade to revenue-only | Real in-view count (23 financial events with highlights) exceeded the threshold; 22 callouts rendered (one skipped by the 80px gap rule), **all** revenue-only, zero EPS/NI |
| 80px collision rule | Measured minimum gap between rendered callouts: 132px — comfortably above the 80px floor, none skipped incorrectly |
| Modal unchanged | Clicking a real financial square (`2025-11-10`) opened the modal with the full `$47.0M` figure and a working "Open on SEC EDGAR" link present |
| Volume strip / log toggle unaffected | Volume dataset still present and populated; the log-scale checkbox still shows for this ticker (has real volume data) — no regression |

**Not verified by execution:** actual pixel-perfect visual spacing in a real browser (the
80px/collision math is verified against the harness's fabricated-but-consistent pixel
mapping, not a real Chart.js canvas layout at a real viewport width) and the legend's visual
wrapping at 1280px (HLD §6's flagged risk) — both need a real browser, which this environment
doesn't have. Flagged as a follow-up, not silently skipped.

---

## How to enable / roll back

Live once the static assets are served — no build step, no env var, no migration, no route
change. Purely client-side against the existing `/timeline` payload (`event.category`,
`event.highlights` were already on the wire from prompts 8/9).

**Roll back:** revert `static/js/company/tab-timeline.js` and `static/css/style.css`. Nothing
else references `financialLane`, `FINANCIAL_LANE_*`, or the callout plugin.

---

## Follow-ups

1. **Real-browser visual check not done** (see Tests, "Not verified by execution") — the
   80px collision gap and legend wrapping at 1280px should get an actual browser pass before
   calling this fully done end-to-end.
2. **P2 (second-line EPS/net-income callout)** remains design-only, per the idea file and HLD
   §5 — not built.
3. **The revenue-value regex is coupled to `HighlightsFromFinancials`'s current `ValueFmt`
   shape** (`"$47.0M (+12.6% YoY)"`) — same coupling risk the LLD's §6 Risks already named;
   this implementation didn't add a second independent parser, by design (per D6/D7's
   "withhold rather than guess" precedent from prompt 8).

---

## Touched files

**Modified (2):** `static/js/company/tab-timeline.js`, `static/css/style.css`

**New (3, this session's doc chain):** `prompt_10_timeline_events_financial-hld.md`,
`prompt_10_timeline_events_financial-lld.md`, this file

No commit was created.
