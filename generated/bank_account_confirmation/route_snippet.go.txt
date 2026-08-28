// Route for bank_account_confirmation — processing strategy: ocr
// TODO: wire OCR/vision pre-pass from internal/fileconv (see prompt_6)
pipeline.Route{
	Name: "bank_account_confirmation",
	OutputFor: func(_ string) models.LLMOutput { return &models.BankAccountConfirmationAnalysis{} },
	// Process: nil — using defaultProcess; add pre/post hooks if needed
},
