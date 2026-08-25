package datamodeling

import (
	"path/filepath"
	"testing"

	"megane/internal/db"
)

func TestUpsertBuildConfig_CreateAndUpdate(t *testing.T) {
	dir := t.TempDir()
	database, err := db.Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()

	typeID, err := CreateFileType(database, "Invoice", "invoice", "", "")
	if err != nil {
		t.Fatal(err)
	}

	cfg := BuildConfig{
		FileTypeID: typeID,
		Status:     "draft",
		TypeRules:  "rule 1",
	}

	saved, err := UpsertBuildConfig(database, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if saved.ID == 0 {
		t.Fatal("expected ID > 0")
	}

	saved.Status = "confirmed"
	updated, err := UpsertBuildConfig(database, saved)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Status != "confirmed" {
		t.Fatalf("expected confirmed, got %s", updated.Status)
	}
}

func TestGetBuildDetail_FieldRowMerge(t *testing.T) {
	dir := t.TempDir()
	database, err := db.Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()

	typeID, err := CreateFileType(database, "Invoice", "invoice", "", "")
	if err != nil {
		t.Fatal(err)
	}

	schemaJSON := `{"fields":[{"name":"total","type":"number","required":true,"extraction_confidence":"high"}]}`
	_, err = CreateSchema(database, typeID, 1, schemaJSON, "{}", 0.9)
	if err != nil {
		t.Fatal(err)
	}

	detail, err := GetBuildDetail(database, typeID)
	if err != nil {
		t.Fatal(err)
	}

	if len(detail.FieldRows) != 1 {
		t.Fatalf("expected 1 field row, got %d", len(detail.FieldRows))
	}
	if !detail.FieldRows[0].Override.Included {
		t.Fatal("expected field to be included by default")
	}
	if detail.FieldRows[0].Override.Emphasis != "normal" {
		t.Fatalf("expected normal emphasis, got %s", detail.FieldRows[0].Override.Emphasis)
	}
}
