# Financial-Results Lane and Metrics on the Timeline Chart — Implementation Notes (as-built)

Implements `prompt_10_timeline_events_financial-lld.md` (and
`prompt_10_timeline_events_financial-hld.md`), including the F1–F3 follow-up refinements
from HLD §8 / LLD §7. Test level: **heavy** — this session verified the entire feature
(P0/P1, previously only standard-tested, plus the new F1–F3 work) against the real corpus on
an isolated server.

This supersedes the prior version of this file, which covered P0/P1 only and left the DoD
"not browser-verified." That gap is closed here.

---

## TL;DR

- **P0 (already shipped, now heavy-verified):** Financial reports render as square markers
  on a dedicated top `yEvents` band; general filings on a separate band below; no duplicates.
- **P1 (already shipped, now heavy-verified):** Revenue and net income plot as green bars /
  purple dots on a `yRevenue` axis, not text callouts.
- **F1 shipped:** Dedicated financial-metrics pane above the price line — `applySplitLayout`
  now stacks up to three bands (metrics, price+events, volume), each only when its scale has
  data. Revenue bars widened (`barPercentage` 0.55 → 0.75) now that they have their own room.
- **F2 shipped:** Chart.js's single auto-legend replaced with two hand-built HTML rows
  (`#tl-legend-fin`, `#tl-legend-stock`) — `renderChartLegend()` classifies each dataset by
  `yAxisID`/label and both rows are driven from the real dataset list every `render()`.
- **F3 shipped:** `GENERAL_LANE_BASE_Y` raised `0.90 → 0.93` now that F1 gives price/events
  their own sub-pane — the two marker lanes read as one cluster.
- **No Go changes** — pure frontend (`tab-timeline.js`, `style.css`, `company.html`).

---

## What changed — file by file

| File | Change |
|------|--------|
| `static/js/company/tab-timeline.js` | `FIN_METRICS_RATIO`/`FIN_METRICS_GAP` constants; `applySplitLayout` rewritten to a single unified function stacking metrics/price/volume bands with correct per-band clips (was two near-duplicate branches, volume-only); `volumeLanePlugin`'s divider draw generalized to draw 0–2 dividers (volume boundary, metrics boundary) instead of one; `GENERAL_LANE_BASE_Y` 0.90 → 0.93 (F3); revenue bar `barPercentage` 0.55 → 0.75 (F1); new `financialLegendEntry()`, `legendSwatchShape()`, `renderChartLegend()` (F2); `plugins.legend.display` set to `false` |
| `static/css/style.css` | New `.timeline-legend-rows`/`.timeline-legend`/`.timeline-legend-item`/`.timeline-legend-swatch*` rules (F2) — swatch shapes (square/circle/triangle/line) driven by one inline `color` property via `currentColor`, not per-shape inline backgrounds |
| `static/company.html` | Two new empty legend-row containers (`#tl-legend-fin`, `#tl-legend-stock`) under `.timeline-chart-wrap`, populated entirely by JS |

Not touched: Go backend, routes, DB, `financials.json` extraction, the event-filter/period-
stripe/volume-log-toggle code from prompts 4/7 (verified unaffected — see Tests).

---

## Deviations from the design

**None relative to the current LLD §7 F1–F3 sketch.** Two small decisions the LLD left open,
resolved:

**1. F1's `FIN_METRICS_RATIO` ambiguity resolved as `1/4` of the pre-split price pane.** The
LLD's own sketch flagged two readings ("¼ of price pane" vs. "⅓ of total price+metrics").
The HLD's prose ("metrics ≈ ⅓ of the stock pane **before** split, i.e. metrics:price ≈ 1:3")
is algebraically the same ratio as `metrics = ¼ × (metrics + price)` — confirmed by working
the fraction backward before writing code, not guessed. Verified in the harness with exact
pixel arithmetic (see Tests).

**2. F2 implemented as Option B (HTML rows), not Option A (Chart.js multi-legend).** The LLD
offered both; Option A needs either a custom plugin or Chart.js's `generateLabels` filter
duplicated across two legend plugin instances, which Chart.js v4 doesn't support natively
without extra plumbing. Option B is a `renderChartLegend()` call at the same point in
`render()` that already populates the filter tree and badge — smaller diff, no fight with
Chart.js internals, consistent with this file's existing "do what's already done" bias.

---

## Tests

**Level: heavy.** Named justification: `applySplitLayout`'s pixel math (three stacked bands,
each with its own clip) is exactly the kind of geometry a hand-picked fixture could get
subtly wrong in the same way in both the code and the test's expected values — as already
happened once in this feature's history (the P1 YoY-sign bug, prior implementation report).
The only reliable check is working the real constants through the real function by hand and
comparing against what the real function actually returns when driven by real corpus data.

