package admin

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"megane/internal/auth"
	"megane/internal/datamodeling"
	"megane/internal/db"
	"megane/internal/llm"
)

const defaultDatamodelingTimeout = 10 * time.Minute

type DMHandler struct {
	Analyzer        *datamodeling.Analyzer
	DB              *db.DB
	ProjectsDir     string
	AnalysisTimeout time.Duration // HTTP + LLM budget for analyze / reprocess
}

func (h *DMHandler) analysisContext(parent context.Context) (context.Context, context.CancelFunc) {
	d := defaultDatamodelingTimeout
	if h != nil && h.AnalysisTimeout > 0 {
		d = h.AnalysisTimeout
	}
	return context.WithTimeout(parent, d)
}

// ResolveDatamodelingTimeout reads LLM_REQUEST_TIMEOUT for analyze routes (default 10m).
func ResolveDatamodelingTimeout() time.Duration {
	if v := strings.TrimSpace(os.Getenv("LLM_REQUEST_TIMEOUT")); v != "" {
		if d, err := time.ParseDuration(v); err == nil && d > 0 {
			return d
		}
	}
	return defaultDatamodelingTimeout
}

// POST /api/admin/datamodeling/analyze
func (h *DMHandler) Analyze(c *gin.Context) {
	if err := c.Request.ParseMultipartForm(50 << 20); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_form", "message": err.Error()})
		return
	}

	var fileTypeIDPtr *int64
	if s := c.Request.FormValue("file_type_id"); s != "" {
		id, err := strconv.ParseInt(s, 10, 64)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_file_type_id", "message": "invalid file_type_id"})
			return
		}
		fileTypeIDPtr = &id
	}

	hints := datamodeling.NormalizeFieldHintsFromForm(c.Request.Form["field_hints"])

	var fileIDs []int64
	for _, s := range c.Request.Form["file_ids"] {
		if id, err := strconv.ParseInt(s, 10, 64); err == nil {
			fileIDs = append(fileIDs, id)
		}
	}

	maxCostTier := c.Request.FormValue("max_cost_tier")
	if maxCostTier == "" {
		maxCostTier = "premium"
	}
	extractionBrief := strings.TrimSpace(c.Request.FormValue("extraction_brief"))

	adminUserID := getUserID(c)
	fileMeta := make(map[int64]struct {
		Hash string
		Size int64
	})
	var duplicateWarnings []datamodeling.DuplicateMatch
	if fhs := c.Request.MultipartForm.File["files"]; len(fhs) > 0 {
		userDir := filepath.Join(h.ProjectsDir, strconv.FormatInt(adminUserID, 10))
		if err := os.MkdirAll(userDir, 0755); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "storage_error", "message": "could not create storage directory"})
			return
		}
		for _, fh := range fhs {
			src, err := fh.Open()
			if err != nil {
				slog.Warn("datamodeling upload: open file", "name", fh.Filename, "err", err)
				continue
			}
			ext := filepath.Ext(fh.Filename)
			storedName := uuid.New().String() + ext
			destPath := filepath.Join(userDir, storedName)
			dst, err := os.Create(destPath)
			if err != nil {
				src.Close()
				slog.Warn("datamodeling upload: create dest", "path", destPath, "err", err)
				continue
			}
			if _, err := io.Copy(dst, src); err != nil {
				dst.Close()
				src.Close()
				_ = os.Remove(destPath)
				slog.Warn("datamodeling upload: copy file", "name", fh.Filename, "err", err)
				continue
			}
			dst.Close()
			src.Close()

			mimeType := dmDetectMIME(fh.Header.Get("Content-Type"), ext)
			projID, err := h.DB.CreateProject(adminUserID, destPath, fh.Filename, mimeType, "", fh.Size)
			if err != nil {
				_ = os.Remove(destPath)
				slog.Warn("datamodeling upload: create project", "name", fh.Filename, "err", err)
				continue
			}
			fileIDs = append(fileIDs, projID)
			contentHash, hashErr := datamodeling.HashFile(destPath)
			if hashErr != nil {
				slog.Warn("datamodeling upload: hash file", "name", fh.Filename, "err", hashErr)
			}
			fileMeta[projID] = struct {
				Hash string
				Size int64
			}{Hash: contentHash, Size: fh.Size}
			if fileTypeIDPtr != nil {
				if match, err := datamodeling.FindDuplicateSample(h.DB, *fileTypeIDPtr, fh.Filename, fh.Size, contentHash); err != nil {
					slog.Warn("datamodeling upload: duplicate check", "name", fh.Filename, "err", err)
				} else if match != nil {
					duplicateWarnings = append(duplicateWarnings, *match)
				}
			}
			slog.Info("datamodeling file staged",
				slog.Int64("user_id", adminUserID),
				slog.Int64("project_id", projID),
				slog.String("original_name", fh.Filename),
				slog.String("mime_type", mimeType),
			)
		}
	}

	if len(fileIDs) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "no_files", "message": "no valid files to analyze"})
		return
	}

	progress, err := datamodeling.BuildInitialFileProgress(h.DB, fileIDs)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "db_error", "message": "could not initialize file progress"})
		return
	}
	if fileTypeIDPtr != nil && len(fileMeta) > 0 {
		progress = datamodeling.AnnotateFileProgressDuplicates(h.DB, *fileTypeIDPtr, progress, fileMeta)
	}

	req := datamodeling.AnalysisRequest{
		FileTypeID:      fileTypeIDPtr,
		FieldHints:      hints,
		FileIDs:         fileIDs,
		MaxCostTier:     maxCostTier,
		ExtractionBrief: extractionBrief,
	}
	jobID, err := datamodeling.CreateAnalysisJob(h.DB, req, progress)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "db_error", "message": "could not create analysis job"})
		return
	}

	// Detached context: HTTP returns immediately while LLM runs in a background goroutine.
	go h.runAnalysisJob(jobID, req)

	resp := gin.H{
		"job_id":        jobID,
		"status":        datamodeling.JobPending,
		"file_progress": progress,
	}
	if len(duplicateWarnings) > 0 {
		resp["duplicate_warnings"] = duplicateWarnings
	}
	c.JSON(http.StatusAccepted, gin.H{"data": resp})
}

