# LLD: Financial-Results Lane on the Timeline Chart

Implements `prompt_10_timeline_events_financial.txt` under the contract in
`prompt_10_timeline_events_financial-hld.md` (D1–D7). Triage: **STANDARD**.

## 1. Scope

**In (P0):** partition financial events out of the general lane; two-band `yEvents` layout
(financial top, general bottom — a real repositioning, see HLD §2); square markers, cadence
colors, per-cadence legend entries.
**In (P1):** `afterDatasetsDraw` callout plugin — one short revenue/EPS/net-income line above
a financial marker, collision-aware, with a >12-marker downgrade.
**Out:** everything the HLD's §5 lists (P2 second-line callout, candlesticks, server field).

## 2. Current state

See HLD §2 for the corrected premise (general lane is already near the top today, this
design moves it to the bottom). Real anchors in `static/js/company/tab-timeline.js`:

- `EVENT_LANE_Y`/`_MAX_SPREAD`/`_STEP` and `eventLaneY()` — lines 731–739.
- `weightRank` grouping + `WEIGHTS.forEach` dataset build — lines 741–781.
- `visible` (prompt-7-filtered events) — line 692.
- Plugin registration precedent (`volumeLanePlugin` line 581, `periodStripesPlugin` line 649)
  — the callout plugin follows the same `Chart.register(...)` pattern at module scope.
- `cssVar()` (line 314) and `openEventModal`/`renderHighlights` (lines 931–960) are reused
  unmodified.

## 3. File-by-file changes

### 3.1 `static/css/style.css` — two new tokens

In `:root`, next to `--chart-volume` (line 33):

```css
--chart-financial-q: #3b82f6;   /* quarterly_results */
--chart-financial-a: #1e3a8a;   /* annual_report */
```

### 3.2 `tab-timeline.js` — partition helper

Near `eventVisible()` (line 117):

```javascript
function isFinancialReport(e) {
  return e.category === 'quarterly_results' || e.category === 'annual_report';
}
```

### 3.3 `tab-timeline.js` — replace the single-lane block (lines 728–781)

Replace `EVENT_LANE_Y`/`_MAX_SPREAD`/`_STEP`/`eventLaneY` with two parameterized bands and a
generalized stagger function:

```javascript
// Two disjoint Y-bands on the shared hidden yEvents axis (min 0, max 1, not
// reversed — high Y renders near the TOP of the price pane). Financial stays
// near the top (where the single general lane used to sit); general moves to
// the bottom band, per prompt_10 HLD D1/§2.
const GENERAL_LANE_BASE_Y = 0.13;
const GENERAL_LANE_MAX_SPREAD = 0.10;   // band ~0.02–0.14
const GENERAL_LANE_STEP = 0.018;

const FINANCIAL_LANE_BASE_Y = 0.98;
const FINANCIAL_LANE_MAX_SPREAD = 0.08; // band ~0.90–0.98
const FINANCIAL_LANE_STEP = 0.015;

function laneY(base, maxSpread, step, slot, count) {
  if (count <= 1) return base;
  const s = Math.min(step, maxSpread / (count - 1));
  return base - slot * s;
}
```

Partition right where `visible` is built (line 692):

```javascript
const visible = current.events.filter(eventVisible);
const financialEvents = visible.filter(isFinancialReport);
const generalEvents = visible.filter(function (e) { return !isFinancialReport(e); });
```

