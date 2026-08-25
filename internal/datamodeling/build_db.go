package datamodeling

import (
	"database/sql"
	"encoding/json"
	"time"

	"megane/internal/db"
)

// UpsertBuildConfig creates or updates dm_build_configs for a file type.
func UpsertBuildConfig(database *db.DB, cfg BuildConfig) (BuildConfig, error) {
	fieldOverridesJSON, _ := json.Marshal(cfg.FieldOverrides)
	if string(fieldOverridesJSON) == "null" {
		fieldOverridesJSON = []byte("[]")
	}
	mimeTypesJSON, _ := json.Marshal(cfg.MIMETypes)
	if string(mimeTypesJSON) == "null" {
		mimeTypesJSON = []byte("[]")
	}
	builtArtifactsJSON, _ := json.Marshal(cfg.BuiltArtifacts)

	now := time.Now().UTC()
	var builtAt interface{}
	if cfg.BuiltAt != nil {
		builtAt = *cfg.BuiltAt
	}

	if cfg.ID > 0 {
		_, err := database.Exec(
			`UPDATE dm_build_configs SET status = ?, field_overrides_json = ?, type_rules = ?, production_prompt = ?, mime_types_json = ?, built_at = ?, built_artifacts_json = ?, updated_at = ? WHERE id = ?`,
			cfg.Status, string(fieldOverridesJSON), cfg.TypeRules, cfg.ProductionPrompt, string(mimeTypesJSON), builtAt, string(builtArtifactsJSON), now, cfg.ID,
		)
		if err != nil {
			return cfg, err
		}
		cfg.UpdatedAt = now
		return cfg, nil
	}

	res, err := database.Exec(
		`INSERT INTO dm_build_configs (file_type_id, status, field_overrides_json, type_rules, production_prompt, mime_types_json, built_at, built_artifacts_json, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(file_type_id) DO UPDATE SET status = excluded.status, field_overrides_json = excluded.field_overrides_json, type_rules = excluded.type_rules, production_prompt = excluded.production_prompt, mime_types_json = excluded.mime_types_json, built_at = excluded.built_at, built_artifacts_json = excluded.built_artifacts_json, updated_at = excluded.updated_at`,
		cfg.FileTypeID, cfg.Status, string(fieldOverridesJSON), cfg.TypeRules, cfg.ProductionPrompt, string(mimeTypesJSON), builtAt, string(builtArtifactsJSON), now,
	)
	if err != nil {
		return cfg, err
	}
	id, _ := res.LastInsertId()
	if id > 0 {
		cfg.ID = id
	} else {
		// If it was an update, we need to fetch the ID
		row := database.QueryRow(`SELECT id FROM dm_build_configs WHERE file_type_id = ?`, cfg.FileTypeID)
		_ = row.Scan(&cfg.ID)
	}
	cfg.UpdatedAt = now
	return cfg, nil
}

