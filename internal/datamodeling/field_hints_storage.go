package datamodeling

import (
	"encoding/json"
	"strings"
	"time"
)

// ParseFieldHintsJSON decodes stored field_hints_json; returns nil slice on empty/invalid.
func ParseFieldHintsJSON(raw string) []FieldHint {
	raw = strings.TrimSpace(raw)
	if raw == "" || raw == "[]" {
		return nil
	}
	var hints []FieldHint
	if err := json.Unmarshal([]byte(raw), &hints); err != nil {
		return nil
	}
	return hints
}

// MarshalFieldHintsJSON encodes hints for dm_file_types.field_hints_json.
func MarshalFieldHintsJSON(hints []FieldHint) (string, error) {
	if len(hints) == 0 {
		return "[]", nil
	}
	raw, err := json.Marshal(hints)
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

// FieldHintLabels returns display labels for admin UI chips.
func FieldHintLabels(hints []FieldHint) []string {
	if len(hints) == 0 {
		return nil
	}
	out := make([]string, 0, len(hints))
	for _, h := range hints {
		if label := strings.TrimSpace(h.Label); label != "" {
			out = append(out, label)
		}
	}
	return out
}

// NormalizeFieldHintsFromForm converts multipart/JSON form values to FieldHint rows.
func NormalizeFieldHintsFromForm(values []string) []FieldHint {
	var out []FieldHint
	for _, s := range values {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		var fh FieldHint
		if json.Unmarshal([]byte(s), &fh) == nil && strings.TrimSpace(fh.Label) != "" {
			out = append(out, fh)
			continue
		}
		out = append(out, FieldHint{Label: s})
	}
	return out
}

// FieldHintsFromLabels builds hints from plain admin UI strings.
func FieldHintsFromLabels(labels []string) []FieldHint {
	var out []FieldHint
	for _, s := range labels {
		if label := strings.TrimSpace(s); label != "" {
			out = append(out, FieldHint{Label: label})
		}
	}
	return out
}

// FileTypeViewFrom builds an API view from a dm_file_types row.
func FileTypeViewFrom(ft *FileType) FileTypeView {
	if ft == nil {
		return FileTypeView{}
	}
	return FileTypeView{
		ID:              ft.ID,
		Name:            ft.Name,
		Slug:            ft.Slug,
		ExtractionBrief: ft.ExtractionBrief,
		FieldHints:      ParseFieldHintsJSON(ft.FieldHintsJSON),
		ExcludedFields:  ParseExcludedFieldsJSON(ft.ExcludedFieldsJSON),
		CreatedAt:       ft.CreatedAt.Format(time.RFC3339),
		UpdatedAt:       ft.UpdatedAt.Format(time.RFC3339),
	}
}
