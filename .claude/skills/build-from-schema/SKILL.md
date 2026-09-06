---
description: >
  Turn a converged data-modeling schema into production code (struct, prompt, route).
  Use when the user says "build the model", "generate code for this schema", or
  "deploy the data model".
---

# Skill: build-from-schema

Turn a converged data-modeling schema into production code (struct, prompt, route).
Use when the user says "build the model", "generate code for this schema", or "deploy the data model".

## What this does

This skill uses the `internal/datamodeling/codegen` package to convert a converged schema (from the Data Modeling admin tab) into runnable production artifacts:
1. `internal/models/<slug>_analysis.go` (the Go struct)
2. `prompts/<slug>.txt` (the extraction prompt)
3. `generated/<slug>/route_snippet.go` (the pipeline route)
4. `generated/<slug>/renderer.js` (the UI renderer)

## How to use

The user should use the **Data Modeling -> Build Production Models** UI in the admin panel to confirm fields, set rules, and generate the prompt.

If you need to trigger it programmatically or explain it to the user:
1. Ensure the schema is finalized (has a `dm_schemas` entry).
2. The user configures fields in the UI (saved to `dm_build_configs`).
3. Clicking **Build** calls `POST /api/admin/datamodeling/types/:id/build/apply`.
4. The generated files are written to the codebase.
5. You may need to manually merge the `route_snippet.go` into `cmd/server/main.go` and `renderer.js` into `static/js/analyze.js`.
