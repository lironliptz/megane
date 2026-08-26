# LLD: Timeline Event Filter & Period Stripes

Implements `prompts/dev/prompt_7_event_filter_on_timeline_page.txt` and
`prompt_7_event_filter_on_timeline_page-hld.md`. Triage: **STANDARD**
(explicit) — the HLD itself was LIGHT-triaged, but this LLD needs real
code diffs (popover wiring, localStorage merge, canvas plugin), which is
more than a one-screen doc can hold.

## 1. Scope

**In:** P0 filter tree + popover + localStorage persistence, replacing
`#tl-lane`; P1 period-stripe canvas plugin.
**Out (per HLD):** P2 server-side prefs, filtering `#events` tab, filtering
the price series, per-company saved filters.

## 2. Sequencing dependency (not in the HLD — found while grounding this LLD)

Neither prompt 6 nor prompt 7 has landed in the repo yet — `tab-timeline.js`
still has the **pre-prompt-6** shape: events are pinned to the price `y`
value, filtered by the old `lane` var (`all`/`major`/`financials`) at
`tab-timeline.js:218-222`, then grouped into `WEIGHTS` scatter datasets at
`tab-timeline.js:239-264`. Prompt 6 replaces that grouping with a dual
`yEvents` axis and per-day Y stagger but **keeps the same shape**: a
`visible = current.events.filter(...)` array feeding the same
`WEIGHTS.forEach` loop. This LLD's `eventVisible()` predicate (§4.1) is a
drop-in replacement for whatever `lane`-based filter exists in `visible`'s
`.filter()` at the time this ships — same array, same downstream loop,
regardless of whether prompt 6 has landed first. Build prompt 6 first if
possible; if not, apply this LLD's `visible` filter change directly to the
current (pre-prompt-6) `tab-timeline.js:218-222`.

## 3. File-by-file changes

| File | Change |
|------|--------|
| `static/company.html` | remove `#tl-lane` + its `<label>` (lines ~100-105); add filter button + popover markup |
| `static/js/company/tab-timeline.js` | filter-tree build, `eventVisible()`, localStorage read/write/merge, popover wiring, period-stripe plugin |
| `static/css/style.css` | `.timeline-event-filter*`, popover, badge; `--chart-period-a/b` in `:root` (line 12) |

No `static/img/` asset — inline `<svg>` in the button markup (no existing
icon-file precedent in this repo to follow either way).

### 3.1 `company.html` — replace `#tl-lane`

Remove:
```html
<label class="timeline-lane-label" for="tl-lane">Show</label>
<select id="tl-lane" aria-label="Which filings to show">...</select>
```

Add in the same spot inside `.timeline-controls`:
```html
<div class="timeline-event-filter">
  <button type="button" class="btn btn-ghost timeline-event-filter-btn"
    id="tl-filter-btn" aria-expanded="false" aria-controls="tl-filter-panel">
    <svg width="14" height="14" viewBox="0 0 16 16" aria-hidden="true">
      <path d="M1 2h14l-5 6v5l-4 2v-7z" fill="none" stroke="currentColor" stroke-width="1.4"/>
    </svg>
    <span id="tl-filter-badge">0 / 0</span>
  </button>
  <div class="timeline-event-filter-panel" id="tl-filter-panel"
    role="region" aria-label="Filter event types" hidden>
    <div class="timeline-event-filter-actions">
      <button type="button" class="btn btn-ghost" id="tl-filter-all">Select all</button>
      <button type="button" class="btn btn-ghost" id="tl-filter-none">Clear all</button>
      <button type="button" class="modal-close" id="tl-filter-close" aria-label="Close">&times;</button>
    </div>
    <div id="tl-filter-tree"></div>
  </div>
</div>
```
(`hidden` here is fine — this is a new component, not bound by the
existing modal precedent, and `hidden` + CSS `display` toggling is the
plain-HTML default anyway.)

### 3.2 `tab-timeline.js` — filter tree + state

```js
const FILTER_KEY = 'megane.timelineEventFilter.v1';
let filterChecked = null;   // { "form|category": bool } — null until buildFilterTree runs
let filterOpen = false;

function filterLeafKey(e) { return e.form + '|' + e.category; }

function buildFilterTree(events) {
  const byForm = {};   // form -> { count, cats: { category -> count } }
  events.forEach(function (e) {
    const f = byForm[e.form] || (byForm[e.form] = { count: 0, cats: {} });
    f.count++;
    f.cats[e.category] = (f.cats[e.category] || 0) + 1;
  });
  return Object.keys(byForm)
    .map(function (form) { return { form: form, count: byForm[form].count, cats: byForm[form].cats }; })
    .sort(function (a, b) { return b.count - a.count || a.form.localeCompare(b.form); });
}

function loadStoredFilter() {
  try {
    const raw = localStorage.getItem(FILTER_KEY);
    if (!raw) return {};
    const parsed = JSON.parse(raw);
    return (parsed && parsed.version === 1 && parsed.checked) || {};
  } catch (e) { return {}; }   // corrupt/blocked storage: fall back to defaults
}

function saveFilter() {
  try {
    localStorage.setItem(FILTER_KEY, JSON.stringify({ version: 1, checked: filterChecked }));
  } catch (e) { /* storage full/blocked: filter still works this session */ }
}

// Merge stored choices onto the tree built from THIS window's events.
// Unseen leaves default to true (visible) — new categories opt in automatically.
function mergeFilterState(tree) {
  const stored = loadStoredFilter();
  const next = {};
  tree.forEach(function (f) {
    Object.keys(f.cats).forEach(function (cat) {
      const key = f.form + '|' + cat;
      next[key] = key in stored ? !!stored[key] : true;
    });
  });
  filterChecked = next;
}

function eventVisible(e) {
  if (!filterChecked) return true;   // tree not built yet (first render): show everything
  const v = filterChecked[filterLeafKey(e)];
  return v === undefined ? true : v;
}
```

