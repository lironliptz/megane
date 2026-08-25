package llm

import (
	"os"
	"testing"
)

func TestMaxConcurrentFromEnv(t *testing.T) {
	t.Setenv("MAX_CONCURRENT_LLM", "5")
	if got := MaxConcurrent(); got != 5 {
		t.Errorf("MaxConcurrent() = %d, want 5", got)
	}
	t.Setenv("MAX_CONCURRENT_LLM", "")
	if got := MaxConcurrent(); got != 2 {
		t.Errorf("default MaxConcurrent() = %d, want 2", got)
	}
	t.Setenv("MAX_CONCURENT_LLM", "3")
	t.Setenv("MAX_CONCURRENT_LLM", "")
	if got := MaxConcurrent(); got != 3 {
		t.Errorf("typo alias MaxConcurrent() = %d, want 3", got)
	}
	_ = os.Unsetenv("MAX_CONCURENT_LLM")
}
