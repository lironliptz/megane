// Route for payment_order — processing strategy: ocr
// TODO: wire OCR/vision pre-pass from internal/fileconv (see prompt_6)
pipeline.Route{
	Name: "payment_order",
	OutputFor: func(_ string) models.LLMOutput { return &models.PaymentOrderAnalysis{} },
	// Process: nil — using defaultProcess; add pre/post hooks if needed
},
