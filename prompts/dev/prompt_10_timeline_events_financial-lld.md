# LLD: Financial-Results Lane and Metrics on the Timeline Chart

Implements `prompt_10_timeline_events_financial.txt` under
`prompt_10_timeline_events_financial-hld.md` (D1–D8, as-built + follow-ups F1–F3).
Triage: **STANDARD**.

---

## 1. Scope

| In scope (shipped) | Out of scope |
|--------------------|--------------|
| Partition financial vs general events (HLD D2) | Server `HighlightHeadline` field (HLD D7) |
| Dual `yEvents` marker bands (D1) | Text callout plugin (D6 — explicitly removed) |
| Cadence-colored square markers, radius 4 (D3) | Dedicated metrics pane (HLD F1 — follow-up) |
| `yRevenue` revenue bars + net-income dots (D4) | Split two-row legend (HLD F2 — follow-up) |
| Filter parity, modal click path (D5) | EPS / cash metric series (future) |

---

## 2. As-built layout (`applySplitLayout`)

Vertical split when `yVolume` exists (`volumeLanePlugin`):

| Band | Fraction of `chartArea` | Scales pinned |
|------|-------------------------|---------------|
| Price + events + metrics | top `100% − band − gap` | `y`, `yEvents`, `yRevenue` |
| Volume | bottom `band` (~17% linear, ~33% log) | `yVolume` |

```text
areaH = chartArea.bottom − chartArea.top
band  = round(areaH × volumeBandRatio())     // 0.17 or 0.33
priceBottom = area.bottom − band − VOLUME_BAND_GAP   // 5px
volTop      = priceBottom + VOLUME_BAND_GAP

y.top = yEvents.top = yRevenue.top = area.top
y.bottom = yEvents.bottom = yRevenue.bottom = priceBottom
yVolume.top = volTop;  yVolume.bottom = area.bottom
```

Clip per dataset:

| `yAxisID` | Clip |
|-----------|------|
| `yVolume` | bottom band only |
| `y`, `yRevenue` | exclude volume band (`bottom: band + gap`) |

When no volume dataset, all scales use full `chartArea`.

---

## 3. File-by-file changes (as-built)

### 3.1 `static/css/style.css`

In `:root` (alongside `--chart-price` / `--chart-volume`):

```css
--chart-financial-q: #3b82f6;
--chart-financial-a: #1e3a8a;
--chart-revenue: #059669;
--chart-net-income: #7c3aed;
```

### 3.2 `tab-timeline.js` — helpers (near `eventVisible`)

```javascript
function isFinancialReport(e) {
  return e.category === 'quarterly_results' || e.category === 'annual_report';
}

function parseDollarAmount(str) { /* $47.0M / $5.3M / $120K → number */ }

function findMetricAmount(ev, labelRe) { /* first matching highlights.metrics label */ }

function findMetricDisplay(ev, labelRe) { /* full { label, value } for tooltips */ }
```

`parseDollarAmount` uses `/\$([\d,.]+)\s*([BMK])?/i` and multiplies by 1e3 / 1e6 / 1e9.
Returns `null` on mismatch — **no bar is drawn** (withholding beats guessing).

### 3.3 Partition (in `render()`)

```javascript
const visible = current.events.filter(eventVisible);
const financialEvents = visible.filter(isFinancialReport);
const generalEvents = visible.filter(function (e) { return !isFinancialReport(e); });
```

### 3.4 Marker lanes — `yEvents` constants (as-built)

```javascript
const GENERAL_LANE_BASE_Y = 0.90;
const GENERAL_LANE_MAX_SPREAD = 0.08;
const GENERAL_LANE_STEP = 0.018;

const FINANCIAL_LANE_BASE_Y = 0.98;
const FINANCIAL_LANE_MAX_SPREAD = 0.05;
const FINANCIAL_LANE_STEP = 0.012;

function laneY(base, maxSpread, step, slot, count) {
  if (count <= 1) return base;
  const s = Math.min(step, maxSpread / (count - 1));
  return base - slot * s;
}
```

