# HLD: Financial-Results Lane and Metrics on the Timeline Chart

Implements `prompts/dev/prompt_10_timeline_events_financial.txt`. Builds on prompt 6 (event
lane + modal), prompt 7 (client filter, period stripes), prompt 8 (`financials.json`,
`BuildHighlights`), prompt 9 (gap-fill to **66/66** qualifying accessions), and the volume
strip (`yVolume`, `applySplitLayout`, log-volume toggle).

Triage: **STANDARD** — no DB or API change; almost all work lives in `tab-timeline.js` plus
a few CSS tokens. Layout spans three vertical concerns (financial metrics, price + event
lanes, volume).

---

## 1. Objective

On the company Timeline tab, make quarterly and annual public reports **visually distinct**
from other filings and plot **publishable headline figures** (revenue, net income) as chart
geometry — bars and dots on a dollar scale — so an analyst can compare reported fundamentals
against price and volume over time, not only after opening the modal.

| Priority | Deliverable | Status |
|----------|-------------|--------|
| **P0** | Top **financial marker lane** (blue / dark-blue squares); general events on a **separate** lane below; no duplicate markers | **Shipped** |
| **P0** | Prompt-7 filter applies to both lanes | **Shipped** |
| **P1** | **Metric series** on chart (`yRevenue`) — revenue bars + net-income dots from `Event.highlights` | **Shipped** (own metrics pane as of F1) |
| **P1b** (F1) | Dedicated **financial-metrics pane** above the price line (¼ of the pre-split price pane), wider bars, right scale | **Shipped** |
| **P2** (F2) | Split legend (financial vs stock/volume rows) | **Shipped** |
| (F3) | Lane proximity — general lane raised toward financial (`0.90 → 0.93`) | **Shipped** |

---

## 2. Current state (as-built)

### 2.1 What the chart shows today

```
┌─ price pane (y, yEvents, yRevenue share this band) ──────────────────────┐
│  ■ ■ ■  ← financial markers (yEvents ~0.93–0.98), radius 4              │
│  · ▲ ·  ← general markers   (yEvents ~0.82–0.90)                         │
│  ~~~ adjusted close (y, left) ~~~                                        │
│  ▌ ▌    ← revenue bars (yRevenue, right, green) at report dates only     │
│  ●      ← net-income dots (yRevenue, purple)                             │
├─ divider ────────────────────────────────────────────────────────────────┤
│  volume bars (yVolume, bottom ~17% linear / ~33% log)                    │
└──────────────────────────────────────────────────────────────────────────┘
```

- **`yEvents`** (hidden, 0 = bottom of price pane, 1 = top): marker lanes only — not dollar values.
- **`yRevenue`** (visible, right, `"Reported ($)"`): sparse **revenue bars** + **net-income scatter** at snapped trading-day indices; values parsed client-side from `highlights.metrics` (no new API field).
- **`yVolume`**: unchanged from prompt 4; layout plugin shrinks the price pane when volume is present.

### 2.2 Correcting common misconceptions

| Claim | Reality |
|-------|---------|
| Old single lane was "at the bottom" | On a non-reversed `yEvents` scale, **`Y = 0.965` is near the top** of the price pane. Prompt 10 **added** a financial band above and moved general markers **down** to ~0.82–0.90 so both are visible. |
| Bottom band at `Y ≈ 0.13` | **Rejected in practice** — markers clipped against the volume divider and were hard to see. As-built general lane uses **`GENERAL_LANE_BASE_Y = 0.90`**. |
| Text callouts above squares (`Rev $47M`) | **Rejected** — user feedback: figures must be **bars/dots on a scale**, comparable to price and volume, not canvas labels. The `financialCalloutPlugin` approach in early LLD drafts was removed. |
| Server needs `HighlightHeadline` | **No** — `HighlightsFromFinancials` already emits stable `label`/`value` pairs; client regex-picks revenue and net income. |

### 2.3 Data on the wire (unchanged)

`GET /api/companies/:cik/timeline` → `events[].highlights`:

