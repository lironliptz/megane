# Timeline Event Filter & Period Stripes — Implementation Notes (as-built)

Implements `prompt_7_event_filter_on_timeline_page-lld.md` under the contract in
`prompt_7_event_filter_on_timeline_page-hld.md`. Built 2026-08-26, on top of prompt 6
(event lane + modal), which was already landed in the working tree when this started.

---

## TL;DR

Shipped exactly as specified: `#tl-lane` removed and replaced with a Form → category filter
popover (badge, live client-side filtering, `localStorage` persistence, select-all/clear-all,
Escape/outside-click dismissal), plus a `beforeDatasetsDraw` period-stripe plugin (monthly ≤2Y,
yearly beyond). Zero new files; three files touched, matching the LLD's plan exactly.

Verified against a real, isolated verification server (its own DB copy, port `:8199`, the user's
`:8123` server untouched) with a **41-assertion Node harness** that loads the actual
`static/js/app.js` and `static/js/company/tab-timeline.js` into a `vm` context with a fake DOM and
a mocked `Chart`, and drives the real delegated listeners, the real `buildFilterTree`/
`eventVisible`/`mergeFilterState`, and the real registered `periodStripes` plugin — not
reimplementations. All 41 passed. The harness caught one real bug in the first implementation pass
(see Deviations #1), which was fixed and re-verified.

`make test` — same three **pre-existing** invalid `generated/*/route_snippet.go` failures as prompt
6's report; every real Go package passes.

---

## What changed (file by file)

| File | Change |
|---|---|
| `static/company.html` | Removed `#tl-lane` `<select>` + its label. Added `.timeline-event-filter` (button with inline SVG funnel icon + `n / m` badge) and `#tl-filter-panel` popover (Select all / Clear all / × buttons, `#tl-filter-tree` host) in the same spot in `.timeline-controls`. |
| `static/js/company/tab-timeline.js` | New: `FILTER_KEY`, `filterChecked`, `filterOpen` state; `filterLeafKey`, `buildFilterTree`, `loadStoredFilter`, `saveFilter`, `mergeFilterState`, `eventVisible`, `updateFilterBadge`, `renderFilterTree`, `setFilterOpen`; `periodBoundaries` + the `periodStripes` plugin (registered once via `Chart.register` at module load). `render()`'s old `lane`-based filter replaced with `buildFilterTree`/`eventVisible`; `renderFilterTree`/`updateFilterBadge` called at the end of every render. `wireControls()`: removed `#tl-lane` wiring, added filter-tree change delegation, open/close/select-all/clear-all/outside-click wiring, and folded filter-popover dismissal into the existing Escape listener alongside the modal. |
| `static/css/style.css` | Added `--chart-period-a`/`--chart-period-b` to `:root`. Added `.timeline-event-filter*`, `.tl-filter-form`, `.tl-filter-cat` rules for the button/popover/tree. |

No `static/img/` asset — the funnel icon is inline `<svg>` in `company.html`, per the LLD (no
existing icon-file precedent to follow either way).

---

## Deviations from the design

**1. Fixed a real bug the LLD didn't anticipate: mid-session window widening desynced the badge
from actual filtering.** `mergeFilterState` only ran once (`if (!filterChecked)`), so switching
from the 2Y default to the `All` preset within one page visit surfaced form/category leaves never
seen before. `eventVisible()` already treats an unknown leaf as visible (`v === undefined ? true :
v`), but `renderFilterTree`/`updateFilterBadge` read `filterChecked[key]` directly, where
`undefined` is falsy — so newly-discovered leaves would silently render **unchecked** in the
popover and be **excluded from the checked count**, while `eventVisible` was actually **showing**
their markers. The harness's widen-to-`All` assertions (`badge after widening to All: 26 / 27`
with `total0=15` for the 2Y window) caught this directly. Fixed by extending the "unseen leaf
defaults to visible" rule to every `render()`, not just the first — any render after the initial
merge now backfills `filterChecked[key] = true` for any leaf not already present, keeping
`eventVisible`, the badge, and the checkboxes in agreement. See the comment at the fix site in
`tab-timeline.js` (`render()`, right after `buildFilterTree`).

**2. Escape closes both dismissible surfaces from one listener**, not two separate ones. The LLD
sketched the modal's existing Escape handler and the filter panel's Escape handling as separate
snippets; implemented as one `keydown` listener that calls `closeEventModal()` unconditionally (a
no-op if already closed, per existing behavior) and `setFilterOpen(false)` only `if (filterOpen)`.
One less global listener, same observable behavior as two.

**3. Slightly richer CSS than the LLD's terse sketch** — `.timeline-event-filter-btn svg` color,
flex layout on `.tl-filter-form label`/`.tl-filter-cat` (icon/checkbox alignment), and
`.timeline-event-filter-actions .modal-close { margin-left: auto }` so the reused `.modal-close`
(which is `float: right` by default, ignored on a flex item) sits at the row's end. Cosmetic only;
same class names and structure the LLD specified.

**4. Small doc-comment fix at the top of `tab-timeline.js`.** The file header still described "the
lane toggle" as a dataset `hidden` flag — stale even before this prompt (no code ever did that;
`#tl-lane` worked by excluding events from `visible`, same mechanism the new filter uses one level
up). Updated the comment to describe the actual mechanism and point at the prompt 7 filter.

