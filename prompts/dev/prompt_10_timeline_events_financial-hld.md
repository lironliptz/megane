# HLD: Financial-Results Lane on the Timeline Chart

Implements `prompts/dev/prompt_10_timeline_events_financial.txt`. Builds on prompt 6 (event
lane + modal), prompt 7 (client-side filter, period stripes), prompt 8 (`financials.json`,
`BuildHighlights`), prompt 9 (gap-fill to 66/66 accessions), and the volume-strip work
(`yVolume`, `applySplitLayout`) that landed alongside prompt 7.

Triage: **STANDARD** — no DB/API change for P0–P1, single file (`tab-timeline.js`) carries
almost all of the work, but it's real canvas/layout logic (a second staggered lane plus a
collision-aware label plugin), not pure wiring.

## 1. Objective

Give every `quarterly_results`/`annual_report` filing its own square marker on a dedicated
top lane, visually separate from the general event lane, and (P1) paint a short revenue
callout above it when `Event.highlights` has a publishable figure — so an analyst can scan
"when did we report?" independently of "what else happened?".

## 2. Current state — correcting the idea file's own premise

The idea file's "Problem" table says events sit in "one scatter lane near the **bottom** of
the price pane (`EVENT_LANE_Y ≈ 0.965`)". That description is stale against the as-built
code (`static/js/company/tab-timeline.js:731-739`):

```javascript
const EVENT_LANE_Y = 0.965;   // yEvents axis: min 0, max 1, NOT reversed
```

On a non-reversed Chart.js linear y-scale, pixel-top corresponds to the axis **max**, so
`Y = 0.965` already renders **near the top** of the price pane, not the bottom. (`yEvents`
itself is confined to the top ~83% of chart height by `applySplitLayout` — the volume strip
occupies the bottom ~17% on its own `yVolume` scale, added since prompt 6.) The idea file's
own diagram (general lane drawn at the bottom, financial lane above it) is the actual intent,
so this HLD implements that intent, but the mechanism is a **real repositioning of the
existing general lane downward**, not just an addition:

| Lane | Today | This design |
|---|---|---|
| General (major/medium/minor) | `Y ≈ 0.965` (top) | `Y ≈ 0.08` band (bottom) |
| Financial (new) | doesn't exist — financials render in the general "major" dataset | `Y ≈ 0.95` band (top) |

`quarterly_results`/`annual_report` already carry `Weight = WeightMajor`
(`internal/companyview/classify.go`'s `categoryRules`), so today they render in the general
lane's major (triangle) dataset, competing for the same Y slots as `business_deal`,
`regulatory_clinical`, and `annual_guidance` — exactly the idea file's complaint #1.

`Event.Highlights` (`internal/companyview/timeline.go:49`) and `Event.Highlights.Source`
(`"financials" | "summary_parse"`, `internal/companyview/highlights.go:15`) are already on
the wire — confirmed no server change is needed for P0/P1, matching the idea file's own
"None required" call.

## 3. Architecture decisions

### D1 — Two independent Y-bands on the existing hidden `yEvents` axis

No new Chart.js scale. Two disjoint bands on `yEvents` (`min:0, max:1`, unchanged):

| Lane | Band | Base Y | Categories |
|---|---|---|---|
| Financial | `0.90–0.98` | `0.95` | `quarterly_results`, `annual_report` only |
| General | `0.02–0.14` | `0.08` | everything else |

Each lane reuses prompt 6's per-day stagger (`eventLaneY(slot, count)`) independently, with
its own base/step/spread constants — a busy financial day (e.g. Q2 6-K + 20-F same date)
spreads within `0.90–0.98`; a busy general day spreads within `0.02–0.14`. The two bands
never overlap, so a mixed day (a 6-K quarterly report plus a governance filing) always shows
a square above and a dot below, per the idea file's example.

### D2 — Financial events are excluded from the general lane's dataset build, not just filtered

`isFinancialReport(ev)` (`ev.category === 'quarterly_results' || ev.category ===
'annual_report'`) partitions `visible` into two arrays **before** the existing
`WEIGHTS.forEach` grouping loop runs. The general lane's major/medium/minor datasets are
built from the non-financial partition only — this is what prevents the "no duplicates"
requirement from becoming a rendering bug (a financial event is never eligible for a
major-weight triangle once excluded upstream).

### D3 — Financial lane is two scatter datasets (cadence = dataset), not one

One dataset per cadence, matching the existing `WEIGHTS` pattern (one dataset per weight)
rather than per-point `pointStyle`/color overrides — same reasoning prompt 6 used: separate
datasets give per-cadence legend entries and tooltip labels for free, and Chart.js's legend
already auto-generates one entry per dataset (no separate HTML key needed for P0 — extends
the existing bottom legend `usePointStyle` row instead of introducing prompt_10's optional
"small HTML key" alternative).

| Dataset | `pointStyle` | Color token | Fallback |
|---|---|---|---|
| Quarterly | `'rect'` | `--chart-financial-q` | `#3b82f6` |
| Annual | `'rect'` | `--chart-financial-a` | `#1e3a8a` |

Radius 6 (between general's medium=5 and major=7, per the idea file's "match or slightly
larger than major markers").

### D4 — `eventVisible()` (prompt 7 filter) gates both lanes from one predicate

No change to `buildFilterTree`/`eventVisible` — they already operate on `event.form` +
`event.category` pairs pulled from whatever's in `current.events`, which includes financial
categories today. The only change is **where** the filtered result is partitioned (D2). One
filter tree continues to cover every category across both lanes, per the idea file's
"Interaction with prompt 7" section.

### D5 — Click/modal path is unchanged

`onClick` already resolves `elements[0]` to whichever dataset/point Chart.js's `nearest`
interaction picked, reads `pt.ev`, and calls `openEventModal(ev)`
(`tab-timeline.js:844-850`). A financial-lane point's `ev` is a normal `Event` object with
the same shape as a general-lane point's — no branching needed. `renderHighlights` already
renders `ev.highlights.metrics` or "Financial highlights unavailable." (`tab-timeline.js:931-941`)
regardless of which lane the click came from.

### D6 — On-chart callouts (P1): a fourth canvas plugin, same family as the other three

`tab-timeline.js` already registers three module-scope `Chart.register(...)` plugins
(`volumeLanePlugin`, `periodStripesPlugin`, and the layout logic inside them) using
`beforeDatasetsDraw`/`afterDraw` hooks that read `chart.data`/`chart.scales` at draw time,
never a stale closure. The callout plugin (`financialCalloutPlugin`, `afterDatasetsDraw`)
follows the identical shape: walk the financial lane's rendered points (via
`chart.getDatasetMeta`, so it uses the *actual* pixel positions Chart.js computed, including
the D1 stagger), pick a metric to print per the idea file's priority table (revenue → EPS →
net income, first `label` match by regex), and skip a label within 80px of the previous one
(idea file's collision rule, applied left-to-right in x order since the x-axis is
chronological). Text only (`fillText`), no HTML — matches D5's precedent that canvas text
never carries injectable content.