// GetBuildConfig returns the build config for a file type, or nil, nil if absent.
func GetBuildConfig(database *db.DB, fileTypeID int64) (*BuildConfig, error) {
	row := database.QueryRow(
		`SELECT id, file_type_id, status, field_overrides_json, type_rules, production_prompt, mime_types_json, built_at, built_artifacts_json, updated_at FROM dm_build_configs WHERE file_type_id = ?`,
		fileTypeID,
	)
	var cfg BuildConfig
	var fieldOverridesJSON, mimeTypesJSON, builtArtifactsJSON string
	var builtAt sql.NullTime
	err := row.Scan(&cfg.ID, &cfg.FileTypeID, &cfg.Status, &fieldOverridesJSON, &cfg.TypeRules, &cfg.ProductionPrompt, &mimeTypesJSON, &builtAt, &builtArtifactsJSON, &cfg.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if builtAt.Valid {
		cfg.BuiltAt = &builtAt.Time
	}
	_ = json.Unmarshal([]byte(fieldOverridesJSON), &cfg.FieldOverrides)
	_ = json.Unmarshal([]byte(mimeTypesJSON), &cfg.MIMETypes)
	_ = json.Unmarshal([]byte(builtArtifactsJSON), &cfg.BuiltArtifacts)
	return &cfg, nil
}

// ListBuildViews returns one BuildView per file type that has at least one schema.
func ListBuildViews(database *db.DB) ([]BuildView, error) {
	rows, err := database.Query(
		`SELECT ft.id, ft.name, ft.slug, ft.extraction_brief, ft.field_hints_json, ft.excluded_fields_json, ft.created_at, ft.updated_at,
		 s.id, s.version, s.schema_json, s.stats_json, s.convergence_score, s.is_finalized, s.created_at,
		 (SELECT COUNT(*) FROM dm_type_samples WHERE file_type_id = ft.id),
		 bc.id, bc.status, bc.field_overrides_json, bc.type_rules, bc.production_prompt, bc.mime_types_json, bc.built_at, bc.built_artifacts_json, bc.updated_at
		 FROM dm_file_types ft
		 JOIN dm_schemas s ON s.file_type_id = ft.id
		 LEFT JOIN dm_build_configs bc ON bc.file_type_id = ft.id
		 WHERE s.version = (SELECT MAX(version) FROM dm_schemas WHERE file_type_id = ft.id)
		 ORDER BY ft.name ASC`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var views []BuildView
	for rows.Next() {
		var ft FileType
		var s Schema
		var sampleCount int
		var bcID sql.NullInt64
		var bcStatus, bcFieldOverridesJSON, bcTypeRules, bcProductionPrompt, bcMIMETypesJSON, bcBuiltArtifactsJSON sql.NullString
		var bcBuiltAt, bcUpdatedAt sql.NullTime
		var isFinalized int

		err := rows.Scan(
			&ft.ID, &ft.Name, &ft.Slug, &ft.ExtractionBrief, &ft.FieldHintsJSON, &ft.ExcludedFieldsJSON, &ft.CreatedAt, &ft.UpdatedAt,
			&s.ID, &s.Version, &s.SchemaJSON, &s.StatsJSON, &s.ConvergenceScore, &isFinalized, &s.CreatedAt,
			&sampleCount,
			&bcID, &bcStatus, &bcFieldOverridesJSON, &bcTypeRules, &bcProductionPrompt, &bcMIMETypesJSON, &bcBuiltAt, &bcBuiltArtifactsJSON, &bcUpdatedAt,
		)
		if err != nil {
			return nil, err
		}
		s.FileTypeID = ft.ID
		s.IsFinalized = isFinalized == 1

		view := BuildView{
			FileType:         FileTypeViewFrom(&ft),
			Schema:           &s,
			ConvergenceScore: s.ConvergenceScore,
			SampleCount:      sampleCount,
		}

		if bcID.Valid {
			cfg := BuildConfig{
				ID:               bcID.Int64,
				FileTypeID:       ft.ID,
				Status:           bcStatus.String,
				TypeRules:        bcTypeRules.String,
				ProductionPrompt: bcProductionPrompt.String,
				UpdatedAt:        bcUpdatedAt.Time,
			}
			if bcBuiltAt.Valid {
				cfg.BuiltAt = &bcBuiltAt.Time
			}
			_ = json.Unmarshal([]byte(bcFieldOverridesJSON.String), &cfg.FieldOverrides)
			_ = json.Unmarshal([]byte(bcMIMETypesJSON.String), &cfg.MIMETypes)
			_ = json.Unmarshal([]byte(bcBuiltArtifactsJSON.String), &cfg.BuiltArtifacts)
			view.Config = &cfg
		}
		views = append(views, view)
	}
	return views, nil
}

// GetBuildDetail returns the full detail view for one type.
func GetBuildDetail(database *db.DB, fileTypeID int64) (*BuildDetailView, error) {
	ft, err := GetFileTypeByID(database, fileTypeID)
	if err != nil {
		return nil, err
	}
	schema, err := GetLatestSchema(database, fileTypeID)
	if err != nil {
		return nil, err
	}
	if schema == nil {
		return nil, ErrNoSchema{}
	}

	cfg, err := GetBuildConfig(database, fileTypeID)
	if err != nil {
		return nil, err
	}
	if cfg == nil {
		cfg = &BuildConfig{FileTypeID: fileTypeID, Status: "draft"}
	}

	var ps ProposedSchema
	_ = json.Unmarshal([]byte(schema.SchemaJSON), &ps)

	stats := ParseSchemaStats(schema.StatsJSON)

	overrideMap := make(map[string]FieldOverride)
	for _, o := range cfg.FieldOverrides {
		overrideMap[o.Name] = o
	}

	var fieldRows []FieldRow
	for _, f := range ps.Fields {
		override, ok := overrideMap[f.Name]
		if !ok {
			override = FieldOverride{
				Name:     f.Name,
				Included: true,
				Required: f.Required,
				Emphasis: "normal",
			}
		}
		seenInN := 0
		if stat, ok := stats.Fields[f.Name]; ok {
			seenInN = stat.DocumentCount
		}
		fieldRows = append(fieldRows, FieldRow{
			SchemaField: f,
			Override:    override,
			SeenInN:     seenInN,
		})
	}

	// Strategy consensus
	rows, err := database.Query(
		`SELECT result_json FROM dm_analyses WHERE file_type_id = ? ORDER BY created_at DESC LIMIT 5`, fileTypeID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	strategyCounts := make(map[string]int)
	var maxStrategy string
	maxCount := 0
	for rows.Next() {
		var resultJSON string
		if err := rows.Scan(&resultJSON); err != nil {
			continue
		}
		var out LLMAnalysisOutput
		if err := json.Unmarshal([]byte(resultJSON), &out); err == nil {
			s := "text"
			if out.ProcessingStrategy.UseOCR {
				s = "ocr"
			} else if out.ProcessingStrategy.ConvertToImage {
				s = "convert_to_image"
			} else if out.ProcessingStrategy.MultiPass {
				s = "multi_pass"
			}
			strategyCounts[s]++
			if strategyCounts[s] > maxCount {
				maxCount = strategyCounts[s]
				maxStrategy = s
			}
		}
	}

	if maxStrategy == "" {
		maxStrategy = "text"
	}

	return &BuildDetailView{
		FileType:          FileTypeViewFrom(ft),
		Schema:            schema,
		Config:            *cfg,
		FieldRows:         fieldRows,
		StrategyConsensus: maxStrategy,
	}, nil
}

// MarkBuilt updates status='built', built_at=now(), built_artifacts_json.
func MarkBuilt(database *db.DB, fileTypeID int64, artifacts BuildArtifacts) error {
	artifactsJSON, _ := json.Marshal(artifacts)
	now := time.Now().UTC()
	_, err := database.Exec(
		`UPDATE dm_build_configs SET status = 'built', built_at = ?, built_artifacts_json = ?, updated_at = ? WHERE file_type_id = ?`,
		now, string(artifactsJSON), now, fileTypeID,
	)
	return err
}
