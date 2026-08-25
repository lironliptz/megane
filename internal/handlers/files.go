package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"megane/internal/auth"
	"megane/internal/db"
	"megane/internal/llm"
	"megane/internal/pipeline"
)

var allowedMIMETypes = map[string]bool{
	"application/pdf":  true,
	"image/jpeg":       true,
	"image/png":        true,
	"image/gif":        true,
	"image/webp":       true,
	"text/plain":       true,
	"text/csv":         true,
	"text/markdown":    true,
	"application/json": true,
	"application/vnd.openxmlformats-officedocument.spreadsheetml.sheet":       true,
	"application/vnd.ms-excel":                                                 true,
	"application/vnd.openxmlformats-officedocument.wordprocessingml.document": true,
	"application/msword": true,
	// DWG/DXF: re-add "image/vnd.dwg" / "image/vnd.dxf" once DWGConverter is wired.
}

// FileHandler handles file upload, listing, status polling, and result retrieval.
type FileHandler struct {
	DB          *db.DB
	Pipeline    *pipeline.Pipeline
	ProjectsDir string
	LLM         LLMRouteConfig
}

// ListLLMModels returns Gemini model ids for the upload-page picker (Gemini provider only).
func (h *FileHandler) ListLLMModels(c *gin.Context) {
	prov := strings.TrimSpace(strings.ToLower(h.LLM.Provider))
	def := strings.TrimSpace(h.LLM.EffectiveGeminiModel)
	if prov != "gemini" {
		c.JSON(http.StatusOK, gin.H{"data": gin.H{
			"provider":      prov,
			"models":        []string{},
			"default_model": "",
		}, "error": nil})
		return
	}
	if strings.TrimSpace(h.LLM.GeminiAPIKey) == "" {
		c.JSON(http.StatusOK, gin.H{"data": gin.H{
			"provider":      prov,
			"models":        []string{},
			"default_model": def,
		}, "error": nil})
		return
	}

	models, err := llm.ListGeminiGenerativeModels(c.Request.Context(), h.LLM.GeminiAPIKey)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"data": nil, "error": err.Error()})
		return
	}
	models = ensureGeminiDefaultInList(models, def)
	c.JSON(http.StatusOK, gin.H{"data": gin.H{
		"provider":      prov,
		"models":        models,
		"default_model": def,
	}, "error": nil})
}

func ensureGeminiDefaultInList(list []string, def string) []string {
	def = strings.TrimSpace(def)
	if def == "" {
		sort.Strings(list)
		return list
	}
	for _, x := range list {
		if x == def {
			sort.Strings(list)
			return list
		}
	}
	list = append(list, def)
	sort.Strings(list)
	return list
}

