package fileconv

import "testing"

func TestIsVisionMIME(t *testing.T) {
	if !IsVisionMIME("image/png") {
		t.Fatal("expected png")
	}
	if IsVisionMIME("image/svg+xml") {
		t.Fatal("svg should not be vision MIME in this pipeline")
	}
	if _, err := VisionImageFormat("image/png"); err != nil {
		t.Fatal(err)
	}
	if _, err := VisionImageFormat("image/bmp"); err == nil {
		t.Fatal("expected error for bmp")
	}
}