// GET /api/admin/datamodeling/jobs/:id
func (h *DMHandler) GetAnalysisJob(c *gin.Context) {
	jobID := strings.TrimSpace(c.Param("id"))
	if jobID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_id", "message": "job id required"})
		return
	}
	job, err := datamodeling.GetAnalysisJob(h.DB, jobID)
	if err == sql.ErrNoRows {
		c.JSON(http.StatusNotFound, gin.H{"error": "not_found", "message": "analysis job not found"})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "db_error", "message": err.Error()})
		return
	}
	resp, err := datamodeling.JobStatusResponseFrom(job)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "db_error", "message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": resp})
}

func (h *DMHandler) runAnalysisJob(jobID string, req datamodeling.AnalysisRequest) {
	ctx, cancel := h.analysisContext(context.Background())
	defer cancel()

	if err := datamodeling.SetJobRunning(h.DB, jobID); err != nil {
		slog.Error("datamodeling: mark job running", "job_id", jobID, "err", err)
		return
	}

	req.JobID = jobID

	slog.Info("datamodeling: job started",
		"job_id", jobID,
		"file_count", len(req.FileIDs),
	)

	result, err := h.Analyzer.AnalyzeFiles(ctx, req)
	if err != nil {
		_, body := datamodeling.ClassifyAnalysisError(err)
		_ = datamodeling.SetJobError(h.DB, jobID, body)
		slog.Error("datamodeling: job failed", "job_id", jobID, "reason", body.Code, "err", llm.SafeErr(err))
		return
	}
	if err := datamodeling.SetJobComplete(h.DB, jobID, result); err != nil {
		slog.Error("datamodeling: save job result", "job_id", jobID, "err", err)
		return
	}
	slog.Info("datamodeling: job complete", "job_id", jobID)
}

