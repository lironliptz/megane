package fileconv

import (
	"bytes"
	"fmt"
	"image/png"
	"os"

	"github.com/gen2brain/go-fitz"
)

const minUsefulExtractedChars = 50

// VisionPart is raw image bytes suitable for Gemini inline vision parts.
type VisionPart struct {
	MIMEType string
	Data     []byte
}

// VisionPartsForFile returns vision input when text extraction is insufficient.
// Images are always sent as vision. PDFs fall back to rendered page PNGs when extractedLen is low.
func VisionPartsForFile(filePath, mimeType string, extractedLen, maxPDFPages int) ([]VisionPart, error) {
	if IsVisionMIME(mimeType) {
		raw, err := os.ReadFile(filePath)
		if err != nil {
			return nil, fmt.Errorf("vision: read image %q: %w", filePath, err)
		}
		return []VisionPart{{MIMEType: mimeType, Data: raw}}, nil
	}
	if mimeType == "application/pdf" && extractedLen < minUsefulExtractedChars {
		return pdfPagePNGImages(filePath, maxPDFPages)
	}
	return nil, nil
}

func pdfPagePNGImages(filePath string, maxPages int) ([]VisionPart, error) {
	doc, err := fitz.New(filePath)
	if err != nil {
		return nil, fmt.Errorf("vision pdf: open %q: %w", filePath, err)
	}
	defer doc.Close()

	pageCount := doc.NumPage()
	if pageCount == 0 {
		return nil, fmt.Errorf("vision pdf: no pages in %q", filePath)
	}
	if maxPages <= 0 {
		maxPages = 5
	}
	if pageCount > maxPages {
		pageCount = maxPages
	}

	parts := make([]VisionPart, 0, pageCount)
	for i := 0; i < pageCount; i++ {
		img, err := doc.Image(i)
		if err != nil {
			return nil, fmt.Errorf("vision pdf: render page %d: %w", i, err)
		}
		var buf bytes.Buffer
		if err := png.Encode(&buf, img); err != nil {
			return nil, fmt.Errorf("vision pdf: encode page %d: %w", i, err)
		}
		parts = append(parts, VisionPart{MIMEType: "image/png", Data: buf.Bytes()})
	}
	return parts, nil
}
