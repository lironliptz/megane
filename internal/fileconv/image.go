package fileconv

import (
	"context"
	"fmt"
	"os"
)

// ImageConverter extracts placeholder text for images. The pipeline sends raw
// image bytes to Gemini as inline vision parts — not as base64 inside the text
// prompt (which explodes token usage).
type ImageConverter struct{}

// visionImageFormats maps MIME type → Gemini ImageData suffix.
var visionImageFormats = map[string]string{
	"image/jpeg": "jpeg",
	"image/jpg":  "jpeg",
	"image/png":  "png",
	"image/gif":  "gif",
	"image/webp": "webp",
}

func (i *ImageConverter) SupportedMIME() []string {
	return []string{
		"image/jpeg",
		"image/jpg",
		"image/png",
		"image/gif",
		"image/webp",
	}
}

// IsVisionMIME reports whether mimeType is sent as inline vision input (not text extraction).
func IsVisionMIME(mimeType string) bool {
	_, ok := visionImageFormats[mimeType]
	return ok
}

// VisionImageFormat returns the Gemini ImageData suffix for mimeType (e.g. "png").
func VisionImageFormat(mimeType string) (string, error) {
	suffix, ok := visionImageFormats[mimeType]
	if !ok {
		return "", fmt.Errorf("unsupported vision MIME type %q", mimeType)
	}
	return suffix, nil
}

// Extract returns a short placeholder; the pipeline reads the file as raw bytes for Gemini.
func (i *ImageConverter) Extract(_ context.Context, filePath string) (string, error) {
	if _, err := os.Stat(filePath); err != nil {
		return "", fmt.Errorf("image: stat %q: %w", filePath, err)
	}
	return "(binary image — analyzed via vision input)", nil
}