// Upload accepts one or more files, persists them, and starts the pipeline.
func (h *FileHandler) Upload(c *gin.Context) {
	userIDStr, _ := c.Get(auth.ContextUserID)
	userID, err := strconv.ParseInt(userIDStr.(string), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"data": nil, "error": "invalid user id"})
		return
	}

	form, err := c.MultipartForm()
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"data": nil, "error": "invalid multipart form"})
		return
	}

	files := form.File["files"]
	if len(files) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"data": nil, "error": "no files provided"})
		return
	}

	// Validate each file before persisting any.
	for _, fh := range files {
		if fh.Size > maxUploadBytes {
			c.JSON(http.StatusRequestEntityTooLarge, gin.H{"data": nil, "error": fmt.Sprintf("%q exceeds 50 MB limit", fh.Filename)})
			return
		}
		mt := fh.Header.Get("Content-Type")
		if idx := strings.Index(mt, ";"); idx != -1 {
			mt = strings.TrimSpace(mt[:idx])
		}
		if mt == "" {
			mt = detectMIME(filepath.Ext(fh.Filename))
		}
		if !allowedMIMETypes[mt] {
			c.JSON(http.StatusUnsupportedMediaType, gin.H{"data": nil, "error": fmt.Sprintf("unsupported file type %q", mt)})
			return
		}
	}

	llmModelChoice := ""
	if strings.TrimSpace(strings.ToLower(h.LLM.Provider)) == "gemini" {
		if vals := form.Value["model"]; len(vals) > 0 {
			llmModelChoice = strings.TrimSpace(vals[0])
		}
		if llmModelChoice != "" {
			if err := llm.ValidateGeminiModelID(llmModelChoice); err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"data": nil, "error": err.Error()})
				return
			}
		}
	}

	userDir := filepath.Join(h.ProjectsDir, strconv.FormatInt(userID, 10))
	if err := os.MkdirAll(userDir, 0755); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"data": nil, "error": "could not create storage directory"})
		return
	}

	type uploadedFile struct {
		ID           int64  `json:"id"`
		OriginalName string `json:"original_name"`
		Status       string `json:"status"`
	}
	var uploaded []uploadedFile

	for _, fh := range files {
		originalName := fh.Filename
		ext := filepath.Ext(originalName)
		storedName := uuid.New().String() + ext
		destPath := filepath.Join(userDir, storedName)

		if err := c.SaveUploadedFile(fh, destPath); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"data": nil, "error": fmt.Sprintf("save file %q: %v", originalName, err)})
			return
		}

		mimeType := fh.Header.Get("Content-Type")
		if mimeType == "" {
			mimeType = detectMIME(ext)
		}
		// Strip parameters (e.g., "application/pdf; charset=utf-8").
		if idx := strings.Index(mimeType, ";"); idx != -1 {
			mimeType = strings.TrimSpace(mimeType[:idx])
		}

		projectID, err := h.DB.CreateProject(userID, destPath, originalName, mimeType, llmModelChoice, fh.Size)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"data": nil, "error": "database error"})
			return
		}

		_ = h.DB.AddEvent(projectID, "uploaded", fmt.Sprintf("file %q uploaded (%s)", originalName, mimeType))

		modelLog := llmModelChoice
		if modelLog == "" {
			modelLog = "server default"
		}
		slog.Info("file uploaded",
			slog.Int64("user_id", userID),
			slog.Int64("project_id", projectID),
			slog.String("original_name", originalName),
			slog.String("mime_type", mimeType),
			slog.Int64("size_bytes", fh.Size),
			slog.String("llm_model", modelLog),
		)

		// Use a detached context: the HTTP request context is canceled when this handler returns,
		// which would abort the LLM call running in the background goroutine.
		go h.Pipeline.EnqueueProcess(context.Background(), projectID)

		uploaded = append(uploaded, uploadedFile{
			ID:           projectID,
			OriginalName: originalName,
			Status:       db.StatusUploaded,
		})
	}

	if len(uploaded) > 0 {
		slog.Info("upload batch queued for processing",
			slog.Int64("user_id", userID),
			slog.Int("file_count", len(uploaded)),
		)
	}

	c.JSON(http.StatusOK, gin.H{"data": uploaded, "error": nil})
}

// List returns all projects belonging to the authenticated user.
func (h *FileHandler) List(c *gin.Context) {
	userIDStr, _ := c.Get(auth.ContextUserID)
	userID, err := strconv.ParseInt(userIDStr.(string), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"data": nil, "error": "invalid user id"})
		return
	}

	projects, err := h.DB.GetProjectsByUser(userID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"data": nil, "error": "database error"})
		return
	}
	if projects == nil {
		projects = []*db.Project{}
	}
	c.JSON(http.StatusOK, gin.H{"data": projects, "error": nil})
}

// Status returns all pipeline events for a given project.
func (h *FileHandler) Status(c *gin.Context) {
	projectID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"data": nil, "error": "invalid project id"})
		return
	}

	// Verify ownership.
	if err := h.verifyOwnership(c, projectID); err != nil {
		return
	}

	project, err := h.DB.GetProjectByID(projectID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"data": nil, "error": "project not found"})
		return
	}

	events, err := h.DB.GetEvents(projectID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"data": nil, "error": "database error"})
		return
	}
	if events == nil {
		events = []*db.PipelineEvent{}
	}

	payload := gin.H{
		"status":                project.Status,
		"events":                events,
		"pipeline_progress":     PipelineStatusPct(project.Status),
		"pipeline_status_label": PipelineStatusLabel(project.Status),
	}
	if project.Status == db.StatusError {
		if detail, err := h.DB.LastPipelineError(projectID); err == nil && detail != "" {
			payload["error_detail"] = detail
		}
	}
	timing, err := h.DB.LLMTimingForProject(projectID)
	if err == nil {
		if timing.DurationSec > 0 || timing.InProgress {
			payload["llm_duration_sec"] = int(timing.DurationSec + 0.5)
			payload["llm_in_progress"] = timing.InProgress
		}
	}
	if info, err := h.DB.LLMInfoForProject(projectID); err == nil && info != nil {
		payload["llm_info"] = info
	}
	if project.CompletedAt != nil {
		payload["completed_at"] = project.CompletedAt.UTC().Format(time.RFC3339Nano)
	}

	c.JSON(http.StatusOK, gin.H{
		"data":  payload,
		"error": nil,
	})
}

