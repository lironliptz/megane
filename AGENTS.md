# Agent index — megane / sprout-apps

**Read this file first** when taking over a new sprout (fork/copy of this template). It lists
*where application-specific logic lives* so you avoid scanning the whole repo.

| Doc | Role |
|-----|------|
| **This file (`AGENTS.md`)** | Sprout checklist + hot paths (start here). |
| **`sprout.index.json`** | Same facts as structured metadata (scripts / tools). |
| **`CLAUDE.md`** | Deeper architecture and conventions for Claude Code. |

---

## Skills — invoke these for common tasks

Skills live in `.claude/skills/<name>/SKILL.md`. Each has a step-by-step procedure for one
specific task. Use whichever matches the user's request.

| Skill | When to use |
|-------|-------------|
| `sprout-quickstart` | First orientation in a new sprout — where to start, key files, gotchas |
| `new-field` | Add a field to the LLM output model + prompt + UI in sync |
| `customize-output` | Replace `DocumentAnalysis` with a domain struct end-to-end |
| `customize-ui` | Colors, copy, layout widths, result modal renderer |
| `add-file-type` | Support a new MIME / file format in the pipeline |
| `add-db-table` | Add a SQLite table + query helpers + optional API |
| `new-route` | Add a new HTTP API endpoint |
| `new-admin-tab` | Add a tab to the admin panel |
| `new-jumpstart-field` | Add a config field to the Jump-Start wizard |
| `build-from-schema` | Turn a converged data-modeling schema into production code (struct, prompt, route) |
| `test-pipeline` | Upload a file via curl and check pipeline result end-to-end |
| `production-debugging` | Trace errors from logs to exact Go/static files |

---

## Jump-Start scaffolding

Admin **Jump-Start** (`POST /api/admin/jumpstart`) clones this repo into a target directory via
`internal/admin/jumpstart.go` (`CreateSprout`). Generated files include `.vscode/settings.json`:
**`internal/admin/jumpstart_vscode.go`** owns window title, Peacock seed color, and workbench color
customizations — extend there when adding more editor/workspace defaults for new sprouts (documented
in-file). Keep generated behaviour in sync when you change `.vscode/settings.json` in this template.
Default SQLite location for new sprouts is **`./.db/<app_slug>.db`** (see **`internal/db`**:
`DefaultSQLitePathForSlug`, `DefaultSQLitePathRelative`).

**Porting plans** from reference sprouts live in **`docs/`** (e.g. **`docs/learn-from-financial-statements.md`**). Run **`prompts/dev/prompt_learn_from_example.txt`** to regenerate or analyze another sprout.

---

## What sprouts usually change (priority order)

1. **`prompts/*.txt`** — What the LLM should do (tone, domain rules, output expectations).
   Loaded at startup; **`system.txt`** is primary, **`analyze.txt`** supplements.
   Keep wording aligned with the JSON shape in step 2.

2. **`internal/models/document_analysis.go`** (or a new domain file alongside it) —
   `DocumentAnalysis` and nested structs: `json` names + `` `description:"..."` `` tags define
   the **Gemini response schema** via `llm.BuildSchema`. Changing fields here **requires** prompt
   updates. Implement `SchemaName() string` on every new struct.

3. **`static/`** — UI copy (`index.html`, `login.html`, `admin.html`), colors (`css/style.css`
   `:root` vars), result renderer (`js/analyze.js` → `renderAnalysis` line ~161).

4. **`internal/db/migrations.sql`** — Only if adding tables/columns (append-only, `CREATE TABLE
   IF NOT EXISTS`).

5. **`internal/pipeline/route.go`** via `cmd/server/main.go` — Only if the processing lifecycle
   changes or different MIME types need different output models.

---

## Stable infrastructure (change rarely)

| Area | Path | Notes |
|------|------|-------|
| LLM transport | `internal/llm/` | New **provider** = new `Client` impl; pipeline stays dumb. |
| Schema reflection | `internal/llm/schema_builder.go` | Shared; don't fork per sprout. |
| Auth | `internal/auth/` | JWT + middleware. |
| HTTP glue | `internal/handlers/router.go`, `files.go`, `auth.go` | Routes and uploads. |
| Admin JSON APIs | `internal/admin/` | Users/projects/stats. |
| File extraction | `internal/fileconv/` | MIME → text or vision bytes. |

---

## Request flow

```
Upload → handlers/files.go (save file + DB row)
       → go pipeline.Process(goroutine)
            └─ fileconv.GetConverter(mime).Extract()   → text
            └─ llm.Client.Complete(schema, prompt, text) → JSON
            └─ result written to disk (beside upload)
       ← UI polls GET /api/files/:id/status every 2s
       ← UI fetches GET /api/files/:id/result → renderAnalysis()
```

Pipeline lifecycle states: `uploaded → converting → llm_pending → llm_done → post_processing → complete | error`

---

## Commands

```bash
make run    # dev server (CGO_ENABLED=1 via Makefile)
make test
```

Copy **`.env.example` → `.env`**; set keys for chosen `LLM_PROVIDER`.

---

## Sprout checklist (minimal)

- [ ] Rename module in `go.mod` / all `import "megane/..."` if the app gets a new path.
- [ ] Edit `prompts/system.txt` for domain instructions.
- [ ] Edit `internal/models/document_analysis.go` (or create a new domain file) for structured output.
- [ ] Register the output struct via `pipe.Routes` in `cmd/server/main.go`.
- [ ] Adjust `static/js/analyze.js` (`renderAnalysis`) to match the new JSON shape.
- [ ] Update `static/css/style.css` `:root` variables for brand colors.
- [ ] Update app name/copy in `static/*.html` (search "megane").
- [ ] Run `make test`; smoke upload + open analysis modal.

---

## Pitfalls

- **Model vs prompts drift** — `internal/models/` struct fields and prompts must describe the same
  fields. Add a field to one → update the other.
- **Renderer gap** — Adding a struct field without updating `renderAnalysis` means data is computed
  but never shown.
- **Images** — Vision path uses Gemini inline image parts + `LLM_MAX_IMAGE_BYTES`; not every
  provider supports multimodal.
- **CGO** — SQLite and PDF paths need `CGO_ENABLED=1` (Makefile handles it).
