package admin

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"io/fs"
	"net/mail"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"megane/internal/db"
)

func randomSecret() string {
	b := make([]byte, 32)
	_, _ = rand.Read(b)
	return base64.URLEncoding.EncodeToString(b)
}

// JumpStartConfig holds every customization the user supplies via the UI.
type JumpStartConfig struct {
	AppName      string `json:"app_name"`      // display name, e.g. "Contract Analyzer"
	AppSlug      string `json:"app_slug"`      // Go module + folder slug, e.g. "contract-analyzer"
	TargetPath   string `json:"target_path"`   // absolute path for the new project
	Description  string `json:"description"`   // used in system prompt
	Language     string `json:"language"`      // "en" | "he"
	PrimaryColor string `json:"primary_color"` // CSS hex color, e.g. "#4f8ef7"
	SessionTTL   int    `json:"session_ttl"`   // seconds, default 86400
	AdminEmail   string `json:"admin_email"`
	AdminPassword string `json:"admin_password"`
	LLMProvider  string `json:"llm_provider"`  // "gemini" | "openai" | "local"
	GeminiAPIKey string `json:"gemini_api_key"`
	OpenAIAPIKey string `json:"openai_api_key"`
	LocalLLMURL  string `json:"local_llm_url"`
	Port         int    `json:"port"`          // default 8080
}

var slugRe = regexp.MustCompile(`^[a-z][a-z0-9-]{1,62}$`)
var hexColorRe = regexp.MustCompile(`^#[0-9a-fA-F]{3}([0-9a-fA-F]{3})?$`)

// CreateSprout copies the template at sourcePath into cfg.TargetPath, applying
// all transformations described in cfg.
func CreateSprout(sourcePath string, cfg JumpStartConfig) error {
	normalizeJumpStartConfig(&cfg)
	if err := validateConfig(cfg); err != nil {
		return err
	}

	abs, err := filepath.Abs(cfg.TargetPath)
	if err != nil {
		return fmt.Errorf("resolve target path: %w", err)
	}
	cfg.TargetPath = abs

	if _, err := os.Stat(cfg.TargetPath); err == nil {
		entries, _ := os.ReadDir(cfg.TargetPath)
		if len(entries) > 0 {
			return fmt.Errorf("target directory %q already exists and is not empty", cfg.TargetPath)
		}
	}

	if err := os.MkdirAll(cfg.TargetPath, 0755); err != nil {
		return fmt.Errorf("create target dir: %w", err)
	}

	src, err := filepath.Abs(sourcePath)
	if err != nil {
		return fmt.Errorf("resolve source path: %w", err)
	}

	if err := walkAndCopy(src, cfg.TargetPath, cfg); err != nil {
		return fmt.Errorf("copy template: %w", err)
	}

	if err := writeEnv(cfg); err != nil {
		return fmt.Errorf("write .env: %w", err)
	}

	if err := writeSystemPrompt(cfg); err != nil {
		return fmt.Errorf("write system prompt: %w", err)
	}

	if err := copyLocalizedStatic(src, cfg); err != nil {
		return fmt.Errorf("copy localized static: %w", err)
	}

	if err := writeAnalyzePrompt(src, cfg); err != nil {
		return fmt.Errorf("write analyze prompt: %w", err)
	}

	return nil
}

func validateConfig(cfg JumpStartConfig) error {
	if strings.TrimSpace(cfg.AppName) == "" {
		return fmt.Errorf("app_name is required")
	}
	if !slugRe.MatchString(cfg.AppSlug) {
		return fmt.Errorf("app_slug must be lowercase letters, digits, and hyphens (2-63 chars, start with letter)")
	}
	if strings.TrimSpace(cfg.TargetPath) == "" {
		return fmt.Errorf("target_path is required")
	}
	if strings.TrimSpace(cfg.Description) == "" {
		return fmt.Errorf("description is required")
	}
	if cfg.SessionTTL < 60 {
		return fmt.Errorf("session_ttl must be at least 60 seconds")
	}
	if cfg.Port <= 0 || cfg.Port > 65535 {
		return fmt.Errorf("port must be 1-65535")
	}
	if cfg.Language != "en" && cfg.Language != "he" {
		return fmt.Errorf("language must be 'en' or 'he'")
	}
	if err := validatePrimaryColor(cfg.PrimaryColor); err != nil {
		return err
	}
	if err := validateAdminCredentials(cfg.AdminEmail, cfg.AdminPassword); err != nil {
		return err
	}
	if err := validateLLMProvider(cfg); err != nil {
		return err
	}
	return nil
}