// Cancel stops an in-flight pipeline run for the authenticated user's project.
func (h *FileHandler) Cancel(c *gin.Context) {
	projectID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"data": nil, "error": "invalid project id"})
		return
	}
	if err := h.verifyOwnership(c, projectID); err != nil {
		return
	}
	project, err := h.DB.GetProjectByID(projectID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"data": nil, "error": "project not found"})
		return
	}
	if project.Status == db.StatusComplete || project.Status == db.StatusError {
		c.JSON(http.StatusConflict, gin.H{"data": nil, "error": "project is not running"})
		return
	}
	if err := h.Pipeline.CancelProject(c.Request.Context(), projectID, "cancelled by user"); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"data": nil, "error": "could not cancel"})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"data":  gin.H{"cancelled": projectID},
		"error": nil,
	})
}

// Reprocess re-runs the pipeline on an existing upload (same file on disk).
func (h *FileHandler) Reprocess(c *gin.Context) {
	projectID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"data": nil, "error": "invalid project id"})
		return
	}
	if err := h.verifyOwnership(c, projectID); err != nil {
		return
	}

	project, err := h.DB.GetProjectByID(projectID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"data": nil, "error": "project not found"})
		return
	}
	if _, err := os.Stat(project.Filename); err != nil {
		c.JSON(http.StatusNotFound, gin.H{"data": nil, "error": "uploaded file missing on disk"})
		return
	}
	if projectInProgress(project.Status) {
		c.JSON(http.StatusConflict, gin.H{"data": nil, "error": "file is still processing; cancel first or wait"})
		return
	}

	_ = os.Remove(project.Filename + ".result.json")
	if err := h.DB.UpdateProjectStatus(projectID, db.StatusUploaded); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"data": nil, "error": "database error"})
		return
	}
	_ = h.DB.AddEvent(projectID, "reprocess", fmt.Sprintf("re-queued %q for processing", project.OriginalName))

	go h.Pipeline.EnqueueProcess(context.Background(), projectID)

	c.JSON(http.StatusOK, gin.H{
		"data":  gin.H{"id": projectID, "status": db.StatusUploaded},
		"error": nil,
	})
}

// Delete removes a project, its events, and files on disk.
func (h *FileHandler) Delete(c *gin.Context) {
	projectID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"data": nil, "error": "invalid project id"})
		return
	}
	if err := h.verifyOwnership(c, projectID); err != nil {
		return
	}

	project, err := h.DB.GetProjectByID(projectID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"data": nil, "error": "project not found"})
		return
	}

	if projectInProgress(project.Status) {
		_ = h.Pipeline.CancelProject(c.Request.Context(), projectID, "deleted by user")
	}

	_ = os.Remove(project.Filename)
	_ = os.Remove(project.Filename + ".result.json")

	if err := h.DB.DeleteProject(projectID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"data": nil, "error": "could not delete project"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"data":  gin.H{"deleted": projectID},
		"error": nil,
	})
}

func projectInProgress(status string) bool {
	switch status {
	case db.StatusUploaded, db.StatusConverting, db.StatusLLMPending, db.StatusLLMDone, db.StatusPostProcessing:
		return true
	default:
		return false
	}
}

// Result returns the parsed JSON result for a completed project.
func (h *FileHandler) Result(c *gin.Context) {
	projectID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"data": nil, "error": "invalid project id"})
		return
	}

	if err := h.verifyOwnership(c, projectID); err != nil {
		return
	}

	project, err := h.DB.GetProjectByID(projectID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"data": nil, "error": "project not found"})
		return
	}

	resultPath := project.Filename + ".result.json"
	data, err := os.ReadFile(resultPath)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"data": nil, "error": "result not yet available"})
		return
	}

	var result interface{}
	if err := json.Unmarshal(data, &result); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"data": nil, "error": "corrupt result file"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"data": result, "error": nil})
}

// verifyOwnership checks that the authenticated user owns the project.
// If not, it writes the appropriate error response and returns a non-nil error.
func (h *FileHandler) verifyOwnership(c *gin.Context, projectID int64) error {
	userIDStr, _ := c.Get(auth.ContextUserID)
	role, _ := c.Get(auth.ContextRole)

	// Admins can access any project.
	if role == auth.RoleAdmin {
		return nil
	}

	userID, err := strconv.ParseInt(userIDStr.(string), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"data": nil, "error": "invalid user id"})
		return fmt.Errorf("invalid user id")
	}

	project, err := h.DB.GetProjectByID(projectID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"data": nil, "error": "project not found"})
		return fmt.Errorf("project not found")
	}

	if project.UserID != userID {
		c.JSON(http.StatusForbidden, gin.H{"data": nil, "error": "access denied"})
		return fmt.Errorf("access denied")
	}
	return nil
}

// detectMIME provides a fallback MIME type based on file extension.
func detectMIME(ext string) string {
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
