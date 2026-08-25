package handlers

import (
	"context"
	"net/http"
	"path/filepath"
	"time"

	"github.com/gin-gonic/gin"

	"megane/internal/admin"
	"megane/internal/auth"
	"megane/internal/datamodeling"
	"megane/internal/db"
	"megane/internal/pipeline"
	"megane/internal/version"
)

const maxUploadBytes = 50 << 20 // 50 MB

const defaultRequestTimeout = 60 * time.Second

// NewRouter creates and returns a fully configured Gin engine.
// ctx is the application lifetime context; it is used to stop background goroutines on shutdown.
func NewRouter(ctx context.Context, database *db.DB, pipe *pipeline.Pipeline, projectsDir string, llmCfg LLMRouteConfig, prompts map[string]string) *gin.Engine {
	r := gin.New()
	r.Use(gin.Recovery())
	r.MaxMultipartMemory = maxUploadBytes

	// Health check (unauthenticated)
	r.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"status":                "ok",
			"jump_starter":          version.Name,
			"jump_starter_version": version.Version(),
		})
	})

	// Static pages
	r.StaticFile("/", "./static/index.html")
	r.StaticFile("/login", "./static/login.html")
	r.StaticFile("/admin", "./static/admin.html")
	r.Static("/static", "./static")

	// Auth routes
	loginLimiter := newRateLimiter(ctx, 10, time.Minute) // 10 attempts/IP/minute
	authHandler := &AuthHandler{DB: database}
	authGroup := r.Group("/auth", requestTimeout(defaultRequestTimeout))
	{
		authGroup.POST("/login", loginLimiter.Middleware(), authHandler.Login)
		authGroup.GET("/me", auth.AuthRequired(), authHandler.Me)
	}

	// File routes (authenticated users)
	fileHandler := &FileHandler{DB: database, Pipeline: pipe, ProjectsDir: projectsDir, LLM: llmCfg}
	apiFiles := r.Group("/api/files", auth.AuthRequired(), requestTimeout(defaultRequestTimeout))
	{
		apiFiles.GET("/llm-models", fileHandler.ListLLMModels)
		apiFiles.POST("/upload", fileHandler.Upload)
		apiFiles.GET("", fileHandler.List)
		apiFiles.GET("/:id/status", fileHandler.Status)
		apiFiles.POST("/:id/cancel", fileHandler.Cancel)
		apiFiles.POST("/:id/reprocess", fileHandler.Reprocess)
		apiFiles.DELETE("/:id", fileHandler.Delete)
		apiFiles.GET("/:id/result", fileHandler.Result)
	}

	// Admin routes
	adminHandler := &admin.Handler{DB: database}
	apiAdmin := r.Group("/api/admin", auth.AdminRequired(), requestTimeout(defaultRequestTimeout))
	{
		apiAdmin.GET("/users", adminHandler.ListUsers)
		apiAdmin.POST("/users", adminHandler.CreateUser)
		apiAdmin.DELETE("/users/:id", adminHandler.DeleteUser)
		apiAdmin.GET("/projects", adminHandler.ListProjects)
		apiAdmin.GET("/stats", adminHandler.Stats)
		apiAdmin.GET("/crawler-stats", adminHandler.CrawlerStats)
		apiAdmin.POST("/jumpstart", adminHandler.JumpStart)
	}

	// Data modeling — analyze/reprocess call Gemini Pro + vision; use LLM request timeout (default 10m).
	dmAnalyzer := datamodeling.NewAnalyzer(datamodeling.AnalyzerConfig{
		DB:          database,
		LLMClient:   llmCfg.Client,
		Prompts:     prompts,
		ProjectsDir: projectsDir,
		GeminiModel: llmCfg.DatamodelingGeminiModel,
	})
	dmHandler := &admin.DMHandler{
		Analyzer:        dmAnalyzer,
		DB:              database,
		ProjectsDir:     projectsDir,
		AnalysisTimeout: admin.ResolveDatamodelingTimeout(),
	}
	buildH := &admin.BuildHandler{
		DB:        database,
		SrcRoot:   ".", // Repo root
		ExportDir: filepath.Join(".", "generated"),
	}

	apiAdminDMFast := apiAdmin.Group("/datamodeling")
	{
		apiAdminDMFast.POST("/types", dmHandler.CreateType)
		apiAdminDMFast.GET("/types", dmHandler.ListTypes)
		apiAdminDMFast.GET("/types/:id/schema", dmHandler.GetSchema)
		apiAdminDMFast.GET("/types/:id/samples", dmHandler.ListTypeSamples)
		apiAdminDMFast.POST("/types/:id/check-samples", dmHandler.CheckTypeSamples)
		apiAdminDMFast.PATCH("/types/:id/config", dmHandler.UpdateTypeConfig)
		apiAdminDMFast.POST("/types/:id/exclude-field", dmHandler.ExcludeSchemaField)
		apiAdminDMFast.POST("/types/:id/finalize", dmHandler.FinalizeSchema)
		apiAdminDMFast.DELETE("/types/:id", dmHandler.DeleteType)
		// Analyze returns 202 immediately; LLM runs in a background goroutine.
		apiAdminDMFast.POST("/analyze", dmHandler.Analyze)
		apiAdminDMFast.GET("/jobs/:id", dmHandler.GetAnalysisJob)
		apiAdminDMFast.POST("/types/:id/manual-answer", dmHandler.SubmitManualAnswer)

		// Build endpoints
		apiAdminDMFast.GET("/build", buildH.ListBuild)
		apiAdminDMFast.GET("/types/:id/build", buildH.GetBuild)
		apiAdminDMFast.PATCH("/types/:id/build", buildH.PatchBuild)
		apiAdminDMFast.POST("/types/:id/build/generate-prompt", buildH.GeneratePrompt)
		apiAdminDMFast.POST("/types/:id/build/apply", buildH.Apply)
		apiAdminDMFast.POST("/build/apply-all", buildH.ApplyAll)
	}

	r.NoRoute(func(c *gin.Context) {
		c.JSON(http.StatusNotFound, gin.H{"data": nil, "error": "not found"})
	})

	return r
}

func requestTimeout(d time.Duration) gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx, cancel := context.WithTimeout(c.Request.Context(), d)
		defer cancel()
		c.Request = c.Request.WithContext(ctx)
		c.Next()
	}
}
