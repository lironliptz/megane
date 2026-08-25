package datamodeling

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	"megane/internal/db"
	"megane/internal/fileconv"
	"megane/internal/llm"
)

type AnalyzerConfig struct {
	DB          *db.DB
	LLMClient   llm.Client
	Prompts     map[string]string
	ProjectsDir string
	// GeminiModel overrides the model for schema inference (Gemini provider only).
	GeminiModel string
}

type Analyzer struct{ cfg AnalyzerConfig }

func NewAnalyzer(cfg AnalyzerConfig) *Analyzer { return &Analyzer{cfg: cfg} }

type fileEntry struct {
	project         *db.Project
	textContent     string
	hasEmbeddedText bool
}

func (a *Analyzer) AnalyzeFiles(ctx context.Context, req AnalysisRequest) (*AnalyzeResponse, error) {
	start := time.Now()

	prog, _ := newProgressReporter(a.cfg.DB, req.JobID, req.FileIDs)

	// Step 1 — Validate file IDs
	var missingIDs []int64
	var entries []fileEntry
	for _, id := range req.FileIDs {
		proj, err := a.cfg.DB.GetProjectByID(id)
		if err == sql.ErrNoRows {
			missingIDs = append(missingIDs, id)
			continue
		}
		if err != nil {
			return nil, err
		}
		entries = append(entries, fileEntry{project: proj})
	}
	if len(missingIDs) > 0 {
		return nil, ErrInvalidFileIDs{IDs: missingIDs}
	}

	// Step 2 — Extract file content (Filename is the on-disk path, same as the upload pipeline).
	for i, e := range entries {
		prog.setFile(e.project.ID, FileStageExtracting, prog.extractionProgress(i), "Extracting text")
		filePath := e.project.Filename
		if _, statErr := os.Stat(filePath); statErr != nil {
			slog.Warn("datamodeling: file missing on disk",
				"project_id", e.project.ID,
				"path", filePath,
				"original_name", e.project.OriginalName,
				"err", statErr,
			)
		}
		conv, ok := fileconv.GetConverter(e.project.MimeType)
		if !ok {
			slog.Warn("datamodeling: no converter for mime type",
				"project_id", e.project.ID,
				"mime_type", e.project.MimeType,
				"original_name", e.project.OriginalName,
			)
		} else {
			text, err := conv.Extract(ctx, filePath)
			if err != nil {
				slog.Warn("datamodeling: text extraction failed",
					"project_id", e.project.ID,
					"path", filePath,
					"mime_type", e.project.MimeType,
					"original_name", e.project.OriginalName,
					"err", err,
				)
			} else {
				entries[i].textContent = text
			}
		}
		entries[i].hasEmbeddedText = len(entries[i].textContent) > 50
		if !entries[i].hasEmbeddedText {
			slog.Warn("datamodeling: little or no extracted text",
				"project_id", e.project.ID,
				"path", filePath,
				"chars", len(entries[i].textContent),
				"original_name", e.project.OriginalName,
			)
		}
		prog.setFile(e.project.ID, FileStageExtracted, 42, "Text extracted")
	}

	// Step 3 — Detect strategy & check cost (all sample files, not just the first).
	costInputs := make([]FileCostInput, len(entries))
	for i, e := range entries {
		costInputs[i] = FileCostInput{
			MimeType:        e.project.MimeType,
			FileSize:        e.project.FileSize,
			TextChars:       len(e.textContent),
			HasEmbeddedText: e.hasEmbeddedText,
		}
	}
	estimatedTokens := EstimateRequestTokens(costInputs)
	maxTier := req.MaxCostTier
	if maxTier == "" {
		maxTier = "premium" // admin schema inference: pro model, multi-file samples, vision
	}
	if err := CheckCostTier(estimatedTokens, maxTier); err != nil {
		return nil, err
	}
	var preStrategy ProcessingStrategy
	if len(entries) > 0 {
		e := entries[0]
		preStrategy = DetectStrategy(e.project.MimeType, e.project.FileSize, e.hasEmbeddedText)
		preStrategy.EstimatedTokens = estimatedTokens
	}

	// Step 4 — Load current schema and admin-excluded fields
	var currentSchema *Schema
	var excludedNames []string
	if req.FileTypeID != nil {
		currentSchema, _ = GetLatestSchema(a.cfg.DB, *req.FileTypeID)
		if ft, err := GetFileTypeByID(a.cfg.DB, *req.FileTypeID); err == nil && ft != nil {
			excludedNames = ExcludedFieldNames(ParseExcludedFieldsJSON(ft.ExcludedFieldsJSON))
		}
	}

	// Step 5 — Build prompt context
	contextStr := a.buildContextStr(entries, req, currentSchema, excludedNames)

	// Step 5b — Vision fallback for scanned PDFs / images when text extraction was insufficient.
	visionImages, visErr := a.collectVisionImages(entries, prog)
	if visErr != nil {
		return nil, visErr
	}
	if len(visionImages) > 0 {
		contextStr += "\n\n=== VISION INPUT ===\nSample file page image(s) are attached. Text extraction was insufficient; analyze the visual content.\n"
	}

	// Step 6 — Select prompt key
	promptKey := "datamodeling_analyze"
	if currentSchema != nil {
		promptKey = "datamodeling_refine"
	}

	// Step 7 — Call LLM (Pro by default — better at abstract schema / type inference)
	llmReq := llm.Request{
		SystemPrompt: a.cfg.Prompts[promptKey],
		UserPrompt:   contextStr,
		Images:       visionImages,
		Model:        a.datamodelingLLMModel(),
	}
	if strings.EqualFold(strings.TrimSpace(a.cfg.LLMClient.Name()), "gemini") {
		responseSchema, schemaErr := llm.BuildSchema(LLMAnalysisOutput{})
		if schemaErr != nil {
			return nil, fmt.Errorf("build datamodeling schema: %w", schemaErr)
		}
		llmReq.GeminiResponseSchema = responseSchema
	}

	prog.setAll(FileStageLLM, 58, "Analyzing with LLM")

	slog.Info("datamodeling: calling LLM",
		"prompt", promptKey,
		"model", llmReq.Model,
		"file_count", len(entries),
		"context_chars", len(contextStr),
		"vision_images", len(visionImages),
		"structured_output", llmReq.GeminiResponseSchema != nil,
	)
	resp, err := llm.CompleteGated(ctx, a.cfg.LLMClient, llmReq)
	if err != nil {
		slog.Error("datamodeling: LLM request failed", "err", err)
		return nil, err
	}

	prog.setAll(FileStageValidating, 88, "Validating response")

	// Step 8 — Parse & validate; recover on failure
	llmOut, parseErr := a.parseLLMResponse(resp)
	if parseErr != nil {
		slog.Warn("datamodeling: LLM response parse failed, attempting recovery",
			"response_chars", len(resp.Text),
			"err", parseErr,
		)
		llmOut, parseErr = a.recover(ctx, resp.Text)
		if parseErr != nil {
			slog.Error("datamodeling: LLM response unrecoverable",
				"response_chars", len(resp.Text),
				"err", parseErr,
			)
			return nil, ErrInvalidLLMResponse{Details: llmErrorDetail(parseErr)}
		}
	}
	ApplyStrategyDefaults(llmOut, preStrategy)
	if valErr := ValidateLLMOutput(llmOut); valErr != nil {
		slog.Error("datamodeling: LLM response validation failed", "err", valErr)
		return nil, ErrInvalidLLMResponse{Details: valErr.Error()}
	}

	// Step 9 — Resolve or create file type
	isNew := false
	if req.FileTypeID == nil {
		baseSlug := Slugify(llmOut.FileTypeName)
		if baseSlug == "" {
			baseSlug = "unknown"
		}
		slug := baseSlug
		for i := 2; ; i++ {
			_, err := GetFileTypeBySlug(a.cfg.DB, slug)
			if err == sql.ErrNoRows {
				break
			}
			slug = fmt.Sprintf("%s_%d", baseSlug, i)
			if i > 99 {
				return nil, fmt.Errorf("slug collision limit exceeded")
			}
		}
		hintsJSON, _ := MarshalFieldHintsJSON(req.FieldHints)
		newID, err := CreateFileType(a.cfg.DB, llmOut.FileTypeName, slug, strings.TrimSpace(req.ExtractionBrief), hintsJSON)
		if err != nil {
			return nil, err
		}
		req.FileTypeID = &newID
		isNew = true
	}
	if req.FileTypeID != nil {
		_ = UpdateFileTypeConfig(a.cfg.DB, *req.FileTypeID, strings.TrimSpace(req.ExtractionBrief), req.FieldHints)
	}

	// Step 10 — Resolve hints
	resolutions := ResolveHints(req.FieldHints, llmOut.ProposedSchema.Fields)

	// Step 11 — Merge schema
	var prevProposed *ProposedSchema
	if currentSchema != nil {
		var ps ProposedSchema
		if err := json.Unmarshal([]byte(currentSchema.SchemaJSON), &ps); err == nil {
			prevProposed = &ps
		}
	}
	llmOut.ProposedSchema.Fields = filterSchemaFieldsNotExcluded(llmOut.ProposedSchema.Fields, excludedNames)
	merged, notes := MergeSchema(prevProposed, llmOut.ProposedSchema, req.FieldHints, resolutions, excludedNames)
	if llmOut.RefinementNotes != "" && notes == "" {
		notes = llmOut.RefinementNotes
	}

	// Step 12 — Diff
	diff := DiffSchemas(prevProposed, &merged)

	// Step 13 — Update stats
	fieldsFoundThisRun := map[string]string{}
	for _, f := range llmOut.ProposedSchema.Fields {
		fieldsFoundThisRun[f.Name] = f.SampleValue
	}
	existingStats := "{}"
	if currentSchema != nil {
		existingStats = currentSchema.StatsJSON
	}
	statsJSON, _ := UpdateStats(existingStats, merged, fieldsFoundThisRun)
	fieldStats := ParseSchemaStats(statsJSON)

	// Step 14 — Persist schema
	merged.SchemaHash = HashSchema(merged)
	mergedJSON, _ := json.Marshal(merged)
	nextVer := NextVersion(currentSchema)

	convergenceScore, _ := RecordSchemaMetrics(a.cfg.DB, *req.FileTypeID, prevProposed, merged)
	_, _ = CreateSchema(a.cfg.DB, *req.FileTypeID, nextVer, string(mergedJSON), statsJSON, convergenceScore)
	_ = TouchFileType(a.cfg.DB, *req.FileTypeID)

	// Step 15 — Persist analysis records
	manualAnswersJSON, _ := json.Marshal(req.ManualAnswers)
	resultJSON, _ := json.Marshal(llmOut)
	elapsed := time.Since(start)
	var sampleInputs []SampleRegisterInput
	for _, e := range entries {
		_, _ = CreateAnalysis(a.cfg.DB, &Analysis{
			FileTypeID:       *req.FileTypeID,
			SchemaVersion:    nextVer,
			FileID:           e.project.ID,
			ResultJSON:       string(resultJSON),
			ManualAnswers:    string(manualAnswersJSON),
			ProcessingTimeMs: elapsed.Milliseconds(),
			LLMTokenUsage:    "{}",
		})
		sampleInputs = append(sampleInputs, SampleRegisterInput{
			FileID:       e.project.ID,
			OriginalName: e.project.OriginalName,
			SizeBytes:    e.project.FileSize,
			FilePath:     e.project.Filename,
		})
	}
	_ = RegisterTypeSamples(a.cfg.DB, *req.FileTypeID, sampleInputs)

	// Step 16 — Build response
	convergence, _ := BuildConvergenceIndicators(a.cfg.DB, *req.FileTypeID, convergenceScore, merged)
	fileTypeRow, _ := GetFileTypeByID(a.cfg.DB, *req.FileTypeID)
	filesAnalyzed, _ := CountAnalysesByFileType(a.cfg.DB, *req.FileTypeID)

	prevVer := nextVer - 1

	return &AnalyzeResponse{
		FileType: &FileTypeView{
			ID:               fileTypeRow.ID,
			Name:             fileTypeRow.Name,
			Slug:             fileTypeRow.Slug,
			ExtractionBrief:  fileTypeRow.ExtractionBrief,
			FieldHints:       ParseFieldHintsJSON(fileTypeRow.FieldHintsJSON),
			LatestVersion:    nextVer,
			FilesAnalyzed:    filesAnalyzed,
			ConvergenceScore: convergenceScore,
			IsNew:            isNew,
			CreatedAt:        fileTypeRow.CreatedAt.Format(time.RFC3339),
			UpdatedAt:        fileTypeRow.UpdatedAt.Format(time.RFC3339),
		},
		SchemaVersion:         nextVer,
		PreviousVersion:       prevVer,
		ProcessingStrategy:    llmOut.ProcessingStrategy,
		Metadata:              llmOut.Metadata,
		HintResolution:        resolutions,
		ProposedSchema:        merged,
		SchemaDiff:            diff,
		ConvergenceIndicators: convergence,
		FieldStatistics:       fieldStats,
		RefinementNotes:       notes,
		NeedsManualHelp:       llmOut.NeedsManualHelp,
		ManualQuestions:       llmOut.ManualQuestions,
		ProcessingStats: ProcessingStats{
			DurationMs: elapsed.Milliseconds(),
		},
	}, nil
}

