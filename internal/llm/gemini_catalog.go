package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"
)

// ListGeminiGenerativeModels lists Gemini model ids from the Google API that support
// generateContent and satisfy the same minimum-version policy as ValidateGeminiModelID,
// with noisy modalities (TTS, image-only, agents, etc.) filtered out for text chat UX.
func ListGeminiGenerativeModels(ctx context.Context, apiKey string) ([]string, error) {
	if strings.TrimSpace(apiKey) == "" {
		return nil, fmt.Errorf("gemini: empty API key")
	}

	base := catalogFallback()
	u := "https://generativelanguage.googleapis.com/v1beta/models?key=" + url.QueryEscape(apiKey)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return dedupeSortedModels(base), nil
	}

	client := &http.Client{Timeout: 18 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return dedupeSortedModels(base), nil
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil || resp.StatusCode != http.StatusOK {
		return dedupeSortedModels(base), nil
	}

	var parsed struct {
		Models []struct {
			Name                       string   `json:"name"`
			SupportedGenerationMethods []string `json:"supportedGenerationMethods"`
		} `json:"models"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return dedupeSortedModels(base), nil
	}

	seen := map[string]bool{}
	var out []string
	add := func(id string) {
		id = strings.TrimSpace(strings.TrimPrefix(id, "models/"))
		if id == "" || seen[id] {
			return
		}
		if !includeGeminiCatalogEntry(id) {
			return
		}
		if ValidateGeminiModelID(id) != nil {
			return
		}
		seen[id] = true
		out = append(out, id)
	}
	for _, id := range base {
		add(id)
	}
	for _, m := range parsed.Models {
		okMethod := false
		for _, meth := range m.SupportedGenerationMethods {
			if meth == "generateContent" {
				okMethod = true
				break
			}
		}
		if !okMethod {
			continue
		}
		add(m.Name)
	}

	sort.Strings(out)
	return out, nil
}

func includeGeminiCatalogEntry(short string) bool {
	lower := strings.ToLower(short)
	for _, bad := range []string{
		"-tts", "preview-tts", "-image", "robotics", "computer-use",
		"-agent", "embed", "embedding",
	} {
		if strings.Contains(lower, bad) {
			return false
		}
	}
	return true
}

func catalogFallback() []string {
	return []string{
		defaultGeminiModel,
		"gemini-3.1-pro-preview",
		"gemini-3-pro-preview",
		"gemini-3.1-flash-lite",
	}
}

func dedupeSortedModels(ids []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" || seen[id] {
			continue
		}
		if ValidateGeminiModelID(id) != nil {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	sort.Strings(out)
	return out
}
