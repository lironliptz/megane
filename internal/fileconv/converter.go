package fileconv

import "context"

// Converter extracts text from a file for LLM consumption. Image MIME types use a
// placeholder string from Extract; the pipeline attaches raw bytes as Gemini vision parts.
type Converter interface {
	// Extract reads the file at filePath and returns its textual representation.
	Extract(ctx context.Context, filePath string) (string, error)
	// SupportedMIME returns the MIME types this converter handles.
	SupportedMIME() []string
}

var registry []Converter

func init() {
	registry = []Converter{
		&TextConverter{},
		&PDFConverter{},
		&ImageConverter{},
		&ExcelConverter{},
		&WordConverter{},
		// DWGConverter is intentionally excluded: Extract always errors until an external
		// tool (LibreCAD / oda_converter) is wired in. Re-add &DWGConverter{} here and
		// restore the DWG MIME types in handlers/files.go once the tool is available.
	}
}

// GetConverter returns the Converter for the given MIME type, or false if none exists.
func GetConverter(mimeType string) (Converter, bool) {
	for _, c := range registry {
		for _, m := range c.SupportedMIME() {
			if m == mimeType {
				return c, true
			}
		}
	}
	return nil, false
}
