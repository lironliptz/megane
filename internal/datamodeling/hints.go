package datamodeling

import "strings"

func ResolveHints(hints []FieldHint, fields []SchemaField) []HintResolution {
	result := make([]HintResolution, 0, len(hints))
	for _, hint := range hints {
		res := resolveOne(hint, fields)
		result = append(result, res)
	}
	return result
}

func resolveOne(hint FieldHint, fields []SchemaField) HintResolution {
	hLabel := strings.ToLower(hint.Label)

	// 1. Exact case-insensitive
	for _, f := range fields {
		if strings.ToLower(f.Name) == hLabel {
			return matched(hint, f)
		}
	}

	// 2. Normalised (strip spaces, underscores, hyphens)
	normHint := normaliseFieldName(hLabel)
	for _, f := range fields {
		if normaliseFieldName(f.Name) == normHint {
			return matched(hint, f)
		}
	}

	// 3. Substring
	for _, f := range fields {
		fLower := strings.ToLower(f.Name)
		if strings.Contains(fLower, hLabel) || strings.Contains(hLabel, fLower) {
			return matched(hint, f)
		}
	}

	return HintResolution{UserLabel: hint.Label, Found: false, Note: "not found in extracted fields"}
}

func matched(hint FieldHint, f SchemaField) HintResolution {
	return HintResolution{
		UserLabel:   hint.Label,
		Found:       true,
		MappedTo:    f.Name,
		Confidence:  f.ExtractionConfidence,
		SampleValue: f.SampleValue,
	}
}

