package llm

import (
	"context"

	"github.com/google/generative-ai-go/genai"
)

// RequestLog optional audit metadata passed through to pipeline LLM events.
type RequestLog struct {
	SourceFile  string
	PromptFiles []string
}

// RequestImage is raw image bytes for vision-capable backends (Gemini inline image parts).
type RequestImage struct {
	MIMEType string
	Data     []byte
}

// Request contains everything the LLM needs to produce a completion.
type Request struct {
	SystemPrompt string
	UserPrompt   string
	Log          *RequestLog
	// Images, when non-empty, are sent as separate vision parts (Gemini only).
	Images []RequestImage
	// Model optionally overrides the client's default model id (Gemini: pass a vetted model name).
	Model string
	// Schema, when non-nil, instructs OpenAI (json_schema) or prompts Gemini with an
	// inline JSON-schema description when GeminiResponseSchema is nil.
	Schema map[string]interface{}
	// GeminiResponseSchema, when non-nil, sets Gemini ResponseMIMEType to application/json
	// and sends this schema as ResponseSchema for API-enforced structured output.
	// Other Client implementations ignore this field.
	GeminiResponseSchema *genai.Schema
}

// Response holds the model's reply. Text is always populated; JSON is set when
// the model was asked to return structured JSON and parsing succeeded.
type Response struct {
	Text string
	JSON map[string]interface{}
}

// Client is the vendor-neutral LLM interface used throughout the pipeline.
type Client interface {
	// Complete sends a request to the LLM and returns the response.
	Complete(ctx context.Context, req Request) (*Response, error)
	// Name returns a human-readable identifier for the backend (e.g. "gemini").
	Name() string
}