func validatePrimaryColor(color string) error {
	color = strings.TrimSpace(color)
	if color == "" {
		return nil
	}
	if !hexColorRe.MatchString(color) {
		return fmt.Errorf("primary_color must be a hex color like #4f8ef7")
	}
	return nil
}

func validateAdminCredentials(email, password string) error {
	email = strings.TrimSpace(email)
	if email == "" {
		return fmt.Errorf("admin_email is required")
	}
	if _, err := mail.ParseAddress(email); err != nil {
		return fmt.Errorf("admin_email must be a valid email address")
	}
	password = strings.TrimSpace(password)
	if password == "" {
		return fmt.Errorf("admin_password is required")
	}
	if len(password) < 4 {
		return fmt.Errorf("admin_password must be at least 4 characters")
	}
	return nil
}

func validateLLMProvider(cfg JumpStartConfig) error {
	provider := strings.TrimSpace(strings.ToLower(cfg.LLMProvider))
	if provider == "" {
		provider = "gemini"
	}
	switch provider {
	case "gemini":
		if strings.TrimSpace(cfg.GeminiAPIKey) == "" {
			return fmt.Errorf("gemini_api_key is required when LLM provider is gemini")
		}
	case "openai":
		if strings.TrimSpace(cfg.OpenAIAPIKey) == "" {
			return fmt.Errorf("openai_api_key is required when LLM provider is openai")
		}
	case "local":
		if strings.TrimSpace(cfg.LocalLLMURL) == "" {
			return fmt.Errorf("local_llm_url is required when LLM provider is local")
		}
	default:
		return fmt.Errorf("llm_provider must be gemini, openai, or local")
	}
	return nil
}

func normalizeJumpStartConfig(cfg *JumpStartConfig) {
	if cfg == nil {
		return
	}
	cfg.AppName = strings.TrimSpace(cfg.AppName)
	cfg.AppSlug = strings.TrimSpace(cfg.AppSlug)
	cfg.TargetPath = strings.TrimSpace(cfg.TargetPath)
	cfg.Description = strings.TrimSpace(cfg.Description)
	cfg.Language = strings.TrimSpace(cfg.Language)
	cfg.PrimaryColor = strings.TrimSpace(cfg.PrimaryColor)
	cfg.AdminEmail = strings.TrimSpace(cfg.AdminEmail)
	cfg.AdminPassword = strings.TrimSpace(cfg.AdminPassword)
	cfg.LLMProvider = strings.TrimSpace(strings.ToLower(cfg.LLMProvider))
	cfg.GeminiAPIKey = strings.TrimSpace(cfg.GeminiAPIKey)
	cfg.OpenAIAPIKey = strings.TrimSpace(cfg.OpenAIAPIKey)
	cfg.LocalLLMURL = strings.TrimSpace(cfg.LocalLLMURL)
}

// skipEntry returns true for top-level entries that should not be copied.
func skipEntry(name string) bool {
	switch name {
		case ".git", "projects", "jump-starter", ".env",
		"downloads", "maya-financial-reports", "prompt0.txt":
		return true
	}
	// SQLite sidecar files — do not copy local DB into a new sprout.
	// The ".db" directory name must not match (we ship `.db/.gitkeep` in the template).
	if name != ".db" {
		for _, ext := range []string{".db", ".db-shm", ".db-wal"} {
			if strings.HasSuffix(name, ext) {
				return true
			}
		}
	}
	return false
}

// skipDBRuntimeFile skips local SQLite files under .db/ when cloning (template ships .db/.gitkeep only).
func skipDBRuntimeFile(rel string, d fs.DirEntry) bool {
	if d.IsDir() {
		return false
	}
	slash := filepath.ToSlash(rel)
	if !strings.HasPrefix(slash, ".db/") {
		return false
	}
	base := filepath.Base(rel)
	if base == ".gitkeep" {
		return false
	}
	for _, ext := range []string{".db", ".db-shm", ".db-wal"} {
		if strings.HasSuffix(base, ext) {
			return true
		}
	}
	return false
}

// skipRel returns true for relative paths (within the tree) to exclude.
var skipRels = map[string]bool{
	filepath.Join("cmd", "maya-financial-reports"): true,
	filepath.Join("prompts", "dev"):                true,
	filepath.Join("static", "i18n"):                true, // handled by copyLocalizedStatic
	filepath.Join("prompts", "i18n"):               true, // handled by writeAnalyzePrompt
}

