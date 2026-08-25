package pipeline

import (
	"fmt"
	"log/slog"

	"megane/internal/db"
	"megane/internal/llm"
	"megane/internal/models"
)

// Run carries all context a pipeline process function needs.
// It is passed to Route.Process and to the built-in defaultProcess.
type Run struct {
	DB      *db.DB
	LLM     llm.Client
	Project *db.Project
	Prompts   map[string]string
	Output    models.LLMOutput // resolved by the matched Route
	PromptKey string           // optional override for the prompt key
}

// Fail records an error event, sets project status to "error", and logs.
// Call return immediately after Fail — it does not stop execution by itself.
func (r *Run) Fail(stage, msg string, err error) {
	full := msg
	if err != nil {
		full = fmt.Sprintf("%s: %s", msg, llm.SafeErr(err))
	}
	slog.Error("pipeline failure", "project", r.Project.ID, "stage", stage, "error", full)
	if dbErr := r.DB.AddEventEx(r.Project.ID, stage, full, "error", ""); dbErr != nil {
		slog.Error("pipeline: record failure event", "project", r.Project.ID, "err", dbErr)
	}
	if dbErr := r.DB.UpdateProjectStatus(r.Project.ID, db.StatusError); dbErr != nil {
		slog.Error("pipeline: set error status", "project", r.Project.ID, "err", dbErr)
	}
}

// Event appends a pipeline_events row (fire-and-forget).
func (r *Run) Event(stage, msg string) {
	r.EventEx(stage, msg, "", "")
}

// EventEx appends a pipeline event with optional outcome and JSON snippet (LLM audit).
func (r *Run) EventEx(stage, msg, outcome, snippet string) {
	if err := r.DB.AddEventEx(r.Project.ID, stage, msg, outcome, snippet); err != nil {
		slog.Error("pipeline: record event", "project", r.Project.ID, "stage", stage, "err", err)
	}
}

// SetStatus updates the project status column.
func (r *Run) SetStatus(status string) {
	if err := r.DB.UpdateProjectStatus(r.Project.ID, status); err != nil {
		slog.Error("pipeline: set status", "project", r.Project.ID, "status", status, "err", err)
	}
}

// SelectPrompt returns the text and key of the best available prompt.
// Priority: "system" > "analyze" > "".
func (r *Run) SelectPrompt() (text, name string) {
	for _, k := range []string{"system", "analyze"} {
		if v, ok := r.Prompts[k]; ok {
			return v, k
		}
	}
	return "", ""
}
