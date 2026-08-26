# HLD: Timeline Event Lane, Drill-Down & Highlights

Implements `prompts/dev/prompt_6_timeline_events.txt`. Builds on
`prompt_3_company_view-hld.md` (timeline API, `internal/companyview`
category rules) and the just-fixed `tab-timeline.js` (category-scale
alignment bug — see `prompt_4`-era conversation, not a separate prompt).
Triage: **STANDARD** (medium, explicit) — no DB migration, extends an
existing package and an existing frontend file; the idea file is already
concrete AS-IS + steps, but two of its claims are corrected by data below.

## 1. Objective

P0: move event markers off the price line onto a fixed horizontal lane, and
make them clickable into a detail modal. P1: attach best-effort financial
highlights to `quarterly_results`/`annual_report` events, rescoped against
what the real summary data actually contains (see audit). P2: a prioritized
backlog, design-only.

## 2. Context — what exists today

- `tab-timeline.js` (just fixed): scatter datasets per weight
  (major/medium/minor), `y` = the price at the snapped trading day, `x` =
  the date-string label (category scale). This is exactly the "pinned to
  price" behavior the idea file wants replaced with a fixed lane.
- Modal chrome already exists as reusable CSS (`style.css` `.modal-overlay`,
  `.modal`, `.modal-close`) and one real precedent (`index.html` +
  `analyze.js`): open via `.classList.add('open')`, close via
  `.classList.remove('open')`, dismiss on backdrop click
  (`e.target === e.currentTarget`). **No existing precedent for Escape-key
  dismiss or focus-trap** — `company.html` itself has zero modal markup
  today; this prompt adds the first one there.
- `internal/companyview.Event` already carries `Why` (populated by
  `WhyItMatters(category)`, a pure lookup in `classify.go`) — adding
  `Highlights *EventHighlight` is the same shape of change, not a new
  pattern.