The existing `weightRank`/grouping loop (lines 741–752) now runs on `generalEvents` instead
of `visible` — one-word change (`visible.forEach` → `generalEvents.forEach`) — and
`WEIGHTS.forEach`'s dataset push (lines 754–781) uses `laneY(GENERAL_LANE_BASE_Y,
GENERAL_LANE_MAX_SPREAD, GENERAL_LANE_STEP, slot, g.length)` in place of `eventLaneY(slot,
g.length)`. Dense-day radius reduction (`dense`, minor-only) is unchanged — it's a general-
lane concern.

### 3.4 `tab-timeline.js` — financial lane datasets

New block, same shape as the `WEIGHTS.forEach` loop, right after it:

```javascript
const FINANCIAL_CADENCES = [
  { key: 'annual_report', label: 'Annual report', color: cssVar('--chart-financial-a', '#1e3a8a') },
  { key: 'quarterly_results', label: 'Quarterly report', color: cssVar('--chart-financial-q', '#3b82f6') },
];
const finGroups = {};   // label -> [{e, i}], annual sorted ahead of quarterly on a same-day tie
financialEvents.forEach(function (e) {
  const i = snapIndex(e.filingDate);
  if (i < 0 || series[i] == null) return;
  (finGroups[labels[i]] = finGroups[labels[i]] || []).push({ e: e, i: i });
});
Object.keys(finGroups).forEach(function (label) {
  finGroups[label].sort(function (a, b) {
    return (a.e.category === 'annual_report' ? 0 : 1) - (b.e.category === 'annual_report' ? 0 : 1);
  });
});
FINANCIAL_CADENCES.forEach(function (c) {
  const pts = [];
  Object.keys(finGroups).forEach(function (label) {
    const g = finGroups[label];
    g.forEach(function (item, slot) {
      if (item.e.category !== c.key) return;
      const y = laneY(FINANCIAL_LANE_BASE_Y, FINANCIAL_LANE_MAX_SPREAD, FINANCIAL_LANE_STEP, slot, g.length);
      pts.push({ x: label, y: y, ev: item.e });
    });
  });
  datasets.push({
    type: 'scatter',
    label: c.label,             // Chart.js legend entry text — "Quarterly report" / "Annual report"
    data: pts,
    backgroundColor: c.color,
    borderColor: c.color,
    pointRadius: 6,
    pointHoverRadius: 9,
    pointStyle: 'rect',
    showLine: false,
    yAxisID: 'yEvents',
    order: 1,
    financialLane: true,        // custom flag the callout plugin (§3.6) reads; Chart.js ignores it
  });
});
```

`snapIndex`/`labels`/`series` are the same closures the general lane already uses — no
duplication of the non-trading-day snap logic (HLD didn't call out a separate decision for
this because there isn't one: it's the same date-alignment problem prompt 6 already solved).

### 3.5 `tab-timeline.js` — legend

No markup change. Chart.js's existing `legend: { position: 'bottom', labels: { usePointStyle:
true, boxWidth: 8 } }` (line ~853) auto-adds one entry per dataset with a `label`, so the two
new datasets appear automatically once pushed. Verify at 1280px per HLD §6's legend-growth
risk; if entries wrap awkwardly, that's a `boxWidth`/font-size CSS tweak, not a logic change.

### 3.6 `tab-timeline.js` — P1 callout plugin

New constants + functions near the other plugins (after `periodStripesPlugin`
registration, line 649):

```javascript
const CALLOUT_MAX_LABELED = 12;   // idea file's ">12 visible financial markers" threshold
const CALLOUT_MIN_GAP_PX = 80;

// Priority waterfall for the ONE line shown per marker (not multiple lines —
// P1 MVP is one line; a second line is P2, design-only). "Major financial
// events only" (the idea file's other suggested downgrade) is rejected here:
// every quarterly_results/annual_report event is already WeightMajor
// (classify.go's categoryRules), so that filter would be a no-op. Revenue-
// only is the only downgrade that actually reduces label count.
function calloutFor(ev) {
  const h = ev.highlights;
  if (!h || !h.metrics || !h.metrics.length) return null;
  const pick = function (re) {
    for (let i = 0; i < h.metrics.length; i++) if (re.test(h.metrics[i].label)) return h.metrics[i];
    return null;
  };
  const rev = pick(/revenue/i);
  if (rev) return shortCallout('Rev', rev.value, true);
  const eps = pick(/eps/i);
  if (eps) return shortCallout('EPS', eps.value, false);
  const ni = pick(/net income/i);
  if (ni) return shortCallout('NI', ni.value, false);
  return null;
}

// "$47.0M (+12.6% YoY)" -> "Rev $47.0M +13%" (strip the metric's own label —
// "Total revenues" etc. — keep the figure and a rounded YoY sign+percent).
function shortCallout(prefix, value, isRevenue) {
  const amt = /\$[\d.,]+[BMK]?/.exec(value);
  if (!amt) return null;
  const pct = /([+-]\d+(?:\.\d+)?)%/.exec(value);
  let text = prefix + ' ' + amt[0];
  if (pct) text += ' ' + Math.round(parseFloat(pct[1])) + '%';
  return { text: text, isRevenue: isRevenue };
}