func walkAndCopy(src, dst string, cfg JumpStartConfig) error {
	return filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, path)
		if rel == "." {
			return nil
		}

		// Skip unwanted top-level entries
		top := strings.SplitN(rel, string(filepath.Separator), 2)[0]
		if skipEntry(top) {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}

		// Skip unwanted sub-paths
		if skipRels[rel] {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}

		// Skip SQLite runtime files under .db/ (keep .gitkeep only).
		if skipDBRuntimeFile(rel, d) {
			return nil
		}

		target := filepath.Join(dst, rel)

		if d.IsDir() {
			return os.MkdirAll(target, 0755)
		}

		return copyFile(path, target, cfg)
	})
}

func copyFile(src, dst string, cfg JumpStartConfig) error {
	in, err := os.ReadFile(src)
	if err != nil {
		return err
	}

	content, err := transformContent(src, string(in), cfg)
	if err != nil {
		return err
	}

	return os.WriteFile(dst, []byte(content), 0644)
}

// transformContent applies slug/name/color replacements based on file type.
func transformContent(path, content string, cfg JumpStartConfig) (string, error) {
	ext := strings.ToLower(filepath.Ext(path))
	base := filepath.Base(path)

	// Go source and mod files: replace module import paths.
	if ext == ".go" || base == "go.mod" {
		content = strings.ReplaceAll(content, `module megane`, `module `+cfg.AppSlug)
		content = strings.ReplaceAll(content, `"megane/`, `"`+cfg.AppSlug+`/`)
	}

	// Makefile: rename binary target.
	if base == "Makefile" {
		content = strings.ReplaceAll(content, "-o jump-starter", "-o "+cfg.AppSlug)
	}

	// HTML files: update title, heading, lang attribute, RTL.
	if ext == ".html" {
		content = strings.ReplaceAll(content, "jump-starter", cfg.AppName)
		content = strings.ReplaceAll(content, "Jump-starter", cfg.AppName)
		if cfg.Language == "he" {
			content = strings.ReplaceAll(content, `<html lang="en">`, `<html lang="he" dir="rtl">`)
		}
	}

	// CSS: replace primary accent color.
	if ext == ".css" && cfg.PrimaryColor != "" {
		content = regexp.MustCompile(`--accent:\s*#[0-9a-fA-F]{3,8}`).
			ReplaceAllString(content, "--accent: "+cfg.PrimaryColor)
	}

	// docker-compose: update service name.
	if base == "docker-compose.yml" {
		content = strings.ReplaceAll(content, "jump-starter:", cfg.AppSlug+":")
	}

	// Dockerfile (sprout Docker build — module slug + binary name + SQLite path).
	if base == "Dockerfile" {
		content = strings.ReplaceAll(content, `jump-starter/internal/version`, cfg.AppSlug+`/internal/version`)
		content = strings.ReplaceAll(content, `-o /app/jump-starter`, `-o /app/`+cfg.AppSlug)
		content = strings.ReplaceAll(content, `COPY --from=builder /app/jump-starter ./jump-starter`, `COPY --from=builder /app/`+cfg.AppSlug+` ./`+cfg.AppSlug)
		content = strings.ReplaceAll(content, `ENTRYPOINT ["./jump-starter"]`, `ENTRYPOINT ["./`+cfg.AppSlug+`"]`)
		content = strings.ReplaceAll(content, `DB_PATH=/app/.db/jump-starter.db`, `DB_PATH=/app/.db/`+cfg.AppSlug+`.db`)
	}

	// .env.example: header + SQLite path match generated .env.
	if base == ".env.example" {
		content = strings.ReplaceAll(content, "jump-starter — environment configuration", cfg.AppName+" — environment configuration")
		content = strings.ReplaceAll(content, "DB_PATH=./.db/jump-starter.db",
			fmt.Sprintf("DB_PATH=%s", db.DefaultSQLitePathForSlug(cfg.AppSlug)))
	}

	// README: replace template title.
	if base == "README.md" {
		lines := strings.Split(content, "\n")
		rest := ""
		if len(lines) > 2 {
			rest = strings.Join(lines[2:], "\n")
		}
		content = "# " + cfg.AppName + "\n\n" + cfg.Description + "\n\n" + rest
	}

	// CLAUDE.md / AGENTS.md / sprout.index.json / .cursorrules: replace template references.
	if base == "CLAUDE.md" || base == "AGENTS.md" || base == "sprout.index.json" || base == ".cursorrules" {
		content = strings.ReplaceAll(content, "YOUR_APP_SLUG", cfg.AppSlug)
		content = strings.ReplaceAll(content, "jump-starter", cfg.AppName)
		content = strings.ReplaceAll(content, "Jump-starter", cfg.AppName)
	}

	// VS Code / Cursor settings — see internal/admin/jumpstart_vscode.go.
	if base == "settings.json" && strings.Contains(filepath.ToSlash(path), "/.vscode/") {
		b, err := jumpstartVSCodeSettingsJSON(cfg)
		if err != nil {
			return "", fmt.Errorf("vscode settings for %s: %w", path, err)
		}
		return vscodeSettingsFilePreamble + string(b), nil
	}

	return content, nil
}