// POST /api/admin/datamodeling/types
func (h *DMHandler) CreateType(c *gin.Context) {
	var body struct {
		Name            string                `json:"name" binding:"required"`
		ExtractionBrief string                `json:"extraction_brief"`
		FieldHints      []datamodeling.FieldHint `json:"field_hints"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_body", "message": err.Error()})
		return
	}
	slug := datamodeling.Slugify(body.Name)
	if slug == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_name", "message": "name produces empty slug"})
		return
	}
	// Ensure unique slug
	base := slug
	for i := 2; i <= 100; i++ {
		_, err := datamodeling.GetFileTypeBySlug(h.DB, slug)
		if err == sql.ErrNoRows {
			break
		}
		slug = fmt.Sprintf("%s_%d", base, i)
		if i == 100 {
			c.JSON(http.StatusConflict, gin.H{"error": "slug_conflict", "message": "could not generate unique slug"})
			return
		}
	}
	hintsJSON, err := datamodeling.MarshalFieldHintsJSON(body.FieldHints)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "db_error", "message": "invalid field hints"})
		return
	}
	id, err := datamodeling.CreateFileType(h.DB, body.Name, slug, strings.TrimSpace(body.ExtractionBrief), hintsJSON)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "db_error", "message": err.Error()})
		return
	}
	ft, _ := datamodeling.GetFileTypeByID(h.DB, id)
	v := datamodeling.FileTypeViewFrom(ft)
	v.IsNew = true
	c.JSON(http.StatusOK, gin.H{"data": v})
}

// PATCH /api/admin/datamodeling/types/:id/config
func (h *DMHandler) UpdateTypeConfig(c *gin.Context) {
	fileTypeID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_id", "message": "invalid id"})
		return
	}
	var body struct {
		ExtractionBrief string                   `json:"extraction_brief"`
		FieldHints      []datamodeling.FieldHint `json:"field_hints"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_body", "message": err.Error()})
		return
	}
	if _, err := datamodeling.GetFileTypeByID(h.DB, fileTypeID); err == sql.ErrNoRows {
		c.JSON(http.StatusNotFound, gin.H{"error": "not_found", "message": "file type not found"})
		return
	} else if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "db_error", "message": err.Error()})
		return
	}
	if err := datamodeling.UpdateFileTypeConfig(h.DB, fileTypeID, strings.TrimSpace(body.ExtractionBrief), body.FieldHints); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "db_error", "message": err.Error()})
		return
	}
	ft, _ := datamodeling.GetFileTypeByID(h.DB, fileTypeID)
	c.JSON(http.StatusOK, gin.H{"data": datamodeling.FileTypeViewFrom(ft)})
}

// GET /api/admin/datamodeling/types
func (h *DMHandler) ListTypes(c *gin.Context) {
	summaries, err := datamodeling.ListFileTypeSummaries(h.DB)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "db_error", "message": err.Error()})
		return
	}

	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	perPage, _ := strconv.Atoi(c.DefaultQuery("per_page", "20"))
	if page < 1 {
		page = 1
	}
	if perPage < 1 || perPage > 100 {
		perPage = 20
	}

	start := (page - 1) * perPage
	end := start + perPage
	if start > len(summaries) {
		start = len(summaries)
	}
	if end > len(summaries) {
		end = len(summaries)
	}
	page_summaries := summaries[start:end]

	views := make([]datamodeling.FileTypeView, 0, len(page_summaries))
	for _, s := range page_summaries {
		v := datamodeling.FileTypeViewFrom(&s.FileType)
		v.FilesAnalyzed = s.FilesAnalyzed
		v.LatestVersion = s.LatestVersion
		if s.LatestSchema != nil {
			v.ConvergenceScore = s.LatestSchema.ConvergenceScore
			v.IsFinalized = s.LatestSchema.IsFinalized
		}
		views = append(views, v)
	}

	c.JSON(http.StatusOK, gin.H{"data": views, "total": len(summaries), "page": page, "per_page": perPage})
}

// GET /api/admin/datamodeling/types/:id/schema
func (h *DMHandler) GetSchema(c *gin.Context) {
	fileTypeID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_id", "message": "invalid id"})
		return
	}

	ft, err := datamodeling.GetFileTypeByID(h.DB, fileTypeID)
	if err == sql.ErrNoRows {
		c.JSON(http.StatusNotFound, gin.H{"error": "not_found", "message": "file type not found"})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "db_error", "message": err.Error()})
		return
	}

	var schema *datamodeling.Schema
	if vStr := c.Query("version"); vStr != "" {
		v, _ := strconv.Atoi(vStr)
		schema, err = datamodeling.GetSchemaByVersion(h.DB, fileTypeID, v)
	} else {
		schema, err = datamodeling.GetLatestSchema(h.DB, fileTypeID)
	}
	if err != nil && err != sql.ErrNoRows {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "db_error", "message": err.Error()})
		return
	}

	ftView := datamodeling.FileTypeViewFrom(ft)

	if schema == nil {
		c.JSON(http.StatusOK, gin.H{"data": gin.H{"file_type": ftView, "current_schema": nil, "message": "No schema yet"}})
		return
	}

	var proposedSchema datamodeling.ProposedSchema
	_ = json.Unmarshal([]byte(schema.SchemaJSON), &proposedSchema)

	fieldStats := datamodeling.ParseSchemaStats(schema.StatsJSON)

	resp := gin.H{
		"file_type":         ftView,
		"current_schema":    proposedSchema,
		"field_statistics":  fieldStats,
		"schema_version":    schema.Version,
		"convergence_score": schema.ConvergenceScore,
		"is_finalized":      schema.IsFinalized,
	}

	if c.Query("include_history") == "true" {
		count, _ := datamodeling.CountSchemaVersions(h.DB, fileTypeID)
		resp["version_count"] = count
	}

	samples, err := datamodeling.ListTypeSamples(h.DB, fileTypeID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "db_error", "message": err.Error()})
		return
	}
	resp["sample_files"] = samples

	c.JSON(http.StatusOK, gin.H{"data": resp})
}

