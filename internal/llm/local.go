package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// LocalClient implements Client by calling an Ollama-compatible local API.
type LocalClient struct {
	baseURL string
	model   string
	http    *http.Client
}

// NewLocalClient creates a LocalClient.
// baseURL is e.g. "http://localhost:11434".
func NewLocalClient(baseURL, model string) *LocalClient {
	model = NormalizeEnvModel(model, "llama3")
	return &LocalClient{
		baseURL: strings.TrimRight(baseURL, "/"),
		model:   model,
		http:    &http.Client{},
	}
}

// Name returns "local".
func (l *LocalClient) Name() string { return "local" }

type ollamaRequest struct {
	Model  string `json:"model"`
	Prompt string `json:"prompt"`
	Stream bool   `json:"stream"`
	System string `json:"system,omitempty"`
	Format string `json:"format,omitempty"`
}

type ollamaResponse struct {
	Response string `json:"response"`
	Error    string `json:"error,omitempty"`
}

// Complete sends a generate request to the local Ollama API.
func (l *LocalClient) Complete(ctx context.Context, req Request) (*Response, error) {
	if len(req.Images) > 0 {
		return nil, fmt.Errorf("local: image uploads are not supported; use LLM_PROVIDER=gemini for vision")
	}
	payload := ollamaRequest{
		Model:  l.model,
		Prompt: req.UserPrompt,
		Stream: false,
	}
	if req.SystemPrompt != "" {
		payload.System = req.SystemPrompt
	}
	if req.Schema != nil {
		payload.Format = "json"
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("local: marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, l.baseURL+"/api/generate", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("local: create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := l.http.Do(httpReq)
	if err != nil {
		return nil, WrapSanitizedErr("local: do request", err)
	}
	defer resp.Body.Close()

	rawBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("local: read response: %w", err)
	}

	var ollamaResp ollamaResponse
	if err := json.Unmarshal(rawBody, &ollamaResp); err != nil {
		return nil, fmt.Errorf("local: decode response: %w", err)
	}
	if ollamaResp.Error != "" {
		return nil, fmt.Errorf("local: api error: %s", ollamaResp.Error)
	}

	text := ollamaResp.Response
	r := &Response{Text: text}
	if req.Schema != nil {
		var parsed map[string]interface{}
		if err := json.Unmarshal([]byte(text), &parsed); err == nil {
			r.JSON = parsed
		}
	}
	return r, nil
}
