---
description: >
  Add support for a new file format / MIME type to the upload pipeline.
  Use when the user says "I want to upload X files", "support Y format", or
  "add a converter for Z".
---

## What you do

Wire a new MIME type end-to-end: converter → MIME allowlist → (optionally) a dedicated pipeline
route with its own output struct.

## Files to touch

| File | Change |
|------|--------|
| `internal/fileconv/<format>.go` | New converter implementing `fileconv.Converter` |
| `internal/fileconv/converter.go` | Register the new converter in `init()` |
| `internal/handlers/files.go` | Add MIME type(s) to `allowedMIMETypes` |
| `cmd/server/main.go` | (optional) Add a `pipeline.Route` for the new MIME |

## Background — converter interface

```go
// internal/fileconv/converter.go
type Converter interface {
    Extract(ctx context.Context, filePath string) (string, error)
    SupportedMIME() []string
}
```

`Extract` returns the textual representation of the file. For non-text formats:
- **Text-only**: return extracted text, passed to the LLM as a text part.
- **Image-based**: return a placeholder string (e.g. `"[image]"`). The pipeline attaches
  raw bytes as Gemini inline image parts when it detects an image MIME.

## Procedure

### Step 1 — Write the converter

Create `internal/fileconv/<format>.go`:

```go
package fileconv

import (
    "context"
    "fmt"
    "os"
)

type CSVConverter struct{}

func (c *CSVConverter) SupportedMIME() []string {
    return []string{"text/csv", "application/csv"}
}

func (c *CSVConverter) Extract(ctx context.Context, filePath string) (string, error) {
    data, err := os.ReadFile(filePath)
    if err != nil {
        return "", fmt.Errorf("csv read: %w", err)
    }
    return string(data), nil
}
```

For binary formats, import a parsing library (e.g. `github.com/PuerkitoBio/goquery` for HTML,
`github.com/gen2brain/go-fitz` for EPUB, etc.) and extract the text content.

For image formats that Gemini handles natively, return `"[image]"` — the pipeline reads the raw
bytes via the `image/*` MIME path in `internal/pipeline/pipeline.go`.

### Step 2 — Register in the converter registry

In `internal/fileconv/converter.go`, add to the `init()` function:

```go
func init() {
    registry = []Converter{
        &TextConverter{},
        &PDFConverter{},
        &ImageConverter{},
        &ExcelConverter{},
        &WordConverter{},
        &DWGConverter{},
        &CSVConverter{},   // ← add here
    }
}
```

### Step 3 — Allow the MIME type in the upload handler

In `internal/handlers/files.go`, add to `allowedMIMETypes`:

```go
var allowedMIMETypes = map[string]bool{
    // ... existing types ...
    "text/csv":         true,   // ← add
    "application/csv":  true,   // ← add if needed
}
```

Note: the existing list already covers `text/csv`. Check before adding duplicates.

### Step 4 — (Optional) Add a dedicated pipeline route

If the new format needs a different LLM output model than the default:

```go
// cmd/server/main.go
pipe.Routes = []pipeline.Route{
    {
        Name:    "csv-financial",
        Matches: func(mime string) bool { return mime == "text/csv" },
        OutputFor: func(_ string) models.LLMOutput { return &models.FinancialData{} },
    },
    // ... other routes ...
}
```

Omit this step to use the default `DocumentAnalysis` struct for the new format.

### Step 5 — Verify

```bash
go build ./...    # must compile clean
make run
# Upload a file of the new type
# Check it reaches status "complete"
# Open the result modal and verify field values make sense
```

## Notes

- The `TextConverter` already handles `text/plain`, `text/csv`, `text/markdown`, `application/json` —
  check `internal/fileconv/text.go` before writing a new one for text-based formats.
- For Excel-like formats, inspect `internal/fileconv/excel.go` — it converts sheets to text tables
  using `github.com/tealeg/xlsx`.
- CGO is required for PDF (`github.com/gen2brain/go-fitz`). Other formats can be CGO-free.
- Upload size limits are controlled by `MAX_UPLOAD_MB` in `.env` (or Gin's `MaxMultipartMemory`).
