# LLD: Timeline Event Lane, Drill-Down & Highlights

Implements `prompts/dev/prompt_6_timeline_events.txt` and
`prompt_6_timeline_events-hld.md` (D1–D6). Triage: **STANDARD**.

## 1. Scope

**In (P0):** dual y-axis event lane (D1), same-day Y stagger (D2), click →
`#timeline-event-modal` (D3), Escape-to-close (D4).
**In (P1):** `internal/companyview/highlights.go` — growth-percentage
extraction only (D5), `Event.Highlights` field (D6), metrics table in modal.
**Out:** P2 overlays (backlog only, idea file §P2), full focus-trap, real
dollar-figure extraction, changing hover-tooltip behavior, on-chart callout
label (**new decision below**, D5/D6 didn't fix this).

**New decision — P1 on-chart callout: Option B (modal-only), not Option A.**
The idea file offers a choice; HLD is silent on it. Given P1a was rescoped to
a single growth-% metric (no revenue/EPS), a canvas `afterDraw` label plugin
(~40 lines, collision logic) buys little for one number per ~2 events/year.
Ship metrics in the modal only; acceptance criterion "at least one on-chart
callout OR documented deferral" is satisfied by this paragraph.

**New decision — reuse `.meta-grid`/`.meta-item` for highlights, no new
CSS.** `ctx.metaItem(label, value)` (`shell.js:28-31`) already renders the
label/value pair markup `tab-general.js` uses elsewhere. The modal needs no
CSS beyond the existing `.modal-overlay`/`.modal`/`.badge` (D3) — **style.css
gets zero new rules**, correcting the idea file's "lane spacing tokens /
callout typography" file-touch entry, which assumed Option A.

## 2. Current state (verified against source, not the idea file's description)

- `tab-timeline.js:239-264` — one scatter dataset per `WEIGHTS` entry
  (`major`/`medium`/`minor`, `tab-timeline.js:22-26`), each point
  `{x: labels[i], y: series[i], ev: e}` — pinned to price, exactly what D1
  replaces.
- `tab-timeline.js:274-311` — Chart.js config has `scales: {x, y}` only, no
  `onClick`, no `yEvents`.
- `internal/companyview/timeline.go:36-47` — `Event` has no `Highlights`
  field yet; `BuildEvents` (`timeline.go:145-170`) calls `WhyItMatters` per
  row, nothing else.
- `company.html:78-113` — timeline panel has no modal markup. Precedent
  modal (`index.html:85-91`, JS in `analyze.js:265-434`) is a sibling `div`
  right after `</main>` (`index.html:83-85`), plain `.modal-overlay` +
  `.modal` (no `hidden` attr, no `role`/`aria-modal` — **not** what the idea
  file's snippet shows; following real precedent per D3 means dropping
  those).
- Uncommitted working-tree changes to these three files (date-range slider,
  `git diff` confirms) are an unrelated in-flight feature — this LLD's diffs
  land on top of them, not instead of them.
- Verified against the real corpus (`fileDB/companies/0001567529/`, 66
  `quarterly_results`/`annual_report` rows): only 18 distinct summary
  strings exist. 4 of 66 contain a growth percentage; two word orders occur
  — `"...GROWTH OF 23%..."` and `"11%... GROWTH"`/`"35% INCREASE..."` — see
  §4.3 for the regex that handles both (verified against all 66, not just
  the HLD's 8-sample check).

## 3. File-by-file changes

| File | Change |
|------|--------|
| `static/js/company/tab-timeline.js` | `yEvents` axis; per-day Y stagger; `onClick`; open/close modal; Escape listener; render highlights |
| `static/company.html` | append `#timeline-event-modal` after `</main>` (D3 precedent) |
| `internal/companyview/timeline.go` | add `Event.Highlights *EventHighlight`; call `BuildHighlights` from `BuildEvents` |
| `internal/companyview/highlights.go` | new — `EventHighlight`, `HighlightMetric`, `BuildHighlights` |
| `internal/companyview/highlights_test.go` | new — table test against all 66 real summaries |
| `static/css/style.css` | none (see §1) |

### 3.1 `tab-timeline.js` — event lane (D1)

In the chart config's `scales` (`tab-timeline.js:280-284`), add:

```js
yEvents: { type: 'linear', display: false, min: 0, max: 1, grid: { display: false } },
```

### 3.2 `tab-timeline.js` — per-day Y stagger (D2)

Replace the `WEIGHTS.forEach` block (`tab-timeline.js:239-264`). Group
*before* building datasets, keyed by the snapped label (same `labels[i]`
value used for the price line — not raw `filingDate`, so non-trading-day
filings that snap forward still collide correctly):

```js
const weightRank = { major: 0, medium: 1, minor: 2 };
const groups = {};   // label -> [{e, i}]
visible.forEach(function (e) {
  const i = snapIndex(e.filingDate);
  if (i < 0 || series[i] == null) return;
  (groups[labels[i]] = groups[labels[i]] || []).push({ e: e, i: i });
});
Object.keys(groups).forEach(function (label) {
  groups[label].sort(function (a, b) {
    return weightRank[a.e.weight] - weightRank[b.e.weight];
  });
});

WEIGHTS.forEach(function (w) {
  const pts = [];
  Object.keys(groups).forEach(function (label) {
    const g = groups[label];
    g.forEach(function (item, slot) {
      if (item.e.weight !== w.key) return;
      const y = 0.12 + (slot / Math.max(g.length - 1, 1)) * 0.76;
      pts.push({ x: label, y: y, ev: item.e, dayCount: g.length });
    });
  });
  const dense = pts.some(function (p) { return p.dayCount > 5; });
  datasets.push({
    type: 'scatter',
    label: w.label + ' filings',
    data: pts,
    backgroundColor: w.color,
    borderColor: w.color,
    pointRadius: (w.key === 'minor' && dense) ? Math.max(w.radius - 2, 2) : w.radius,
    pointHoverRadius: w.radius + 3,
    pointStyle: w.style,
    showLine: false,
    yAxisID: 'yEvents',
    order: 1,
  });
});
```

Formula uses D2's authoritative constants (`0.12` / `0.76`), not the
data-audit section's illustrative `0.15`/`0.70` — the two disagree in the
HLD; D2 is the actual decision.

Prices still plot on `y` (unchanged, `tab-timeline.js:225-237`); events now
plot on `yEvents`, so price zoom/window never moves the lane.

### 3.3 `tab-timeline.js` — click → modal (D3)

Add `onClick` to the chart config, alongside existing `interaction: {mode:
'nearest', intersect: true}` (`tab-timeline.js:279`) so `elements[0]` is
already the nearest point:

```js
onClick: function (evt, elements) {
  if (!elements.length) return;
  const el = elements[0];
  const ds = chart.data.datasets[el.datasetIndex];
  const pt = ds.data[el.index];
  if (pt && pt.ev) openEventModal(pt.ev);
},
```

`openEventModal(ev)` / `closeEventModal()`:

```js
function secFilingUrl(cik, accession) {
  const cikNum = String(Number(cik));
  const noDash = accession.replace(/-/g, '');
  return 'https://www.sec.gov/Archives/edgar/data/' + cikNum + '/' + noDash +
    '/' + accession + '-index.htm';
}

function renderHighlights(h) {
  if (!h || !h.metrics || !h.metrics.length) {
    return '<p class="muted">Financial highlights unavailable.</p>';
  }
  let html = '<div class="meta-grid">';
  h.metrics.forEach(function (m) {
    html += ctx.metaItem(m.label, m.value + (m.delta ? ' (' + m.delta + ')' : ''));
  });
  html += '</div>';
  return html;
}

function openEventModal(ev) {
  const overlay = el('timeline-event-modal');
  if (!overlay) return;
  el('timeline-event-modal-title').textContent = ev.filingDate + ' · ' + ev.form;
  const w = WEIGHTS.filter(function (x) { return x.key === ev.weight; })[0];
  let html = '<span class="badge">' + escHtml(w ? w.label : ev.weight) + '</span> ';
  html += '<strong>' + escHtml(String(ev.category || '').replace(/_/g, ' ')) + '</strong>';
  if (ev.why) html += '<p class="muted">' + escHtml(ev.why) + '</p>';
  html += '<p>' + escHtml(ev.summary || '—') + '</p>';
  html += renderHighlights(ev.highlights);
  html += '<p><code>' + escHtml(ev.accessionNumber) + '</code></p>';
  html += '<p><a href="' + secFilingUrl(ctx.cik, ev.accessionNumber) +
    '" target="_blank" rel="noopener" class="btn btn-ghost">Open on SEC EDGAR</a></p>';
  el('timeline-event-modal-body').innerHTML = html;
  overlay.classList.add('open');
}

function closeEventModal() {
  const overlay = el('timeline-event-modal');
  if (overlay) overlay.classList.remove('open');
}
```

Wire once, e.g. at the end of `wireControls()`:

```js
const overlay = el('timeline-event-modal');
if (overlay) {
  overlay.addEventListener('click', function (e) {
    if (e.target === e.currentTarget) closeEventModal();
  });
  const closeBtn = el('timeline-event-modal-close');
  if (closeBtn) closeBtn.addEventListener('click', closeEventModal);
}
document.addEventListener('keydown', function (e) {
  if (e.key === 'Escape') closeEventModal();
});
```

(D4: Escape only, no focus-trap; backdrop-click uses the exact
`analyze.js:432-434` check.)

### 3.4 `company.html` — modal markup (D3)

Append immediately after `</main>` (`company.html:162`), matching
`index.html:85-91` exactly (no `hidden`, no `role`, no `aria-modal` — none
of those exist on the one real precedent in this codebase):

```html
<div class="modal-overlay" id="timeline-event-modal">
  <div class="modal">
    <button type="button" class="modal-close" id="timeline-event-modal-close" aria-label="Close">&times;</button>
    <h3 id="timeline-event-modal-title"></h3>
    <div id="timeline-event-modal-body"></div>
  </div>
</div>
```

### 3.5 `internal/companyview/highlights.go` (new, D5/D6)

Mirrors `classify.go`'s pure-function style:

```go
package companyview

import "regexp"

// EventHighlight is a best-effort financial callout attached to
// quarterly_results/annual_report events. Nil when nothing could be parsed.
type EventHighlight struct {
	Metrics []HighlightMetric `json:"metrics"`
	Source  string            `json:"source"` // always "summary_parse" for now
}

type HighlightMetric struct {
	Label string `json:"label"`
	Value string `json:"value"`
}

// numThenWord matches "23% ... growth" (filler words, incl. ones with
// digits like "6-MONTH", up to 4 deep).
var numThenWord = regexp.MustCompile(`(?i)(\d+)%\s+(?:[\w-]+\s+){0,4}?(growth|increase|decrease)`)

// wordOfNum matches "growth of 23%".
var wordOfNum = regexp.MustCompile(`(?i)(growth|increase|decrease)\s+of\s+(\d+)%`)

// BuildHighlights extracts a growth-percentage claim from a filing summary
// when the category is one of the two financial-results categories. Returns
// nil when no percentage is found — callers must not fabricate a metric.
//
// This is intentionally narrow: the corpus's quarterly_results/annual_report
// `summary` field is a press-release headline (see LLD §2), not body prose —
// zero of 66 real Kamada summaries contain a dollar figure. Only a
// growth/increase/decrease percentage is ever extracted.
func BuildHighlights(category, summary string) *EventHighlight {
	if category != "quarterly_results" && category != "annual_report" {
		return nil
	}
	if summary == "" {
		return nil
	}
	m1 := numThenWord.FindStringSubmatch(summary)
	m2 := wordOfNum.FindStringSubmatch(summary)
	loc1 := numThenWord.FindStringIndex(summary)
	loc2 := wordOfNum.FindStringIndex(summary)

	var pct, word string
	switch {
	case m1 != nil && (m2 == nil || loc1[0] <= loc2[0]):
		pct, word = m1[1], m1[2]
	case m2 != nil:
		word, pct = m2[1], m2[2]
	default:
		return nil
	}

	label := "Growth"
	if regexp.MustCompile(`(?i)decrease`).MatchString(word) {
		label = "Decline"
	}
	return &EventHighlight{
		Metrics: []HighlightMetric{{Label: label, Value: pct + "%"}},
		Source:  "summary_parse",
	}
}
```

### 3.6 `internal/companyview/timeline.go`

Add the field and wire the call:

```go
// Event, add after Why:
Highlights *EventHighlight `json:"highlights,omitempty"`
```

```go
// BuildEvents, in the loop, after Why: WhyItMatters(r.Category):
Highlights: BuildHighlights(r.Category, r.Summary),
```

## 4. Tests

`internal/companyview/highlights_test.go` — table test seeded from the real
corpus (all 18 distinct summaries in the 66 `quarterly_results`/
`annual_report` rows under `fileDB/companies/0001567529/`, not a
re-sample):

| summary (abridged) | category | want |
|---|---|---|
| `6-K` | quarterly_results | nil |
| `EXHIBIT 99.1` | quarterly_results | nil |
| `Annual report with audited financials and MD&A` | annual_report | nil |
| `...GROWTH OF 23% AND A 96% INCREASE...` | quarterly_results | `{Growth, 23%}` |
| `...GROWTH OF 17% AND A 54% INCREASE...` | quarterly_results | `{Growth, 17%}` |
| `...11% YEAR-OVER-YEAR 6-MONTH TOP LINE GROWTH AND A 35% INCREASE...` | quarterly_results | `{Growth, 11%}` |
| `...OVER 30% YEAR-OVER-YEAR PROFITABILITY GROWTH` | quarterly_results | `{Growth, 30%}` |
| `...REPRESENTING DOUBLE-DIGIT PROFITABLE GROWTH` | quarterly_results | nil (no digit) |
| any category not in {quarterly_results, annual_report} | e.g. `current_report` | nil |
| empty summary | quarterly_results | nil |

`go test ./internal/companyview/...` must pass with these plus existing
tests.

Frontend: no test harness exists for `tab-timeline.js` (none of the other
tab modules have one) — verified by manual smoke instead, per idea file
step 3 and HLD risk #2:

- Kamada CIK `0001567529`, default 2Y window: markers sit on one row,
  independent of price scale.
- Switch to `All` (or a custom range covering `2026-04-09`): confirm the
  17-filing `insider_trade` day renders 17 separable minor markers at
  reduced radius, not one collapsed badge.
- Click a `quarterly_results` marker with a growth headline (e.g.
  `2025-11-10`): modal shows summary, why, a `Growth 30%` metric row,
  accession, and a working SEC EDGAR link.
- Click a marker with no growth match: modal shows "Financial highlights
  unavailable."
- Escape and backdrop-click both dismiss; a second click re-opens cleanly.
- Toggle `tl-lane` to "Major only" — lane stagger recomputes correctly for
  the smaller visible set (grouping runs on `visible`, already filtered).

## 5. Build order

1. `internal/companyview/highlights.go` + `highlights_test.go`; `go test
   ./internal/companyview/...` green.
2. `internal/companyview/timeline.go` — add field, wire `BuildHighlights`
   into `BuildEvents`.
3. `tab-timeline.js` §3.2 (grouping + `yEvents` axis) — verify lane renders
   before adding click behavior, per idea file's implementation order.
4. `company.html` modal markup (§3.4).
5. `tab-timeline.js` §3.3 (`onClick`, open/close, Escape) + `renderHighlights`.
6. Manual smoke (§4) against CIK `0001567529`.
7. `make test`.

## 6. Risks

- **Growth-regex word order:** the two-pattern approach (§3.5) resolves the
  `"GROWTH OF 23%"` vs `"23%... GROWTH"` split seen in the real corpus, but a
  third phrasing outside this single-company sample could still slip
  through silently to `nil` — acceptable per D5 (best-effort, nullable).
- **Multiple percentages in one headline** (e.g. the `2025-08-13` row has
  both an 11% and a 35% figure): `BuildHighlights` picks the leftmost
  match, which is usually but not guaranteed to be the "top-line" one the
  reader cares about most — one `metrics` entry, not a ranked list, per D5.
- **Dense-day radius reduction (D2 §5)** is a single fixed threshold
  (`n > 5`, minor weight only); the `2026-04-09` 17-marker day is the only
  real case that exercises it — confirm hit-targets stay clickable at
  1280px in the manual smoke, not just visually separable.

## 7. Done when

Given CIK `0001567529`: event markers render on one fixed horizontal lane
independent of price value; same-day filings appear at distinct Y positions
(the `2026-04-09` day shows 17 separable minor markers); clicking a marker
opens `#timeline-event-modal` with summary, category, tier, why, accession,
and a working SEC EDGAR link; `quarterly_results` events with a
growth-percentage headline show a metrics row, others show "Financial
highlights unavailable"; Escape and backdrop-click both dismiss the modal;
`go test ./internal/companyview/...` and `make test` pass.
