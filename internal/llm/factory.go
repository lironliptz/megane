package llm

import "fmt"

// NewClient constructs the appropriate Client implementation based on provider.
// provider values: "gemini", "openai", "local".
func NewClient(provider, apiKey, model, localURL string) (Client, error) {
	switch provider {
	case "gemini", "":
		if apiKey == "" {
			return nil, fmt.Errorf("llm: GEMINI_API_KEY is required for provider=gemini")
		}
		return NewGeminiClient(apiKey, model)
	case "openai":
		if apiKey == "" {
			return nil, fmt.Errorf("llm: OPENAI_API_KEY is required for provider=openai")
		}
		return NewOpenAIClient(apiKey, model), nil
	case "local":
		if localURL == "" {
			return nil, fmt.Errorf("llm: LLM_LOCAL_URL is required for provider=local")
		}
		return NewLocalClient(localURL, model), nil
	default:
		return nil, fmt.Errorf("llm: unknown provider %q (use gemini, openai, or local)", provider)
	}
}
