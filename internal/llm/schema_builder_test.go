package llm

import (
	"testing"

	"github.com/google/generative-ai-go/genai"
)

type testSource struct {
	DocumentName string `json:"document_name" description:"Document title or filename"`
	PageNumber   int    `json:"page_number" description:"1-based page number"`
}

type testPaymentStage struct {
	Stage           string       `json:"stage" description:"Payment stage name"`
	Percentage      float64      `json:"percentage,omitempty" description:"Percentage of total fee"`
	CumulativeTotal float64      `json:"cumulative_total,omitempty" description:"Cumulative total"`
	Amount          float64      `json:"amount,omitempty" description:"Fixed amount"`
	TriggerEvent    string       `json:"trigger_event" description:"Event triggering payment"`
	Source          *testSource  `json:"source,omitempty" description:"Citation source"`
}

type testFeeStructure struct {
	TotalCostStructure string             `json:"total_cost_structure" description:"Summary of fee structure"`
	VatIncluded        bool               `json:"vat_included" description:"Whether VAT is included"`
	Currency           string             `json:"currency" description:"ISO currency code" enum:"ILS,USD,EUR"`
	PaymentTerms       string             `json:"payment_terms,omitempty" description:"Payment terms"`
	Indexation         string             `json:"indexation,omitempty" description:"Price index"`
	PaymentSchedule    []testPaymentStage `json:"payment_schedule" description:"Payment milestones"`
	Source             *testSource        `json:"source,omitempty" description:"Citation source"`
}

type testSimpleStruct struct {
	Name  string  `json:"name" description:"Display name"`
	Count int     `json:"count"`
	Notes *string `json:"notes,omitempty" description:"Optional notes"`
	Skip  string  `json:"-"`
}

func TestBuildSchema_SimpleStruct(t *testing.T) {
	schema, err := BuildSchema(testSimpleStruct{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if schema.Type != genai.TypeObject {
		t.Errorf("expected TypeObject, got %v", schema.Type)
	}

	if _, ok := schema.Properties["Skip"]; ok {
		t.Error("field with json:\"-\" should be skipped")
	}
	if _, ok := schema.Properties["notes"]; !ok {
		t.Error("notes field should be present")
	}

	requiredMap := make(map[string]bool)
	for _, r := range schema.Required {
		requiredMap[r] = true
	}

	if !requiredMap["name"] {
		t.Error("name should be required")
	}
	if !requiredMap["count"] {
		t.Error("count should be required")
	}
	if requiredMap["notes"] {
		t.Error("notes should NOT be required (pointer + omitempty)")
	}
}

func TestBuildSchema_NestedStruct(t *testing.T) {
	schema, err := BuildSchema(testFeeStructure{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	currencySchema := schema.Properties["currency"]
	if currencySchema == nil {
		t.Fatal("currency property missing")
	}
	if len(currencySchema.Enum) != 3 {
		t.Errorf("expected 3 enum values for currency, got %d", len(currencySchema.Enum))
	}

	scheduleSchema := schema.Properties["payment_schedule"]
	if scheduleSchema == nil {
		t.Fatal("payment_schedule property missing")
	}
	if scheduleSchema.Type != genai.TypeArray {
		t.Errorf("expected TypeArray, got %v", scheduleSchema.Type)
	}
	if scheduleSchema.Items.Type != genai.TypeObject {
		t.Errorf("expected array items to be TypeObject, got %v", scheduleSchema.Items.Type)
	}

	requiredMap := make(map[string]bool)
	for _, r := range schema.Required {
		requiredMap[r] = true
	}
	if requiredMap["source"] {
		t.Error("source should NOT be required (pointer type)")
	}
	if !requiredMap["total_cost_structure"] {
		t.Error("total_cost_structure should be required")
	}
}

func TestBuildSchema_Pointer(t *testing.T) {
	schema, err := BuildSchema(&testSimpleStruct{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if schema.Type != genai.TypeObject {
		t.Errorf("expected TypeObject, got %v", schema.Type)
	}
}

func TestBuildSchema_NonStruct(t *testing.T) {
	_, err := BuildSchema("not a struct")
	if err == nil {
		t.Error("expected error for non-struct input")
	}
}

func TestBuildSchema_Description(t *testing.T) {
	schema, err := BuildSchema(testSimpleStruct{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	nameSchema := schema.Properties["name"]
	if nameSchema.Description != "Display name" {
		t.Errorf("expected description 'Display name', got %q", nameSchema.Description)
	}

	countSchema := schema.Properties["count"]
	if countSchema.Description != "" {
		t.Errorf("expected empty description for count, got %q", countSchema.Description)
	}
}
