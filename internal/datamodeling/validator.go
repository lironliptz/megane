package datamodeling

import (
	"encoding/json"
	"fmt"
	"strings"
)

var ValidFieldTypes = map[string]bool{
	"string": true, "number": true, "date": true,
	"boolean": true, "array": true, "object": true,
}

var ValidConfidenceLevels = map[string]bool{
	"high": true, "medium": true, "low": true,
}

type ValidationError struct{ Errors []string }

func (e *ValidationError) Error() string {
	return "LLM response validation failed: " + strings.Join(e.Errors, "; ")
}

type FieldValidationError struct {
	Field   string `json:"field,omitempty"`
	Message string `json:"message"`
	Code    string `json:"code"`
}

func ValidateLLMOutput(out *LLMAnalysisOutput) error {
	errs := ValidateDetailed(out)
	if len(errs) == 0 {
		return nil
	}
	ve := &ValidationError{}
	for _, e := range errs {
		ve.Errors = append(ve.Errors, e.Message)
	}
	return ve
}

func ValidateDetailed(out *LLMAnalysisOutput) []FieldValidationError {
	var errs []FieldValidationError
	add := func(field, msg, code string) {
		errs = append(errs, FieldValidationError{Field: field, Message: msg, Code: code})
	}

	if out.ProposedSchema.Fields == nil {
		add("proposed_schema.fields", "fields must not be nil", "nil_fields")
	}

	seen := map[string]bool{}
	for i, f := range out.ProposedSchema.Fields {
		if f.Name == "" {
			add("fields", "field name is empty", "empty_name")
		} else if strings.Contains(f.Name, " ") {
			add("fields", "field name must be snake_case (no spaces): "+f.Name, "invalid_name")
		}
		if !ValidFieldTypes[f.Type] {
			add("fields", "invalid type for field "+f.Name+": "+f.Type, "invalid_type")
		}
		if !ValidConfidenceLevels[f.ExtractionConfidence] {
			add("fields", "invalid confidence for field "+f.Name+": "+f.ExtractionConfidence, "invalid_confidence")
		}
		if seen[f.Name] {
			add("fields", "duplicate field name: "+f.Name, "duplicate_name")
		}
		seen[f.Name] = true
		_ = i
	}

	if !out.ProcessingStrategy.UseOCR && !out.ProcessingStrategy.ConvertToImage &&
		!out.ProcessingStrategy.MultiPass && out.ProcessingStrategy.Rationale == "" {
		add("processing_strategy", "processing_strategy is zero-valued", "empty_strategy")
	}

	if out.NeedsManualHelp && len(out.ManualQuestions) == 0 {
		add("manual_questions", "needs_manual_help is true but manual_questions is empty", "missing_questions")
	}

	for _, q := range out.ManualQuestions {
		if q.QuestionID == "" {
			add("manual_questions", "question_id must not be empty", "empty_question_id")
		}
	}

	return errs
}

func ParseLLMResponse(text string) (*LLMAnalysisOutput, error) {
	var out LLMAnalysisOutput

	// Attempt 1: unmarshal directly
	if err := json.Unmarshal([]byte(text), &out); err == nil {
		normalizeLLMOutput(&out)
		return &out, nil
	}

	// Attempt 2: find first '{' and last '}'
	start := strings.Index(text, "{")
	end := strings.LastIndex(text, "}")
	if start >= 0 && end > start {
		sub := text[start : end+1]
		if err := json.Unmarshal([]byte(sub), &out); err == nil {
			normalizeLLMOutput(&out)
			return &out, nil
		}
	}

	return nil, ErrInvalidLLMResponse{Details: "could not parse LLM response as JSON"}
}

// ParseLLMResponseMap unmarshals a Gemini structured-output map into LLMAnalysisOutput.
func ParseLLMResponseMap(parsed map[string]interface{}) (*LLMAnalysisOutput, error) {
	raw, err := json.Marshal(parsed)
	if err != nil {
		return nil, ErrInvalidLLMResponse{Details: "could not marshal LLM JSON map"}
	}
	var out LLMAnalysisOutput
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, ErrInvalidLLMResponse{Details: "could not parse LLM response as JSON"}
	}
	normalizeLLMOutput(&out)
	return &out, nil
}

func normalizeLLMOutput(out *LLMAnalysisOutput) {
	if out == nil {
		return
	}
	if out.ProposedSchema.Fields == nil {
		out.ProposedSchema.Fields = []SchemaField{}
	}
	if out.ManualQuestions == nil {
		out.ManualQuestions = []ManualQuestion{}
	}
	if out.HintResolution == nil {
		out.HintResolution = []HintResolution{}
	}
	if out.ProcessingStrategy.Passes == nil {
		out.ProcessingStrategy.Passes = []string{}
	}
	sanitizeSchemaFieldNames(&out.ProposedSchema.Fields)
}

func isValidFieldName(name string) bool {
	if name == "" {
		return false
	}
	for _, r := range name {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '_' {
			continue
		}
		return false
	}
	return true
}

// sanitizeSchemaFieldNames coerces invalid LLM field names (e.g. copied from extraction briefs) to snake_case.
func sanitizeSchemaFieldNames(fields *[]SchemaField) {
	if fields == nil || len(*fields) == 0 {
		return
	}
	seen := map[string]bool{}
	for i := range *fields {
		f := &(*fields)[i]
		name := strings.TrimSpace(f.Name)
		if !isValidFieldName(name) {
			name = Slugify(name)
			if name == "" {
				name = fmt.Sprintf("field_%d", i+1)
			}
		}
		name = ensureUniqueFieldName(name, seen)
		f.Name = name
		seen[name] = true
	}
}

func ensureUniqueFieldName(name string, seen map[string]bool) string {
	if !seen[name] {
		return name
	}
	for n := 2; n < 1000; n++ {
		candidate := fmt.Sprintf("%s_%d", name, n)
		if !seen[candidate] {
			return candidate
		}
	}
	return name + "_dup"
}

// ApplyStrategyDefaults fills a zero-valued processing_strategy from detected pre-strategy.
func ApplyStrategyDefaults(out *LLMAnalysisOutput, fallback ProcessingStrategy) {
	if out == nil {
		return
	}
	ps := &out.ProcessingStrategy
	if ps.UseOCR || ps.ConvertToImage || ps.MultiPass || strings.TrimSpace(ps.Rationale) != "" {
		return
	}
	*ps = fallback
	if strings.TrimSpace(ps.Rationale) == "" {
		ps.Rationale = "Inferred from file type and text extraction results."
	}
	if ps.Passes == nil {
		ps.Passes = []string{}
	}
}