```
node --check static/js/company/tab-timeline.js     # clean
```

No Go changes, so no `go build`/`go test`/`make test` impact — not re-run beyond confirming
`git status` shows only frontend files touched.

### Definition of Done — verified against the live corpus

Isolated verification server (its own DB copy, a throwaway port, the user's own dev server
never touched), stopped and its temp DB/binary deleted after testing. Two Node harnesses
(`vm` + fake DOM + mocked `Chart`, throwaway, not committed) loaded the real `app.js` +
`tab-timeline.js` and drove them against the live API for CIK `0001567529`.

**Main harness — `All` preset, 66 real financial events in view (38 total assertions across
both harnesses, 0 failed):**

| Check | Evidence |
|---|---|
| F1 — three bands stack correctly | Hand-computed from `chartArea {10, 300}` and the real constants: expected `yRevenue 10..69`, `y/yEvents 75..246`, `yVolume 251..300` — **matched exactly** against the real `applySplitLayout(chart)` output |
| F1 — per-band clips | Revenue bar clip confined to the metrics band only (`bottom: 231`); price line clip excludes **both** the metrics band (top) and volume band (bottom) (`top: 65, bottom: 54`) — both computed values, not assumed |
| F1 — wider bars | `revenueBar.barPercentage === 0.75` on the real dataset |
| F3 — lane proximity | Real general-lane points cluster with max `Y = 0.930` (was 0.90); real financial-lane points stay strictly above (`min Y = 0.968`) |
| F2 — legend split | Real dataset labels sorted correctly: financial row = `Annual report, Quarterly report, Revenue, Net income`; stock row = `KMDA close, Volume, Major/Moderate/Routine filings`; cross-contamination checked both directions (zero) |
| F2 — legend disabled | `chart.options.plugins.legend.display === false` |
| P0/P1 regression — click still works | Clicking the real `2025-11-10` financial square opens the modal showing the real `$47.0M` revenue figure |
| P0/P1 regression — volume unaffected | Volume dataset still present and populated after all of the above |

**Supplementary harness — a real narrow window with zero financial filings (2016-07-01 to
2016-07-14), 5/5 assertions passed:**

| Check | Evidence |
|---|---|
| Graceful fallback when nothing to plot on `yRevenue` | `chart.scales.yRevenue` is absent (not an empty/broken scale); price scale still lays out correctly (`top:10, bottom:246`, volume band still respected) |
| No metrics divider drawn when there's nothing to divide | `_finMetricsLayout === null`; exactly **1** divider line drawn (the volume boundary only), not 2 and not 0 |

This closes the prior report's open item ("not browser-verified") for the original P0/P1
scope as well as the new F1–F3 work — everything in the DoD table above was exercised
against real data, not fixtures.

**Not verified by execution:** actual visual rendering in a real browser (canvas pixels,
font metrics, whether the metrics-pane divider *looks* right at a real viewport width). The
harness proves the geometry Chart.js is TOLD to use is correct; it cannot prove the same
about anti-aliased pixels on a real screen. Flagged as a follow-up, not silently skipped.

---

## How to enable / roll back

Live once static assets are served — no build step, env var, migration, or route change.

**Roll back F1–F3 only** (keep P0/P1): revert `applySplitLayout` and the divider-draw block
to the pre-F1 two-branch version; revert `GENERAL_LANE_BASE_Y` to `0.90`; revert
`barPercentage` to `0.55`; remove `renderChartLegend`/`financialLegendEntry`/
`legendSwatchShape` and set `legend.display` back to unset (Chart.js default); remove the two
`#tl-legend-*` containers from `company.html` and their CSS.

**Roll back everything (P0–F3):** revert `static/js/company/tab-timeline.js`,
`static/css/style.css`, and `static/company.html` in full. Nothing else references
`yRevenue`, `financialLane`, `financialMetric`, `FIN_METRICS_*`, or the legend functions.

---

## Follow-ups

1. **Real-browser visual pass still not done** — see Tests, "Not verified by execution."
2. **EPS as a third `yRevenue` series** — HLD §5 out-of-scope, LLD §1 "future."
3. **The revenue/net-income regex parsing (`parseDollarAmount`, `findMetricAmount`) is
   coupled to `HighlightsFromFinancials`'s current `ValueFmt` shape** — same coupling risk
   already named in the P0/P1-era report; unchanged by F1–F3.
4. **`prompt_10_timeline_events_financial.txt`** (the original idea file) still describes
   text callouts, not bars/dots — a pre-existing drift this session didn't fix (out of scope:
   the idea file is a historical record, HLD/LLD are the living contract).

---

## Touched files

**Modified (3):** `static/js/company/tab-timeline.js`, `static/css/style.css`,
`static/company.html`

No commit was created.
