---
description: >
  Replace or extend the LLM output model with a domain-specific struct, keep prompts and UI in sync.
  Use when the user says "I want to analyze contracts", "change the output to return invoices",
  "replace DocumentAnalysis with my own model", or "the LLM should return X instead of Y".
---

## What you do

Swap (or extend) the LLM response schema end-to-end: Go struct → Gemini JSON schema → prompt
instructions → UI renderer. All three surfaces must change together.

## Background

`internal/models/document_analysis.go` ships `DocumentAnalysis` — the generic catch-all.
Sprout-apps usually **create a new file** alongside it for their domain struct
(e.g. `contract_analysis.go`, `invoice_data.go`) and register it as the pipeline output.

The `description:"..."` tags are sent verbatim to Gemini as field hints — write them as
short instructions ("2-5 sentence overview", "ISO 4217 currency code").

## Files to touch

| File | Change |
|------|--------|
| `internal/models/<domain>.go` | New file with domain struct implementing `models.LLMOutput` |
| `cmd/server/main.go` | Register struct via `pipe.Routes` or `pipe.OutputFor` |
| `prompts/system.txt` | Domain instructions aligned with the new struct fields |
| `prompts/analyze.txt` | (if it exists) supplement system.txt |
| `static/js/analyze.js` → `renderAnalysis()` | Render the new top-level sections |

## Procedure

### Step 1 — Create the domain struct

Create `internal/models/<domain>.go`:

```go
package models

// ContractAnalysis is the LLM output for contract documents.
type ContractAnalysis struct {
    Parties     []string `json:"parties"     description:"Full legal names of all signing parties"`
    EffectiveDate string `json:"effective_date" description:"Contract start date in YYYY-MM-DD format"`
    Obligations []string `json:"obligations" description:"Key obligations per party; one bullet per obligation"`
    TermMonths  int      `json:"term_months"  description:"Contract duration in months; 0 if open-ended"`
    Risks       []string `json:"risks"        description:"Material risks or red flags the analyst should review"`
    Summary     string   `json:"summary"      description:"2-5 sentence plain-language summary"`
    Confidence  float64  `json:"confidence"   description:"Analysis confidence from 0 to 1"`
}

func (c *ContractAnalysis) SchemaName() string { return "contract_analysis" }
```

Rules:
- Implement `SchemaName() string` — this is the entire `models.LLMOutput` interface.
- Use `json` + `description` tags on every field. The description is your Gemini prompt hint.
- Optional: add `enum:"a,b,c"` to constrain string fields.
- Nested structs are fine (see `LLMChartSpec` in `document_analysis.go` as reference).

### Step 2 — Register as pipeline output

In `cmd/server/main.go`, after the pipeline is created:

**Option A — replace the default for all files:**
```go
pipe.Routes = []pipeline.Route{
    {
        Name:      "contract",
        OutputFor: func(_ string) models.LLMOutput { return &models.ContractAnalysis{} },
    },
}
```

**Option B — route by MIME type:**
```go
pipe.Routes = []pipeline.Route{
    {
        Name:      "contract-pdf",
        Matches:   func(mime string) bool { return mime == "application/pdf" },
        OutputFor: func(_ string) models.LLMOutput { return &models.ContractAnalysis{} },
    },
    {
        // catch-all: non-PDF still gets generic analysis
        OutputFor: func(_ string) models.LLMOutput { return &models.DocumentAnalysis{} },
    },
}
```

A `Route` with nil `Matches` is a catch-all. First matching route wins.
A `Route` with nil `Process` uses the built-in document analysis flow (fileconv → LLM → save JSON).

### Step 3 — Update prompts

Edit `prompts/system.txt` to:
1. Describe the domain ("You are a contract analysis expert...")
2. List the expected output fields and what to put in each one (mirrors the struct descriptions)
3. Keep wording tight — Gemini already sees the JSON schema from the struct; the prompt adds context

Example addition:
```
For each contract, identify all signing parties by their full legal name (parties[]).
List every significant obligation as a separate bullet in obligations[].
...
```

### Step 4 — Update the result UI

Edit `renderAnalysis()` in `static/js/analyze.js` (~line 161).

The function receives `result` — the parsed JSON from the LLM. Render each top-level key:

```js
function renderAnalysis(container, result) {
  resetCharts();
  let html = '';

  // Parties
  const parties = Array.isArray(result.parties) ? result.parties : [];
  html += '<section class="result-section">';
  html += '<h4>Parties</h4>';
  if (parties.length) {
    html += '<ul class="insight-list">';
    parties.forEach(p => { html += '<li>' + escHtml(p) + '</li>'; });
    html += '</ul>';
  }
  html += '</section>';

  // Summary
  html += '<section class="result-section">';
  html += '<h4>Summary</h4>';
  html += '<p>' + escHtml(result.summary || '') + '</p>';
  html += '</section>';

  // ... repeat for other fields ...

  container.innerHTML = html;
}
```

You can keep the existing `renderChartsInto` helper if you add `charts` to your struct.

### Step 5 — Verify

```bash
go build ./...       # must be clean
make run
# Upload a test document of the target type
# Open the result modal — confirm all fields appear with real data
```

## Constraints

- Do **not** rename or delete `DocumentAnalysis` if existing stored results use it — those JSON files expect the old schema.
- Keep `description` tags short (1-2 sentences). Long descriptions waste Gemini context.
- If you need both the old and new struct active for different MIMEs, use `pipe.Routes` with `Matches`.
