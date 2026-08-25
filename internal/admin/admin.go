package admin

import (
	"net/http"
	"os"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"megane/internal/auth"
	"megane/internal/db"
)

// Handler handles admin-only endpoints.
type Handler struct {
	DB *db.DB
}

// ListUsers returns all users.
func (h *Handler) ListUsers(c *gin.Context) {
	users, err := h.DB.GetAllUsers()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"data": nil, "error": "database error"})
		return
	}
	type userView struct {
		ID        int64  `json:"id"`
		Email     string `json:"email"`
		Role      string `json:"role"`
		CreatedAt string `json:"created_at"`
	}
	views := make([]userView, 0, len(users))
	for _, u := range users {
		views = append(views, userView{
			ID:        u.ID,
			Email:     u.Email,
			Role:      u.Role,
			CreatedAt: u.CreatedAt.Format("2006-01-02 15:04:05"),
		})
	}
	c.JSON(http.StatusOK, gin.H{"data": views, "error": nil})
}

type createUserRequest struct {
	Email    string `json:"email" binding:"required"`
	Password string `json:"password" binding:"required"`
	Role     string `json:"role"`
}

// CreateUser creates a new user account.
func (h *Handler) CreateUser(c *gin.Context) {
	var req createUserRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"data": nil, "error": err.Error()})
		return
	}
	if req.Role == "" {
		req.Role = auth.RoleUser
	}
	if req.Role != auth.RoleAdmin && req.Role != auth.RoleUser {
		c.JSON(http.StatusBadRequest, gin.H{"data": nil, "error": "role must be 'admin' or 'user'"})
		return
	}
	hash, err := auth.HashPassword(req.Password)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"data": nil, "error": "could not hash password"})
		return
	}
	id, err := h.DB.CreateUser(req.Email, hash, req.Role)
	if err != nil {
		c.JSON(http.StatusConflict, gin.H{"data": nil, "error": "user already exists or database error"})
		return
	}
	c.JSON(http.StatusCreated, gin.H{
		"data":  gin.H{"id": id, "email": req.Email, "role": req.Role},
		"error": nil,
	})
}

// DeleteUser removes a user account.
func (h *Handler) DeleteUser(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"data": nil, "error": "invalid user id"})
		return
	}
	if err := h.DB.DeleteUser(id); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"data": nil, "error": "database error"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": gin.H{"deleted": id}, "error": nil})
}

// ListProjects returns all projects across all users.
func (h *Handler) ListProjects(c *gin.Context) {
	projects, err := h.DB.GetAllProjects()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"data": nil, "error": "database error"})
		return
	}
	if projects == nil {
		projects = []*db.Project{}
	}
	c.JSON(http.StatusOK, gin.H{"data": projects, "error": nil})
}

// JumpStart scaffolds a new sprout-application from the running template.
func (h *Handler) JumpStart(c *gin.Context) {
	var cfg JumpStartConfig
	if err := c.ShouldBindJSON(&cfg); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"data": nil, "error": err.Error()})
		return
	}

	// Default slug from name if omitted.
	if strings.TrimSpace(cfg.AppSlug) == "" {
		cfg.AppSlug = slugify(cfg.AppName)
	}
	if cfg.SessionTTL == 0 {
		cfg.SessionTTL = 86400
	}
	if cfg.Port == 0 {
		cfg.Port = 8080
	}
	if cfg.Language == "" {
		cfg.Language = "en"
	}
	if strings.TrimSpace(cfg.LLMProvider) == "" {
		cfg.LLMProvider = "gemini"
	}
	if strings.TrimSpace(cfg.PrimaryColor) == "" {
		cfg.PrimaryColor = "#4f8ef7"
	}
	normalizeJumpStartConfig(&cfg)

	// Source = directory where the server binary is running.
	sourcePath, err := os.Getwd()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"data": nil, "error": "cannot determine template source path"})
		return
	}

	if err := CreateSprout(sourcePath, cfg); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"data": nil, "error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"data": gin.H{
			"path":    cfg.TargetPath,
			"slug":    cfg.AppSlug,
			"message": "Sprout application created. Run: cd " + cfg.TargetPath + " && go mod tidy && make run",
		},
		"error": nil,
	})
}

// slugify converts a display name to a URL-safe slug.
func slugify(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	var b strings.Builder
	for _, r := range s {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' {
			b.WriteRune(r)
		} else if b.Len() > 0 {
			b.WriteByte('-')
		}
	}
	slug := strings.Trim(b.String(), "-")
	if slug == "" {
		slug = "my-app"
	}
	if len(slug) > 63 {
		slug = slug[:63]
	}
	return slug
}

// Stats returns aggregate statistics about projects and recent events.
func (h *Handler) Stats(c *gin.Context) {
	counts, err := h.DB.ProjectCountByStatus()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"data": nil, "error": "database error"})
		return
	}
	events, err := h.DB.GetRecentEvents(20)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"data": nil, "error": "database error"})
		return
	}
	if events == nil {
		events = []*db.PipelineEvent{}
	}
	c.JSON(http.StatusOK, gin.H{
		"data":  gin.H{"by_status": counts, "recent_events": events},
		"error": nil,
	})
}

// CrawlerStats returns aggregate crawler counters and recent CLI runs persisted in the DB.
func (h *Handler) CrawlerStats(c *gin.Context) {
	summary, err := h.DB.CrawlerRunSummary()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"data": nil, "error": "database error"})
		return
	}
	runs, err := h.DB.ListRecentCrawlerRuns(50)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"data": nil, "error": "database error"})
		return
	}
	if runs == nil {
		runs = []db.CrawlerRunStat{}
	}
	c.JSON(http.StatusOK, gin.H{
		"data": gin.H{
			"summary": summary,
			"runs":    runs,
		},
		"error": nil,
	})
}
