package db

// Project status constants — match the values stored in projects.status.
const (
	StatusUploaded       = "uploaded"
	StatusConverting     = "converting"
	StatusLLMPending     = "llm_pending"
	StatusLLMDone        = "llm_done"
	StatusPostProcessing = "post_processing"
	StatusComplete       = "complete"
	StatusError          = "error"
)
