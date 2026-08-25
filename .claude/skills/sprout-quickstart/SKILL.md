---
description: >
  First-steps orientation for a new sprout-application (fork of jump-starter).
  Use when the user says "I just cloned this", "where do I start", "set this up for X domain",
  or "what do I need to change to make this do Y?". Also useful for Cursor AI onboarding.
---

## What you do

Orient the user (or yourself as an AI agent) in a new sprout fast: read the right files,
identify the three mandatory customization surfaces, and tell the user exactly what to edit.

## The three mandatory changes for every sprout

| # | What | File(s) | Why |
|---|------|---------|-----|
| 1 | Domain prompt | `prompts/system.txt` | Tells the LLM what to do and which fields to populate |
| 2 | LLM output struct | `internal/models/document_analysis.go` (replace or extend) | Defines the JSON schema Gemini returns |
| 3 | Result renderer | `static/js/analyze.js` → `renderAnalysis()` | Displays the new fields in the modal |

These three must stay in sync — see the **new-field** skill for the step-by-step procedure.

## Procedure

1. **Read the project identity markers first:**
   - `AGENTS.md` — sprout checklist (read first; token-efficient index)
   - `sprout.index.json` — same as JSON (good for scripts/tools)
   - `CLAUDE.md` — full architecture (read when you need deeper context)

2. **Understand the current output model:**
   ```
   Read: internal/models/document_analysis.go
   ```
   This is `DocumentAnalysis` — the default generic output struct. Every field has:
   - `json:"..."` — the key in the result JSON the LLM must emit
   - `description:"..."` — passed verbatim to Gemini as the field hint
   - Optional: `enum:"a,b,c"` — constrains Gemini to allowed values

3. **Understand the current prompt:**
   ```
   Read: prompts/system.txt
   Read: prompts/analyze.txt (if it exists)
   ```

4. **Understand the result UI:**
   - `static/js/analyze.js` → `renderAnalysis(container, result)` line ~161
   - It reads `result.metadata`, `result.key_insights`, `result.charts`, etc.
   - When you change the model, update this function to render the new fields.

5. **Run the app and verify the current state:**
   ```
   cp .env.example .env   # fill in LLM_PROVIDER + key
   make run
   ```
   Upload a sample file at `http://localhost:8080`. Confirm the analysis modal shows output.

6. **Then apply domain customizations** using these skills:
   - **new-field** — Add/change a field on the LLM output struct
   - **customize-output** — Replace the whole model with a domain-specific struct
   - **customize-ui** — Colors, copy, layout
   - **add-file-type** — Support a new MIME / file format
   - **add-db-table** — Add a new persistence table

## Key mental model

```
Upload → handlers/files.go → pipeline.Process(goroutine)
                                └─ fileconv (extract text/bytes)
                                └─ llm.Client.Complete(schema from Go struct)
                                └─ result JSON saved beside file
                             ← UI polls /api/files/:id/status
                             ← UI fetches /api/files/:id/result → renderAnalysis()
```

## Common gotchas

- **CGO required**: `SQLite + PDF` use cgo. The Makefile handles it via `CGO_ENABLED=1`.
- **Struct ↔ prompt drift**: If you add a field to the Go struct without mentioning it in the prompt, Gemini may return an empty/null value.
- **Renderer gap**: Adding a struct field without updating `renderAnalysis` means it's computed but never shown.
- **Go module path**: If you rename the module in `go.mod`, update all `import "jump-starter/..."` paths too.
