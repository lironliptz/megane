package datamodeling

import (
	"encoding/json"
	"time"

	"megane/internal/db"
)

// File progress stages (also used by the admin UI).
const (
	FileStageQueued     = "queued"
	FileStageExtracting = "extracting"
	FileStageExtracted  = "extracted"
	FileStageVision     = "vision"
	FileStageLLM        = "llm_pending"
	FileStageValidating = "validating"
	FileStageComplete   = "complete"
	FileStageError      = "error"
)

// FileProgressItem is per-file progress returned in job status polls.
type FileProgressItem struct {
	FileID        int64  `json:"file_id"`
	Name          string `json:"name"`
	Stage         string `json:"stage"`
	Progress      int    `json:"progress"`
	Label         string `json:"label"`
	IsDuplicate   bool   `json:"is_duplicate,omitempty"`
	DuplicateNote string `json:"duplicate_note,omitempty"`
}

// BuildInitialFileProgress creates queued entries for each staged project id.
func BuildInitialFileProgress(database *db.DB, fileIDs []int64) ([]FileProgressItem, error) {
	items := make([]FileProgressItem, 0, len(fileIDs))
	for _, id := range fileIDs {
		proj, err := database.GetProjectByID(id)
		if err != nil {
			return nil, err
		}
		items = append(items, FileProgressItem{
			FileID:   id,
			Name:     proj.OriginalName,
			Stage:    FileStageQueued,
			Progress: 5,
			Label:    "Queued",
		})
	}
	return items, nil
}

func UpdateJobProgress(database *db.DB, jobID string, items []FileProgressItem) error {
	raw, err := json.Marshal(items)
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	_, err = database.Exec(
		`UPDATE dm_jobs SET progress_json = ?, updated_at = ? WHERE id = ?`,
		string(raw), now, jobID,
	)
	return err
}

func ParseJobProgress(raw string) []FileProgressItem {
	if raw == "" || raw == "[]" {
		return nil
	}
	var items []FileProgressItem
	if err := json.Unmarshal([]byte(raw), &items); err != nil {
		return nil
	}
	return items
}

func MarkJobProgressComplete(database *db.DB, jobID string) error {
	job, err := GetAnalysisJob(database, jobID)
	if err != nil {
		return err
	}
	items := ParseJobProgress(job.ProgressJSON)
	for i := range items {
		items[i].Stage = FileStageComplete
		items[i].Progress = 100
		items[i].Label = "Complete"
	}
	return UpdateJobProgress(database, jobID, items)
}

func MarkJobProgressError(database *db.DB, jobID string, label string) error {
	job, err := GetAnalysisJob(database, jobID)
	if err != nil {
		return err
	}
	items := ParseJobProgress(job.ProgressJSON)
	if label == "" {
		label = "Failed"
	}
	for i := range items {
		if items[i].Stage != FileStageComplete {
			items[i].Stage = FileStageError
			items[i].Label = label
		}
	}
	return UpdateJobProgress(database, jobID, items)
}

// progressReporter updates dm_jobs.progress_json during AnalyzeFiles.
type progressReporter struct {
	db    *db.DB
	jobID string
	items []FileProgressItem
	total int
}

func newProgressReporter(database *db.DB, jobID string, fileIDs []int64) (*progressReporter, error) {
	if jobID == "" {
		return nil, nil
	}
	items, err := BuildInitialFileProgress(database, fileIDs)
	if err != nil {
		return nil, err
	}
	r := &progressReporter{db: database, jobID: jobID, items: items, total: len(items)}
	_ = r.sync()
	return r, nil
}

func (r *progressReporter) indexFor(fileID int64) int {
	for i, it := range r.items {
		if it.FileID == fileID {
			return i
		}
	}
	return -1
}

func (r *progressReporter) setFile(fileID int64, stage string, progress int, label string) {
	if r == nil {
		return
	}
	idx := r.indexFor(fileID)
	if idx < 0 {
		return
	}
	r.items[idx].Stage = stage
	r.items[idx].Progress = progress
	r.items[idx].Label = label
	_ = r.sync()
}

func (r *progressReporter) setAll(stage string, progress int, label string) {
	if r == nil {
		return
	}
	for i := range r.items {
		r.items[i].Stage = stage
		r.items[i].Progress = progress
		r.items[i].Label = label
	}
	_ = r.sync()
}

func (r *progressReporter) extractionProgress(fileIndex int) int {
	if r.total <= 1 {
		return 35
	}
	span := 30
	base := 10
	return base + (fileIndex*span)/(r.total-1)
}

func (r *progressReporter) sync() error {
	return UpdateJobProgress(r.db, r.jobID, r.items)
}
