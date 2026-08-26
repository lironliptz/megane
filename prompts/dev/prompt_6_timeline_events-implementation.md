# Timeline Event Lane, Drill-Down & Highlights — Implementation Notes (as-built)

Implements `prompt_6_timeline_events-lld.md` under the contract in
`prompt_6_timeline_events-hld.md` (D1–D6). Built 2026-08-25.

---

## TL;DR

Shipped exactly as specified: the event lane on a hidden `yEvents` axis (D1), same-day Y stagger
(D2), click → `#timeline-event-modal` (D3), Escape/backdrop dismissal (D4), and
growth-percentage highlights (D5/D6). Zero CSS rules added, as the LLD required.

Verified against the running server and the real corpus with a **48-assertion harness driving the
actual page modules**, plus the prompt-3 regression suite (45 assertions, 6 deep links) re-run
green. The dense-day case is real and covered: **2026-04-09 renders 17 separable markers with 17
distinct Y values**. Both generated SEC EDGAR links return **HTTP 200**.

`BuildHighlights` was validated against the live corpus, not just a transcribed table: **66
financial-results rows, 18 distinct summaries, 4 matches** — the exact counts the LLD predicted,
on the exact dates it named.

`make test` still fails only on the three **pre-existing** invalid files in `generated/`.

---

## What changed (file by file)

| File | Change |
|---|---|
| `internal/companyview/highlights.go` (new) | `EventHighlight`, `HighlightMetric`, `BuildHighlights`; the two word-order regexes plus a hoisted `decreaseWord`. |
| `internal/companyview/highlights_test.go` (new) | Table test over all 18 distinct real summaries, plus category gate, empty summary, leftmost-match, and Decline-label cases. |
| `internal/companyview/timeline.go` | `Event.Highlights *EventHighlight` (`json:"highlights,omitempty"`); `BuildEvents` calls `BuildHighlights(r.Category, r.Summary)` beside the existing `WhyItMatters`. |
| `static/js/company/tab-timeline.js` | `yEvents` scale; grouped per-day stagger replacing the flat marker loop; `onClick`; `secFilingUrl`, `renderHighlights`, `openEventModal`, `closeEventModal`; backdrop/close-button/Escape wiring in `wireControls()`. |
| `static/company.html` | `#timeline-event-modal` appended after `</main>`, matching `index.html:85-91` exactly. |
| `static/css/style.css` | **none** — `.modal-overlay` (`:606`), `.modal-overlay.open` (`:618`), `.modal` (`:620`), `.modal-close` (`:751`), `.meta-grid`, `.badge` all reused as-is. |

---

## Deviations from the design

**1. `decreaseWord` hoisted to a package var.** The LLD's `BuildHighlights` body calls
`regexp.MustCompile(`(?i)decrease`)` inline, which recompiles on every invocation. Same behavior,
compiled once — consistent with `numThenWord`/`wordOfNum` above it.

**2. Match selection uses `FindStringSubmatchIndex` once per pattern**, rather than the LLD's
`FindStringSubmatch` + a separate `FindStringIndex` per pattern (four calls). Identical
leftmost-wins semantics, verified by `TestBuildHighlightsPicksLeftmostMatch` in both word orders.

**3. The modal also renders the tier badge.** The LLD's `openEventModal` snippet renders weight,
category, why, summary, highlights, accession, and link — but its own "Done when" (§7) lists
**tier** among the required fields. Added `ev.tierLabel` as a second `.badge`; no new markup class.

**4. `renderHighlights` keeps the LLD's `m.delta` reference** even though `HighlightMetric` has no
`Delta` field. It is a doc inconsistency between §3.3 and §3.5; in JS the property is `undefined`
→ falsy → the suffix is simply omitted. Left as written so the code stays forward-compatible if a
delta is added, but flagged here because the struct does not currently produce one.

**5. Did not revert `DEFAULT_TAB`.** `shell.js:15` reads `'general'`, not the `'timeline'` that
prompt 3's design specified. I restored it once during that task; it was re-applied, so I left it
alone here as out of this LLD's scope — and the user has since **confirmed the General-first
default is intentional** (2026-08-26). `shell.js` and `company.html` are self-consistent about it.
The only consequence for verification is that the Timeline tab is opened explicitly rather than
being the landing tab; the harness does so, as a user would.

**6. Frontend verified by harness, not manual browser smoke.** The LLD (§4) prescribes manual
smoke because "no test harness exists for `tab-timeline.js`". One does exist from prompt 3, so
every §4 smoke item was automated instead — stronger and repeatable. See below.

---

## Tests

```
go test ./internal/companyview/          # ok — 22 new highlight cases + existing
go test ./cmd/... ./internal/...         # ok — all packages
go vet ./internal/companyview/           # clean
gofmt -l <files touched>                 # clean
node --check static/js/company/tab-timeline.js   # clean
make test                                # FAILS — pre-existing generated/ snippets only
```

