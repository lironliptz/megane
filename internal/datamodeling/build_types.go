package datamodeling

import "time"

// FieldOverride is one row in dm_build_configs.field_overrides_json.
type FieldOverride struct {
	Name     string `json:"name"`     // matches SchemaField.Name
	Included bool   `json:"included"`
	Required bool   `json:"required"`
	Emphasis string `json:"emphasis"` // "normal" | "high" | "critical"
	Rules    string `json:"rules"`    // free-text extraction rule
}

// BuildConfig mirrors dm_build_configs.
type BuildConfig struct {
	ID               int64           `json:"id"`
	FileTypeID       int64           `json:"file_type_id"`
	Status           string          `json:"status"` // draft | confirmed | built
	FieldOverrides   []FieldOverride `json:"field_overrides"`
	TypeRules        string          `json:"type_rules"`
	ProductionPrompt string          `json:"production_prompt"`
	MIMETypes        []string        `json:"mime_types"`
	BuiltAt          *time.Time      `json:"built_at,omitempty"`
	BuiltArtifacts   BuildArtifacts  `json:"built_artifacts"`
	UpdatedAt        time.Time       `json:"updated_at"`
}

// BuildArtifacts is stored as JSON in dm_build_configs.built_artifacts_json.
type BuildArtifacts struct {
	SchemaVersion int        `json:"schema_version"`
	Files         []Artifact `json:"files"`
}

type Artifact struct {
	Path   string `json:"path"`
	Action string `json:"action"` // "created" | "overwritten" | "skipped"
	Note   string `json:"note,omitempty"`
}

// BuildView is the GET response for the build list.
type BuildView struct {
	FileType         FileTypeView `json:"file_type"`
	Schema           *Schema      `json:"schema,omitempty"`
	ConvergenceScore float64      `json:"convergence_score"`
	SampleCount      int          `json:"sample_count"`
	Config           *BuildConfig `json:"config,omitempty"` // nil if never opened
}

// BuildDetailView is returned by GET /types/:id/build.
type BuildDetailView struct {
	FileType          FileTypeView `json:"file_type"`
	Schema            *Schema      `json:"schema"`
	Config            BuildConfig  `json:"config"`
	FieldRows         []FieldRow   `json:"field_rows"`         // merged schema + overrides
	StrategyConsensus string       `json:"strategy_consensus"` // most common from last 5 analyses
}

// FieldRow is one row in the Build field editor.
type FieldRow struct {
	SchemaField SchemaField   `json:"schema_field"`
	Override    FieldOverride `json:"override"`
	SeenInN     int           `json:"seen_in_n"` // from stats_json
}

// PatchBuildRequest is the body for PATCH .../build.
type PatchBuildRequest struct {
	Status           *string         `json:"status,omitempty"`
	FieldOverrides   []FieldOverride `json:"field_overrides,omitempty"`
	TypeRules        *string         `json:"type_rules,omitempty"`
	ProductionPrompt *string         `json:"production_prompt,omitempty"`
	MIMETypes        []string        `json:"mime_types,omitempty"`
}

// ApplyResponse is returned by POST .../build/apply.
type ApplyResponse struct {
	FileType      string     `json:"file_type"`
	SchemaVersion int        `json:"schema_version"`
	Artifacts     []Artifact `json:"artifacts"`
	Warnings      []string   `json:"warnings,omitempty"`
}
