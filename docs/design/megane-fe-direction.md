# megane — frontend design direction

Applied from `prompts/dev/prompt_5_fe_design.txt` via the `frontend-design` skill (2026-08-25).

## Subject

**Financial filing intelligence** — SEC documents, company timelines, price overlays.  
**Audience:** Analysts and operators in data-dense sessions.  
**Job:** Pick a company → scan filings → read timeline without visual fatigue.

## Palette

| Name | Hex | Role |
|------|-----|------|
| Canvas | `#f4f6f9` | Page background |
| Surface | `#ffffff` | Cards, header |
| Mist | `#eef1f6` | Hover, nested panels |
| Rule | `#d8dee9` | Borders, dividers |
| Ink | `#0f172a` | Primary text |
| Slate | `#64748b` | Metadata, labels |
| Lens blue | `#2563eb` | Accent, links, chart line |
| Lens deep | `#1d4ed8` | Accent hover, primary buttons |

Semantic: success `#15803d`, warning `#b45309`, danger `#b91c1c` — restrained, no neon.

## Typography

- **UI / data:** IBM Plex Sans (Google Fonts) — tabular figures for CIK, dates, prices.
- **Scale:** 15px body, line-height 1.45 on dense pages; section labels stay uppercase tracked small caps.
- **Display:** Same family, weight 700 — no decorative serif.

## Layout

- Standard max width: **1680px**
- Wide (company / timeline): **1920px** via `.main-content.layout-wide`
- Header: **48px** sticky; compact main padding `1rem 1.25rem`
- Cards: flat border, minimal shadow; padding `1rem 1.15rem`
- Timeline chart hook: `.timeline-chart-wrap` min-height **420px** (360px mobile)

## Signature element

**Geometric eyeglasses mark** beside the lowercase wordmark — the literal meaning of *megane*
(メガネ), rendered as two lens rings and a bridge in lens blue. One bold brand gesture; everything
else stays quiet and terminal-dense.

## Wireframe

```
┌─ header 48px ─────────────────────────────────────────────────────────────┐
│ [◯─◯] megane    Companies  Analyze  Admin              user@…  Sign out   │
└───────────────────────────────────────────────────────────────────────────┘
┌─ company header (compact, no marketing hero) ────────────────────────────┐
│ KAMADA LTD  [KMDA]     CIK … · Nasdaq · Pharmaceutical Preparations      │
│ Timeline | General | Filings | Events    ← underline active tab         │
└───────────────────────────────────────────────────────────────────────────┘
┌─ tab panel (layout-wide, min-height fixed) ───────────────────────────────┐
│ [1Y] [2Y] [5Y] [All]                                                      │
│ ┌──────────────────────────────────────────────────────────────────────┐ │
│ │                    price line + filing event markers                  │ │
│ │                         (Chart.js, full width)                      │ │
│ └──────────────────────────────────────────────────────────────────────┘ │
└───────────────────────────────────────────────────────────────────────────┘
```

## Self-critique (skill pass)

- Rejected warm cream `#faf9f7` + orange gradient hero — replaced with cool canvas and flat cards.
- Rejected terracotta accent — lens blue fits “analytical clarity” and the glasses metaphor.
- Kept Inter fallback in stack but lead with IBM Plex Sans for a less generic SaaS feel.
- No decorative motion; focus rings and reduced-motion respected.

## Chart tokens (prompt 3)

```css
--chart-price: var(--accent);
--chart-event-major: var(--accent);
--chart-event-minor: var(--border);
```
