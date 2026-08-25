package datamodeling

import (
	"fmt"
	"strings"
	"time"
)

// ---- DB row types ----

type FileType struct {
	ID              int64
	Name            string
	Slug            string
	ExtractionBrief string
	FieldHintsJSON      string
	ExcludedFieldsJSON  string
	CreatedAt           time.Time
	UpdatedAt           time.Time
}

type Schema struct {
	ID               int64
	FileTypeID       int64
	Version          int
	SchemaJSON       string
	StatsJSON        string
	ConvergenceScore float64
	IsFinalized      bool
	CreatedAt        time.Time
}

type Analysis struct {
	ID               int64
	FileTypeID       int64
	SchemaVersion    int
	FileID           int64
	ResultJSON       string
	ManualAnswers    string
	ProcessingTimeMs int64
	LLMTokenUsage    string
	CreatedAt        time.Time
}

// ---- Domain types ----

type FieldHint struct {
	Label    string `json:"label"`
	Type     string `json:"type,omitempty"`
	Required bool   `json:"required,omitempty"`
}

type SchemaField struct {
	Name                 string `json:"name" description:"snake_case field name"`
	Type                 string `json:"type" description:"One of: string, number, date, boolean, array, object"`
	Description          string `json:"description,omitempty" description:"What this field represents"`
	Required             bool   `json:"required" description:"Whether this field is required"`
	ExtractionConfidence string `json:"extraction_confidence" description:"One of: high, medium, low"`
	Provenance           string `json:"provenance,omitempty"`
	Deprecated           bool   `json:"deprecated,omitempty"`
	AbsenceStreak        int    `json:"absence_streak,omitempty"`
	SampleValue          string `json:"sample_value,omitempty" description:"Example value from the sample file"`
}

type ProposedSchema struct {
	Fields     []SchemaField `json:"fields" description:"Extracted schema fields"`
	SchemaHash string        `json:"schema_hash,omitempty"`
}

// FieldStat tracks how often a canonical schema field appeared across analyzed documents.
type FieldStat struct {
	DocumentCount int      `json:"document_count"`
	PresenceRate  float64  `json:"presence_rate"`
	SampleValue   string   `json:"sample_value,omitempty"`
	AliasesSeen   []string `json:"aliases_seen,omitempty"`
}

type SchemaStats struct {
	DocumentsAnalyzed int                  `json:"documents_analyzed"`
	Fields            map[string]FieldStat `json:"fields"`
	// FieldPresence is the fraction of documents where each field (or a similar name) appeared.
	FieldPresence map[string]float64 `json:"field_presence"`
	SampleValues  map[string]string  `json:"sample_values"`
}

type ProcessingStrategy struct {
	UseOCR            bool     `json:"use_ocr" description:"Whether OCR is needed"`
	ConvertToImage    bool     `json:"convert_to_image" description:"Whether to convert pages to images for vision"`
	MultiPass         bool     `json:"multi_pass" description:"Whether multi-pass extraction is recommended"`
	Passes            []string `json:"passes" description:"Named passes when multi_pass is true"`
	Rationale         string   `json:"rationale" description:"One sentence explaining the processing strategy"`
	EstimatedCostTier string   `json:"estimated_cost_tier,omitempty" description:"budget, standard, or premium"`
	EstimatedTokens   int      `json:"estimated_tokens,omitempty"`
}

type HintResolution struct {
	UserLabel   string `json:"user_label"`
	Found       bool   `json:"found"`
	MappedTo    string `json:"mapped_to,omitempty"`
	Confidence  string `json:"confidence,omitempty"`
	SampleValue string `json:"sample_value,omitempty"`
	Note        string `json:"note,omitempty"`
}

type ManualQuestion struct {
	QuestionID string `json:"question_id" description:"Stable id e.g. q0, q1"`
	Question   string `json:"question" description:"Question for the admin"`
	Context    string `json:"context,omitempty"`
}

type ManualAnswer struct {
	QuestionID string `json:"question_id"`
	Answer     string `json:"answer"`
}

type SchemaDiff struct {
	AddedFields       []string           `json:"added_fields"`
	RemovedFields     []string           `json:"removed_fields"`
	ModifiedFields    []FieldChange      `json:"modified_fields"`
	ConfidenceChanges []ConfidenceChange `json:"confidence_changes"`
}

type FieldChange struct {
	Name    string `json:"name"`
	OldType string `json:"old_type"`
	NewType string `json:"new_type"`
}

type ConfidenceChange struct {
	Name string `json:"name"`
	Old  string `json:"old"`
	New  string `json:"new"`
}

type ConvergenceIndicators struct {
	SchemaStabilityScore float64 `json:"schema_stability_score"`
	AverageConfidence    float64 `json:"average_confidence"`
	FilesAnalyzed        int     `json:"files_analyzed"`
	Recommendation       string  `json:"recommendation"`
}

type FileMetadata struct {
	Format            string   `json:"format" description:"pdf, image, docx, xlsx, txt, or other"`
	SizeBytes         int64    `json:"size_bytes"`
	PageCount         int      `json:"page_count,omitempty"`
	DetectedStructure string   `json:"detected_structure,omitempty"`
	LanguagesDetected []string `json:"languages_detected,omitempty"`
	HasEmbeddedText   bool     `json:"has_embedded_text,omitempty"`
}