**Deferred to the LLD, per the idea file's own "pick one rule, document in LLD" note:** the
exact `>12 visible financial markers` downgrade rule (revenue-only vs. major-events-only).
Both are cheap to compute from the same partitioned array D2 already produces; the LLD picks
one based on which reads better in the 5Y/All presets where marker counts are highest.

### D7 — No `HighlightHeadline` server field for P0/P1

The idea file offers this as an optional escape hatch "if client matching is too brittle".
Client-side regex matching (`/revenue/i`, `/eps/i`, `/net income/i` against
`highlights.metrics[].label`) is straightforward against the label vocabulary
`HighlightsFromFinancials` already emits (`internal/companyview/highlights.go`'s
`MetricRank`-ordered `Display` strings: "Total revenues", "Basic EPS", "Net income", ...) —
no server round-trip needed. Revisit only if the LLD's implementation finds the regex
genuinely fragile against real corpus label text.

## 4. What this touches

| File | Change |
|---|---|
| `static/js/company/tab-timeline.js` | Partition `visible` (D2); two-lane Y bands (D1); financial scatter datasets (D3); callout plugin (D6) |
| `static/css/style.css` | `--chart-financial-q`, `--chart-financial-a` tokens next to `--chart-price`/`--chart-volume` (`:root`, line ~32) |
| `static/company.html` | None required for P0 — legend is Chart.js-generated (D3). Only touched if the LLD's collision rule needs a UI toggle. |

Not touched: `internal/companyview/*`, `internal/edgar/financials/*`, any Go file, any
migration, any route.

## 5. Out of scope

- Replacing `financials.json` extraction (prompts 8–9) — this prompt only *displays* what's
  already on the wire.
- Candlesticks, secondary price scales, embedding full income statements on the chart.
- Moving financial markers onto the price line itself (rejected already in prompt 6).
- LLM-generated on-chart numbers.
- P2 (second-line EPS/net-income callout) — design-only per the idea file; not built here
  unless the LLD finds it trivial once P1's plugin exists.

## 6. Risks

- **Repositioning the general lane (D1) is a visible behavior change**, not additive — every
  non-financial marker moves from top to bottom of the price pane. Flagged explicitly in §2;
  the LLD's manual smoke must confirm this reads better, not just "different."
- **Dense financial windows** (an `All`-range view spanning 66 accessions) is the real stress
  case for D6's collision logic — untested until the LLD's implementation runs against the
  live Kamada corpus at the `All` preset.
- **Legend growth**: adding two more auto-generated Chart.js legend entries (now up to 7:
  price, volume, major/medium/minor, quarterly, annual) may need `boxWidth`/wrapping
  attention at narrow viewport widths — a CSS-only concern, not deferred to the LLD's
  judgment call.

## 7. Done when

Same as the idea file's Definition of Done: on `/companies/0001567529#timeline`, preset 2Y,
blue/dark-blue squares sit along the top of the price chart on each quarterly/annual filing
date while every other filing renders only on the bottom lane; toggling the prompt-7 filter
shows/hides financial squares with their category checkbox; the `2025-11-10` Q3 marker shows
a revenue callout on the chart; the modal still shows the full metrics table and a working
SEC link; the volume strip and its log-scale toggle are unaffected.

## 8. Deliverables

- Updated `tab-timeline.js` (two-lane split, financial datasets, callout plugin)
- `--chart-financial-q`/`-a` tokens in `style.css`
- `prompt_10_timeline_events_financial-lld.md` — exact stagger constants, callout plugin
  pseudocode, the `>12` downgrade rule, manual-smoke plan (no frontend test harness exists
  for this module, same gap prompts 6/7 already noted)
