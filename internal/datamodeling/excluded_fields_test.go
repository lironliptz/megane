package datamodeling

import "testing"

func TestMatchesExcludedField(t *testing.T) {
	excluded := []string{"invoice_number", "total_amount"}
	if !MatchesExcludedField("invoice_number", excluded) {
		t.Fatal("exact match should be excluded")
	}
	if !MatchesExcludedField("Invoice_Number", excluded) {
		t.Fatal("case-insensitive similar match should be excluded")
	}
	if MatchesExcludedField("vendor_name", excluded) {
		t.Fatal("unrelated field should not match")
	}
}

func TestFilterSchemaFieldsNotExcluded(t *testing.T) {
	fields := []SchemaField{
		{Name: "invoice_number", Type: "string"},
		{Name: "vendor_name", Type: "string"},
		{Name: "total_amount", Type: "number"},
	}
	filtered := filterSchemaFieldsNotExcluded(fields, []string{"total_amount"})
	if len(filtered) != 2 {
		t.Fatalf("len = %d, want 2", len(filtered))
	}
	if filtered[0].Name != "invoice_number" || filtered[1].Name != "vendor_name" {
		t.Fatalf("unexpected fields: %+v", filtered)
	}
}

func TestMergeSchemaRespectsExcluded(t *testing.T) {
	current := &ProposedSchema{
		Fields: []SchemaField{
			{Name: "invoice_number", Type: "string", ExtractionConfidence: "high"},
			{Name: "notes", Type: "string", ExtractionConfidence: "medium"},
		},
	}
	llmProposal := ProposedSchema{
		Fields: []SchemaField{
			{Name: "invoice_number", Type: "string", ExtractionConfidence: "high"},
			{Name: "notes", Type: "string", ExtractionConfidence: "high"},
			{Name: "total_amount", Type: "number", ExtractionConfidence: "high"},
		},
	}
	excluded := []string{"notes", "total_amount"}

	merged, _ := MergeSchema(current, llmProposal, nil, nil, excluded)
	names := make([]string, 0, len(merged.Fields))
	for _, f := range merged.Fields {
		names = append(names, f.Name)
	}
	if len(names) != 1 || names[0] != "invoice_number" {
		t.Fatalf("merged fields = %v, want [invoice_number]", names)
	}
}

func TestAddExcludedFieldDedupes(t *testing.T) {
	items := addExcludedField(nil, "foo")
	items = addExcludedField(items, "Foo")
	if len(items) != 1 {
		t.Fatalf("len = %d, want 1", len(items))
	}
}
