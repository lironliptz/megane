package datamodeling

import (
	"encoding/json"
	"testing"
)

func TestUpdateStats_incrementsDocumentCounts(t *testing.T) {
	schema := ProposedSchema{Fields: []SchemaField{
		{Name: "invoice_number", Type: "string"},
		{Name: "total_due", Type: "number"},
	}}

	raw, err := UpdateStats("{}", schema, map[string]string{
		"invoice_number": "INV-1",
		"total_due":      "100",
	})
	if err != nil {
		t.Fatal(err)
	}
	stats := ParseSchemaStats(raw)
	if stats.DocumentsAnalyzed != 1 {
		t.Fatalf("documents_analyzed=%d want 1", stats.DocumentsAnalyzed)
	}
	if stats.Fields["invoice_number"].DocumentCount != 1 {
		t.Fatalf("invoice_number count=%d want 1", stats.Fields["invoice_number"].DocumentCount)
	}
	if stats.Fields["total_due"].DocumentCount != 1 {
		t.Fatalf("total_due count=%d want 1", stats.Fields["total_due"].DocumentCount)
	}
	if stats.FieldPresence["invoice_number"] != 1 {
		t.Fatalf("presence=%v want 1", stats.FieldPresence["invoice_number"])
	}
}

func TestUpdateStats_matchesSimilarFieldNames(t *testing.T) {
	schema := ProposedSchema{Fields: []SchemaField{
		{Name: "invoice_number", Type: "string"},
	}}

	raw1, _ := UpdateStats("{}", schema, map[string]string{"invoice_no": "A-1"})
	raw2, err := UpdateStats(raw1, schema, map[string]string{"InvoiceNumber": "B-2"})
	if err != nil {
		t.Fatal(err)
	}
	stats := ParseSchemaStats(raw2)
	fs := stats.Fields["invoice_number"]
	if stats.DocumentsAnalyzed != 2 {
		t.Fatalf("documents_analyzed=%d want 2", stats.DocumentsAnalyzed)
	}
	if fs.DocumentCount != 2 {
		t.Fatalf("document_count=%d want 2", fs.DocumentCount)
	}
	if len(fs.AliasesSeen) < 1 {
		t.Fatalf("expected aliases_seen, got %#v", fs.AliasesSeen)
	}
	if fs.PresenceRate != 1 {
		t.Fatalf("presence_rate=%v want 1", fs.PresenceRate)
	}
}

func TestUpdateStats_fieldAbsentInDocument(t *testing.T) {
	schema := ProposedSchema{Fields: []SchemaField{
		{Name: "invoice_number", Type: "string"},
		{Name: "tax_amount", Type: "number"},
	}}

	raw, err := UpdateStats("{}", schema, map[string]string{"invoice_number": "INV-1"})
	if err != nil {
		t.Fatal(err)
	}
	stats := ParseSchemaStats(raw)
	if stats.Fields["tax_amount"].DocumentCount != 0 {
		t.Fatalf("tax_amount count=%d want 0", stats.Fields["tax_amount"].DocumentCount)
	}
	if stats.FieldPresence["tax_amount"] != 0 {
		t.Fatalf("tax_amount presence=%v want 0", stats.FieldPresence["tax_amount"])
	}
}

func TestParseSchemaStats_legacyBlob(t *testing.T) {
	legacy := `{"field_presence":{"amount":0.5},"sample_values":{"amount":"10"}}`
	stats := ParseSchemaStats(legacy)
	if stats.FieldPresence["amount"] != 0.5 {
		t.Fatalf("legacy presence=%v", stats.FieldPresence["amount"])
	}
	if stats.Fields == nil {
		t.Fatal("expected fields map initialized")
	}
}

func TestUpdateStats_roundTripJSON(t *testing.T) {
	schema := ProposedSchema{Fields: []SchemaField{{Name: "vendor_name", Type: "string"}}}
	raw, _ := UpdateStats("{}", schema, map[string]string{"vendor_name": "Acme"})
	var decoded SchemaStats
	if err := json.Unmarshal([]byte(raw), &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.DocumentsAnalyzed != 1 || decoded.Fields["vendor_name"].DocumentCount != 1 {
		t.Fatalf("unexpected decoded stats: %#v", decoded)
	}
}
