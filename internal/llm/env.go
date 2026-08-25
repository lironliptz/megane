package llm

import "strings"

// NormalizeEnvModel trims whitespace, strips trailing inline `#…` fragments (common when a
// `.env.example` comment is pasted into LLM_MODEL), and returns fallback if empty.
func NormalizeEnvModel(s, fallback string) string {
	s = strings.TrimSpace(s)
	if idx := strings.IndexByte(s, '#'); idx >= 0 {
		s = strings.TrimSpace(s[:idx])
	}
	s = strings.TrimSpace(strings.ReplaceAll(strings.ReplaceAll(s, "\r", ""), "\n", ""))
	if s == "" {
		return fallback
	}
	return s
}
