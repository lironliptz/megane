---
description: >
  Debug production-like failures from logs or vague symptoms in jump-starter and sprout-apps:
  infer root cause from log lines, trace errors to exact Go/static files, propose the smallest
  safe fix, and optionally suggest structured logging or tests. Use when the user pastes server
  or pipeline logs, asks where an error comes from, wants minimal patches, or asks for better
  diagnostics around upload/auth/LLM/pipeline stages.
---

## Intent (what you cover)

Treat prompts like these as in-scope:

- “Given these logs, find the likely root cause.”
- “Inspect the repo and trace where this error originates.”
- “Suggest the smallest safe fix.”
- “Add better logging around this stage.”

Prefer evidence over guesses: cite paths and symbols after searching or reading the code.

## Repo map (jump-starter mental model)

When tracing:

| Area | Where to look |
|------|----------------|
| HTTP / JSON errors | `internal/handlers/` (`router.go`, `files.go`, `admin.go`, `auth.go`) |
| JWT / roles | `internal/auth/` |
| Upload → async work | `internal/handlers/files.go` → `go pipeline.Process(...)` |
| Pipeline lifecycle | `internal/pipeline/pipeline.go`; stages `uploaded → … → complete \| error` |
| Pipeline logs | `[pipeline] project=<id> stage=<stage> error=...` via `log.Printf` |
| DB events (UI / ops) | `pipeline_events` rows; API `GET /api/files/:id/status` |
| LLM output types | `internal/models/` — one struct per domain concept implementing `models.LLMOutput`; default is `DocumentAnalysis` in `document_analysis.go` |
| LLM calls | `internal/llm/` (`gemini.go`, `openai.go`, `local.go`, `client.go`) |
| File extraction | `internal/fileconv/` (PDF, image vision parts, Word, Excel, etc.) |
| Prompts | `prompts/*.txt` loaded at startup |
| Static UI | `static/` (`js/analyze.js`, `admin.html`) |

Sprout-apps inherit this layout; customize paths only if the user’s tree differs.

## Procedure

Follow this order unless the user already narrowed the failure:

1. **Reproduce or locate the failing path**  
   - Parse logs for HTTP path, status, `project=` id, `stage=`, provider (`gemini`, `openai`), or SQLite/API messages.  
   - If no logs: ask one tight question (exact action + environment: Docker, local, provider) only if blocking.

2. **Find the exact file/function**  
   - `grep`/`rg` for distinctive substrings (error text, `[pipeline]`, route paths, env keys).  
   - Walk call chain upward from the log line or handler to the failing helper.

3. **Explain root cause**  
   - One short paragraph: what broke, why it surfaced now (e.g. payload shape, token limits, MIME path), and confidence (high/medium/low).

4. **Propose minimal fix**  
   - Smallest diff that fixes the issue or fails gracefully with a clear user-visible error.  
   - No unrelated refactors, renames, or “while we’re here” cleanups.

5. **Logging / tests (when useful)**  
   - Prefer **structured fields** where the codebase already uses `log/slog` (`cmd/server`).  
   - Pipeline still uses `log.Printf`; matching `[pipeline] project=%d stage=%s …` keeps grep-friendly consistency.  
   - Suggest **one** targeted test only if it locks an invariant (e.g. MIME branch, status transition).

6. **Boundary**  
   - Do not redesign architecture in this skill. Escalate only if the minimal fix is unsafe without it.

## Output shape

Answer with:

1. **Likely cause** (bullet or short paragraph).  
2. **Evidence**: file paths + function/handler names (use code citations when editing).  
3. **Minimal fix**: concrete steps or patch sketch.  
4. **Optional**: logging snippet or test idea.  
5. **Verify**: how to confirm (curl, UI step, `make test`, log line to expect).

## Optional local context (when debugging a checkout)

If reproducing locally helps, run once and fold into reasoning:

!`git status --short && git rev-parse --abbrev-ref HEAD 2>/dev/null`

Do not rely on this alone when the user supplied production logs—prioritize their timestamps and messages.
