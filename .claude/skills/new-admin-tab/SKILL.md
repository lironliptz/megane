---
description: >
  Scaffold a new tab in the admin panel: backend endpoint in internal/admin/ and matching
  tab UI in static/admin.html. Use when the user says "add an admin page/tab/section for X".
---

## What you do

Add one admin tab end-to-end: Go handler → route registration → HTML tab + JS fetch.

## Files to touch

| File | Change |
|------|--------|
| `internal/admin/admin.go` | New handler method |
| `internal/handlers/router.go` | Register under `/api/admin/*` with `auth.AdminRequired()` |
| `static/admin.html` | New tab button + panel div + JS fetch |

## Procedure

1. Ask (if not stated): **tab name**, **what data it shows**, **any actions** (read-only or mutating).
2. Read `internal/admin/admin.go` to understand handler style.
3. Read `static/admin.html` — find the tab button list and panel container pattern.
4. Add the handler to `internal/admin/admin.go` returning `gin.H{"data": ..., "error": nil}`.
5. Register the route in `router.go` under the admin group.
6. In `static/admin.html`:
   - Add a `<button class="tab-btn" data-tab="<name>">Label</button>` alongside existing tabs.
   - Add a `<div id="tab-<name>" class="tab-panel">` with a table or card layout.
   - Add a `load<Name>()` JS function that fetches the new endpoint and renders results.
   - Call `load<Name>()` when the tab is activated (follow existing tab-switch pattern).
7. Run `go build ./...` — fix errors before reporting done.
8. Tell the user to open `/admin`, click the new tab, and verify data loads.

## Constraints

- Match the existing tab CSS classes exactly so styling is consistent.
- All admin routes must use `auth.AdminRequired()` — never downgrade to `auth.AuthRequired()`.
- Read-only tabs: GET only. Mutating tabs: confirm with user before adding DELETE/POST actions.
