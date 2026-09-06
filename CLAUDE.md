# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Agent / sprout quick index (read first for token efficiency)

For **new sprout-applications** or any automated takeover of domain-specific work:

1. **`AGENTS.md`** — Ordered checklist and hot paths (prompts, `pipeline_output.go`, static UI).
2. **`sprout.index.json`** — Same customization surfaces as JSON for tooling.
3. Continue below for full architecture detail.

## Project Overview

**megane** is a Go-based template application for building web-based AI solutions. Derivative projects ("sprout-applications") are created from this template. The design emphasizes a clean separation between reusable infrastructure (this repo) and application-specific logic (prompts, data models, pipelines in sprout-apps).

## Stack

| Layer | Technology |
|---|---|
| Backend | Go + Gin |
| Database | SQLite3 |
| Frontend | Plain HTML + JS libraries + CSS |
| Primary LLM | Google Gemini (`gemini-3-flash-preview` default, `gemini-3.1-pro-preview` for heavier tasks) |
| Auth | Email + password, JWT/session expiring after 86400s, `.env` config |
| Storage | Local `projects/` folder for uploaded files |
| Deploy | Docker + Docker Compose |

## Commands

See `Makefile` (`make run` / `build` / `test` / `docker`). `CGO_ENABLED=1` is required
(sqlite3 + go-fitz use cgo); the Makefile sets it.

## Architecture

### Key Design Decisions

**Routing structure** — Gin routes are grouped by domain: `/auth/*`, `/api/files/*`, `/api/admin/*`. Admin routes are protected by a role-checking middleware that rejects non-admin tokens. This grouping must be preserved in sprout-applications.

**LLM abstraction** — All LLM calls go through a single `llm.Client` interface in `internal/llm/`. The active backend is selected via `LLM_PROVIDER` env var (values: `gemini`, `openai`, `local`). Gemini is the default. Local LLMs (e.g. Ollama) are supported when `LLM_PROVIDER=local` and `LLM_LOCAL_URL` is set. Adding a new vendor requires only implementing the interface — no pipeline changes.

**Prompt-driven logic** — Business logic lives in `prompts/` as plain text files (English or Hebrew), not in Go code. Sprout-applications override or extend these files. Prompts are loaded at startup and passed verbatim to the LLM. This is the primary customization surface between megane and its sprout-applications.

**LLM output types** — Each domain concept populated by the LLM is a separate struct in `internal/models/` that implements `models.LLMOutput` (one method: `SchemaName() string`). The template ships `DocumentAnalysis` as the default. Sprout-applications add their own structs (e.g. `ContractAnalysis`, `InvoiceData`) alongside it. The pipeline selects the right type via `Pipeline.OutputFor(mimeType, promptName)` set in `cmd/server/main.go`; it falls back to `DocumentAnalysis` when unset. Field tags `json:"..."` + `description:"..."` drive the Gemini response schema automatically.

**Pipeline routing** — `Pipeline.Routes []Route` (in `internal/pipeline/route.go`) maps MIME types to pipeline variants. Each `Route` carries its own `Matches`, `OutputFor`, and `Process` function. The first matching route wins; a catch-all default runs `defaultProcess` (document analysis). Sprout-applications register routes in `cmd/server/main.go`. Custom `Route.Process` functions receive a `*Run` (in `run.go`) with `Fail/Event/SetStatus` helpers and can call `pipeline.DefaultProcess` to delegate to the built-in stages.

**File pipeline events table** — Tracks the full lifecycle: `uploaded → converting → llm_pending → llm_done → post_processing → complete | error`. The frontend polls `/api/files/:id/status` periodically to drive the per-file spinner in the UI.

**User roles** — `admin` role unlocks three admin pages: (a) user CRUD (create/delete/modify accounts), (b) projects overview (all uploaded files across users), (c) pipeline statistics (active and historical process counts). `user` role accesses only their own uploads and results. Role is stored in the DB and enforced via Gin middleware.

**Supported file types** — PDF, images, DWG/DXF, Excel (.xlsx), Word (.docx). Each type has a dedicated converter in `internal/fileconv/` responsible for extracting text or structured data before the LLM stage.

### Frontend Pattern

Simple server-rendered HTML with fetch-based polling. Drop-zone supports multi-file upload. A per-file processing spinner updates via periodic status polling. No SPA framework — keep it easy to restyle in sprout-applications. Page width and the analysis modal use `--layout-max-width`, `--modal-wide-max-width`, etc. in `static/css/style.css` so sprout-apps can widen or narrow the shell in one place.

## Environment Variables

See `.env.example` — copy to `.env` and fill in. Non-obvious defaults are noted in the
Stack table above.

## Claude Skills

A `.claude/` directory is maintained in this repo to support AI-driven development. Add project-specific skills and hooks there. For sprout takeover tasks, prefer loading **`AGENTS.md`** first, then this file.