# HLD: Timeline Event Filter & Period Stripes

Implements `prompts/dev/prompt_7_event_filter_on_timeline_page.txt`. Builds on
prompt 6 (event lane, modal — unchanged by this prompt).
Triage: **LIGHT** (forced) — idea file is self-contained: concrete AS-IS
(`#tl-lane`, `lane` var in `tab-timeline.js`), file checklist, no DB/API
changes for P0.

## Objective

Replace the coarse `#tl-lane` dropdown with a two-level (Form → category)
checkbox filter popover, applied client-side to the already-loaded timeline
events and persisted per-browser. Add subtle month/year gridlines behind the
chart so long windows are easier to scan.

## Decisions

- **Filter tree is built from `current.events` in the loaded window**, not
  the full classifier vocabulary — only form/category pairs actually present
  get a row, per idea file. Live-apply on every checkbox toggle (`render()`
  re-runs), no separate Apply button — cheapest interaction, matches how
  `#tl-lane`'s `change` handler already re-renders client-side today.
- **Persistence: `localStorage` only for P0** (`megane.timelineEventFilter.v1`),
  global per-browser, not per-CIK — same storage mechanism `app.js` already
  uses for the auth token, no new pattern. Server-side prefs (P2) are
  explicitly deferred; not designed here.
- **Period stripes are a `beforeDatasetsDraw` Chart.js plugin**, same
  inline-plugin approach prompt 6 already established for canvas drawing —
  no annotation plugin, no new deps.

## Out of scope

- P2 server-side/cross-device preference sync
- Filtering the Special events tab (`#events`)
- Filtering the price series or trading days
- Per-company saved filters, export/share of filter presets

## Done when

On `/companies/0001567529#timeline`, unchecking `4 → insider_trade` in the
funnel popover immediately hides that day's Form-4 markers (price line
unchanged), the `n / m` badge updates, `#tl-lane` is gone, and the choice
survives a page reload; at a ≤2Y window the chart shows subtle monthly
bands, at >2Y yearly bands.

## Deliverables

- `#tl-event-filter` button + `#tl-event-filter-panel` popover in
  `static/company.html`, replacing `#tl-lane`
- Filter-tree build, live client-side filtering, localStorage read/write/merge
  in `static/js/company/tab-timeline.js`
- `beforeDatasetsDraw` period-stripe plugin in the same file
- `.timeline-event-filter*` / popover / badge CSS + `--chart-period-a/b`
  tokens in `static/css/style.css`
- Inline SVG funnel icon (16×16)
