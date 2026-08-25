package datamodeling

import (
	"encoding/json"
	"errors"
	"strings"
	"time"

	"megane/internal/db"
)

var ErrFieldNameRequired = errors.New("field_name_required")

// ExcludedField is a schema field the admin removed; must not be re-inferred on future docs.
type ExcludedField struct {
	Name       string `json:"name"`
	ExcludedAt string `json:"excluded_at,omitempty"`
}

func ParseExcludedFieldsJSON(raw string) []ExcludedField {
	raw = strings.TrimSpace(raw)
	if raw == "" || raw == "[]" {
		return nil
	}
	var items []ExcludedField
	if err := json.Unmarshal([]byte(raw), &items); err != nil {
		return nil
	}
	return items
}

func ExcludedFieldNames(items []ExcludedField) []string {
	if len(items) == 0 {
		return nil
	}
	out := make([]string, 0, len(items))
	for _, it := range items {
		if n := strings.TrimSpace(it.Name); n != "" {
			out = append(out, n)
		}
	}
	return out
}

func MarshalExcludedFieldsJSON(items []ExcludedField) (string, error) {
	if len(items) == 0 {
		return "[]", nil
	}
	raw, err := json.Marshal(items)
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

// MatchesExcludedField reports whether name matches any admin-excluded field (incl. similar aliases).
func MatchesExcludedField(name string, excluded []string) bool {
	name = strings.TrimSpace(name)
	if name == "" || len(excluded) == 0 {
		return false
	}
	for _, ex := range excluded {
		if fieldNamesSimilar(name, ex) {
			return true
		}
	}
	return false
}

func filterSchemaFieldsNotExcluded(fields []SchemaField, excluded []string) []SchemaField {
	if len(fields) == 0 || len(excluded) == 0 {
		return fields
	}
	out := make([]SchemaField, 0, len(fields))
	for _, f := range fields {
		if !MatchesExcludedField(f.Name, excluded) {
			out = append(out, f)
		}
	}
	return out
}

func addExcludedField(items []ExcludedField, fieldName string) []ExcludedField {
	fieldName = strings.TrimSpace(fieldName)
	if fieldName == "" {
		return items
	}
	for _, it := range items {
		if fieldNamesSimilar(it.Name, fieldName) {
			return items
		}
	}
	return append(items, ExcludedField{
		Name:       fieldName,
		ExcludedAt: time.Now().UTC().Format(time.RFC3339),
	})
}

func removeFieldFromStats(stats *SchemaStats, fieldName string) {
	if stats == nil {
		return
	}
	delete(stats.Fields, fieldName)
	delete(stats.FieldPresence, fieldName)
	delete(stats.SampleValues, fieldName)
}

// ExcludeSchemaField removes a field from the latest schema, records it as excluded, and bumps schema version.
func ExcludeSchemaField(database *db.DB, fileTypeID int64, fieldName string) (*Schema, []ExcludedField, error) {
	fieldName = strings.TrimSpace(fieldName)
	if fieldName == "" {
		return nil, nil, ErrFieldNameRequired
	}

	ft, err := GetFileTypeByID(database, fileTypeID)
	if err != nil {
		return nil, nil, err
	}

	latest, err := GetLatestSchema(database, fileTypeID)
	if err != nil {
		return nil, nil, err
	}
	if latest == nil {
		return nil, nil, ErrNoSchema{}
	}

	var ps ProposedSchema
	if err := json.Unmarshal([]byte(latest.SchemaJSON), &ps); err != nil {
		return nil, nil, err
	}

	found := false
	filtered := make([]SchemaField, 0, len(ps.Fields))
	for _, f := range ps.Fields {
		if strings.EqualFold(f.Name, fieldName) {
			found = true
			fieldName = f.Name
			continue
		}
		filtered = append(filtered, f)
	}
	if !found {
		return nil, nil, ErrSchemaFieldNotFound{Name: fieldName}
	}
	ps.Fields = filtered
	ps.SchemaHash = HashSchema(ps)

	stats := ParseSchemaStats(latest.StatsJSON)
	removeFieldFromStats(&stats, fieldName)
	statsJSON, err := json.Marshal(stats)
	if err != nil {
		return nil, nil, err
	}

	excluded := addExcludedField(ParseExcludedFieldsJSON(ft.ExcludedFieldsJSON), fieldName)
	excludedJSON, err := MarshalExcludedFieldsJSON(excluded)
	if err != nil {
		return nil, nil, err
	}
	if _, err := database.Exec(
		`UPDATE dm_file_types SET excluded_fields_json = ?, updated_at = ? WHERE id = ?`,
		excludedJSON, time.Now().UTC(), fileTypeID,
	); err != nil {
		return nil, nil, err
	}

	schemaJSON, err := json.Marshal(ps)
	if err != nil {
		return nil, nil, err
	}
	nextVer := NextVersion(latest)
	convergenceScore := latest.ConvergenceScore
	if convergenceScore == 0 {
		convergenceScore = 0.5
	}
	_, err = CreateSchema(database, fileTypeID, nextVer, string(schemaJSON), string(statsJSON), convergenceScore)
	if err != nil {
		return nil, nil, err
	}
	_ = TouchFileType(database, fileTypeID)

	newSchema, err := GetLatestSchema(database, fileTypeID)
	return newSchema, excluded, err
}
