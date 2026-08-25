package codegen

import (
	"fmt"
	"strings"

	"megane/internal/datamodeling"
)

// JSRenderer generates a render<Slug>Analysis(data) JS function body.
// v1 produces a generic key-value table; critical fields get a CSS badge.
// Returns JS source (no module wrapper).
func JSRenderer(slug string, rows []datamodeling.FieldRow) string {
	var b strings.Builder

	funcName := "render" + camelCase(slug) + "Analysis"

	b.WriteString(fmt.Sprintf("function %s(data) {\n", funcName))
	b.WriteString("  // Auto-generated — edit freely, re-generate with POST .../build/apply\n")
	b.WriteString("  let html = '';\n\n")

	var critical []datamodeling.FieldRow
	var others []datamodeling.FieldRow

	for _, r := range rows {
		if !r.Override.Included {
			continue
		}
		if r.Override.Emphasis == "critical" {
			critical = append(critical, r)
		} else {
			others = append(others, r)
		}
	}

	if len(critical) > 0 {
		b.WriteString("  // Critical fields\n")
		b.WriteString("  html += '<div class=\"dm-critical-fields\">';\n")
		for _, r := range critical {
			b.WriteString(fmt.Sprintf("  html += `<div><strong>%s:</strong> ${escHtml(data.%s ?? '')}</div>`;\n", r.SchemaField.Name, r.SchemaField.Name))
		}
		b.WriteString("  html += '</div>';\n\n")
	}

	if len(others) > 0 {
		b.WriteString("  // Other fields\n")
		b.WriteString("  html += '<table class=\"dm-analysis-table\"><tbody>';\n")
		for _, r := range others {
			b.WriteString(fmt.Sprintf("  html += `<tr><th>%s</th><td>${escHtml(data.%s ?? '')}</td></tr>`;\n", r.SchemaField.Name, r.SchemaField.Name))
		}
		b.WriteString("  html += '</tbody></table>';\n")
	}

	b.WriteString("\n  return html;\n")
	b.WriteString("}\n")

	return b.String()
}
