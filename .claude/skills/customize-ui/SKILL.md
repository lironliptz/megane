---
description: >
  Change colors, copy, layout, fonts, or the result display of the frontend.
  Use when the user says "change the color scheme", "rename the app", "make it wider",
  "change what shows in the result modal", or "update the UI for my brand".
---

## What you do

Guide changes to the static UI: CSS variables, HTML copy, layout widths, and the
JavaScript result renderer. No backend changes needed for pure UI work.

## Files

| File | Role |
|------|------|
| `static/css/style.css` | All design tokens (CSS vars), layout, component styles |
| `static/index.html` | Landing page: app name, tagline, upload drop-zone copy |
| `static/admin.html` | Admin panel: tab labels, form copy, Jump-Start wizard |
| `static/login.html` | Login page: app name, branding |
| `static/js/analyze.js` → `renderAnalysis()` | Result modal content (line ~161) |

## CSS design tokens (`:root` in `style.css`)

All colors, spacing, and widths are CSS variables. Change them here — nowhere else.

```css
:root {
  --bg:        #1a1d23;      /* page background */
  --surface:   #22252d;      /* cards, header */
  --surface2:  #2c3040;      /* nested surfaces */
  --border:    #3a3f52;      /* borders */
  --accent:    #4f8ef7;      /* primary brand color (buttons, links, highlights) */
  --accent-h:  #3a7de6;      /* accent hover state */
  --text:      #e2e8f0;      /* body text */
  --text-muted:#8892a4;      /* secondary text */
  --danger:    #e05c5c;      /* error states */
  --success:   #4caf7d;      /* success states */
  --warning:   #f0a946;      /* warning states */
  --radius:    8px;           /* border radius */

  /* Layout — tune these per sprout-app */
  --layout-max-width:     1680px;             /* max page content width */
  --modal-max-width:      min(94vw, 880px);   /* standard result modal */
  --modal-wide-max-width: min(98vw, 1520px);  /* wide modal (charts + tables) */
}
```

### Common theme changes

**Light theme**: flip `--bg` → `#f5f7fa`, `--surface` → `#ffffff`, `--text` → `#1a1d23`.

**Brand accent** (e.g. green): change `--accent: #28a745` and `--accent-h: #218838`.

**Narrower layout**: `--layout-max-width: 1200px; --modal-wide-max-width: min(96vw, 1200px)`.

## App name and copy

Search for "jump-starter" (case-insensitive) across all HTML files and replace:

```bash
grep -rn -i "jump-starter\|jumpstarter\|jump starter" static/
```

Key spots:
- `<title>` tags in all HTML files
- `.app-name` span in `static/index.html` header
- Any tagline text in the drop-zone (`static/index.html`)
- Login page heading (`static/login.html`)
- Admin page heading (`static/admin.html`)

## Result modal renderer (`static/js/analyze.js`)

`renderAnalysis(container, result)` (line ~161) builds the modal HTML from the LLM JSON.

### Adding a new section

```js
function renderAnalysis(container, result) {
  resetCharts();
  let html = '';

  // --- Add your section ---
  const myField = Array.isArray(result.my_field) ? result.my_field : [];
  html += '<section class="result-section">';
  html += '<h4><span class="section-icon">&#128203;</span> My Field</h4>';
  if (myField.length) {
    html += '<ul class="insight-list">';
    myField.forEach(item => { html += '<li>' + escHtml(item) + '</li>'; });
    html += '</ul>';
  } else {
    html += '<p class="muted">None found.</p>';
  }
  html += '</section>';

  // ... rest of sections ...
  container.innerHTML = html;
}
```

### Removing a section

Delete or comment out the `html +=` block for that section. The LLM still computes the field
(it's schema-driven) but it won't be displayed — that's fine.

### Available helper functions

- `escHtml(str)` — HTML-escapes a string (always use for user/LLM data)
- `renderChartsInto(hostEl, chartsArray)` — draws Chart.js charts from `LLMChartSpec[]`
- `resetCharts()` — destroys old Chart.js instances (call at top of `renderAnalysis`)

## Upload drop-zone

The drop-zone text and accepted file types are in `static/index.html`.
To restrict the visible file picker (cosmetic only — server enforces MIME):

```html
<input type="file" id="file-input" multiple
       accept=".pdf,.docx,.xlsx,.png,.jpg,.txt">
```

## CSS classes reference

| Class | Usage |
|-------|-------|
| `.result-section` | Modal section block with border + padding |
| `.insight-list` | Bulleted list for string arrays |
| `.muted` | Gray placeholder text |
| `.badge` | Status chip; variants: `.uploaded`, `.complete`, `.error`, `.llm_done` |
| `.btn-primary` | Blue filled button |
| `.section-icon` | Emoji icon inline before `<h4>` text |

## Constraints

- No inline styles on new UI elements — use CSS classes or extend `:root` variables.
- `escHtml()` is mandatory for any LLM or user-supplied string rendered into innerHTML.
- Don't touch `static/js/app.js` for result rendering — that file handles upload/poll logic only.
