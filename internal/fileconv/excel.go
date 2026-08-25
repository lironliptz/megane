package fileconv

import (
	"context"
	"fmt"
	"strings"

	"github.com/xuri/excelize/v2"
)

// ExcelConverter extracts spreadsheet data as CSV-like plain text.
type ExcelConverter struct{}

func (e *ExcelConverter) SupportedMIME() []string {
	return []string{
		"application/vnd.openxmlformats-officedocument.spreadsheetml.sheet",
		"application/vnd.ms-excel",
	}
}

// Extract reads all sheets from the Excel file and returns their content as plain text.
func (e *ExcelConverter) Extract(_ context.Context, filePath string) (string, error) {
	f, err := excelize.OpenFile(filePath)
	if err != nil {
		return "", fmt.Errorf("excel: open %q: %w", filePath, err)
	}
	defer f.Close()

	var sb strings.Builder
	for _, sheetName := range f.GetSheetList() {
		sb.WriteString("=== Sheet: ")
		sb.WriteString(sheetName)
		sb.WriteString(" ===\n")

		rows, err := f.GetRows(sheetName)
		if err != nil {
			return "", fmt.Errorf("excel: get rows from sheet %q: %w", sheetName, err)
		}
		for _, row := range rows {
			sb.WriteString(strings.Join(row, "\t"))
			sb.WriteRune('\n')
		}
		sb.WriteRune('\n')
	}
	return sb.String(), nil
}
