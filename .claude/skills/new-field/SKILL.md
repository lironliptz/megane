---
description: >
  Add a new field to the LLM output model (PipelineLLMOutput), keep the Gemini JSON schema
  in sync, and wire up display in the result UI. Use when the user says "add a field",
  "track X in the output", or "the LLM should also return Y".
---

## What you do

Add one field end-to-end: Go struct → JSON schema → prompt mention → UI display.

## Background

A sprout-application defines **multiple LLMOutput structs** in `internal/models/` — one per logical
domain concept (e.g. `ContractAnalysis`, `InvoiceData`, `MeetingNotes`). Each implements
`models.LLMOutput` (one method: `SchemaName() string`). The pipeline selects the right struct via
`Pipeline.OutputFor` in `cmd/server/main.go`.

The template ships with `DocumentAnalysis` in `internal/models/document_analysis.go` as the
default/example. Sprout-apps add their own files alongside it.

## Files to touch

| File | Change |
|------|--------|
| `internal/models/<domain>.go` | Add/edit field on the target LLMOutput struct |
| `prompts/<relevant>.txt` | Mention the new field so the LLM knows to populate it |
| `static/js/analyze.js` or result section | Render the new field in the result view |

## Procedure

1. Ask the user: **which LLMOutput struct** (list files in `internal/models/`), **field name**,
   **type** (string / number / bool / []string / object), **one-line description**.
2. Read the target struct to understand tag conventions (`json:"..."` + `description:"..."`).
3. Read the relevant prompt file.
4. Add the field following existing tag style.
5. Update the prompt to include the field and a brief instruction.
6. Add display in `static/`.
7. `go build ./...` — confirm clean.
8. Show diffs; suggest `make run` + upload to verify.

## Constraints

- Never change existing field names/types — breaks stored `.result.json` files.
- Keep `description` tags concise (Gemini uses them as schema hints).
- If the user needs a **new LLMOutput type** (not just a new field): create a new file in
  `internal/models/`, implement `SchemaName()`, and register it in `Pipeline.OutputFor` in
  `cmd/server/main.go`.
