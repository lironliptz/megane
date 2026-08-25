package pipeline

import "strings"

// PromptSelection is a loaded prompt with source file names for LLM audit.
type PromptSelection struct {
	Combined    string
	PromptFiles []string
	SystemChars int
}

func promptFileName(key string) string {
	key = strings.TrimSpace(key)
	if key == "" {
		return ""
	}
	return key + ".txt"
}

// SelectPromptSelection loads the default document-analysis prompts (system, then analyze).
func (r *Run) SelectPromptSelection() PromptSelection {
	var out PromptSelection
	if r == nil || r.Prompts == nil {
		return out
	}
	keys := []string{"system", "analyze"}
	if r.PromptKey != "" {
		keys = append([]string{r.PromptKey}, keys...)
	}
	for _, k := range keys {
		if v, ok := r.Prompts[k]; ok && strings.TrimSpace(v) != "" {
			out.PromptFiles = []string{promptFileName(k)}
			t := strings.TrimSpace(v)
			out.SystemChars = len(t)
			out.Combined = t
			return out
		}
	}
	return out
}