- `companyview.categoryRules` (the "single source of investor-relevance
  judgment", per its own doc comment) already assigns
  major/medium/minor weight per category — the event lane's weight-based
  styling has nothing new to compute; it's the same `Weight` field
  `tab-timeline.js` already reads.

## 3. Data audit (measured, not assumed)

The idea file makes two empirical claims. Both were checked against the
real Kamada corpus (381 filings) rather than accepted as written.

**Claim: "same-day duplicates are rare... e.g. 6-K + 4 on one day."**
Measured: **37 of 294 distinct filing dates (12.6%) have more than one
filing** — not rare. Collision-size distribution: 25 two-way, 5 three-way,
3 four-way, 1 five-way, and **two extreme outliers: 16 and 17 filings on a
single day**. Both outliers are pure `insider_trade` batches (Form 4s) —
`2026-04-09` is 17 `insider_trade` filings, weight `minor`, zero other
categories that day. Of the 37 collision days, 20 include at least one
`major`-weight event mixed with lower-weight filings (e.g. `2022-03-15`:
`annual_report` + `quarterly_results` + two `current_report`).

**What this changes**: same-day events must **not share one `(x, y)`** — they
stack at **distinct Y values within the event lane** so markers remain
visible and clickable individually (see D2). The old "+N filings" cluster
badge (one dot hiding many) is **rejected** for this prompt: even when 16–17
`insider_trade` Form 4s land on one calendar day, each event gets its own
lane slot (tight vertical spacing), and click still opens one filing in the
modal (or a same-day list in the modal header when multiple share a slot —
but never a single marker representing many hidden events). For typical
2–3-way collisions (81% of collision days), three slots at spaced Y values
is enough; for outliers, distribute `n` events evenly across the lane band
(e.g. `y = 0.15 + (i / max(n-1, 1)) * 0.70`).

**Claim (implicit in P1a): summaries contain parseable figures** — "regex
for `$NNN million`, `revenue of`, `EPS`... Low effort." Sampled 8 real
`quarterly_results`/`annual_report` summaries (66 total in corpus):

```
KAMADA REPORTS STRONG THIRD QUARTER FINANCIAL RESULTS DEMONSTRATING
  SUCCESSFUL STRATEGIC TRANSITION AND REITERATES 2022 FINANCIAL GUIDANCE
6-K
Annual report with audited financials and MD&A
6-K
KAMADA REPORTS STRONG THIRD QUARTER AND NINE MONTH 2025 FINANCIAL RESULTS
  WITH OVER 30% YEAR-OVER-YEAR PROFITABILITY GROWTH
```

**Zero of 8 contain a `$NNN million`-style figure.** These are press-release
**headlines** (sourced from the first EX-99 exhibit title in
`build_meta.py`/its Go port, truncated to 240 chars), not body prose with
numbers — and a third of the sample is just the bare form name (`6-K`) with
no headline at all. Two of eight do contain a growth **percentage** in the
headline ("30% Year-Over-Year Profitability Growth"). The actual dollar
figures, if present anywhere, are in the filing body HTML
(`fileDB/.../{accession}/*.htm`), which is never extracted into `meta.json`
today — it's read transiently for classification and discarded.

## 4. Architecture decisions

### D1 — Event lane via a second, hidden linear y-axis

Adopt the idea file's plan as-is: `yEvents` scale (`type: 'linear',
display: false, min: 0, max: 1`), every scatter dataset gets `yAxisID:
'yEvents'`. Base lane height is constant; **per-event Y within the band is
computed by D2** when multiple events share a day. This is the smallest
change consistent with "no annotation plugin, no npm deps"
(prompt 3 LLD §7.4, still true) — it reuses the exact scatter-dataset
pattern already in `tab-timeline.js`, just retargets which axis they plot
against.

### D2 — Same-day collisions: distinct Y per event within the lane band

**Requirement (product):** when several events fall on the same day (or snap
to the same trading day after `snapIndex`), render them at **different Y
values** on the hidden `yEvents` axis — they must **not hide each other**
behind one marker or identical coordinates.

Implementation:

1. Group visible events by snapped trading-day label (the category `x`).
2. For each group of size `n`, assign Y positions evenly across the lane
   band, e.g. `yEventsY(i) = 0.12 + (i / max(n - 1, 1)) * 0.76` for
   `i ∈ [0, n-1]`.
3. When weights differ on the same day (e.g. `annual_report` +
   `quarterly_results` on `2022-03-15`), sort by weight prominence
   (major → medium → minor) before assigning Y so the more important
   marker gets the higher slot.
4. **No cluster badge that collapses multiple filings into one dot** — every
   event in the API payload for the window gets its own scatter point.
5. Optional UX for dense days: slightly reduce `pointRadius` when `n > 5`
   (minor weight only) so 17 Form 4s remain separable; click still targets
   the nearest point.

"Close days" in the brief means same snapped trading day, not adjacent
calendar days on different bars.

### D3 — Click → modal reuses the existing chrome and JS convention exactly

`#timeline-event-modal` follows the `index.html`/`analyze.js` pattern
precisely: `.modal-overlay` + `.modal` classes (no new CSS system),
`classList.add('open')`/`remove('open')` for show/hide, backdrop-click
dismiss via the same `e.target === e.currentTarget` check. This is a
"do what's already done here" decision, not a design choice — inventing a
different modal mechanism for one tab would be the actual overdesign risk.

### D4 — Keyboard dismiss: Escape only for P0; full focus-trap is optional, not blocking

The idea file's acceptance criteria say "keyboard dismiss works" (satisfied
by an `Escape` listener — cheap, no precedent needed) and separately
mention focus-trap in the body text (not in the AC checklist). There is
**zero existing focus-trap precedent anywhere in this codebase** — building
one means enumerating focusable elements, tab-cycling, and restoring focus
on close, real work with no reference implementation to lean on, for an
internal analyst tool. Ship Escape-to-close for P0; treat full focus-trap
as a P1/P2-tier a11y polish item, not a P0 blocker.

