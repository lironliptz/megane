package fileconv

import (
	"context"
	"errors"
)

// DWGConverter is a stub for DWG/DXF files.
// Full conversion requires an external tool such as LibreCAD or oda_converter.
type DWGConverter struct{}

func (d *DWGConverter) SupportedMIME() []string {
	return []string{
		"image/vnd.dwg",
		"image/vnd.dxf",
		"application/acad",
		"application/x-acad",
		"application/autocad_dwg",
		"application/dwg",
		"application/dxf",
	}
}

// Extract always returns an error instructing the caller to use an external tool.
func (d *DWGConverter) Extract(_ context.Context, _ string) (string, error) {
	return "", errors.New("DWG/DXF conversion requires external tool (LibreCAD or oda_converter)")
}
