package datamodeling

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

func MergeSchema(current *ProposedSchema, llmProposal ProposedSchema,
	hints []FieldHint, resolutions []HintResolution, excluded []string) (merged ProposedSchema, notes string) {

	llmProposal.Fields = filterSchemaFieldsNotExcluded(llmProposal.Fields, excluded)

	// Build lookup maps
	llmByName := map[string]SchemaField{}
	for _, f := range llmProposal.Fields {
		llmByName[f.Name] = f
	}

	var prevByName map[string]SchemaField
	if current != nil {
		prevByName = map[string]SchemaField{}
		for _, f := range current.Fields {
			prevByName[f.Name] = f
		}
	}

	var userFields, llmFields, deprecatedFields []SchemaField
	var notesBuf strings.Builder
	hintLabels := map[string]bool{}

	// User-provenance fields (in hint order)
	for i, hint := range hints {
		res := resolutions[i]
		if res.Found {
			f := llmByName[res.MappedTo]
			f.Provenance = "user"
			f.Required = hint.Required || true
			if hint.Label != f.Name {
				f.Name = hint.Label
			}
			userFields = append(userFields, f)
			hintLabels[res.MappedTo] = true
		} else {
			// Hint not found — add as low confidence
			f := SchemaField{
				Name:                 hint.Label,
				Type:                 hint.Type,
				Required:             hint.Required,
				ExtractionConfidence: "low",
				Provenance:           "user",
			}
			if f.Type == "" {
				f.Type = "string"
			}
			userFields = append(userFields, f)
		}
		hintLabels[hint.Label] = true
	}

	// LLM-provenance fields (alphabetical)
	for _, f := range llmProposal.Fields {
		if hintLabels[f.Name] {
			continue
		}
		// Check type conflict with prior schema
		if prevByName != nil {
			if prev, ok := prevByName[f.Name]; ok && prev.Type != f.Type {
				notesBuf.WriteString(fmt.Sprintf("type conflict for %q: %s → %s; ", f.Name, prev.Type, f.Type))
			}
		}
		f.Required = false
		f.Provenance = "llm"
		llmFields = append(llmFields, f)
	}
	sort.Slice(llmFields, func(i, j int) bool { return llmFields[i].Name < llmFields[j].Name })

	// Retain prior-schema fields absent this run
	if prevByName != nil {
		allNewNames := map[string]bool{}
		for _, f := range userFields {
			allNewNames[f.Name] = true
		}
		for _, f := range llmFields {
			allNewNames[f.Name] = true
		}
		for name, prev := range prevByName {
			if allNewNames[name] || MatchesExcludedField(name, excluded) {
				continue
			}
			prev.ExtractionConfidence = DowngradeConfidence(prev.ExtractionConfidence)
			prev.AbsenceStreak++
			if prev.AbsenceStreak > 3 {
				prev.Deprecated = true
				deprecatedFields = append(deprecatedFields, prev)
			} else {
				if prev.Provenance == "user" {
					userFields = append(userFields, prev)
				} else {
					llmFields = append(llmFields, prev)
				}
			}
		}
	}

	merged.Fields = append(userFields, llmFields...)
	merged.Fields = append(merged.Fields, deprecatedFields...)
	notes = notesBuf.String()
	return merged, notes
}

func DiffSchemas(old, new *ProposedSchema) *SchemaDiff {
	if old == nil {
		return nil
	}
	oldByName := map[string]SchemaField{}
	for _, f := range old.Fields {
		oldByName[f.Name] = f
	}
	newByName := map[string]SchemaField{}
	for _, f := range new.Fields {
		if !f.Deprecated {
			newByName[f.Name] = f
		}
	}

	diff := &SchemaDiff{}
	for name := range newByName {
		if _, ok := oldByName[name]; !ok {
			diff.AddedFields = append(diff.AddedFields, name)
		}
	}
	for name := range oldByName {
		if _, ok := newByName[name]; !ok {
			diff.RemovedFields = append(diff.RemovedFields, name)
		}
	}
	for name, newF := range newByName {
		if oldF, ok := oldByName[name]; ok {
			if oldF.Type != newF.Type {
				diff.ModifiedFields = append(diff.ModifiedFields, FieldChange{Name: name, OldType: oldF.Type, NewType: newF.Type})
			}
			if oldF.ExtractionConfidence != newF.ExtractionConfidence {
				diff.ConfidenceChanges = append(diff.ConfidenceChanges, ConfidenceChange{Name: name, Old: oldF.ExtractionConfidence, New: newF.ExtractionConfidence})
			}
		}
	}
	return diff
}