func (a *Analyzer) datamodelingLLMModel() string {
	if a == nil || a.cfg.LLMClient == nil {
		return ""
	}
	if !strings.EqualFold(strings.TrimSpace(a.cfg.LLMClient.Name()), "gemini") {
		return ""
	}
	if m := strings.TrimSpace(a.cfg.GeminiModel); m != "" {
		return m
	}
	return ResolveGeminiModel("")
}

func (a *Analyzer) collectVisionImages(entries []fileEntry, prog *progressReporter) ([]llm.RequestImage, error) {
	var images []llm.RequestImage
	for _, e := range entries {
		prog.setFile(e.project.ID, FileStageVision, 48, "Preparing vision input")
		parts, err := fileconv.VisionPartsForFile(e.project.Filename, e.project.MimeType, len(e.textContent), 5)
		if err != nil {
			slog.Warn("datamodeling: vision parts failed",
				"project_id", e.project.ID,
				"path", e.project.Filename,
				"err", err,
			)
			return nil, fmt.Errorf("vision input for %q: %w", e.project.OriginalName, err)
		}
		for _, p := range parts {
			images = append(images, llm.RequestImage{MIMEType: p.MIMEType, Data: p.Data})
		}
	}
	if len(images) > 0 && !strings.EqualFold(strings.TrimSpace(a.cfg.LLMClient.Name()), "gemini") {
		return nil, fmt.Errorf("scanned or image files require LLM_PROVIDER=gemini for vision analysis")
	}
	if len(images) > 0 {
		slog.Info("datamodeling: using vision input", "image_count", len(images))
	}
	return images, nil
}

