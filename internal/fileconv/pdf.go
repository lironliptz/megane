package fileconv

import (
	"context"
	"fmt"
	"strings"

	"github.com/gen2brain/go-fitz"
)

// PDFConverter extracts plain text from PDF files using go-fitz (MuPDF bindings).
type PDFConverter struct{}

func (p *PDFConverter) SupportedMIME() []string {
	return []string{"application/pdf"}
}

func (p *PDFConverter) Extract(_ context.Context, filePath string) (string, error) {
	doc, err := fitz.New(filePath)
	if err != nil {
		return "", fmt.Errorf("pdf: open %q: %w", filePath, err)
	}
	defer doc.Close()

	var sb strings.Builder
	for i := 0; i < doc.NumPage(); i++ {
		text, err := doc.Text(i)
		if err != nil {
			return "", fmt.Errorf("pdf: extract page %d: %w", i, err)
		}
		sb.WriteString(text)
		sb.WriteRune('\n')
	}
	return sb.String(), nil
}
