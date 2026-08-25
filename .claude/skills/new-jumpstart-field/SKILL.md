# Skill: new-jumpstart-field

Add a new configuration field to the Jump-Start wizard end-to-end.
There are two scenarios: **general field** (applies to every sprout) and
**LLM-provider field** (applies only when a specific provider is selected).

---

## Scenario A — General field (e.g. `max_upload_mb`, `enable_feature_x`)

### 1. Go struct — `internal/admin/jumpstart.go`

Add the field to `JumpStartConfig`:

```go
MaxUploadMB int `json:"max_upload_mb"`
```

Add validation in `validateConfig` if needed.

### 2. Generated .env — `buildEnvLines` in the same file

Add an `envLine` inside the relevant `envSection(...)` call, or create a new section:

```go
lines = append(lines, envSection("Application",
    ...
    envLine{"MAX_UPLOAD_MB", strconv.Itoa(cfg.MaxUploadMB)},
)...)
```

`envSection(heading, lines...)` emits `# ---- heading ----`, the lines, then a blank line.
An `envLine` with an empty `Val` emits `KEY=` (godotenv-safe); an empty `Key` emits a blank line.

### 3. HTML form — `static/admin.html`

Add an `<input>` inside the appropriate `<div class="js-row*">` or create a new section:

```html
<div class="section-label">Upload limits</div>
<div class="form-group" style="margin:0">
  <label>Max upload size <span style="color:var(--text-muted)">(MB)</span></label>
  <input type="number" id="js-max-upload" value="50" min="1">
</div>
```

### 4. JS payload — `doJumpStart()` in `static/admin.html`

```js
max_upload_mb: parseInt(document.getElementById('js-max-upload').value) || 50,
```

---

## Scenario B — LLM-provider field (e.g. a new provider "anthropic" with its API key)

Only **one file** needs to change: `static/js/jumpstart-utils.js`.

Add an entry to `LLM_PROVIDERS`:

```js
{
  value: 'anthropic',
  label: 'Anthropic Claude',
  fields: [
    { id: 'js-anthropic-key', label: 'Anthropic API key', type: 'text', placeholder: 'sk-ant-…', payload: 'anthropic_api_key' },
  ],
},
```

`renderLLMProviders()`, `updateLLMFields()`, and `collectLLMPayload()` all derive from
this registry automatically — no HTML or JS changes needed.

Then wire the new provider on the Go side:

1. Add `AnthropicAPIKey string \`json:"anthropic_api_key"\`` to `JumpStartConfig`.
2. Add `envLine{"ANTHROPIC_API_KEY", cfg.AnthropicAPIKey}` inside the LLM section in `buildEnvLines`.
3. Add the provider to `internal/llm/` (implement the `Client` interface).

---

## Scenario C — VS Code / Cursor generated settings only

Jump-Start **replaces** `.vscode/settings.json` in the new sprout (it does not copy the template file verbatim).

1. Read the extension guide at the top of **`internal/admin/jumpstart_vscode.go`**.
2. Add top-level keys in `jumpstartVSCodeSettingsRoot`, or workbench entries in `jumpstartWorkbenchColorCustomizations`.
3. If the new setting needs user input from the wizard, add a field per **Scenario A**, then thread `cfg` into the vscode helpers.

Keep generated defaults aligned with `.vscode/settings.json` in this template when you change either side.

---

## Key files

| File | Purpose |
|---|---|
| `internal/admin/jumpstart.go` | `JumpStartConfig` struct, `buildEnvLines`, `validateConfig`, `transformContent`, `walkAndCopy` |
| `internal/admin/jumpstart_vscode.go` | Generated `.vscode/settings.json` (window title, Peacock, workbench colors) |
| `static/js/jumpstart-utils.js` | `LLM_PROVIDERS` registry; `renderLLMProviders`, `updateLLMFields`, `collectLLMPayload` |
| `static/admin.html` | Jump-Start form HTML + `doJumpStart()` payload |