// GET /api/admin/datamodeling/types/:id/samples
func (h *DMHandler) ListTypeSamples(c *gin.Context) {
	fileTypeID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_id", "message": "invalid id"})
		return
	}
	if _, err := datamodeling.GetFileTypeByID(h.DB, fileTypeID); err == sql.ErrNoRows {
		c.JSON(http.StatusNotFound, gin.H{"error": "not_found", "message": "file type not found"})
		return
	} else if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "db_error", "message": err.Error()})
		return
	}
	samples, err := datamodeling.ListTypeSamples(h.DB, fileTypeID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "db_error", "message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": samples})
}

// POST /api/admin/datamodeling/types/:id/check-samples
func (h *DMHandler) CheckTypeSamples(c *gin.Context) {
	fileTypeID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_id", "message": "invalid id"})
		return
	}
	if _, err := datamodeling.GetFileTypeByID(h.DB, fileTypeID); err == sql.ErrNoRows {
		c.JSON(http.StatusNotFound, gin.H{"error": "not_found", "message": "file type not found"})
		return
	} else if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "db_error", "message": err.Error()})
		return
	}
	var body struct {
		Samples []struct {
			Name string `json:"name"`
			Size int64  `json:"size"`
		} `json:"samples"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_body", "message": err.Error()})
		return
	}
	names := make([]string, 0, len(body.Samples))
	sizes := make([]int64, 0, len(body.Samples))
	for _, s := range body.Samples {
		names = append(names, s.Name)
		sizes = append(sizes, s.Size)
	}
	matches, err := datamodeling.CheckSamplesByNameSize(h.DB, fileTypeID, names, sizes)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "db_error", "message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": gin.H{"duplicates": matches}})
}

// POST /api/admin/datamodeling/types/:id/exclude-field
func (h *DMHandler) ExcludeSchemaField(c *gin.Context) {
	fileTypeID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_id", "message": "invalid id"})
		return
	}

	var body struct {
		FieldName string `json:"field_name"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_body", "message": err.Error()})
		return
	}

	schema, excluded, err := datamodeling.ExcludeSchemaField(h.DB, fileTypeID, body.FieldName)
	if errors.Is(err, datamodeling.ErrNoSchema{}) {
		c.JSON(http.StatusNotFound, gin.H{"error": "no_schema", "message": "no schema for this type yet"})
		return
	}
	var notFound datamodeling.ErrSchemaFieldNotFound
	if errors.As(err, &notFound) {
		c.JSON(http.StatusNotFound, gin.H{"error": "schema_field_not_found", "message": "field not in schema: " + notFound.Name})
		return
	}
	if errors.Is(err, datamodeling.ErrFieldNameRequired) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "field_name_required", "message": "field_name is required"})
		return
	}
	if err == sql.ErrNoRows {
		c.JSON(http.StatusNotFound, gin.H{"error": "not_found", "message": "file type not found"})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "db_error", "message": err.Error()})
		return
	}

	var proposedSchema datamodeling.ProposedSchema
	_ = json.Unmarshal([]byte(schema.SchemaJSON), &proposedSchema)
	ft, _ := datamodeling.GetFileTypeByID(h.DB, fileTypeID)

	c.JSON(http.StatusOK, gin.H{"data": gin.H{
		"file_type":        datamodeling.FileTypeViewFrom(ft),
		"current_schema":   proposedSchema,
		"field_statistics": datamodeling.ParseSchemaStats(schema.StatsJSON),
		"schema_version":   schema.Version,
		"excluded_fields":  excluded,
	}})
}

