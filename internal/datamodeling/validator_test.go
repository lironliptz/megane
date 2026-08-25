package datamodeling

import "testing"

func TestSanitizeSchemaFieldNames(t *testing.T) {
	fields := []SchemaField{
		{Name: "payments data - all the columns", Type: "array", ExtractionConfidence: "high"},
		{Name: "valid_name", Type: "string", ExtractionConfidence: "medium"},
		{Name: "payments data - all the columns", Type: "string", ExtractionConfidence: "low"},
	}
	sanitizeSchemaFieldNames(&fields)
	if fields[0].Name != "payments_data_all_the_columns" {
		t.Fatalf("field[0] name = %q, want payments_data_all_the_columns", fields[0].Name)
	}
	if fields[1].Name != "valid_name" {
		t.Fatalf("field[1] name = %q, want valid_name", fields[1].Name)
	}
	if fields[2].Name != "payments_data_all_the_columns_2" {
		t.Fatalf("field[2] name = %q, want payments_data_all_the_columns_2", fields[2].Name)
	}
	if err := ValidateLLMOutput(&LLMAnalysisOutput{
		ProposedSchema:     ProposedSchema{Fields: fields},
		ProcessingStrategy: ProcessingStrategy{Rationale: "test"},
	}); err != nil {
		t.Fatalf("ValidateLLMOutput after sanitize: %v", err)
	}
}
