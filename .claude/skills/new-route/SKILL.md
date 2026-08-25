---
description: >
  Scaffold a new API endpoint: handler function, router registration, and auth level.
  Use when the user says "add an endpoint", "create a route for X", or "I need a new API".
---

## What you do

Add one route end-to-end: handler → registration → (optional) frontend call.

## Files to touch

| File | Change |
|------|--------|
| `internal/handlers/<domain>.go` | New handler method (or new file if new domain) |
| `internal/handlers/router.go` | Register the route in the correct group |
| `static/js/app.js` or relevant page | Add fetch call if the user needs frontend wiring |

For admin-only endpoints use `internal/admin/admin.go` and the `/api/admin/*` group instead.

**Adding a pipeline route** (file-processing variant, not HTTP) is different — see `internal/pipeline/route.go` and register in `Pipeline.Routes` in `cmd/server/main.go`.

## Procedure

1. Ask (if not stated): **HTTP method + path**, **auth level** (public / user / admin), **request shape**, **response shape**.
2. Read `internal/handlers/router.go` to see existing groups and handler pattern.
3. Read the most relevant existing handler file as a style reference.
4. Write the handler following the existing pattern:
   - Parse inputs, validate, call DB/LLM/pipeline, return `gin.H{"data": ..., "error": nil}`.
   - Return correct HTTP status codes (400 bad input, 404 not found, 500 internal).
5. Register in `router.go` inside the correct group (`/auth`, `/api/files`, `/api/admin`).
6. Run `go build ./...` — fix any errors before reporting done.
7. Show the diff and a sample `curl` invocation to test it.

## Constraints

- No new Gin groups unless the user explicitly wants a new domain section.
- Admin endpoints go in `internal/admin/`, not `internal/handlers/`.
- Always use `auth.AuthRequired()` or `auth.AdminRequired()` — never leave a stateful endpoint public without explicit user confirmation.
