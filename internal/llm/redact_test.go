package llm

import (
	"strings"
	"testing"
)

func TestRedactSecrets_GeminiURL(t *testing.T) {
	in := `Post "https://generativelanguage.googleapis.com/v1beta/models/gemini-3.1-pro-preview:generateContent?%24alt=json%3Benum-encoding%3Dint&key=AIzaSyBalo3dNhOavxHPan_LI8vxBYlHRjnKtyU": context deadline exceeded`
	got := RedactSecrets(in)
	if strings.Contains(got, "AIzaSy") {
		t.Fatalf("key still present: %q", got)
	}
	if !strings.Contains(got, "key=[REDACTED]") {
		t.Fatalf("expected redacted key param, got %q", got)
	}
}

func TestRedactSecrets_Bearer(t *testing.T) {
	in := `openai: do request: Authorization: Bearer sk-secret123 failed`
	got := RedactSecrets(in)
	if strings.Contains(got, "sk-secret123") {
		t.Fatalf("bearer token still present: %q", got)
	}
	if !strings.Contains(got, "Bearer [REDACTED]") {
		t.Fatalf("expected redacted bearer, got %q", got)
	}
}

func TestWrapSanitizedErr(t *testing.T) {
	err := WrapSanitizedErr("gemini: generate content", &fakeErr{`failed &key=secret123`})
	if strings.Contains(err.Error(), "secret123") {
		t.Fatalf("wrapped error leaked secret: %q", err.Error())
	}
	if !strings.Contains(err.Error(), "key=[REDACTED]") {
		t.Fatalf("expected redacted key in wrapped error, got %q", err.Error())
	}
}

type fakeErr struct{ s string }

func (e *fakeErr) Error() string { return e.s }
