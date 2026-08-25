package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/joho/godotenv"

	"megane/internal/auth"
	"megane/internal/companyview"
	"megane/internal/datamodeling"
	"megane/internal/db"
	"megane/internal/filedb"
	"megane/internal/handlers"
	"megane/internal/llm"
	"megane/internal/marketdata"
	"megane/internal/pipeline"
	"megane/internal/version"
)

func main() {
	if err := godotenv.Load(); err != nil {
		slog.Info("no .env file, reading from environment")
	}

	port := getEnv("PORT", "8080")
	dbPath := db.ResolvePath(getEnv("DB_PATH", ""))
	projectsDir := getEnv("PROJECTS_DIR", "./projects")

	if err := os.MkdirAll(projectsDir, 0755); err != nil {
		slog.Error("create projects dir", "err", err)
		os.Exit(1)
	}

	database, err := db.Open(dbPath)
	if err != nil {
		slog.Error("open database", "err", err)
		os.Exit(1)
	}
	slog.Info("database ready", "path", dbPath)

	seedAdmin(database)

	provider := getEnv("LLM_PROVIDER", "gemini")
	var apiKey string
	switch provider {
	case "gemini":
		apiKey = os.Getenv("GEMINI_API_KEY")
	case "openai":
		apiKey = os.Getenv("OPENAI_API_KEY")
	}
	model := os.Getenv("LLM_MODEL")
	localURL := os.Getenv("LLM_LOCAL_URL")

	llmClient, err := llm.NewClient(provider, apiKey, model, localURL)
	if err != nil {
		slog.Error("create LLM client", "err", err)
		os.Exit(1)
	}
	slog.Info("LLM backend", "provider", llmClient.Name())

	geminiKey := ""
	effectiveGemini := ""
	if strings.TrimSpace(strings.ToLower(provider)) == "gemini" {
		geminiKey = apiKey
		effectiveGemini = llm.EffectiveGeminiModelFromEnv(model)
	}

	dmGeminiModel := ""
	if strings.TrimSpace(strings.ToLower(provider)) == "gemini" {
		dmGeminiModel = datamodeling.ResolveGeminiModel(os.Getenv("DATAMODELING_LLM_MODEL"))
		slog.Info("datamodeling LLM model", "model", dmGeminiModel)
	}

	llmRouteCfg := handlers.LLMRouteConfig{
		Provider:                provider,
		GeminiAPIKey:            geminiKey,
		EffectiveGeminiModel:    effectiveGemini,
		DatamodelingGeminiModel: dmGeminiModel,
		Client:                  llmClient,
	}

	prompts, err := pipeline.LoadPrompts("./prompts")
	if err != nil {
		slog.Warn("could not load prompts", "err", err)
		prompts = map[string]string{}
	}

	// Scan prompts directory for <slug>.txt to add as analyze_<slug>
	entries, err := os.ReadDir("./prompts")
	if err == nil {
		for _, e := range entries {
			if !e.IsDir() && strings.HasSuffix(e.Name(), ".txt") {
				name := strings.TrimSuffix(e.Name(), ".txt")
				if name != "system" && name != "analyze" && !strings.HasPrefix(name, "datamodeling") {
					content, err := os.ReadFile("./prompts/" + e.Name())
					if err == nil {
						prompts["analyze_"+name] = string(content)
					}
				}
			}
		}
	}

	slog.Info("prompts loaded", "count", len(prompts))

	pipe := &pipeline.Pipeline{
		DB:      database,
		LLM:     llmClient,
		Prompts: prompts,
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// Company read model over the on-disk fileDB/ corpus. Construction performs
	// no I/O, so a missing fileDB directory does not block startup: it surfaces
	// as empty search results and 404s on company detail.
	fileDBDir := getEnv("FILEDB_DIR", "./fileDB")
	companyStore := filedb.NewFileDBStore(fileDBDir, filedb.Options{
		TTL: filedb.ParseTTL(os.Getenv("FILEDB_CACHE_TTL")),
	})
	slog.Info("company file store configured", "dir", fileDBDir)

	// Market data for the company timeline. Construction performs no I/O; the
	// first fetch happens when a user opens the Timeline tab.
	priceProvider := marketdata.NewFromEnv(os.Getenv("MARKET_DATA_PROVIDER"))
	timelineSvc := companyview.NewService(companyStore, database, priceProvider, companyview.Config{
		FetchOnOpen: getEnv("STOCK_FETCH_ON_COMPANY_OPEN", "false") == "true",
	})
	slog.Info("market data provider configured", "provider", priceProvider.Name())

	router := handlers.NewRouter(ctx, database, pipe, projectsDir, llmRouteCfg, prompts,
		handlers.CompanyDeps{Store: companyStore, Timeline: timelineSvc})
	addr := ":" + port
	srv := &http.Server{Addr: addr, Handler: router}

	go func() {
		slog.Info("server starting", "addr", addr, "jump_starter_version", version.Version())
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("server error", "err", err)
			os.Exit(1)
		}
	}()

	<-ctx.Done()
	slog.Info("shutting down...")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		slog.Error("shutdown error", "err", err)
	}
}

func getEnv(key, defaultVal string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return defaultVal
}

func seedAdmin(database *db.DB) {
	count, err := database.UserCount()
	if err != nil {
		slog.Warn("could not count users", "err", err)
		return
	}
	if count > 0 {
		return
	}
	email := os.Getenv("ADMIN_EMAIL")
	password := os.Getenv("ADMIN_PASSWORD")
	if email == "" || password == "" {
		slog.Warn("ADMIN_EMAIL/ADMIN_PASSWORD not set; skipping admin seed")
		return
	}
	hash, err := auth.HashPassword(password)
	if err != nil {
		slog.Warn("hash admin password", "err", err)
		return
	}
	id, err := database.CreateUser(email, hash, auth.RoleAdmin)
	if err != nil {
		slog.Warn("seed admin user", "err", err)
		return
	}
	slog.Info("seeded admin user", "email", email, "id", strconv.FormatInt(id, 10))
}
