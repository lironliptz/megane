package handlers

import (
	"strings"

	"megane/internal/db"
)

// PipelineStatusLabel returns a short English label for a pipeline status.
func PipelineStatusLabel(status string) string {
	switch strings.TrimSpace(status) {
	case "":
		return "Pending"
	case db.StatusUploaded:
		return "Queued"
	case db.StatusConverting:
		return "Extracting text"
	case db.StatusLLMPending:
		return "Analyzing with LLM"
	case db.StatusLLMDone:
		return "LLM complete"
	case db.StatusPostProcessing:
		return "Saving results"
	case db.StatusComplete:
		return "Complete"
	case db.StatusError:
		return "Error"
	default:
		return strings.ReplaceAll(status, "_", " ")
	}
}

// PipelineStatusPct returns approximate progress 0–100 for UI progress bars.
func PipelineStatusPct(status string) int {
	switch strings.TrimSpace(status) {
	case db.StatusUploaded:
		return 8
	case db.StatusConverting:
		return 28
	case db.StatusLLMPending:
		return 45
	case db.StatusLLMDone:
		return 62
	case db.StatusPostProcessing:
		return 82
	case db.StatusComplete, db.StatusError:
		return 100
	default:
		return 5
	}
}