### D5 — P1 highlights: rescope P1a to what summaries actually contain

Given the audit (zero of 8 real summaries have a dollar figure; some have a
growth percentage), **do not promise `Revenue: $42.1M`-style callouts from
P1a** as the idea file's example suggests — that data isn't in `summary`
today. Rescope P1a to: extract a growth-percentage claim when the headline
contains one (regex for `\d+%.*(?:growth|increase|decrease)` and similar,
case-insensitive), store it as a single `metrics` entry with `label:
"Growth"` (not fabricated `Revenue`/`EPS` entries with no source data).
When no percentage is found, `Highlights` stays nil — the modal shows
"Financial highlights unavailable" as the idea file already allows. Real
dollar figures remain a P1c (XBRL) or later concern, not this prompt's.

### D6 — `Event.Highlights` is an additive optional field, same shape as `Why`

`highlights.go` mirrors `classify.go`'s existing pure-function,
table-tested style (`ruleFor`-equivalent: a summary string in, an
`*EventHighlight` or `nil` out). No new endpoint, no schema migration —
`BuildEvents` calls it the same way it already calls `WhyItMatters`.

## 5. What this touches

| Area | New | Notes |
|---|---|---|
| `static/js/company/tab-timeline.js` | modify | dual y-axis (D1), per-day Y stagger (D2), `onClick` handler, Escape listener (D4) |
| `static/company.html` | append | `#timeline-event-modal` markup (D3) — first modal in this file |
| `static/css/style.css` | append | lane spacing tokens; reuses existing `.modal-overlay`/`.modal` |
| `internal/companyview/highlights.go` | new | pure functions, rescoped per D5 |
| `internal/companyview/highlights_test.go` | new | table tests against real Kamada headline samples (the 8 above are a start) |
| `internal/companyview/timeline.go` | modify | add `Event.Highlights *EventHighlight`, call from `BuildEvents` |

Not touched: `#events` (Special events) tab, `internal/db`, any migration.

## 6. Out of scope

- P2 overlays (vertical guides, fiscal bands, volume spikes, price-delta,
  insider-cluster lane) — backlog only, per idea file
- Full focus-trap (D4) — optional follow-up
- Real dollar-figure extraction (D5) — needs body-text/XBRL access this
  prompt doesn't have
- Changing hover-tooltip behavior
- Replacing hover with click-only

## 7. Risks

- **Growth-percentage regex (D5) is unverified beyond 2 of 8 samples** —
  broader corpus/company coverage may reveal different headline phrasing
  patterns; the LLD should test against all 66 `quarterly_results`/
  `annual_report` summaries in the corpus, not just the 8 sampled here.
- **Dense same-day stacks (D2)** — 17 markers on one day is correct per spec
  but needs a manual smoke on `2026-04-09` to confirm hit targets and label
  overlap remain usable; reduce radius before reverting to hidden clustering.
- **`yEvents` axis must stay perfectly synced to `y`'s category positions**
  — any future change to the x-axis type (e.g. a real time scale, if a date
  adapter is ever vendored) needs both axes updated together.

## 8. Done when

Given CIK `0001567529`: event markers render on one fixed horizontal lane
independent of price value; clicking a marker opens a modal with full
filing detail and a working SEC link; on days with multiple filings, each
event appears at a **distinct Y** within the lane (none hidden behind a
shared point); the `2026-04-09` 17-filing day shows 17 separable minor
markers (smaller radius if needed), not one collapsed badge;
`quarterly_results` events with a growth-percentage headline show it in the
modal; `go test ./internal/companyview/...` and `make test` pass.

## 9. Deliverables

- Updated `tab-timeline.js` (event lane, per-day Y stagger, click modal)
- `#timeline-event-modal` in `company.html` + supporting CSS
- `internal/companyview/highlights.go` + tests
- This audit, so the LLD doesn't re-litigate collision frequency or
  summary content