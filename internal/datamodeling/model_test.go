package datamodeling

import (
	"testing"

	"megane/internal/llm"
)

func TestResolveGeminiModel_default(t *testing.T) {
	t.Setenv("DATAMODELING_LLM_MODEL", "")
	got := ResolveGeminiModel("")
	if got != DefaultGeminiModel {
		t.Fatalf("got %q want %q", got, DefaultGeminiModel)
	}
	if err := llm.ValidateGeminiModelID(got); err != nil {
		t.Fatal(err)
	}
}

func TestResolveGeminiModel_override(t *testing.T) {
	got := ResolveGeminiModel("gemini-3-flash-preview")
	if got != "gemini-3-flash-preview" {
		t.Fatalf("got %q", got)
	}
}

func TestResolveGeminiModel_invalidFallsBack(t *testing.T) {
	got := ResolveGeminiModel("gemini-1.5-pro")
	if got != DefaultGeminiModel {
		t.Fatalf("got %q want default", got)
	}
}
