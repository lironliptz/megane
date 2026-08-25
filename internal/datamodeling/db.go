package datamodeling

import (
	"database/sql"
	"strings"
	"time"

	"megane/internal/db"
)

// ---- FileType ----

func CreateFileType(database *db.DB, name, slug, extractionBrief, fieldHintsJSON string) (int64, error) {
	if strings.TrimSpace(fieldHintsJSON) == "" {
		fieldHintsJSON = "[]"
	}
	now := time.Now().UTC()
	res, err := database.Exec(
		`INSERT INTO dm_file_types (name, slug, extraction_brief, field_hints_json, excluded_fields_json, created_at, updated_at) VALUES (?, ?, ?, ?, '[]', ?, ?)`,
		name, slug, extractionBrief, fieldHintsJSON, now, now,
	)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func GetFileTypeByID(database *db.DB, id int64) (*FileType, error) {
	row := database.QueryRow(
		`SELECT id, name, slug, extraction_brief, field_hints_json, excluded_fields_json, created_at, updated_at FROM dm_file_types WHERE id = ?`, id,
	)
	ft := &FileType{}
	if err := row.Scan(&ft.ID, &ft.Name, &ft.Slug, &ft.ExtractionBrief, &ft.FieldHintsJSON, &ft.ExcludedFieldsJSON, &ft.CreatedAt, &ft.UpdatedAt); err != nil {
		return nil, err
	}
	return ft, nil
}

func GetFileTypeBySlug(database *db.DB, slug string) (*FileType, error) {
	row := database.QueryRow(
		`SELECT id, name, slug, extraction_brief, field_hints_json, excluded_fields_json, created_at, updated_at FROM dm_file_types WHERE slug = ?`, slug,
	)
	ft := &FileType{}
	if err := row.Scan(&ft.ID, &ft.Name, &ft.Slug, &ft.ExtractionBrief, &ft.FieldHintsJSON, &ft.ExcludedFieldsJSON, &ft.CreatedAt, &ft.UpdatedAt); err != nil {
		return nil, err
	}
	return ft, nil
}

func ListFileTypes(database *db.DB) ([]*FileType, error) {
	rows, err := database.Query(
		`SELECT id, name, slug, extraction_brief, field_hints_json, excluded_fields_json, created_at, updated_at FROM dm_file_types ORDER BY name ASC`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []*FileType
	for rows.Next() {
		ft := &FileType{}
		if err := rows.Scan(&ft.ID, &ft.Name, &ft.Slug, &ft.ExtractionBrief, &ft.FieldHintsJSON, &ft.ExcludedFieldsJSON, &ft.CreatedAt, &ft.UpdatedAt); err != nil {
			return nil, err
		}
		result = append(result, ft)
	}
	return result, rows.Err()
}

func TouchFileType(database *db.DB, id int64) error {
	_, err := database.Exec(`UPDATE dm_file_types SET updated_at = ? WHERE id = ?`, time.Now().UTC(), id)
	return err
}

func UpdateFileTypeConfig(database *db.DB, id int64, brief string, hints []FieldHint) error {
	hintsJSON, err := MarshalFieldHintsJSON(hints)
	if err != nil {
		return err
	}
	_, err = database.Exec(
		`UPDATE dm_file_types SET extraction_brief = ?, field_hints_json = ?, updated_at = ? WHERE id = ?`,
		brief, hintsJSON, time.Now().UTC(), id,
	)
	return err
}

func DeleteFileType(database *db.DB, id int64) error {
	tx, err := database.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	stmts := []string{
		`DELETE FROM dm_analyses WHERE file_type_id = ?`,
		`DELETE FROM dm_type_samples WHERE file_type_id = ?`,
		`DELETE FROM dm_convergence_metrics WHERE file_type_id = ?`,
		`DELETE FROM dm_schemas WHERE file_type_id = ?`,
		`DELETE FROM dm_file_types WHERE id = ?`,
	}
	for _, q := range stmts {
		if _, err := tx.Exec(q, id); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// ---- Schema ----

func GetLatestSchema(database *db.DB, fileTypeID int64) (*Schema, error) {
	row := database.QueryRow(
		`SELECT id, file_type_id, version, schema_json, stats_json, convergence_score, is_finalized, created_at
		 FROM dm_schemas WHERE file_type_id = ? ORDER BY version DESC LIMIT 1`, fileTypeID,
	)
	s := &Schema{}
	var isFinalized int
	err := row.Scan(&s.ID, &s.FileTypeID, &s.Version, &s.SchemaJSON, &s.StatsJSON, &s.ConvergenceScore, &isFinalized, &s.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	s.IsFinalized = isFinalized == 1
	return s, nil
}

func GetSchemaByVersion(database *db.DB, fileTypeID int64, version int) (*Schema, error) {
	row := database.QueryRow(
		`SELECT id, file_type_id, version, schema_json, stats_json, convergence_score, is_finalized, created_at
		 FROM dm_schemas WHERE file_type_id = ? AND version = ?`, fileTypeID, version,
	)
	s := &Schema{}
	var isFinalized int
	if err := row.Scan(&s.ID, &s.FileTypeID, &s.Version, &s.SchemaJSON, &s.StatsJSON, &s.ConvergenceScore, &isFinalized, &s.CreatedAt); err != nil {
		return nil, err
	}
	s.IsFinalized = isFinalized == 1
	return s, nil
}

func CreateSchema(database *db.DB, fileTypeID int64, version int, schemaJSON, statsJSON string, convergenceScore float64) (int64, error) {
	res, err := database.Exec(
		`INSERT INTO dm_schemas (file_type_id, version, schema_json, stats_json, convergence_score, is_finalized, created_at)
		 VALUES (?, ?, ?, ?, ?, 0, ?)`,
		fileTypeID, version, schemaJSON, statsJSON, convergenceScore, time.Now().UTC(),
	)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func SetSchemaFinalized(database *db.DB, fileTypeID int64, finalized bool) error {
	v := 0
	if finalized {
		v = 1
	}
	_, err := database.Exec(
		`UPDATE dm_schemas SET is_finalized = ?
		 WHERE file_type_id = ? AND version = (SELECT MAX(version) FROM dm_schemas WHERE file_type_id = ?)`,
		v, fileTypeID, fileTypeID,
	)
	return err
}

func CountSchemaVersions(database *db.DB, fileTypeID int64) (int, error) {
	var count int
	err := database.QueryRow(`SELECT COUNT(*) FROM dm_schemas WHERE file_type_id = ?`, fileTypeID).Scan(&count)
	return count, err
}

// ---- Analysis ----

func CreateAnalysis(database *db.DB, a *Analysis) (int64, error) {
	now := time.Now().UTC()
	res, err := database.Exec(
		`INSERT INTO dm_analyses (file_type_id, schema_version, file_id, result_json, manual_answers, processing_time_ms, llm_token_usage, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		a.FileTypeID, a.SchemaVersion, a.FileID, a.ResultJSON, a.ManualAnswers, a.ProcessingTimeMs, a.LLMTokenUsage, now,
	)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func GetAnalysisByID(database *db.DB, id int64) (*Analysis, error) {
	row := database.QueryRow(
		`SELECT id, file_type_id, schema_version, file_id, result_json, manual_answers, processing_time_ms, llm_token_usage, created_at
		 FROM dm_analyses WHERE id = ?`, id,
	)
	a := &Analysis{}
	if err := row.Scan(&a.ID, &a.FileTypeID, &a.SchemaVersion, &a.FileID, &a.ResultJSON, &a.ManualAnswers, &a.ProcessingTimeMs, &a.LLMTokenUsage, &a.CreatedAt); err != nil {
		return nil, err
	}
	return a, nil
}

func CountAnalysesByFileType(database *db.DB, fileTypeID int64) (int, error) {
	var count int
	err := database.QueryRow(`SELECT COUNT(*) FROM dm_analyses WHERE file_type_id = ?`, fileTypeID).Scan(&count)
	return count, err
}

type AnalysisSummaryRow struct {
	SchemaVersion int
	ResultJSON    string
}

func ListAnalysisSummaries(database *db.DB, fileTypeID int64, limit int) ([]AnalysisSummaryRow, error) {
	rows, err := database.Query(
		`SELECT schema_version, result_json FROM dm_analyses
		 WHERE file_type_id = ? ORDER BY created_at DESC LIMIT ?`, fileTypeID, limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []AnalysisSummaryRow
	for rows.Next() {
		var r AnalysisSummaryRow
		if err := rows.Scan(&r.SchemaVersion, &r.ResultJSON); err != nil {
			return nil, err
		}
		result = append(result, r)
	}
	return result, rows.Err()
}

// ---- Convergence metrics ----

func RecordConvergenceMetric(database *db.DB, fileTypeID int64, metricType string, value float64) error {
	_, err := database.Exec(
		`INSERT INTO dm_convergence_metrics (file_type_id, metric_type, value, recorded_at) VALUES (?, ?, ?, ?)`,
		fileTypeID, metricType, value, time.Now().UTC(),
	)
	return err
}

func GetRecentMetrics(database *db.DB, fileTypeID int64, metricType string, limit int) ([]float64, error) {
	rows, err := database.Query(
		`SELECT value FROM dm_convergence_metrics
		 WHERE file_type_id = ? AND metric_type = ?
		 ORDER BY recorded_at DESC LIMIT ?`, fileTypeID, metricType, limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []float64
	for rows.Next() {
		var v float64
		if err := rows.Scan(&v); err != nil {
			return nil, err
		}
		result = append(result, v)
	}
	return result, rows.Err()
}

// ---- Aggregated view ----

type FileTypeSummary struct {
	FileType
	LatestVersion int
	LatestSchema  *Schema
	FilesAnalyzed int
}

func ListFileTypeSummaries(database *db.DB) ([]*FileTypeSummary, error) {
	types, err := ListFileTypes(database)
	if err != nil {
		return nil, err
	}
	var result []*FileTypeSummary
	for _, ft := range types {
		s := &FileTypeSummary{FileType: *ft}
		schema, err := GetLatestSchema(database, ft.ID)
		if err != nil {
			return nil, err
		}
		s.LatestSchema = schema
		if schema != nil {
			s.LatestVersion = schema.Version
		}
		count, err := CountAnalysesByFileType(database, ft.ID)
		if err != nil {
			return nil, err
		}
		s.FilesAnalyzed = count
		result = append(result, s)
	}
	return result, nil
}
