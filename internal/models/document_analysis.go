package models

// LLMFileMetadata captures perceived file type and high-level context for the UI.
type LLMFileMetadata struct {
	FileRole          string `json:"file_role" description:"High-level category: contract, invoice, report, spreadsheet, presentation, image, drawing, letter, form, email_thread, technical_spec, other"`
	FileKindSummary   string `json:"file_kind_summary" description:"Readable description of what the file is and its apparent purpose"`
	StructureOverview string `json:"structure_overview" description:"How content is organized: sections, worksheets, slide outline, image layers, etc."`
	PrimaryLanguage   string `json:"primary_language" description:"Dominant human language of the document"`
	ApproxExtent      string `json:"approx_extent,omitempty" description:"Rough scale such as page count guess, row/sheet count, or short/long"`
}

// LLMChartDataset is one numeric series for Chart.js-style rendering.
type LLMChartDataset struct {
	Label string    `json:"label" description:"Legend label"`
	Data  []float64 `json:"data" description:"Values; for bar/line lengths align with labels; pie uses single dataset"`
}

// LLMChartSpec describes a simple chart the browser can draw with Chart.js.
type LLMChartSpec struct {
	Title    string              `json:"title" description:"Chart title shown above the graphic"`
	Type     string              `json:"type" description:"Chart type" enum:"bar,line,pie,doughnut"`
	Labels   []string            `json:"labels" description:"Categories or slice labels"`
	Datasets []LLMChartDataset   `json:"datasets" description:"One or more series (pie/doughnut usually one)"`
}

// DocumentAnalysis is the default LLMOutput for generic document processing.
// Sprout-applications replace or extend this with their own domain structs
// and register them via Pipeline.OutputFor in cmd/server/main.go.
type DocumentAnalysis struct {
	Metadata    LLMFileMetadata `json:"metadata"`
	Summary     string          `json:"summary" description:"2–5 sentence executive overview of the document"`
	KeyInsights []string        `json:"key_insights" description:"Important facts, figures, obligations, or takeaways; at least 3 when content allows"`
	Charts      []LLMChartSpec  `json:"charts" description:"Up to 4 charts for quantitative breakdowns present in the text; empty array if none"`
	Confidence  float64         `json:"confidence" description:"Model confidence in this analysis from 0 to 1"`
	ExtraNotes  *string         `json:"extra_notes,omitempty" description:"Optional caveats or extraction limits"`
}

func (d *DocumentAnalysis) SchemaName() string { return "document_analysis" }
