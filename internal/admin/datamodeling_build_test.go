package admin

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/gin-gonic/gin"

	"megane/internal/datamodeling"
	"megane/internal/db"
)

func TestBuildApply_ReturnsArtifacts(t *testing.T) {
	gin.SetMode(gin.TestMode)

	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")
	database, err := db.Open(dbPath)
	if err != nil {
		t.Fatalf("db open: %v", err)
	}
	defer database.Close()

	// Seed data
	typeID, err := datamodeling.CreateFileType(database, "Invoice", "invoice", "", "")
	if err != nil {
		t.Fatalf("create type: %v", err)
	}

	schemaJSON := `{"fields":[{"name":"total","type":"number","required":true,"extraction_confidence":"high"}]}`
	_, err = datamodeling.CreateSchema(database, typeID, 1, schemaJSON, "{}", 0.9)
	if err != nil {
		t.Fatalf("create schema: %v", err)
	}

	cfg := datamodeling.BuildConfig{
		FileTypeID: typeID,
		Status:     "confirmed",
		FieldOverrides: []datamodeling.FieldOverride{
			{Name: "total", Included: true, Required: true, Emphasis: "critical"},
		},
		MIMETypes: []string{"application/pdf"},
	}
	if _, err := datamodeling.UpsertBuildConfig(database, cfg); err != nil {
		t.Fatalf("upsert config: %v", err)
	}

	h := &BuildHandler{
		DB:        database,
		SrcRoot:   dir,
		ExportDir: filepath.Join(dir, "generated"),
	}

	router := gin.New()
	router.POST("/apply/:id", h.Apply)

	req, _ := http.NewRequest("POST", "/apply/1", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp struct {
		Data datamodeling.ApplyResponse `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}

	if len(resp.Data.Artifacts) != 4 {
		t.Fatalf("expected 4 artifacts, got %d", len(resp.Data.Artifacts))
	}

	// Verify files exist
	for _, a := range resp.Data.Artifacts {
		if _, err := os.Stat(a.Path); os.IsNotExist(err) {
			t.Errorf("artifact file not created: %s", a.Path)
		}
	}
}