No other deviations — filter tree structure, persistence shape (`{version:1, checked:{...}}`),
merge-on-load default-true rule, live-apply-on-toggle, and the period-stripe plugin's monthly/yearly
threshold (≤2Y / >2Y) all match the LLD as written.

---

## Tests

```
node --check static/js/company/tab-timeline.js     # clean
node --check static/company.html                    # n/a (not JS); visual + id-uniqueness check below
make test                                            # FAILS only on the 3 pre-existing generated/ files
go test ./internal/companyview/... ./internal/...    # ok — unaffected (no Go files touched)
```

Element-id sanity: every new id (`tl-filter-btn`, `tl-filter-panel`, `tl-filter-tree`,
`tl-filter-all`, `tl-filter-none`, `tl-filter-close`, `tl-filter-badge`) appears exactly once in
`company.html`.

### Definition of Done — verified literally

Verification server on `:8199` against a **copy** of the real DB (`fileDB` untouched, the user's
`:8123` server never touched). A 41-assertion Node harness (`vm` + fake DOM + mocked `Chart`,
throwaway, not committed) loaded the real `app.js` + `tab-timeline.js`, called the real
`CompanyTabs`-registered module's `init(ctx)` against the live API, and drove the real captured
event listeners directly (not simulated clicks on a real browser DOM, but the exact same JS
functions a browser would call).

| DoD element | Evidence |
|---|---|
| Filter icon + `n / m` badge visible; `#tl-lane` removed | Badge starts `15 / 15` on a fresh 2Y load; `#tl-lane` never referenced by the module |
| Popover lists Form → category with counts | `#tl-filter-tree` HTML parsed back; `4 (26)` / `4\|insider_trade` present and checked |
| Unchecking hides matching markers immediately, price unchanged | `2026-04-09`: 17 form-4/insider_trade markers → 0 after one delegated `change` event; price dataset (type `line`) untouched, same length |
| Parent form toggle affects all its categories | Unchecking the `4` form checkbox drops all 26 of its markers to 0; the parent checkbox itself renders unchecked |
| Selection persisted in `localStorage`, restored on reload | Stored payload `{version:1, checked:{"4\|insider_trade":false,...}}`; a **fresh module instance** sharing the same `localStorage` object (simulating a reload) restores the "4" form unchecked with zero user action |
| New categories default to visible | A genuinely narrower window (`2026-06-20..2026-08-20`, 8 leaves) saved first, then widened to the full corpus (27 leaves) on the same simulated browser: badge stays `n / n` — every newly-seen leaf defaults checked |
| Windows ≤2Y monthly / >2Y yearly stripes | 2Y chart → 25 stripe segments (~24 months); full ~10.6y chart → 11 segments (~10 years); both via the real registered `periodStripes.beforeDatasetsDraw` |
| Stripes behind markers, subtle, align to calendar boundaries | Plugin hooks `beforeDatasetsDraw` (draws before any dataset); boundaries snap forward to the first trading-day label ≥ each calendar start, same philosophy as `snapIndex` |
| Escape / outside-click / × dismiss the popover | All three drive `panel.hidden` to `true`; a click *inside* the panel does not close it (delegated-listener contains-check) |
| Quality: `escHtml` on filter labels | `renderFilterTree` escapes form/category text even though it's classifier-controlled, per LLD |
| Manual smoke item: uncheck `4/insider_trade`, `2026-04-09` cluster disappears, re-enable persists | Covered directly (see rows above) plus a fresh "Select all" re-check restoring the badge to `n / n` |

**Regression (prompt 6, same file):** re-ran a click on a real `quarterly_results` scatter point
through `chart.options.onClick` — modal still opens (`classList.contains('open') === true`);
Escape still closes it, and leaves the (already-closed) filter popover untouched. No interference
between the two Escape-handled surfaces.

---

## How to enable / roll back

Live once the static assets are served (no build step, no env var, no migration, no new endpoint —
purely `static/`). Filtering and stripes are client-side only against the existing `/timeline`
payload.

**Roll back:** revert the three files. `localStorage` key `megane.timelineEventFilter.v1` is
simply ignored by older code if left behind (dead key, no migration needed either direction).

---

## Follow-ups

1. **No focus-trap in the filter popover** (not required by the LLD — "small popover", matches D4's
   precedent from prompt 6 for the modal).
2. **`.timeline-event-filter-panel` has no explicit dark-mode / RTL check** — it reuses `--surface`/
   `--border`/`--text-muted` tokens exactly like the rest of the page, so it should inherit both for
   free, but wasn't visually smoke-tested in a real browser (no browser available in this
   environment — verified via harness + code review instead, per the LLD's own note that no
   frontend test harness previously existed for this module).
3. **The mid-session widen fix (Deviation #1) has no dedicated unit test** beyond the harness
   assertions here (throwaway, not committed). If a real frontend test suite is ever added for
   `tab-timeline.js`, this is the first case worth codifying — it is not obvious from reading the
   LLD alone.
4. **P2 (server-side prefs) remains out of scope**, as designed — `localStorage` only.

---

## Touched files

**Modified (3):** `static/company.html`, `static/js/company/tab-timeline.js`, `static/css/style.css`

**New:** none

No commit was created.
