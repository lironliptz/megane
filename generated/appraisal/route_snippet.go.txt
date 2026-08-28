// Route for appraisal — processing strategy: ocr
// TODO: wire OCR/vision pre-pass from internal/fileconv (see prompt_6)
pipeline.Route{
	Name: "appraisal",
	OutputFor: func(_ string) models.LLMOutput { return &models.AppraisalAnalysis{} },
	// Process: nil — using defaultProcess; add pre/post hooks if needed
},