### 3.3 `tab-timeline.js` — wire into `render()`

Replace the `visible` filter (`tab-timeline.js:218-222` today, or prompt 6's
equivalent line — same transform either way):

```js
const tree = buildFilterTree(current.events);
if (!filterChecked) mergeFilterState(tree);   // first load only; toggles don't rebuild
const visible = current.events.filter(eventVisible);
```

Keep tree/badge/panel in sync after every `render()` call:
```js
renderFilterPanel(tree);
updateFilterBadge(tree);
```

### 3.4 `tab-timeline.js` — panel render + badge

```js
function updateFilterBadge(tree) {
  let total = 0, checked = 0;
  tree.forEach(function (f) {
    Object.keys(f.cats).forEach(function (cat) {
      total++;
      if (filterChecked[f.form + '|' + cat]) checked++;
    });
  });
  const badge = el('tl-filter-badge');
  if (badge) badge.textContent = checked + ' / ' + total;
  const btn = el('tl-filter-btn');
  if (btn) btn.classList.toggle('active', checked < total);
}

function renderFilterTree(tree) {
  var html = '';
  tree.forEach(function (f) {
    const cats = Object.keys(f.cats).sort();
    const allOn = cats.every(function (c) { return filterChecked[f.form + '|' + c]; });
    html += '<div class="tl-filter-form">';
    html += '<label><input type="checkbox" data-form="' + escHtml(f.form) + '"' +
      (allOn ? ' checked' : '') + '> ' + escHtml(f.form) + ' (' + f.count + ')</label>';
    cats.forEach(function (cat) {
      const key = f.form + '|' + cat;
      html += '<label class="tl-filter-cat"><input type="checkbox" data-key="' +
        escHtml(key) + '"' + (filterChecked[key] ? ' checked' : '') + '> ' +
        escHtml(cat.replace(/_/g, ' ')) + ' (' + f.cats[cat] + ')</label>';
    });
    html += '</div>';
  });
  el('tl-filter-tree').innerHTML = html;
}
```
(No `escHtml` gap: `form`/`category` are classifier-controlled strings, not
filing prose, but escaping costs nothing and matches the idea file's
acceptance criterion.)

Event delegation for checkbox toggles (bind once, in `wireControls()`):
```js
el('tl-filter-tree').addEventListener('change', function (e) {
  const t = e.target;
  if (t.dataset.key) {
    filterChecked[t.dataset.key] = t.checked;
  } else if (t.dataset.form) {
    Object.keys(filterChecked).forEach(function (k) {
      if (k.indexOf(t.dataset.form + '|') === 0) filterChecked[k] = t.checked;
    });
  }
  saveFilter();
  render();   // live-apply, client-side only — no fetch
});
```

### 3.5 `tab-timeline.js` — popover open/close

```js
function setFilterOpen(open) {
  filterOpen = open;
  el('tl-filter-panel').hidden = !open;
  el('tl-filter-btn').setAttribute('aria-expanded', String(open));
}
// in wireControls():
el('tl-filter-btn').addEventListener('click', function () { setFilterOpen(!filterOpen); });
el('tl-filter-close').addEventListener('click', function () { setFilterOpen(false); });
el('tl-filter-all').addEventListener('click', function () {
  Object.keys(filterChecked).forEach(function (k) { filterChecked[k] = true; });
  saveFilter(); render();
});
el('tl-filter-none').addEventListener('click', function () {
  Object.keys(filterChecked).forEach(function (k) { filterChecked[k] = false; });
  saveFilter(); render();
});
document.addEventListener('click', function (e) {
  if (!filterOpen) return;
  if (el('tl-filter-panel').contains(e.target) || el('tl-filter-btn').contains(e.target)) return;
  setFilterOpen(false);
});
document.addEventListener('keydown', function (e) {
  if (e.key === 'Escape' && filterOpen) setFilterOpen(false);
});
```

### 3.6 `tab-timeline.js` — period stripes (P1)

Per idea file's own spec (its `getPixelForValue`/snap approach is taken
as-is, not re-derived here):