```json
{
  "source": "financials",
  "metrics": [
    { "label": "Total revenues", "value": "$47.0M (+12.6% YoY)" },
    { "label": "Net income", "value": "$5.3M (+37.1% YoY)" }
  ]
}
```

Only metrics that passed `Line.Publishable()` in prompt 8/9 appear. Withheld or absent
highlights → square marker still renders; no bar/dot; modal may show "Financial highlights unavailable."

---

## 3. Architecture decisions

### D1 — Two marker bands on the existing hidden `yEvents` axis

No second event scale. As-built constants:

| Lane | Base Y | Spread | Categories |
|------|--------|--------|--------------|
| Financial | `0.98` | `0.05` | `quarterly_results`, `annual_report` |
| General | `0.90` | `0.08` | all other visible categories |

Each lane uses the same stagger helper (`laneY(base, maxSpread, step, slot, count)`) as prompt 6.
Financial squares sit **above** general markers; bands do not overlap.

**Follow-up:** nudge general lane slightly closer to financial (`~0.92` base) once the dedicated metrics pane (§8) frees vertical space.

### D2 — Partition before the general-lane dataset build

`isFinancialReport(ev)` splits `visible` into `financialEvents` and `generalEvents`.
The `WEIGHTS.forEach` loop runs on **`generalEvents` only** — financial filings never appear as major triangles.

### D3 — Financial markers: two scatter datasets (cadence = dataset)

| Dataset | Style | Token | Radius |
|---------|-------|-------|--------|
| Quarterly report | `'rect'` | `--chart-financial-q` | **4** |
| Annual report | `'rect'` | `--chart-financial-a` | **4** |

Chart.js legend picks up one entry per dataset (`usePointStyle: true`).

### D4 — Financial **metrics** use a new `yRevenue` scale (P1)

Separate from `yEvents` (markers) and `y` (price):

- **Revenue:** `type: 'bar'`, sparse array aligned to price `labels` (null except report days), `barPercentage: 0.55`, green (`--chart-revenue`).
- **Net income:** `type: 'scatter'`, circle dots, purple (`--chart-net-income`), same axis.
- **Scale:** linear, `position: 'right'`, `min: 0`, `max: max(revenue, netIncome) × 1.08`, ticks via `formatVolume()`.
- **Layout:** `applySplitLayout` sets `yRevenue.top/bottom` equal to the **price pane** (same as `y` / `yEvents`), not the volume strip. Revenue datasets clip with the price line (exclude volume band).

Parsing: `parseDollarAmount()` on `$47.0M`-style strings from `highlights.metrics`; `findMetricAmount(ev, /revenue/i)`.

**Why overlay in the price pane (as-built):** fastest path to comparable geometry without a fourth layout band. **Limitation:** revenue bars share vertical space with the price line — readable but crowded on long windows.

### D5 — Interaction unchanged

- **Click** square, dot, or revenue bar → `#timeline-event-modal` via `pt.ev` or `chart._finMetricEvents[index]`.
- **Tooltips:** revenue bar → `Revenue: …`; net-income dot → full metric line; filing markers → form / summary as before.
- **`eventVisible()`** gates both lanes; no filter-tree changes.

### D6 — No text callout plugin

On-chart numbers are **only** bar/dot positions on `yRevenue`. Do not reintroduce `fillText` metric labels above markers — they do not share a scale with price or volume and were explicitly rejected in review.

### D7 — No server change for P0–P1

`BuildHighlights(category, summary, r.Financials)` in `internal/companyview/timeline.go` already attaches highlights. Revisit `HighlightHeadline` only if client label matching proves fragile against future formatter changes.

### D8 — Volume strip independence (regression guard)

Financial lanes and `yRevenue` live in the price pane only. Volume log mode (~33% band) and custom log axis labels must not alter marker or revenue layout beyond `applySplitLayout`'s `priceBottom` shrink.

---

## 4. What this touches

