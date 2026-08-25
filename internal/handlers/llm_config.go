package handlers

import "megane/internal/llm"

// LLMRouteConfig is routing metadata for upload UI (model picker) and Gemini catalog calls.
type LLMRouteConfig struct {
	Provider                  string
	GeminiAPIKey              string
	EffectiveGeminiModel      string
	DatamodelingGeminiModel   string
	Client                    llm.Client
}
