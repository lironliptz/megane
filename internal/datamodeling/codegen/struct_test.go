package codegen

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"megane/internal/datamodeling"
)

func TestGoStruct_Invoice(t *testing.T) {
	fields := []datamodeling.FieldRow{
		{
			SchemaField: datamodeling.SchemaField{Name: "invoice_number", Type: "string", Description: "The invoice number"},
			Override:    datamodeling.FieldOverride{Included: true, Required: true, Emphasis: "critical", Rules: "Must start with INV-"},
		},
		{
			SchemaField: datamodeling.SchemaField{Name: "total_amount", Type: "number", Description: "Total amount"},
			Override:    datamodeling.FieldOverride{Included: true, Required: true, Emphasis: "high", Rules: "2 decimal places"},
		},
		{
			SchemaField: datamodeling.SchemaField{Name: "invoice_date", Type: "date", Description: "Date of invoice"},
			Override:    datamodeling.FieldOverride{Included: true, Required: false, Emphasis: "normal"},
		},
		{
			SchemaField: datamodeling.SchemaField{Name: "line_items", Type: "array", Description: "List of items"},
			Override:    datamodeling.FieldOverride{Included: true, Required: false, Emphasis: "normal"},
		},
		{
			SchemaField: datamodeling.SchemaField{Name: "is_paid", Type: "boolean", Description: "Payment status"},
			Override:    datamodeling.FieldOverride{Included: true, Required: false, Emphasis: "normal"},
		},
	}

	got, err := GoStruct("invoice", fields)
	if err != nil {
		t.Fatalf("GoStruct failed: %v", err)
	}

	goldenPath := filepath.Join("testdata", "golden", "invoice_analysis.go.golden")
	if os.Getenv("UPDATE_GOLDEN") != "" {
		_ = os.WriteFile(goldenPath, got, 0644)
	}

	want, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatalf("Failed to read golden file: %v", err)
	}

	if string(got) != string(want) {
		t.Errorf("GoStruct output mismatch.\nGot:\n%s\nWant:\n%s", string(got), string(want))
	}
}

func TestGoStruct_ExcludesDroppedFields(t *testing.T) {
	fields := []datamodeling.FieldRow{
		{
			SchemaField: datamodeling.SchemaField{Name: "included_field", Type: "string"},
			Override:    datamodeling.FieldOverride{Included: true, Required: true, Emphasis: "normal"},
		},
		{
			SchemaField: datamodeling.SchemaField{Name: "dropped_field", Type: "string"},
			Override:    datamodeling.FieldOverride{Included: false, Required: true, Emphasis: "normal"},
		},
	}

	got, err := GoStruct("test", fields)
	if err != nil {
		t.Fatalf("GoStruct failed: %v", err)
	}

	if bytesContains(got, "DroppedField") {
		t.Errorf("Output contains dropped field")
	}
}

func bytesContains(b []byte, s string) bool {
	return string(b) != strings.ReplaceAll(string(b), s, "")
}
