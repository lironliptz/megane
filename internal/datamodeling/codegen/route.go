package codegen

import (
	"fmt"
	"strings"
)

// RouteSnippet generates the Go source for a pipeline.Route literal.
// mimeTypes is the admin-confirmed list from BuildConfig.MIMETypes.
// slug is the file type slug.
// strategy is the consensus processing strategy (drives comment/TODO).
func RouteSnippet(slug string, mimeTypes []string, strategy string) string {
	var b strings.Builder

	structName := camelCase(slug) + "Analysis"

	b.WriteString(fmt.Sprintf("// Route for %s — processing strategy: %s\n", slug, strategy))
	if strings.Contains(strategy, "ocr") || strings.Contains(strategy, "convert_to_image") {
		b.WriteString("// TODO: wire OCR/vision pre-pass from internal/fileconv (see prompt_6)\n")
	}

	b.WriteString("pipeline.Route{\n")
	b.WriteString(fmt.Sprintf("\tName: %q,\n", slug))
	
	if len(mimeTypes) > 0 {
		b.WriteString("\tMatches: func(mime string) bool {\n")
		var conditions []string
		for _, m := range mimeTypes {
			conditions = append(conditions, fmt.Sprintf("mime == %q", m))
		}
		b.WriteString(fmt.Sprintf("\t\treturn %s\n", strings.Join(conditions, " || ")))
		b.WriteString("\t},\n")
	}

	b.WriteString(fmt.Sprintf("\tOutputFor: func(_ string) models.LLMOutput { return &models.%s{} },\n", structName))

	if strings.Contains(strategy, "multi_pass") {
		b.WriteString("\t// Process: func(r *pipeline.Run) error { ... }, // TODO: implement multi-pass logic\n")
	} else {
		b.WriteString("\t// Process: nil — using defaultProcess; add pre/post hooks if needed\n")
	}

	b.WriteString("},\n")

	return b.String()
}