func (a *Analyzer) parseLLMResponse(resp *llm.Response) (*LLMAnalysisOutput, error) {
	if resp == nil {
		return nil, ErrInvalidLLMResponse{Details: "empty LLM response"}
	}
	if resp.JSON != nil {
		return ParseLLMResponseMap(resp.JSON)
	}
	return ParseLLMResponse(resp.Text)
}

func llmErrorDetail(err error) string {
	var inv ErrInvalidLLMResponse
	if errors.As(err, &inv) && strings.TrimSpace(inv.Details) != "" {
		return inv.Details
	}
	return llm.SafeErr(err)
}

func (a *Analyzer) buildContextStr(entries []fileEntry, req AnalysisRequest, currentSchema *Schema, excluded []string) string {
	var b strings.Builder

	b.WriteString("=== FILE CONTENT ===\n")
	for _, e := range entries {
		b.WriteString("--- [" + e.project.OriginalName + "] ---\n")
		b.WriteString(e.textContent + "\n")
	}

	b.WriteString("\n=== FIELD HINTS ===\n")
	if len(req.FieldHints) > 0 {
		data, _ := json.Marshal(req.FieldHints)
		b.Write(data)
		b.WriteString("\n")
	} else {
		b.WriteString("None provided.\n")
	}

	b.WriteString("\n=== EXTRACTION EXPECTATIONS (guidance only — NOT schema field names) ===\n")
	b.WriteString("Free-form context: document sections to focus on, alternate section titles (any language), ")
	b.WriteString("or business instructions. Do NOT copy these phrases into proposed_schema.fields[].name; ")
	b.WriteString("derive snake_case field names from actual columns/labels in the file content.\n")
	if brief := strings.TrimSpace(req.ExtractionBrief); brief != "" {
		b.WriteString(brief)
		b.WriteString("\n")
	} else {
		b.WriteString("None provided.\n")
	}

	b.WriteString("\n=== EXCLUDED FIELDS (admin removed — never propose again) ===\n")
	if len(excluded) > 0 {
		for _, name := range excluded {
			b.WriteString("- ")
			b.WriteString(name)
			b.WriteString("\n")
		}
	} else {
		b.WriteString("None.\n")
	}

	b.WriteString("\n=== CURRENT SCHEMA ===\n")
	if currentSchema != nil {
		var ps ProposedSchema
		if err := json.Unmarshal([]byte(currentSchema.SchemaJSON), &ps); err == nil {
			data, _ := json.Marshal(ps.Fields)
			b.Write(data)
			b.WriteString("\n")
		}
	} else {
		b.WriteString("No prior schema. This is the first analysis.\n")
	}

	b.WriteString("\n=== FIELD STATISTICS ===\n")
	if currentSchema != nil && currentSchema.StatsJSON != "" && currentSchema.StatsJSON != "{}" {
		b.WriteString(currentSchema.StatsJSON + "\n")
	} else {
		b.WriteString("No statistics yet.\n")
	}

	if len(req.ManualAnswers) > 0 {
		b.WriteString("\n=== ADMIN ANSWERS TO PREVIOUS QUESTIONS ===\n")
		for i, ans := range req.ManualAnswers {
			fmt.Fprintf(&b, "%d. [%s] %s\n", i+1, ans.QuestionID, ans.Answer)
		}
	}

	return b.String()
}

func (a *Analyzer) recover(ctx context.Context, rawText string) (*LLMAnalysisOutput, error) {
	userPrompt := "The following text was returned by an AI. Extract the JSON:\n" + rawText
	req := llm.Request{
		SystemPrompt: a.cfg.Prompts["datamodeling_fix_json"],
		UserPrompt:   userPrompt,
		Model:        a.datamodelingLLMModel(),
	}
	if strings.EqualFold(strings.TrimSpace(a.cfg.LLMClient.Name()), "gemini") {
		if responseSchema, err := llm.BuildSchema(LLMAnalysisOutput{}); err == nil {
			req.GeminiResponseSchema = responseSchema
		}
	}
	resp, err := llm.CompleteGated(ctx, a.cfg.LLMClient, req)
	if err != nil {
		return nil, err
	}
	return a.parseLLMResponse(resp)
}
