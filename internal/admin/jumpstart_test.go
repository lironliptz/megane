package admin

import (
	"net/mail"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestValidateConfig(t *testing.T) {
	valid := JumpStartConfig{
		AppName:       "Contract Analyzer",
		AppSlug:       "contract-analyzer",
		TargetPath:    "/tmp/my-sprout",
		Description:   "Analyzes contracts.",
		Language:      "en",
		PrimaryColor:  "#4f8ef7",
		SessionTTL:    86400,
		AdminEmail:    "admin@example.com",
		AdminPassword: "secret",
		LLMProvider:   "gemini",
		GeminiAPIKey:  "test-key",
		Port:          8080,
	}

	if err := validateConfig(valid); err != nil {
		t.Fatalf("valid config: %v", err)
	}

	tests := []struct {
		name    string
		mutate  func(*JumpStartConfig)
		wantErr string
	}{
		{"missing app_name", func(c *JumpStartConfig) { c.AppName = "" }, "app_name"},
		{"bad slug", func(c *JumpStartConfig) { c.AppSlug = "1bad" }, "app_slug"},
		{"missing target_path", func(c *JumpStartConfig) { c.TargetPath = "" }, "target_path"},
		{"missing description", func(c *JumpStartConfig) { c.Description = "" }, "description"},
		{"bad port", func(c *JumpStartConfig) { c.Port = 0 }, "port"},
		{"bad language", func(c *JumpStartConfig) { c.Language = "fr" }, "language"},
		{"bad color", func(c *JumpStartConfig) { c.PrimaryColor = "red" }, "primary_color"},
		{"missing admin email", func(c *JumpStartConfig) { c.AdminEmail = "" }, "admin_email"},
		{"bad admin email", func(c *JumpStartConfig) { c.AdminEmail = "not-an-email" }, "admin_email"},
		{"missing admin password", func(c *JumpStartConfig) { c.AdminPassword = "" }, "admin_password"},
		{"short admin password", func(c *JumpStartConfig) { c.AdminPassword = "ab" }, "admin_password"},
		{"missing gemini key", func(c *JumpStartConfig) { c.GeminiAPIKey = "" }, "gemini_api_key"},
		{"missing openai key", func(c *JumpStartConfig) {
			c.LLMProvider = "openai"
			c.OpenAIAPIKey = ""
		}, "openai_api_key"},
		{"missing local url", func(c *JumpStartConfig) {
			c.LLMProvider = "local"
			c.LocalLLMURL = ""
		}, "local_llm_url"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := valid
			tt.mutate(&cfg)
			err := validateConfig(cfg)
			if err == nil {
				t.Fatal("expected error")
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("error %q, want substring %q", err.Error(), tt.wantErr)
			}
		})
	}
}

func TestValidateAdminEmail(t *testing.T) {
	if err := validateAdminCredentials("admin@example.com", "pass"); err != nil {
		t.Fatal(err)
	}
	if _, err := mail.ParseAddress("admin@example.com"); err != nil {
		t.Fatal(err)
	}
}

func TestRenderEnvFile(t *testing.T) {
	cfg := JumpStartConfig{
		AppName:       "Sales Agreements",
		AppSlug:       "sales-agreements",
		TargetPath:    "/tmp/x",
		Description:   "Analyzes sales agreements.",
		Language:      "en",
		SessionTTL:    86400,
		AdminEmail:    "admin@example.com",
		AdminPassword: "111admin",
		LLMProvider:   "gemini",
		GeminiAPIKey:  "test-gemini-key",
		Port:          8887,
	}
	out := renderEnvFile(cfg)
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if !strings.Contains(line, "=") {
			t.Fatalf("invalid .env line (missing =): %q", line)
		}
	}
	for _, want := range []string{
		"LLM_PROVIDER=gemini",
		"GEMINI_API_KEY=test-gemini-key",
		"OPENAI_API_KEY=",
		"LLM_LOCAL_URL=",
		"LLM_MODEL=",
		"ADMIN_EMAIL=admin@example.com",
		"ADMIN_PASSWORD=111admin",
		"PORT=8887",
		"DB_PATH=./.db/sales-agreements.db",
	} {
		if !strings.Contains(out, want+"\n") && !strings.HasSuffix(strings.TrimSpace(out), want) {
			if !strings.Contains(out, want) {
				t.Fatalf("missing %q in:\n%s", want, out)
			}
		}
	}
}

func helperBaseConfig(targetPath string) JumpStartConfig {
	return JumpStartConfig{
		AppName:       "Test App",
		AppSlug:       "test-app",
		TargetPath:    targetPath,
		Description:   "Test description.",
		Language:      "en",
		PrimaryColor:  "#4f8ef7",
		SessionTTL:    86400,
		AdminEmail:    "admin@example.com",
		AdminPassword: "secret123",
		LLMProvider:   "gemini",
		GeminiAPIKey:  "fake-key",
		Port:          8080,
	}
}

func TestCreateSprout_HebrewLanguage(t *testing.T) {
	srcRoot, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatalf("resolve src root: %v", err)
	}
	heLogin := filepath.Join(srcRoot, "static", "i18n", "he", "login.html")
	if _, err := os.Stat(heLogin); os.IsNotExist(err) {
		t.Skip("static/i18n/he/login.html not present; skipping")
	}

	dst := t.TempDir()
	cfg := helperBaseConfig(dst)
	cfg.Language = "he"

	if err := CreateSprout(srcRoot, cfg); err != nil {
		t.Fatalf("CreateSprout Hebrew: %v", err)
	}

	loginContent, err := os.ReadFile(filepath.Join(dst, "static", "login.html"))
	if err != nil {
		t.Fatalf("read login.html: %v", err)
	}
	if !strings.Contains(string(loginContent), `lang="he"`) {
		t.Error("login.html: missing lang=he")
	}
	if !strings.Contains(string(loginContent), `dir="rtl"`) {
		t.Error("login.html: missing dir=rtl")
	}
	if !strings.Contains(string(loginContent), "כניסה") {
		t.Error("login.html: missing Hebrew sign-in text")
	}

	indexContent, err := os.ReadFile(filepath.Join(dst, "static", "index.html"))
	if err != nil {
		t.Fatalf("read index.html: %v", err)
	}
	if !strings.Contains(string(indexContent), "יציאה") {
		t.Error("index.html: missing Hebrew sign-out")
	}

	analyzeContent, err := os.ReadFile(filepath.Join(dst, "prompts", "analyze.txt"))
	if err != nil {
		t.Fatalf("read analyze.txt: %v", err)
	}
	if !strings.Contains(string(analyzeContent), "עברית") {
		t.Error("analyze.txt: missing Hebrew content")
	}
}

func TestCreateSprout_EnglishLanguage(t *testing.T) {
	srcRoot, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatalf("resolve src root: %v", err)
	}

	dst := t.TempDir()
	cfg := helperBaseConfig(dst)
	cfg.Language = "en"

	if err := CreateSprout(srcRoot, cfg); err != nil {
		t.Fatalf("CreateSprout English: %v", err)
	}

	loginContent, err := os.ReadFile(filepath.Join(dst, "static", "login.html"))
	if err != nil {
		t.Fatalf("read login.html: %v", err)
	}
	if strings.Contains(string(loginContent), `lang="he"`) {
		t.Error("English sprout: login.html unexpectedly has lang=he")
	}
	if !strings.Contains(string(loginContent), "Sign in") {
		t.Error("English sprout: login.html missing English text")
	}
	if strings.Contains(string(loginContent), "כניסה") {
		t.Error("English sprout: login.html contains Hebrew text")
	}
}
