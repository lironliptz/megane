package fileconv

import (
	"context"
	"fmt"
	"os"
)

// TextConverter reads UTF-8 text-like files as-is for LLM consumption.
type TextConverter struct{}

func (TextConverter) SupportedMIME() []string {
	return []string{
		"text/plain",
		"text/markdown",
		"text/csv",
		"application/json",
	}
}

func (TextConverter) Extract(ctx context.Context, filePath string) (string, error) {
	select {
	case <-ctx.Done():
		return "", ctx.Err()
	default:
	}
	b, err := os.ReadFile(filePath)
	if err != nil {
		return "", fmt.Errorf("read text file: %w", err)
	}
	return string(b), nil
}