**Corpus validation (throwaway, not committed):** ran `BuildHighlights` over every
`meta.json` under `fileDB/companies/0001567529/` — **66** financial-results rows, **18** distinct
summaries, **4** matches (`2024-05-08` 23%, `2025-05-14` 17%, `2025-08-13` 11%, `2025-11-10` 30%),
and **zero** metrics produced for any non-financial category. Exactly the LLD §2 audit.

**On the wire:** `GET /api/companies/0001567529/timeline?from=2016-01-06&to=2026-08-20` returns
381 events, 4 carrying `highlights`, 51 of 55 `quarterly_results` without — the honest
"unavailable" majority.

### Definition of Done — verified literally

Verification server on **:8199** with its own DB; the user's server on :8123 was left untouched.
A 48-assertion harness loaded the real `shell.js` → `tab-general` → `tab-filings` →
`tab-timeline` → `tab-events` in page order against the live API, opened the Timeline tab, and
drove the real `chart.config.options.onClick`. **All 48 passed:**

| DoD element | Evidence |
|---|---|
| Markers on a fixed lane, independent of price | `yEvents` exists, `linear/display:false/0..1`; all 3 marker datasets carry `yAxisID:'yEvents'`; price line has none; every marker Y within `[0.12, 0.88]` |
| Same-day filings at distinct Y | **2026-04-09 → 17 markers, 17 distinct Y**; no two markers share a coordinate anywhere; stagger spans >0.5 of the band |
| Dense-day radius reduction | minor radius reduced below 3; major radius unchanged at 7 |
| Weight ordering | major occupies a higher slot than minor on mixed days |
| Click opens modal | opens with date+form title, category, **tier badge**, why, summary, accession |
| Working SEC EDGAR link | `…/data/1567529/000121390025107836/0001213900-25-107836-index.htm` → **HTTP 200** (second link also 200); unpadded CIK, undashed path, dashed filename; `target="_blank" rel="noopener"` |
| Growth metric shown | `2025-11-10` modal renders **Growth 30%** in `.meta-grid`/`.meta-item` |
| "Unavailable" otherwise | an `insider_trade` event renders "Financial highlights unavailable." with no `meta-grid` |
| Escape dismisses | yes; other keys do not toggle |
| Backdrop click dismisses | yes; a click inside the modal does **not** close it |
| Re-open works | modal re-opens cleanly after each dismissal |
| Lane toggle recomputes | "Major only" re-renders, keeps `yEvents`, drops minor markers, restaggers |

**Regression:** the prompt-3 harness re-ran green — **45 assertions × 6 start hashes** (`none`,
`#timeline`, `#filings`, `#general`, `#events`, `#bogus`). Two of its assertions needed updating,
neither a defect in this work:

- `TL marker y sits on price line` asserted markers pin to the price value — **the exact behavior
  D1 deliberately replaces**. Rewritten to assert the lane (`yAxisID === 'yEvents'`).
- `T start tab from hash is timeline` hardcoded the old default; it now reads `DEFAULT_TAB` from
  `shell.js` so the harness does not fight deviation 5.
- The harness also needed `getComputedStyle`, which current `tab-timeline.js` requires via
  `cssVar()` — that helper belongs to the **in-flight theming/date-range feature**, not to this work.

---

## How to enable / roll back

Live once rebuilt; no env, no migration, no new endpoint — `Highlights` rides the existing
`/timeline` payload as an optional field.

**Roll back:** delete `internal/companyview/highlights.go` + its test, drop the two lines in
`timeline.go`, revert the `tab-timeline.js` hunks, and remove the `#timeline-event-modal` block
from `company.html`. Nothing else references any of it; `style.css` was never touched. Older
clients ignore the added JSON field.

---

## Follow-ups

1. **`m.delta` in `renderHighlights` has no producer** (deviation 4). Either add a `Delta` field to
   `HighlightMetric` or drop the expression — currently dead but harmless.
2. **No focus-trap** in the modal (D4, deliberate). Escape + backdrop + close button work; full
   focus management remains the P1/P2 a11y item the HLD deferred.
3. **On-chart callout deferred** (LLD §1, Option B). The acceptance criterion is satisfied by that
   documented deferral, not by a canvas label plugin.
4. **Growth regex is single-company-tuned.** Both corpus word orders are handled, but a third
   phrasing would silently yield `nil` — acceptable per D5, worth revisiting when a second company
   lands.
5. **`generated/*/route_snippet.go` still breaks `go test ./...`** — three files that are snippets,
   not valid Go, present at `HEAD`. Carried from the prompt-1 and prompt-3 reports; renaming to
   `.go.txt` is the one-line fix.
6. **`DEFAULT_TAB` is `'general'` — confirmed intentional**, not an open item. Recorded here only
   because prompt 3's design doc still says the page lands on Timeline; see that prompt's
   implementation report, follow-up 1, for the superseding note.


---

## Touched files

**New (2):** `internal/companyview/highlights.go`, `internal/companyview/highlights_test.go`

**Modified (3):** `internal/companyview/timeline.go`, `static/js/company/tab-timeline.js`,
`static/company.html`

**Unchanged by design:** `static/css/style.css` (zero new rules, per LLD §1)

No commit was created.