// darkenHex darkens a hex color by pct (0–1).
func darkenHex(hex string, pct float64) string {
	r, g, b := parseHex(hex)
	return fmt.Sprintf("#%02x%02x%02x",
		clamp(int(float64(r)*(1-pct))),
		clamp(int(float64(g)*(1-pct))),
		clamp(int(float64(b)*(1-pct))))
}

// lightenHex lightens a hex color by pct (0–1).
func lightenHex(hex string, pct float64) string {
	r, g, b := parseHex(hex)
	return fmt.Sprintf("#%02x%02x%02x",
		clamp(r+int(float64(255-r)*pct)),
		clamp(g+int(float64(255-g)*pct)),
		clamp(b+int(float64(255-b)*pct)))
}

func parseHex(hex string) (r, g, b int) {
	hex = strings.TrimPrefix(strings.ToLower(strings.TrimSpace(hex)), "#")
	if len(hex) == 3 {
		hex = string([]byte{hex[0], hex[0], hex[1], hex[1], hex[2], hex[2]})
	}
	if len(hex) < 6 {
		return 128, 128, 128
	}
	n, _ := strconv.ParseInt(hex[:6], 16, 32)
	return int((n >> 16) & 0xff), int((n >> 8) & 0xff), int(n & 0xff)
}

func clamp(v int) int {
	if v < 0 { return 0 }
	if v > 255 { return 255 }
	return v
}

// envLine is a single KEY=value line in a generated .env file.
// Use a blank Key to emit an empty line. Comments use Key starting with "#".
// Every variable line is emitted as KEY=value (empty value → KEY=).
type envLine struct {
	Key string
	Val string
}

// envSection returns a labelled block of env lines.
// To add a new config group to the generated .env, add one envSection call in writeEnv.
func envSection(heading string, lines ...envLine) []envLine {
	out := []envLine{{Key: "# ---- " + heading + " ----"}}
	out = append(out, lines...)
	out = append(out, envLine{}) // blank line after section
	return out
}

func buildEnvLines(cfg JumpStartConfig) []envLine {
	provider := cfg.LLMProvider
	if provider == "" {
		provider = "gemini"
	}
	ttl := cfg.SessionTTL
	if ttl == 0 {
		ttl = 86400
	}
	port := cfg.Port
	if port == 0 {
		port = 8080
	}

	var lines []envLine
	lines = append(lines,
		envLine{Key: "# " + cfg.AppName + " — environment configuration"},
		envLine{Key: "# Generated by jump-starter Jump-Start wizard."},
		envLine{},
	)
	lines = append(lines, envSection("LLM provider",
		envLine{"LLM_PROVIDER", provider},
		envLine{"GEMINI_API_KEY", cfg.GeminiAPIKey},
		envLine{"OPENAI_API_KEY", cfg.OpenAIAPIKey},
		envLine{"LLM_LOCAL_URL", cfg.LocalLLMURL},
		envLine{"LLM_MODEL", ""},
		envLine{"LLM_MAX_IMAGE_BYTES", ""},
		envLine{"MAX_CONCURRENT_LLM", "2"},
		envLine{"LLM_REQUEST_TIMEOUT", "10m"},
		envLine{"PIPELINE_QUEUE_WORKERS", "1"},
	)...)
	lines = append(lines, envSection("Authentication",
		envLine{"SESSION_SECRET", randomSecret()},
		envLine{"SESSION_TTL", strconv.Itoa(ttl)},
	)...)
	lines = append(lines, envSection("Admin seed user",
		envLine{"ADMIN_EMAIL", cfg.AdminEmail},
		envLine{"ADMIN_PASSWORD", cfg.AdminPassword},
	)...)
	lines = append(lines, envSection("Application",
		envLine{"DB_PATH", db.DefaultSQLitePathForSlug(cfg.AppSlug)},
		envLine{"PROJECTS_DIR", "./projects"},
		envLine{"PORT", strconv.Itoa(port)},
	)...)
	lines = append(lines, envSection("Caddy (docker compose)",
		envLine{"CADDY_DOMAIN", ":80"},
		envLine{"CADDY_ACME_EMAIL", ""},
	)...)
	return lines
}