// POST /api/admin/datamodeling/types/:id/manual-answer
func (h *DMHandler) SubmitManualAnswer(c *gin.Context) {
	fileTypeID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_id", "message": "invalid id"})
		return
	}

	var req datamodeling.ManualAnswerRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_body", "message": err.Error()})
		return
	}

	var analysis *datamodeling.Analysis
	if req.AnalysisID != 0 {
		var err error
		analysis, err = datamodeling.GetAnalysisByID(h.DB, req.AnalysisID)
		if err == sql.ErrNoRows {
			c.JSON(http.StatusNotFound, gin.H{"error": "not_found", "message": "analysis not found"})
			return
		}
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "db_error", "message": err.Error()})
			return
		}
		if analysis.FileTypeID != fileTypeID {
			c.JSON(http.StatusBadRequest, gin.H{"error": "mismatch", "message": "analysis does not belong to this file type"})
			return
		}
	}

	if req.ReprocessFiles && analysis != nil {
		reprocessReq := datamodeling.AnalysisRequest{
			FileTypeID:    &fileTypeID,
			FileIDs:       []int64{analysis.FileID},
			ManualAnswers: req.Answers,
			MaxCostTier:   "premium",
		}
		progress, err := datamodeling.BuildInitialFileProgress(h.DB, reprocessReq.FileIDs)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "db_error", "message": "could not initialize file progress"})
			return
		}
		jobID, err := datamodeling.CreateAnalysisJob(h.DB, reprocessReq, progress)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "db_error", "message": "could not create analysis job"})
			return
		}
		go h.runAnalysisJob(jobID, reprocessReq)
		c.JSON(http.StatusAccepted, gin.H{"data": gin.H{
			"job_id":        jobID,
			"status":        datamodeling.JobPending,
			"file_progress": progress,
		}})
		return
	}

	c.JSON(http.StatusOK, gin.H{"data": gin.H{"status": "recorded"}})
}

// POST /api/admin/datamodeling/types/:id/finalize
func (h *DMHandler) FinalizeSchema(c *gin.Context) {
	fileTypeID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_id", "message": "invalid id"})
		return
	}

	var req datamodeling.FinalizeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_body", "message": err.Error()})
		return
	}

	if err := datamodeling.SetSchemaFinalized(h.DB, fileTypeID, req.IsFinalized); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "db_error", "message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": gin.H{"is_finalized": req.IsFinalized}})
}

// DELETE /api/admin/datamodeling/types/:id
func (h *DMHandler) DeleteType(c *gin.Context) {
	fileTypeID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_id", "message": "invalid id"})
		return
	}

	_, err = datamodeling.GetFileTypeByID(h.DB, fileTypeID)
	if err == sql.ErrNoRows {
		c.JSON(http.StatusNotFound, gin.H{"error": "not_found", "message": "file type not found"})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "db_error", "message": err.Error()})
		return
	}

	if err := datamodeling.DeleteFileType(h.DB, fileTypeID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "db_error", "message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": gin.H{"deleted": true}})
}

func dmDetectMIME(contentType, ext string) string {
	mt := strings.TrimSpace(contentType)
	if idx := strings.Index(mt, ";"); idx != -1 {
		mt = strings.TrimSpace(mt[:idx])
	}
	if mt != "" {
		return mt
	}
	switch strings.ToLower(ext) {
	case ".pdf":
		return "application/pdf"
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".png":
		return "image/png"
	case ".gif":
		return "image/gif"
	case ".webp":
		return "image/webp"
	case ".xlsx":
		return "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"
	case ".xls":
		return "application/vnd.ms-excel"
	case ".docx":
		return "application/vnd.openxmlformats-officedocument.wordprocessingml.document"
	case ".doc":
		return "application/msword"
	case ".txt":
		return "text/plain"
	case ".md", ".markdown":
		return "text/markdown"
	case ".csv":
		return "text/csv"
	case ".json":
		return "application/json"
	default:
		return "application/octet-stream"
	}
}

func getUserID(c *gin.Context) int64 {
	raw, _ := c.Get(auth.ContextUserID)
	switch v := raw.(type) {
	case int64:
		return v
	case string:
		id, _ := strconv.ParseInt(v, 10, 64)
		return id
	}
	return 0
}
