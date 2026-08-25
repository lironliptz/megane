package models

// LLMOutput is implemented by every struct the LLM populates.
//
// Each field should carry json and description struct tags:
//
//	Summary string `json:"summary" description:"2-5 sentence overview"`
//
// Sprout-applications define their domain outputs in this package
// (one struct per logical concept) and register them via Pipeline.OutputFor.
type LLMOutput interface {
	// SchemaName is used for logging and, in multi-output pipelines,
	// for naming separate result files (e.g. "contract", "invoice").
	SchemaName() string
}