func renderEnvFile(cfg JumpStartConfig) string {
	var sb strings.Builder
	for _, l := range buildEnvLines(cfg) {
		if l.Key == "" {
			sb.WriteByte('\n')
			continue
		}
		if strings.HasPrefix(l.Key, "#") {
			sb.WriteString(l.Key + "\n")
			continue
		}
		sb.WriteString(l.Key + "=" + l.Val + "\n")
	}
	return sb.String()
}

func writeEnv(cfg JumpStartConfig) error {
	return os.WriteFile(filepath.Join(cfg.TargetPath, ".env"), []byte(renderEnvFile(cfg)), 0600)
}

// copyLocalizedStatic copies the HTML templates and strings.json for cfg.Language
// from static/i18n/{lang}/ (in the template source tree) into the target sprout's static/ dir.
// If no i18n directory exists for the language, this is a no-op.
func copyLocalizedStatic(srcRoot string, cfg JumpStartConfig) error {
	lang := cfg.Language
	if lang == "" {
		lang = "en"
	}
	i18nDir := filepath.Join(srcRoot, "static", "i18n", lang)
	if _, err := os.Stat(i18nDir); os.IsNotExist(err) {
		return nil
	}

	// Copy localized HTML pages, overwriting whatever walkAndCopy already placed.
	for _, name := range []string{"index.html", "login.html", "admin.html"} {
		src := filepath.Join(i18nDir, name)
		if _, err := os.Stat(src); os.IsNotExist(err) {
			continue
		}
		dst := filepath.Join(cfg.TargetPath, "static", name)
		if err := copyFile(src, dst, cfg); err != nil {
			return fmt.Errorf("copy %s: %w", name, err)
		}
	}

	// Copy strings.json to static/i18n/strings.json so JS can load it at runtime.
	stringsSrc := filepath.Join(i18nDir, "strings.json")
	if _, err := os.Stat(stringsSrc); err == nil {
		targetDir := filepath.Join(cfg.TargetPath, "static", "i18n")
		if err := os.MkdirAll(targetDir, 0755); err != nil {
			return fmt.Errorf("create i18n dir: %w", err)
		}
		dst := filepath.Join(targetDir, "strings.json")
		if err := copyFile(stringsSrc, dst, cfg); err != nil {
			return fmt.Errorf("copy strings.json: %w", err)
		}
	}

	return nil
}

// writeAnalyzePrompt copies a localized analyze.txt from prompts/i18n/{lang}/analyze.txt
// to the sprout's prompts/analyze.txt. Falls back to the default (already copied) if absent.
func writeAnalyzePrompt(srcRoot string, cfg JumpStartConfig) error {
	lang := cfg.Language
	if lang == "" {
		lang = "en"
	}
	localizedSrc := filepath.Join(srcRoot, "prompts", "i18n", lang, "analyze.txt")
	if _, err := os.Stat(localizedSrc); os.IsNotExist(err) {
		return nil
	}
	content, err := os.ReadFile(localizedSrc)
	if err != nil {
		return err
	}
	dst := filepath.Join(cfg.TargetPath, "prompts", "analyze.txt")
	return os.WriteFile(dst, content, 0644)
}

func writeSystemPrompt(cfg JumpStartConfig) error {
	var prompt string

	name := cfg.AppName
	desc := cfg.Description
	if desc == "" {
		desc = "Analyze the uploaded document and extract structured information."
	}

	if cfg.Language == "he" {
		prompt = fmt.Sprintf(`אתה עוזר AI של מערכת "%s".
%s

בתגובה שלך החזר אובייקט JSON תקני בלבד — ללא כל טקסט נוסף.
ודא שכל השדות הנדרשים קיימים ומדויקים.
השתמש בעברית לכל שדות טקסט פתוח.
`, name, desc)
	} else {
		prompt = fmt.Sprintf(`You are the AI assistant for "%s".
%s

Return a valid JSON object only — no additional text or markdown fences.
Ensure all required fields are present and accurate.
`, name, desc)
	}

	promptPath := filepath.Join(cfg.TargetPath, "prompts", "system.txt")
	return os.WriteFile(promptPath, []byte(prompt), 0644)
}