func HashSchema(s ProposedSchema) string {
	sorted := make([]SchemaField, len(s.Fields))
	copy(sorted, s.Fields)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Name < sorted[j].Name })
	data, _ := json.Marshal(sorted)
	return fmt.Sprintf("%x", sha256.Sum256(data))
}

func DowngradeConfidence(c string) string {
	switch c {
	case "high":
		return "medium"
	case "medium":
		return "low"
	default:
		return "low"
	}
}

func NextVersion(latest *Schema) int {
	if latest == nil {
		return 1
	}
	return latest.Version + 1
}

// ParseSchemaStats unmarshals stats JSON, migrating legacy blobs that only stored field_presence.
func ParseSchemaStats(existingStatsJSON string) SchemaStats {
	stats := emptySchemaStats()
	if existingStatsJSON == "" || existingStatsJSON == "{}" {
		return stats
	}
	if err := json.Unmarshal([]byte(existingStatsJSON), &stats); err != nil {
		return emptySchemaStats()
	}
	if stats.Fields == nil {
		stats.Fields = map[string]FieldStat{}
	}
	if stats.FieldPresence == nil {
		stats.FieldPresence = map[string]float64{}
	}
	if stats.SampleValues == nil {
		stats.SampleValues = map[string]string{}
	}
	return stats
}

func emptySchemaStats() SchemaStats {
	return SchemaStats{
		Fields:        map[string]FieldStat{},
		FieldPresence: map[string]float64{},
		SampleValues:  map[string]string{},
	}
}

// UpdateStats records one more analyzed document and updates per-field document counts.
// fieldsFoundThisRun maps discovered field names (this document) to sample values.
// Similar discovered names are matched to canonical merged schema field names.
func UpdateStats(existingStatsJSON string, mergedSchema ProposedSchema, fieldsFoundThisRun map[string]string) (string, error) {
	stats := ParseSchemaStats(existingStatsJSON)
	stats.DocumentsAnalyzed++

	used := map[string]bool{}
	for _, f := range mergedSchema.Fields {
		name := f.Name
		fs := stats.Fields[name]

		if matched, ok := matchDiscoveredField(name, fieldsFoundThisRun, used); ok {
			used[matched] = true
			fs.DocumentCount++
			if sample := strings.TrimSpace(fieldsFoundThisRun[matched]); sample != "" {
				fs.SampleValue = sample
				stats.SampleValues[name] = sample
			}
			if !strings.EqualFold(matched, name) {
				fs.AliasesSeen = appendUniqueString(fs.AliasesSeen, matched)
			}
		}

		if stats.DocumentsAnalyzed > 0 {
			fs.PresenceRate = float64(fs.DocumentCount) / float64(stats.DocumentsAnalyzed)
			stats.FieldPresence[name] = fs.PresenceRate
		}
		stats.Fields[name] = fs
	}

	// Ensure schema fields removed from merged schema retain historical stats keys.
	for name, fs := range stats.Fields {
		if stats.DocumentsAnalyzed > 0 {
			fs.PresenceRate = float64(fs.DocumentCount) / float64(stats.DocumentsAnalyzed)
			stats.FieldPresence[name] = fs.PresenceRate
			stats.Fields[name] = fs
		}
	}

	data, err := json.Marshal(stats)
	if err != nil {
		return "{}", err
	}
	return string(data), nil
}

func appendUniqueString(list []string, s string) []string {
	for _, x := range list {
		if strings.EqualFold(x, s) {
			return list
		}
	}
	return append(list, s)
}