| File | Change |
|------|--------|
| `static/js/company/tab-timeline.js` | Partition, dual `yEvents` bands, financial scatter datasets, `yRevenue` + bar/scatter metrics, `applySplitLayout` hooks, helpers (`parseDollarAmount`, …) |
| `static/css/style.css` | `--chart-financial-q`, `--chart-financial-a`, `--chart-revenue`, `--chart-net-income` |

Not touched: Go backend, routes, DB, `financials.json` extraction.

---

## 5. Out of scope

- Replacing prompts 8–9 extraction.
- Candlesticks, full income-statement embed, LLM on-chart numbers.
- Moving report squares onto the price line (rejected in prompt 6).
- EPS as a third metric series (easy add later on `yRevenue`).

---

## 6. Risks

| Risk | Mitigation |
|------|------------|
| **Crowded price pane** — price line + bars + two marker bands | Follow-up §8: dedicated metrics pane above price |
| **Right-axis clutter** — `yRevenue` + `yVolume` both right | Volume ticks confined to bottom band; log volume uses hand-drawn labels |
| **Legend length** — up to ~9 entries | Follow-up: two-row legend (§8) |
| **Regex parse drift** if `ValueFmt` changes | Silent omit (no bar); modal still shows full metrics |

---

## 7. Done when (as-built acceptance)

On `/companies/0001567529#timeline`, preset **2Y**:

1. Blue / dark-blue **squares** (radius 4) on the **top** marker band for each quarterly/annual filing; other categories only on the **general** band below.
2. Prompt-7 filter hides/shows financial squares with their category checkbox; no duplicate markers.
3. **`2025-11-10`** shows a **green revenue bar** and (when present) purple **net-income dot** on the right dollar scale — not a text callout.
4. Click bar or square → modal with full metrics grid; SEC link works.
5. Volume strip + log-volume checkbox unchanged.

---

## 8. Follow-up refinements (F1–F3 — shipped)

Captured from product review, implemented as a follow-on slice after the P0/P1 as-built
stabilized. See `prompt_10_timeline_events_financial-implementation.md` for the as-built
detail and heavy-level verification evidence.

### F1 — Dedicated financial-metrics pane

Split the **price pane** vertically (extend `applySplitLayout`):

```
┌─ financial metrics pane (~25% of former price height) ─── yRevenue only ─┐
│  ▌▌▌▌  wider revenue bars (barPercentage ~0.75), right $ scale            │
├─ divider ──────────────────────────────────────────────────────────────────┤
│  ■ financial markers  (yEvents top band — **stay as today**)               │
│  · general markers    (yEvents — slightly closer to financial, ~0.92)      │
│  ~~~ price line (y) ~~~                                                    │
├─ volume strip ─────────────────────────────────────────────────────────────┤
```

- Metrics pane height ≈ **⅓ of the stock (price) pane** before split — i.e. metrics : price ≈ 1 : 3.
- Wider bars because reports are ~quarterly, not daily.
- Report **squares remain on the price sub-pane** at the top of that sub-pane (current behavior relative to price, not absolute canvas top).

### F2 — Split legend (two rows)

| Row | Entries |
|-----|---------|
| Financial | Quarterly report, Annual report, Revenue, Net income |
| Stock / volume | Price, Volume, Major / Moderate / Routine filings |

Implement via Chart.js `legend.labels.generateLabels` filter or a small HTML legend under the canvas — LLD follow-up picks whichever avoids fighting Chart.js defaults.

### F3 — Lane proximity

After F1, raise `GENERAL_LANE_BASE_Y` toward `0.92–0.94` so general markers sit closer to financial squares without overlapping.

---

## 9. Deliverables

| Doc / code | Role |
|------------|------|
| `prompt_10_timeline_events_financial-lld.md` | As-built constants, dataset shapes, layout hooks, smoke plan, F1–F3 sketch |
| `static/js/company/tab-timeline.js` | Implementation |
| `static/css/style.css` | Chart tokens |

Optional later: `prompt_10_timeline_events_financial-implementation.md` after F1 ships (same pattern as prompt 8/9).