type LLMTokenUsage struct {
	InputTokens  int    `json:"input_tokens"`
	OutputTokens int    `json:"output_tokens"`
	Model        string `json:"model"`
}

// ---- HTTP request/response types ----

type AnalysisRequest struct {
	FileTypeID      *int64
	FieldHints      []FieldHint
	FileIDs         []int64
	SkipCache       bool
	MaxCostTier     string
	ManualAnswers   []ManualAnswer
	ExtractionBrief string // domain context: what to extract (stored on the file type)
	JobID           string // optional: persist per-file progress to dm_jobs
}

type AnalyzeResponse struct {
	FileType              *FileTypeView         `json:"file_type"`
	SchemaVersion         int                   `json:"schema_version"`
	PreviousVersion       int                   `json:"previous_version,omitempty"`
	ProcessingStrategy    ProcessingStrategy    `json:"processing_strategy"`
	Metadata              FileMetadata          `json:"metadata"`
	HintResolution        []HintResolution      `json:"user_field_hints_resolution"`
	ProposedSchema        ProposedSchema        `json:"proposed_schema"`
	SchemaDiff            *SchemaDiff           `json:"schema_diff,omitempty"`
	ConvergenceIndicators ConvergenceIndicators `json:"convergence_indicators"`
	FieldStatistics       SchemaStats           `json:"field_statistics"`
	RefinementNotes       string                `json:"refinement_notes,omitempty"`
	NeedsManualHelp       bool                  `json:"needs_manual_help"`
	ManualQuestions       []ManualQuestion      `json:"manual_questions"`
	ProcessingStats       ProcessingStats       `json:"processing_stats"`
}

type ProcessingStats struct {
	DurationMs int64         `json:"duration_ms"`
	LLMTokens  LLMTokenUsage `json:"llm_tokens"`
}

type FileTypeView struct {
	ID               int64   `json:"id"`
	Name             string  `json:"name"`
	Slug             string  `json:"slug"`
	ExtractionBrief  string      `json:"extraction_brief,omitempty"`
	FieldHints       []FieldHint       `json:"field_hints,omitempty"`
	ExcludedFields   []ExcludedField   `json:"excluded_fields,omitempty"`
	LatestVersion    int               `json:"latest_version"`
	FilesAnalyzed    int     `json:"files_analyzed"`
	ConvergenceScore float64 `json:"convergence_score"`
	IsFinalized      bool    `json:"is_finalized"`
	IsNew            bool    `json:"is_new,omitempty"`
	CreatedAt        string  `json:"created_at"`
	UpdatedAt        string  `json:"updated_at"`
}

type ManualAnswerRequest struct {
	AnalysisID     int64          `json:"analysis_id"`
	Answers        []ManualAnswer `json:"answers" binding:"required"`
	ReprocessFiles bool           `json:"reprocess_files"`
}

type FinalizeRequest struct {
	IsFinalized bool   `json:"is_finalized"`
	Notes       string `json:"notes,omitempty"`
}

type LLMAnalysisOutput struct {
	FileTypeName       string             `json:"file_type" description:"Suggested file type name e.g. invoice or payment_instruction"`
	ProcessingStrategy ProcessingStrategy `json:"processing_strategy"`
	Metadata           FileMetadata       `json:"metadata"`
	HintResolution     []HintResolution   `json:"user_field_hints_resolution"`
	ProposedSchema     ProposedSchema     `json:"proposed_schema"`
	RefinementNotes    string             `json:"refinement_notes" description:"Summary of schema changes; empty on first analysis"`
	NeedsManualHelp    bool               `json:"needs_manual_help"`
	ManualQuestions    []ManualQuestion   `json:"manual_questions"`
}

// ---- Error sentinel types ----

type ErrInvalidFileIDs struct{ IDs []int64 }

func (e ErrInvalidFileIDs) Error() string { return "invalid_file_ids" }

type ErrNoSchema struct{}

func (e ErrNoSchema) Error() string { return "no_schema" }

type ErrSchemaFieldNotFound struct{ Name string }

func (e ErrSchemaFieldNotFound) Error() string { return "schema_field_not_found" }

type ErrInvalidLLMResponse struct{ Details string }

func (e ErrInvalidLLMResponse) Error() string {
	if strings.TrimSpace(e.Details) == "" {
		return "invalid_llm_response"
	}
	return "invalid_llm_response: " + e.Details
}

type ErrCostThresholdExceeded struct {
	Estimated int
	Limit     int
	Tier      string
}

func (e ErrCostThresholdExceeded) Error() string {
	return fmt.Sprintf("cost_threshold_exceeded: estimated %d tokens exceeds %s limit %d",
		e.Estimated, e.Tier, e.Limit)
}

// Slugify converts a display name to a URL-safe identifier.
func Slugify(name string) string {
	s := strings.ToLower(name)
	var b strings.Builder
	inSep := false
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			if inSep && b.Len() > 0 {
				b.WriteByte('_')
			}
			inSep = false
			b.WriteRune(r)
		} else {
			inSep = true
		}
	}
	result := b.String()
	if len(result) > 64 {
		result = result[:64]
	}
	return result
}