const financialCalloutPlugin = {
  id: 'financialCallouts',
  afterDatasetsDraw: function (chart) {
    const candidates = [];
    chart.data.datasets.forEach(function (ds, di) {
      if (!ds.financialLane) return;
      const meta = chart.getDatasetMeta(di);
      (ds.data || []).forEach(function (pt, idx) {
        if (!pt || !pt.ev) return;
        const c = calloutFor(pt.ev);
        if (!c) return;
        const el = meta.data[idx];
        if (!el) return;
        candidates.push({ x: el.x, y: el.y, text: c.text, isRevenue: c.isRevenue });
      });
    });
    if (!candidates.length) return;
    candidates.sort(function (a, b) { return a.x - b.x; });   // chronological, left to right

    const revenueOnly = candidates.length > CALLOUT_MAX_LABELED;
    const c2d = chart.ctx;
    c2d.save();
    c2d.font = '11px "IBM Plex Sans", sans-serif';
    c2d.fillStyle = cssVar('--text', '#0f172a');
    c2d.textAlign = 'center';
    c2d.textBaseline = 'bottom';
    let lastX = -Infinity;
    candidates.forEach(function (cnd) {
      if (revenueOnly && !cnd.isRevenue) return;
      if (cnd.x - lastX < CALLOUT_MIN_GAP_PX) return;   // skip the lower-priority (later, since
      c2d.fillText(cnd.text, cnd.x, cnd.y - 8);          // sorted) label on overlap
      lastX = cnd.x;
    });
    c2d.restore();
  },
};
Chart.register(financialCalloutPlugin);
```

Uses `chart.getDatasetMeta(di).data[idx]` for pixel position — the *actual* rendered point
(post-stagger, post-`applySplitLayout`), not a recomputed `getPixelForValue`, so labels track
the real marker even when `applySplitLayout` has resized the price/events band around the
volume strip.

## 4. Tests

The idea file itself calls this **"Tests (light)"** — manual smoke, no existing frontend
harness for this module (same gap prompts 6/7 already documented; not reopened here). Manual
smoke plan:

- Kamada `0001567529`, 2Y preset: blue/dark-blue squares along the top; every other category
  (governance, insider_trade, business_deal, ...) only on the bottom lane; no category
  appears on both.
- Same-day collision: none expected in-corpus for two financial filings, but the stagger
  math is exercised by any general-lane multi-filing day (e.g. `2026-04-09`'s 17-marker day
  from prompt 6) — confirms the bottom band still separates markers now that its base moved
  from `0.965` to `0.13`.
- Toggle the prompt-7 filter's `6-K → quarterly_results` leaf off: top-lane blue squares
  disappear; dark-blue annual squares unaffected.
- `2025-11-10` (Q3 2025, known-good revenue highlight from prompt 8's corpus audit): shows a
  `Rev $47.0M +13%`-style callout above its square.
- `All` preset (66 accessions in view): confirms the `>12` downgrade actually engages —
  non-revenue callouts (EPS/NI) disappear, revenue-only labels remain, spaced ≥80px.
- Click a financial square: modal opens with the full metrics grid, unchanged from prompt 8.
- Volume strip + its log-scale checkbox: no visual/functional change (regression check per
  HLD §7's done-when).

## 5. Build order

1. CSS tokens (§3.1) — no visible effect alone, safe first commit.
2. Partition + two-band general-lane relocation (§3.2, §3.3) — verify the general lane alone
   still renders correctly at its new bottom position before adding anything financial.
3. Financial lane datasets (§3.4) — verify squares appear, colors correct, no duplicates on
   the general lane.
4. Legend check (§3.5) at 1280px.
5. Callout plugin (§3.6) — verify on `2025-11-10` first, then the `All`-preset downgrade.
6. Manual smoke (§4) end to end.

## 6. Risks

- **General-lane repositioning (HLD §2) is the one part of this LLD most likely to look
  "wrong" on first render** — build order step 2 isolates it so it's debugged before the
  financial lane adds visual noise on top.
- **`chart.getDatasetMeta` timing**: `afterDatasetsDraw` is guaranteed to run after Chart.js
  has laid out every dataset element for this frame, so `meta.data[idx]` is safe to read
  here — confirmed against Chart.js's documented plugin hook ordering, not just assumed.
- **Revenue-value regex (`shortCallout`) is tuned to `HighlightsFromFinancials`'s current
  format** (`"$47.0M (+12.6% YoY)"`) — if prompt 8/9's formatter ever changes `ValueFmt`
  shape, this regex silently stops matching (returns `null`, falls through to no callout,
  never a garbled one) rather than breaking loudly. Acceptable per D6/D7's "withholding
  beats guessing" philosophy already established in prompt 8.

## 7. Done when

Same as the HLD §7 / idea file's Definition of Done: two visually distinct lanes, no
duplicate rendering, filter parity across both, a real revenue callout on `2025-11-10`, an
unchanged modal, and no volume-strip regression.
