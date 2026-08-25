package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/google/generative-ai-go/genai"
	"google.golang.org/api/option"
)

// GeminiClient implements Client using the Google Generative AI Go SDK.
type GeminiClient struct {
	client *genai.Client
	model  string
}

const defaultGeminiModel = "gemini-3-flash-preview"

// normalizeGeminiModelAliases maps retired preview IDs to their GA equivalents so LLM_MODEL and
// UI selections keep working without silent API failures. See Google’s Gemini API release notes:
// gemini-3.1-flash-lite-preview was discontinued after GA of gemini-3.1-flash-lite (May 2026).
func normalizeGeminiModelAliases(model string) string {
	raw := strings.TrimSpace(model)
	base := strings.ToLower(strings.TrimPrefix(raw, "models/"))
	base = strings.TrimSpace(base)
	switch base {
	case "gemini-3.1-flash-lite-preview":
		return "gemini-3.1-flash-lite"
	default:
		return raw
	}
}

// NewGeminiClient creates a GeminiClient. model defaults to gemini-3-flash-preview.
// Only Gemini 3.0+ model IDs are accepted (see ValidateGeminiModelID).
func NewGeminiClient(apiKey, model string) (*GeminiClient, error) {
	model = NormalizeEnvModel(model, defaultGeminiModel)
	model = normalizeGeminiModelAliases(model)
	model = strings.TrimSpace(model)
	if err := ValidateGeminiModelID(model); err != nil {
		return nil, err
	}
	ctx := context.Background()
	c, err := genai.NewClient(ctx, option.WithAPIKey(apiKey))
	if err != nil {
		return nil, WrapSanitizedErr("gemini: create client", err)
	}
	return &GeminiClient{client: c, model: model}, nil
}

// EffectiveGeminiModelFromEnv normalizes LLM_MODEL from the environment (same rules as the client default).
func EffectiveGeminiModelFromEnv(envModel string) string {
	m := NormalizeEnvModel(envModel, defaultGeminiModel)
	return normalizeGeminiModelAliases(m)
}

// ValidateGeminiModelID rejects legacy Gemini 1.x / 2.x IDs and unknown gemini-* names.
func ValidateGeminiModelID(id string) error {
	id = strings.TrimSpace(id)
	base := strings.TrimPrefix(strings.ToLower(id), "models/")
	base = strings.TrimSpace(base)
	if strings.HasPrefix(base, "tunedmodels/") {
		return nil
	}
	maj, ok := geminiMajorVersion(base)
	if !ok {
		return fmt.Errorf("gemini: model %q is not a recognized Gemini 3.0+ id (got legacy or unknown name)", id)
	}
	if maj < 3 {
		return fmt.Errorf("gemini: model %q is below minimum Gemini 3.0 (configure LLM_MODEL to a 3.x model)", id)
	}
	return nil
}

// geminiMajorVersion parses the leading numeric major from ids like "gemini-3-flash-preview".
func geminiMajorVersion(id string) (major int, ok bool) {
	id = strings.ToLower(strings.TrimSpace(id))
	if !strings.HasPrefix(id, "gemini-") {
		return 0, false
	}
	rest := id[len("gemini-"):]
	major = 0
	found := false
	for i := 0; i < len(rest); i++ {
		c := rest[i]
		if c < '0' || c > '9' {
			break
		}
		found = true
		major = major*10 + int(c-'0')
	}
	if !found {
		return 0, false
	}
	return major, true
}

// Name returns "gemini".
func (g *GeminiClient) Name() string { return "gemini" }

// Complete calls the Gemini API and returns the model response.
// When GeminiResponseSchema is set, the SDK enforces JSON via ResponseSchema and
// ResponseMIMEType. Otherwise, if Schema is set, JSON instructions are appended to the prompt.
func (g *GeminiClient) Complete(ctx context.Context, req Request) (*Response, error) {
	modelID := strings.TrimSpace(req.Model)
	if modelID == "" {
		modelID = g.model
	} else if err := ValidateGeminiModelID(modelID); err != nil {
		return nil, err
	}

	m := g.client.GenerativeModel(modelID)

	if req.SystemPrompt != "" {
		m.SystemInstruction = genai.NewUserContent(genai.Text(req.SystemPrompt))
	}

	userText := req.UserPrompt

	if req.GeminiResponseSchema != nil {
		m.ResponseMIMEType = "application/json"
		m.ResponseSchema = req.GeminiResponseSchema
	} else if req.Schema != nil {
		schemaBytes, err := json.MarshalIndent(req.Schema, "", "  ")
		if err != nil {
			return nil, fmt.Errorf("gemini: marshal schema: %w", err)
		}
		userText += "\n\n---\n\nRespond ONLY with valid JSON that conforms to this schema:\n" + string(schemaBytes)
	}

	var parts []genai.Part
	parts = append(parts, genai.Text(userText))
	for _, im := range req.Images {
		if len(im.Data) == 0 {
			continue
		}
		format, err := visionFormatForGemini(im.MIMEType)
		if err != nil {
			return nil, fmt.Errorf("gemini: image: %w", err)
		}
		parts = append(parts, genai.ImageData(format, im.Data))
	}

	resp, err := m.GenerateContent(ctx, parts...)
	if err != nil {
		return nil, WrapSanitizedErr("gemini: generate content", err)
	}

	if len(resp.Candidates) == 0 || resp.Candidates[0].Content == nil {
		return nil, fmt.Errorf("gemini: no candidates in response")
	}

	var text string
	for _, part := range resp.Candidates[0].Content.Parts {
		if t, ok := part.(genai.Text); ok {
			text += string(t)
		}
	}

	r := &Response{Text: text}
	if req.GeminiResponseSchema != nil || req.Schema != nil {
		// Strip markdown code fences if present.
		cleaned := stripJSONFences(text)
		var parsed map[string]interface{}
		if err := json.Unmarshal([]byte(cleaned), &parsed); err == nil {
			r.JSON = parsed
		}
	}
	return r, nil
}

// stripJSONFences removes optional ```json ... ``` or ``` ... ``` wrappers.
func stripJSONFences(s string) string {
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "```json") {
		s = strings.TrimPrefix(s, "```json")
	} else if strings.HasPrefix(s, "```") {
		s = strings.TrimPrefix(s, "```")
	} else {
		return s
	}
	if idx := strings.LastIndex(s, "```"); idx != -1 {
		s = s[:idx]
	}
	return strings.TrimSpace(s)
}

func visionFormatForGemini(mimeType string) (string, error) {
	mimeType = strings.TrimSpace(strings.ToLower(mimeType))
	switch mimeType {
	case "image/jpeg", "image/jpg":
		return "jpeg", nil
	case "image/png":
		return "png", nil
	case "image/gif":
		return "gif", nil
	case "image/webp":
		return "webp", nil
	default:
		return "", fmt.Errorf("unsupported image MIME type %q (use jpeg, png, gif, or webp)", mimeType)
	}
}

