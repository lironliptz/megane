package pipeline

import (
	"context"

	"megane/internal/models"
)

// Route maps a file pattern to a pipeline variant.
// Sprout-applications register routes in cmd/server/main.go.
//
// Example:
//
//	pipe.Routes = []pipeline.Route{
//	    {
//	        Name:      "contract-pdf",
//	        Matches:   func(mime string) bool { return mime == "application/pdf" },
//	        OutputFor: func(_ string) models.LLMOutput { return &models.ContractAnalysis{} },
//	        // Process: nil → uses built-in document analysis flow
//	    },
//	    {
//	        Name:      "financial-excel",
//	        Matches:   func(mime string) bool { return strings.Contains(mime, "spreadsheet") },
//	        OutputFor: func(_ string) models.LLMOutput { return &models.FinancialData{} },
//	        Process:   myExcelPipeline, // completely custom stages
//	    },
//	}
type Route struct {
	// Name is used in log output (e.g. "contract-pdf", "image-vision").
	Name string

	// Matches reports whether this route handles the given MIME type.
	// A nil Matches acts as a catch-all (place last in the slice).
	Matches func(mimeType string) bool

	// OutputFor returns the LLMOutput struct for this route.
	// A nil OutputFor defaults to &models.DocumentAnalysis{}.
	OutputFor func(mimeType string) models.LLMOutput

	// Process is the pipeline function for this route.
	// A nil Process runs the built-in document analysis pipeline.
	// Custom implementations must call run.SetStatus / run.Event / run.Fail
	// to keep the DB lifecycle consistent.
	Process func(ctx context.Context, run *Run) error
}

func (rt *Route) matches(mimeType string) bool {
	return rt.Matches == nil || rt.Matches(mimeType)
}

func (rt *Route) resolveOutput(mimeType string) models.LLMOutput {
	if rt.OutputFor != nil {
		return rt.OutputFor(mimeType)
	}
	return &models.DocumentAnalysis{}
}
