package datamodeling

import (
	"log/slog"
	"os"
	"strings"

	"megane/internal/llm"
)

// DefaultGeminiModel is used for admin data-modeling (schema inference / type guessing).
const DefaultGeminiModel = "gemini-3.1-pro-preview"

// ResolveGeminiModel returns the Gemini model id for datamodeling LLM calls.
// Reads DATAMODELING_LLM_MODEL from the environment when envOverride is empty.
func ResolveGeminiModel(envOverride string) string {
	raw := strings.TrimSpace(envOverride)
	if raw == "" {
		raw = strings.TrimSpace(os.Getenv("DATAMODELING_LLM_MODEL"))
	}
	if raw == "" {
		raw = DefaultGeminiModel
	}
	model := llm.EffectiveGeminiModelFromEnv(raw)
	if err := llm.ValidateGeminiModelID(model); err != nil {
		slog.Warn("datamodeling: invalid Gemini model, using default",
			"configured", raw,
			"default", DefaultGeminiModel,
			"err", err,
		)
		return DefaultGeminiModel
	}
	return model
}
