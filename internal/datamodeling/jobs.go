package datamodeling

import (
	"database/sql"
	"encoding/json"
	"time"

	"github.com/google/uuid"

	"megane/internal/db"
)

const (
	JobPending  = "pending"
	JobRunning  = "running"
	JobComplete = "complete"
	JobError    = "error"
)

// AnalysisJob tracks a background datamodeling analyze run.
type AnalysisJob struct {
	ID           string    `json:"job_id"`
	Status       string    `json:"status"`
	RequestJSON  string    `json:"-"`
	ResultJSON   string    `json:"-"`
	ErrorJSON    string    `json:"-"`
	ProgressJSON string    `json:"-"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

// JobStatusResponse is returned by GET /jobs/:id.
type JobStatusResponse struct {
	JobID        string             `json:"job_id"`
	Status       string             `json:"status"`
	FileProgress []FileProgressItem `json:"file_progress,omitempty"`
	Result       *AnalyzeResponse   `json:"result,omitempty"`
	Error        *AnalysisErrorBody `json:"error,omitempty"`
}

// StoredAnalysisRequest is persisted in dm_jobs.request_json.
type StoredAnalysisRequest struct {
	FileTypeID      *int64         `json:"file_type_id,omitempty"`
	FieldHints      []FieldHint    `json:"field_hints"`
	FileIDs         []int64        `json:"file_ids"`
	MaxCostTier     string         `json:"max_cost_tier"`
	ManualAnswers   []ManualAnswer `json:"manual_answers,omitempty"`
	ExtractionBrief string         `json:"extraction_brief,omitempty"`
}

func StoredRequestFrom(req AnalysisRequest) StoredAnalysisRequest {
	return StoredAnalysisRequest{
		FileTypeID:      req.FileTypeID,
		FieldHints:      req.FieldHints,
		FileIDs:         req.FileIDs,
		MaxCostTier:     req.MaxCostTier,
		ManualAnswers:   req.ManualAnswers,
		ExtractionBrief: req.ExtractionBrief,
	}
}

func (s StoredAnalysisRequest) ToAnalysisRequest() AnalysisRequest {
	return AnalysisRequest{
		FileTypeID:      s.FileTypeID,
		FieldHints:      s.FieldHints,
		FileIDs:         s.FileIDs,
		MaxCostTier:     s.MaxCostTier,
		ManualAnswers:   s.ManualAnswers,
		ExtractionBrief: s.ExtractionBrief,
	}
}

func CreateAnalysisJob(database *db.DB, req AnalysisRequest, progress []FileProgressItem) (string, error) {
	id := uuid.New().String()
	raw, err := json.Marshal(StoredRequestFrom(req))
	if err != nil {
		return "", err
	}
	progRaw := "[]"
	if len(progress) > 0 {
		b, err := json.Marshal(progress)
		if err != nil {
			return "", err
		}
		progRaw = string(b)
	}
	now := time.Now().UTC()
	_, err = database.Exec(
		`INSERT INTO dm_jobs (id, status, request_json, result_json, error_json, progress_json, created_at, updated_at)
		 VALUES (?, ?, ?, '', '', ?, ?, ?)`,
		id, JobPending, string(raw), progRaw, now, now,
	)
	if err != nil {
		return "", err
	}
	return id, nil
}

func SetJobRunning(database *db.DB, jobID string) error {
	now := time.Now().UTC()
	_, err := database.Exec(
		`UPDATE dm_jobs SET status = ?, updated_at = ? WHERE id = ? AND status = ?`,
		JobRunning, now, jobID, JobPending,
	)
	return err
}

func SetJobComplete(database *db.DB, jobID string, result *AnalyzeResponse) error {
	_ = MarkJobProgressComplete(database, jobID)
	raw, err := json.Marshal(result)
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	_, err = database.Exec(
		`UPDATE dm_jobs SET status = ?, result_json = ?, error_json = '', updated_at = ? WHERE id = ?`,
		JobComplete, string(raw), now, jobID,
	)
	return err
}

func SetJobError(database *db.DB, jobID string, body AnalysisErrorBody) error {
	_ = MarkJobProgressError(database, jobID, body.Message)
	raw, err := json.Marshal(body)
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	_, err = database.Exec(
		`UPDATE dm_jobs SET status = ?, error_json = ?, updated_at = ? WHERE id = ?`,
		JobError, string(raw), now, jobID,
	)
	return err
}

func GetAnalysisJob(database *db.DB, jobID string) (*AnalysisJob, error) {
	row := database.QueryRow(
		`SELECT id, status, request_json, result_json, error_json, progress_json, created_at, updated_at
		 FROM dm_jobs WHERE id = ?`, jobID,
	)
	j := &AnalysisJob{}
	if err := row.Scan(&j.ID, &j.Status, &j.RequestJSON, &j.ResultJSON, &j.ErrorJSON, &j.ProgressJSON, &j.CreatedAt, &j.UpdatedAt); err != nil {
		return nil, err
	}
	return j, nil
}

func JobStatusResponseFrom(job *AnalysisJob) (*JobStatusResponse, error) {
	if job == nil {
		return nil, sql.ErrNoRows
	}
	out := &JobStatusResponse{
		JobID:        job.ID,
		Status:       job.Status,
		FileProgress: ParseJobProgress(job.ProgressJSON),
	}
	if job.Status == JobComplete && job.ResultJSON != "" {
		var result AnalyzeResponse
		if err := json.Unmarshal([]byte(job.ResultJSON), &result); err != nil {
			return nil, err
		}
		out.Result = &result
	}
	if job.Status == JobError && job.ErrorJSON != "" {
		var body AnalysisErrorBody
		if err := json.Unmarshal([]byte(job.ErrorJSON), &body); err != nil {
			return nil, err
		}
		out.Error = &body
	}
	return out, nil
}
