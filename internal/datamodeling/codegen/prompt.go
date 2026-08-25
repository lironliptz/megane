package codegen

import (
	"bytes"
	"fmt"
	"sort"

	"megane/internal/datamodeling"
)

// ProductionPrompt assembles the extraction prompt from the build config.
// Returns the prompt string (plain text, not Go source).
func ProductionPrompt(typeName string, rows []datamodeling.FieldRow, cfg datamodeling.BuildConfig, lang string) string {
	var b bytes.Buffer

	b.WriteString("You are a structured data extraction engine. Return valid JSON only — no markdown fences.\n\n")

	b.WriteString("## Document type\n")
	b.WriteString(fmt.Sprintf("%s: %s\n\n", typeName, cfg.TypeRules)) // Assuming TypeRules extends extraction_brief

	b.WriteString("## Output schema\n")
	b.WriteString("Return a JSON object with exactly these keys (snake_case):\n\n")

	var critical, required, optional []datamodeling.FieldRow
	var excluded []string

	for _, r := range rows {
		if !r.Override.Included {
			excluded = append(excluded, r.SchemaField.Name)
			continue
		}
		if r.Override.Emphasis == "critical" {
			critical = append(critical, r)
		} else if r.Override.Required {
			required = append(required, r)
		} else {
			optional = append(optional, r)
		}
	}

	sortFields(critical)
	sortFields(required)
	sortFields(optional)

	if len(critical) > 0 {
		b.WriteString("### Critical fields (must always be present)\n")
		writeFields(&b, critical)
		b.WriteString("\n")
	}

	if len(required) > 0 {
		b.WriteString("### Required fields\n")
		writeFields(&b, required)
		b.WriteString("\n")
	}

	if len(optional) > 0 {
		b.WriteString("### Optional fields\n")
		writeFields(&b, optional)
		b.WriteString("\n")
	}

	if cfg.TypeRules != "" {
		b.WriteString("## Field rules\n")
		b.WriteString(cfg.TypeRules)
		b.WriteString("\n\n")
	}

	if len(excluded) > 0 {
		b.WriteString("## Do NOT extract\n")
		for _, e := range excluded {
			b.WriteString(fmt.Sprintf("- %s\n", e))
		}
		b.WriteString("\n")
	}

	b.WriteString("## Language policy\n")
	if lang == "he" {
		b.WriteString("Hebrew values are acceptable; JSON keys must remain English snake_case.\n")
	} else {
		b.WriteString("All values in English.\n")
	}

	return b.String()
}

func sortFields(rows []datamodeling.FieldRow) {
	sort.Slice(rows, func(i, j int) bool {
		return rows[i].SchemaField.Name < rows[j].SchemaField.Name
	})
}

func writeFields(b *bytes.Buffer, rows []datamodeling.FieldRow) {
	for _, r := range rows {
		desc := r.SchemaField.Description
		if r.Override.Rules != "" {
			desc += " — RULE: " + r.Override.Rules
		}
		b.WriteString(fmt.Sprintf("- %s (%s): %s\n", r.SchemaField.Name, r.SchemaField.Type, desc))
	}
}