**History:** early draft used `GENERAL_LANE_BASE_Y = 0.13` (bottom band) — markers were
clipped / invisible; reverted to **0.90** per review.

General lane: existing `WEIGHTS.forEach` loop on `generalEvents`, `laneY(GENERAL_…)`.

Financial lane: `FINANCIAL_CADENCES` loop (annual + quarterly datasets), `pointRadius: 4`,
`pointStyle: 'rect'`, colors from CSS vars.

### 3.5 Financial metrics — `yRevenue` datasets

Built after marker datasets when any publishable amount exists:

```javascript
const revenueBars = labels.map(function () { return null; });
const finMetricEvents = labels.map(function () { return null; });
let finScaleMax = 0;

financialEvents.forEach(function (e) {
  const i = snapIndex(e.filingDate);
  if (i < 0 || series[i] == null) return;
  const rev = findMetricAmount(e, /revenue/i);
  if (rev != null) {
    revenueBars[i] = rev;
    finMetricEvents[i] = e;
    if (rev > finScaleMax) finScaleMax = rev;
  }
  const ni = findMetricAmount(e, /net income/i);
  if (ni != null) {
    netIncomePts.push({ x: labels[i], y: ni, ev: e });
    if (ni > finScaleMax) finScaleMax = ni;
  }
});
```

**Revenue bar dataset:**

```javascript
{
  type: 'bar',
  label: 'Revenue',
  data: revenueBars,
  yAxisID: 'yRevenue',
  backgroundColor: hexToRgba(revenueColor, 0.55),
  borderColor: revenueColor,
  barPercentage: 0.55,
  order: 3,
  financialMetric: true,   // click routing
}
```

**Net income scatter** (only if any points):

```javascript
{
  type: 'scatter',
  label: 'Net income',
  data: netIncomePts,       // { x: label, y: ni, ev: e }
  yAxisID: 'yRevenue',
  pointRadius: 4,
  pointStyle: 'circle',
  order: 2,
}
```

**Scale** (only when `finScaleMax > 0`):

```javascript
scales.yRevenue = {
  type: 'linear',
  position: 'right',
  min: 0,
  max: finScaleMax * 1.08,
  title: { display: true, text: 'Reported ($)', color: revenueColor },
  ticks: { maxTicksLimit: 5, callback: function (v) { return formatVolume(v); } },
  grid: { display: false, drawOnChartArea: false },
};
```

After chart construction: `chart._finMetricEvents = finMetricEvents` (parallel to `labels`).

### 3.6 `applySplitLayout` — `yRevenue` hooks

Inside the volume-present branch (and the no-volume early-return branch), set
`yRevenue.top`, `.bottom`, `.height` to match `y` / `yEvents`. Extend clip loop:

```javascript
} else if (ds.yAxisID === 'y' || ds.yAxisID === 'yRevenue') {
  ds.clip = { top: 0, left: 0, right: 0, bottom: band + VOLUME_BAND_GAP };
}
```

### 3.7 Click and tooltip

**`onClick`:**

```javascript
if (pt && pt.ev) { openEventModal(pt.ev); return; }
if (ds.financialMetric && chart._finMetricEvents) {
  const ev = chart._finMetricEvents[el0.index];
  if (ev) openEventModal(ev);
}
```

**Tooltip `label` callback** — add branch for `ds.yAxisID === 'yRevenue'`:

- Scatter with `item.raw.ev` → net-income metric display string.
- Bar → `Revenue: ` + `formatVolume(item.parsed.y)`.

### 3.8 Legend (as-built)

Single Chart.js bottom legend (`usePointStyle: true`, `boxWidth: 8`). Auto-includes new
datasets. **Follow-up F2** splits into two rows — not implemented.

### 3.9 Removed — do not re-add

| Artifact | Reason |
|----------|--------|
| `financialCalloutPlugin` / `calloutFor` / `shortCallout` | Replaced by D4 bar/dot geometry |
| `financialLane: true` flag | Was callout-only |
| `EVENT_LANE_Y = 0.965` single constant | Replaced by dual-band `laneY` |

---

## 4. Label vocabulary (client regex targets)

