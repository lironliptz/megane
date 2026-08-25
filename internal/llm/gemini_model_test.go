package llm

import "testing"

func TestValidateGeminiModelID(t *testing.T) {
	good := []string{
		"gemini-3-flash-preview",
		"gemini-3.1-flash-lite",
		"gemini-3.1-flash-lite-preview", // alias resolves before API use; still valid major version
		"models/gemini-3.1-pro-preview",
	}
	for _, id := range good {
		if err := ValidateGeminiModelID(id); err != nil {
			t.Errorf("ValidateGeminiModelID(%q) = %v, want nil", id, err)
		}
	}

	bad := []string{
		"gemini-2.0-flash-exp",
		"gemini-1.5-flash",
		"gemini-pro",
		"gemini-ultra",
	}
	for _, id := range bad {
		if err := ValidateGeminiModelID(id); err == nil {
			t.Errorf("ValidateGeminiModelID(%q) want error", id)
		}
	}
}

func TestGeminiMajorVersion(t *testing.T) {
	tests := []struct {
		id   string
		maj  int
		ok   bool
	}{
		{"gemini-3-flash-preview", 3, true},
		{"gemini-10-banana", 10, true},
		{"gemini-2.5-preview", 2, true},
		{"gemini-pro", 0, false},
		{"gpt-4", 0, false},
	}
	for _, tt := range tests {
		gotMaj, gotOk := geminiMajorVersion(tt.id)
		if gotOk != tt.ok || gotMaj != tt.maj {
			t.Errorf("geminiMajorVersion(%q) = (%d,%v), want (%d,%v)", tt.id, gotMaj, gotOk, tt.maj, tt.ok)
		}
	}
}
