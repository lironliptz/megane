package llm

import "testing"

func TestNormalizeEnvModel(t *testing.T) {
	fallback := "gemini-3-flash-preview"
	tests := []struct {
		in, want string
	}{
		{"", fallback},
		{"   ", fallback},
		{"# Optional: override default model name", fallback},
		{"  gemini-3-flash-preview  ", "gemini-3-flash-preview"},
		{"gemini-3.1-pro-preview # prod", "gemini-3.1-pro-preview"},
	}
	for _, tt := range tests {
		got := NormalizeEnvModel(tt.in, fallback)
		if got != tt.want {
			t.Errorf("NormalizeEnvModel(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestEffectiveGeminiModelFromEnv_flashLitePreviewToGA(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		{"gemini-3.1-flash-lite-preview", "gemini-3.1-flash-lite"},
		{"  models/gemini-3.1-flash-lite-preview  ", "gemini-3.1-flash-lite"},
		{"gemini-3.1-flash-lite", "gemini-3.1-flash-lite"},
		{"", "gemini-3-flash-preview"},
	}
	for _, tt := range tests {
		got := EffectiveGeminiModelFromEnv(tt.in)
		if got != tt.want {
			t.Errorf("EffectiveGeminiModelFromEnv(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}