From `HighlightsFromFinancials` (`internal/companyview/highlights.go`), `MetricRank` order:

| Key | Display label | Used on chart |
|-----|---------------|---------------|
| `total_revenues` | Total revenues | Revenue bar (`/revenue/i`) |
| `eps_basic` | Basic EPS | — (modal only) |
| `net_income` | Net income | Net-income dot |
| `cash_and_equivalents` | Cash and cash equivalents | — (modal only) |

YoY and prior strings stay inside `value`; bars use numeric height only (not YoY text).

---

## 5. Tests

No frontend unit harness (same gap as prompts 6/7). Manual smoke on Kamada `0001567529`:

| # | Check |
|---|--------|
| 1 | **2Y:** blue/dark-blue squares top band; triangles/dots/circles on general band **below**; never both for same filing |
| 2 | **Filter:** uncheck `6-K → quarterly_results` → squares gone; annual squares remain |
| 3 | **`2025-11-10`:** green revenue bar + purple net-income dot; **no** floating `Rev $…` text |
| 4 | **Click** bar or square → modal metrics; `source: financials` |
| 5 | **Gap-filled** `2024-08-14` (if in window): bar height matches modal revenue |
| 6 | **Withheld** filing (e.g. mis-tagged Q1 2024 if in window): square only, no bar |
| 7 | **Volume regression:** linear + log toggle; markers and revenue bars still visible |
| 8 | **All preset:** bars readable; no right-axis label pile-up with log-volume labels |

Backend regression: `go test ./internal/companyview/ -run TimelineDefinitionOfDone` unchanged.

---

## 6. Build order (as-built — already landed)

1. CSS tokens (§3.1)
2. Partition + dual marker bands (§3.3–3.4) — verify general lane visible before metrics
3. `yRevenue` datasets + scale (§3.5) + layout hooks (§3.6)
4. Click/tooltip (§3.7)
5. Manual smoke (§5)

---

## 7. Follow-up LLD sketch (HLD F1–F3)

### F1 — Metrics pane split

Extend `applySplitLayout`:

```javascript
const FIN_METRICS_RATIO = 1 / 4;   // metrics band = ¼ of price pane → price : metrics = 3 : 1
// or FIN_METRICS_RATIO = 1/3 of total price+metrics if interpreted as "⅓ of stock graph"

metricsBottom = area.top + round((priceBottom - area.top) * FIN_METRICS_RATIO);
yRevenue.top = area.top;
yRevenue.bottom = metricsBottom;

y.top = yEvents.top = metricsBottom + GAP;
y.bottom = yEvents.bottom = priceBottom;
```

- Increase `barPercentage` to **~0.75**.
- Draw a divider line (same pattern as volume divider in `volumeLanePlugin`).
- Financial / general **marker bands unchanged relative to price sub-pane** (still top of `yEvents` range within `metricsBottom…priceBottom`).

### F2 — Split legend

Option A — filter `generateLabels` into two Chart.js legends (requires plugin or v4 multi-legend config).

Option B — HTML under `#timeline-chart`:

```html
<div class="timeline-legend timeline-legend-fin">…</div>
<div class="timeline-legend timeline-legend-stock">…</div>
```

Populate from dataset metadata on each `render()`.

### F3 — Lane proximity

After F1, set `GENERAL_LANE_BASE_Y = 0.93` (tune in smoke); keep `FINANCIAL_LANE_BASE_Y = 0.98`.

---

## 8. Done when

Matches HLD §7 as-built acceptance. Follow-ups F1–F3 are **not** required for prompt 10
closure; track as a separate small PR.

---

## 9. Risks (LLD-specific)

| Risk | Note |
|------|------|
| `yRevenue` + `yVolume` both `position: 'right'` | OK while pixel ranges disjoint; F1 increases separation |
| Sparse bar array on category axis | Chart.js skips nulls; bar width follows `barPercentage` |
| `finMetricEvents[i]` drift if `labels` window changes | Rebuilt every `render()`; no stale state |
| Re-adding text callouts | Conflicts with D6 — reject in review |
