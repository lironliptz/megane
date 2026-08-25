package codegen

import (
	"strings"
	"testing"
)

func TestRouteSnippet_OCRStrategy(t *testing.T) {
	snippet := RouteSnippet("invoice", []string{"application/pdf"}, "ocr")
	if !strings.Contains(snippet, "TODO: wire OCR") {
		t.Errorf("Expected OCR TODO comment in snippet")
	}
}
