package fileconv

import (
	"archive/zip"
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"strings"
)

// WordConverter extracts plain text from .docx files.
// A .docx file is a ZIP archive; the main content lives in word/document.xml.
type WordConverter struct{}

func (w *WordConverter) SupportedMIME() []string {
	return []string{
		"application/vnd.openxmlformats-officedocument.wordprocessingml.document",
		"application/msword",
	}
}

// Extract reads word/document.xml from the docx ZIP and strips XML tags.
func (w *WordConverter) Extract(_ context.Context, filePath string) (string, error) {
	r, err := zip.OpenReader(filePath)
	if err != nil {
		return "", fmt.Errorf("word: open zip %q: %w", filePath, err)
	}
	defer r.Close()

	for _, f := range r.File {
		if f.Name != "word/document.xml" {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return "", fmt.Errorf("word: open document.xml: %w", err)
		}
		defer rc.Close()
		text, err := extractTextFromXML(rc)
		if err != nil {
			return "", fmt.Errorf("word: parse document.xml: %w", err)
		}
		return text, nil
	}
	return "", fmt.Errorf("word: word/document.xml not found in %q", filePath)
}

// extractTextFromXML walks the XML token stream and collects character data,
// inserting newlines after paragraph (w:p) and run (w:r) elements.
func extractTextFromXML(r io.Reader) (string, error) {
	var sb strings.Builder
	dec := xml.NewDecoder(r)
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", err
		}
		switch t := tok.(type) {
		case xml.CharData:
			sb.Write(t)
		case xml.EndElement:
			switch t.Name.Local {
			case "p": // paragraph
				sb.WriteRune('\n')
			case "tr": // table row
				sb.WriteRune('\n')
			case "tc": // table cell
				sb.WriteRune('\t')
			}
		}
	}
	return strings.TrimSpace(sb.String()), nil
}
