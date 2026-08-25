package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

const openAIBaseURL = "https://api.openai.com/v1"

// OpenAIClient implements Client using direct HTTP calls to the OpenAI API.
type OpenAIClient struct {
	apiKey string
	model  string
	http   *http.Client
}

// NewOpenAIClient creates an OpenAIClient.
func NewOpenAIClient(apiKey, model string) *OpenAIClient {
	model = NormalizeEnvModel(model, "gpt-4o")
	return &OpenAIClient{
		apiKey: apiKey,
		model:  model,
		http:   &http.Client{},
	}
}

// Name returns "openai".
func (o *OpenAIClient) Name() string { return "openai" }

type openAIMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type openAIResponseFormat struct {
	Type       string                 `json:"type"`
	JSONSchema map[string]interface{} `json:"json_schema,omitempty"`
}

type openAIRequest struct {
	Model          string                `json:"model"`
	Messages       []openAIMessage       `json:"messages"`
	ResponseFormat *openAIResponseFormat `json:"response_format,omitempty"`
}

type openAIChoice struct {
	Message openAIMessage `json:"message"`
}

type openAIResponse struct {
	Choices []openAIChoice `json:"choices"`
	Error   *struct {
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

// Complete sends a chat completion request to the OpenAI API.
func (o *OpenAIClient) Complete(ctx context.Context, req Request) (*Response, error) {
	if len(req.Images) > 0 {
		return nil, fmt.Errorf("openai: image uploads are not supported yet; use LLM_PROVIDER=gemini for PNG/JPEG/GIF/WebP")
	}
	messages := []openAIMessage{}
	if req.SystemPrompt != "" {
		messages = append(messages, openAIMessage{Role: "system", Content: req.SystemPrompt})
	}
	messages = append(messages, openAIMessage{Role: "user", Content: req.UserPrompt})

	payload := openAIRequest{
		Model:    o.model,
		Messages: messages,
	}

	if req.Schema != nil {
		payload.ResponseFormat = &openAIResponseFormat{
			Type: "json_schema",
			JSONSchema: map[string]interface{}{
				"name":   "response",
				"schema": req.Schema,
				"strict": true,
			},
		}
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("openai: marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, openAIBaseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("openai: create request: %w", err)
	}
	httpReq.Header.Set("Authorization", "Bearer "+o.apiKey)
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := o.http.Do(httpReq)
	if err != nil {
		return nil, WrapSanitizedErr("openai: do request", err)
	}
	defer resp.Body.Close()

	rawBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("openai: read response: %w", err)
	}

	var oaiResp openAIResponse
	if err := json.Unmarshal(rawBody, &oaiResp); err != nil {
		return nil, fmt.Errorf("openai: decode response: %w", err)
	}
	if oaiResp.Error != nil {
		return nil, fmt.Errorf("openai: api error: %s", oaiResp.Error.Message)
	}
	if len(oaiResp.Choices) == 0 {
		return nil, fmt.Errorf("openai: no choices in response")
	}

	text := oaiResp.Choices[0].Message.Content
	r := &Response{Text: text}
	if req.Schema != nil {
		var parsed map[string]interface{}
		if err := json.Unmarshal([]byte(text), &parsed); err == nil {
			r.JSON = parsed
		}
	}
	return r, nil
}
