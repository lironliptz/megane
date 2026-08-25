package admin

import (
	"database/sql"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"megane/internal/datamodeling"
	"megane/internal/datamodeling/codegen"
	"megane/internal/db"
)

type BuildHandler struct {
	DB        *db.DB
	SrcRoot   string
	ExportDir string // e.g. "<repo>/generated"
}

// GET /api/admin/datamodeling/build
// Returns []BuildView sorted by file_type name.
func (h *BuildHandler) ListBuild(c *gin.Context) {
	views, err := datamodeling.ListBuildViews(h.DB)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "db_error", "message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": views})
}

// GET /api/admin/datamodeling/types/:id/build
// Lazily creates a draft BuildConfig if none exists. Returns BuildDetailView.
func (h *BuildHandler) GetBuild(c *gin.Context) {
	typeID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_id", "message": "invalid id"})
		return
	}

	cfg, err := datamodeling.GetBuildConfig(h.DB, typeID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "db_error", "message": err.Error()})
		return
	}
	if cfg == nil {
		newCfg := datamodeling.BuildConfig{FileTypeID: typeID, Status: "draft"}
		savedCfg, err := datamodeling.UpsertBuildConfig(h.DB, newCfg)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "db_error", "message": err.Error()})
			return
		}
		cfg = &savedCfg
	}

	detail, err := datamodeling.GetBuildDetail(h.DB, typeID)
	if err == sql.ErrNoRows {
		c.JSON(http.StatusNotFound, gin.H{"error": "not_found", "message": "file type not found"})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "db_error", "message": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"data": detail})
}

// PATCH /api/admin/datamodeling/types/:id/build
// Body: PatchBuildRequest. Returns updated BuildConfig.
func (h *BuildHandler) PatchBuild(c *gin.Context) {
	typeID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_id", "message": "invalid id"})
		return
	}

	var req datamodeling.PatchBuildRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_body", "message": err.Error()})
		return
	}

	cfg, err := datamodeling.GetBuildConfig(h.DB, typeID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "db_error", "message": err.Error()})
		return
	}
	if cfg == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "not_found", "message": "build config not found; call GET first"})
		return
	}

	if req.Status != nil {
		cfg.Status = *req.Status
	}
	if req.FieldOverrides != nil {
		cfg.FieldOverrides = req.FieldOverrides
	}
	if req.TypeRules != nil {
		cfg.TypeRules = *req.TypeRules
	}
	if req.ProductionPrompt != nil {
		cfg.ProductionPrompt = *req.ProductionPrompt
	}
	if req.MIMETypes != nil {
		cfg.MIMETypes = req.MIMETypes
	}

	updated, err := datamodeling.UpsertBuildConfig(h.DB, *cfg)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "db_error", "message": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"data": updated})
}

// POST /api/admin/datamodeling/types/:id/build/generate-prompt
func (h *BuildHandler) GeneratePrompt(c *gin.Context) {
	typeID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_id", "message": "invalid id"})
		return
	}

	var req struct {
		UseLLM bool `json:"use_llm"`
	}
	_ = c.ShouldBindJSON(&req)

	detail, err := datamodeling.GetBuildDetail(h.DB, typeID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "db_error", "message": err.Error()})
		return
	}

	// Assuming English for now
	promptText := codegen.ProductionPrompt(detail.FileType.Name, detail.FieldRows, detail.Config, "en")

	if req.UseLLM {
		// TODO: Implement LLM refinement if needed
		// For now, just return the generated prompt
	}

	c.JSON(http.StatusOK, gin.H{"data": gin.H{"production_prompt": promptText}})
}

// POST /api/admin/datamodeling/types/:id/build/apply
func (h *BuildHandler) Apply(c *gin.Context) {
	typeID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_id", "message": "invalid id"})
		return
	}

	force := c.Query("force") == "true"

	opts := codegen.ApplyOptions{
		SrcRoot:   h.SrcRoot,
		ExportDir: h.ExportDir,
		Force:     force,
	}

	resp, err := codegen.Apply(h.DB, typeID, opts)
	if err != nil {
		if err.Error() == "artifacts already built; use ?force=true to overwrite" {
			c.JSON(http.StatusConflict, gin.H{"error": "conflict", "message": err.Error()})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "apply_error", "message": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"data": resp})
}

// POST /api/admin/datamodeling/build/apply-all
func (h *BuildHandler) ApplyAll(c *gin.Context) {
	force := c.Query("force") == "true"
	opts := codegen.ApplyOptions{
		SrcRoot:   h.SrcRoot,
		ExportDir: h.ExportDir,
		Force:     force,
	}

	views, err := datamodeling.ListBuildViews(h.DB)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "db_error", "message": err.Error()})
		return
	}

	var results []datamodeling.ApplyResponse
	for _, v := range views {
		if v.Config != nil && v.Config.Status == "confirmed" {
			resp, err := codegen.Apply(h.DB, v.FileType.ID, opts)
			if err == nil {
				results = append(results, resp)
			}
		}
	}

	c.JSON(http.StatusOK, gin.H{"data": results})
}