```js
const periodStripesPlugin = {
  id: 'periodStripes',
  beforeDatasetsDraw: function (chart) {
    if (!labels.length) return;   // module-scope `labels` set each render()
    const spanDays = (parseDay(labels[labels.length - 1]) - parseDay(labels[0]));
    const yearly = spanDays > 730;
    const bounds = { top: chart.chartArea.top, bottom: chart.chartArea.bottom };
    const boundaries = periodBoundaries(labels, yearly);
    const ctx2d = chart.ctx;
    for (let i = 0; i < boundaries.length - 1; i++) {
      ctx2d.fillStyle = (i % 2 === 0) ? cssVar('--chart-period-a', 'rgba(15,23,42,0.025)')
                                       : cssVar('--chart-period-b', 'rgba(15,23,42,0.055)');
      const x0 = chart.scales.x.getPixelForValue(boundaries[i]);
      const x1 = chart.scales.x.getPixelForValue(boundaries[i + 1]);
      ctx2d.fillRect(x0, bounds.top, x1 - x0, bounds.bottom - bounds.top);
    }
  },
};
// periodBoundaries(labels, yearly) returns an array of INDEXES into `labels`
// at each month/year start, snapped forward to the first label >= boundary
// (same snap philosophy as snapIndex()); register once: Chart.register(periodStripesPlugin).
```

Register the plugin once at module load (`Chart.register(...)`, not inside
`render()` — Chart.js plugins are global, registering per-render would
duplicate it).

### 3.7 `style.css`

```css
:root {
  /* ...existing tokens... */
  --chart-period-a: rgba(15, 23, 42, 0.025);
  --chart-period-b: rgba(15, 23, 42, 0.055);
}

.timeline-event-filter { position: relative; }
.timeline-event-filter-btn.active { color: var(--accent); border-color: var(--accent); }
.timeline-event-filter-panel {
  position: absolute; top: calc(100% + .35rem); left: 0; z-index: 50;
  background: var(--surface); border: 1px solid var(--border); border-radius: var(--radius);
  padding: .75rem; width: min(90vw, 340px); max-height: 60vh; overflow-y: auto;
  box-shadow: 0 12px 32px rgba(0,0,0,.14);
}
.timeline-event-filter-actions { display: flex; gap: .5rem; margin-bottom: .5rem; }
.tl-filter-form { margin-bottom: .5rem; font-weight: 600; font-size: .82rem; }
.tl-filter-cat { display: block; margin-left: 1.1rem; font-weight: 400; font-size: .8rem; color: var(--text-muted); }
```

## 4. Tests

No frontend test harness exists for any tab module (same gap noted in
prompt 6's LLD) — manual smoke only, per idea file's own acceptance
criteria:

- Kamada `0001567529`, 2Y window: badge shows `n / n` on first load.
- Uncheck `4 → insider_trade`: the `2026-04-09` cluster (17 Form-4s, per
  prompt 6's audit) disappears immediately; badge decrements; price line
  unchanged.
- Uncheck the `4` form checkbox itself: all its categories uncheck together.
- Refresh the page: unchecked state persists (reads `localStorage`).
- Switch preset to `5Y`/`All`: any category newly appearing in the wider
  window defaults to checked (visible) unless it was explicitly
  unchecked before.
- Click outside the popover, then Escape while open: both close it.
- ≤2Y window shows monthly stripes; `All` (>2Y for this corpus, spanning
  2016–2026) shows yearly stripes; stripes stay behind markers and the
  price line.

## 5. Build order

1. `buildFilterTree` + `eventVisible` + wire into `render()`'s `visible`
   filter (keep `#tl-lane` temporarily, per idea file's own order).
2. Popover HTML/CSS + badge, without persistence (`filterChecked` in
   memory only) — verify live filtering end-to-end.
3. `loadStoredFilter`/`saveFilter`/`mergeFilterState` — verify reload
   persistence.
4. Remove `#tl-lane` and the old `lane` variable entirely.
5. Period-stripe plugin + `--chart-period-a/b`.
6. Manual smoke (§4) at 1Y/2Y/5Y/All.

## 6. Risks

- **Depends on prompt 6 landing first** (§2) — if prompt 7 ships alone
  against the current pre-prompt-6 code, `eventVisible()` still works
  (same `visible.filter()` shape) but there is no per-day Y stagger yet,
  so a wide "select all" filter reproduces prompt 6's known same-day
  overlap problem. Not this LLD's bug to fix.
- **`localStorage` can throw** (private-mode Safari, quota, disabled
  storage) — `loadStoredFilter`/`saveFilter` catch and fall back to
  in-memory defaults (§3.2); filtering still works for the session, just
  doesn't persist. Worth a manual check in a private window.
- **Click-outside-to-close listener is global** (`document.addEventListener('click', ...)`,
  §3.5) — cheap for one popover, but if a future tab adds a second
  popover this pattern needs a shared helper instead of copy-paste.

## 7. Done when

Same as the HLD: on `/companies/0001567529#timeline`, unchecking
`4 → insider_trade` hides that day's markers immediately with the price
line unchanged, the `n / m` badge updates, `#tl-lane` is gone, the choice
survives a reload, and the chart shows monthly stripes at ≤2Y / yearly
stripes beyond.
