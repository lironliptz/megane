package pipeline

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"sync"

	"megane/internal/db"
	"megane/internal/fileconv"
	"megane/internal/llm"
)

// Pipeline dispatches uploaded files to the first matching Route.
type Pipeline struct {
	DB      *db.DB
	LLM     llm.Client
	Prompts map[string]string // loaded from prompts/ directory

	// Routes are checked in order; the first whose Matches returns true is used.
	// A Route with nil Matches acts as a catch-all.
	// If Routes is empty, the built-in DocumentAnalysis pipeline runs for all files.
	Routes []Route

	projectCancels sync.Map // projectID → *projectCancel
	projectBusy    sync.Map // projectID → struct{}
	queue          *Queue   // optional; see InitQueue / EnqueueProcess
}

// Process dispatches a project to the correct route and runs it in the current goroutine.
// Call with `go pipeline.Process(ctx, projectID)`.
func (p *Pipeline) Process(ctx context.Context, projectID int64) {
	ctx, release := p.beginProjectRun(ctx, projectID)
	defer release()
	p.projectBusy.Store(projectID, struct{}{})
	defer p.projectBusy.Delete(projectID)

	project, err := p.DB.GetProjectByID(projectID)
	if err != nil {
		slog.Error("pipeline: project missing from DB", "project", projectID, "err", err)
		return
	}

	route := p.matchRoute(project.MimeType)
	slog.Info("pipeline: processing started",
		"project", projectID,
		"file", project.OriginalName,
		"mime", project.MimeType,
		"route", route.Name,
	)

	run := &Run{
		DB:      p.DB,
		LLM:     p.LLM,
		Project: project,
		Prompts: p.Prompts,
		Output:  route.resolveOutput(project.MimeType),
	}

	processFn := route.Process
	if processFn == nil {
		processFn = defaultProcess
	}

	if err := processFn(ctx, run); err != nil {
		// processFn is expected to call run.Fail itself; this is a safety net.
		slog.Error("pipeline: unhandled route error", "project", projectID, "route", route.Name, "err", err)
	}
}

// matchRoute returns the first route whose Matches function accepts mimeType,
// or a default catch-all route when none match.
func (p *Pipeline) matchRoute(mimeType string) Route {
	for _, r := range p.Routes {
		if r.matches(mimeType) {
			return r
		}
	}
	return Route{Name: "default"}
}

// defaultProcess is the built-in document-analysis pipeline.
// It handles extraction, LLM call, and result persistence.
// Custom Route.Process functions can call this for the standard flow
// or implement their own stage sequence entirely.
func defaultProcess(ctx context.Context, run *Run) error {
	filePath := run.Project.Filename
	mimeType := run.Project.MimeType
	originalName := run.Project.OriginalName

	// --- Stage: converting ---
	run.SetStatus(db.StatusConverting)
	run.Event(db.StatusConverting, "starting file conversion")

	converter, ok := fileconv.GetConverter(mimeType)
	if !ok {
		run.Fail("converting", fmt.Sprintf("no converter for mime type %q", mimeType), nil)
		return nil
	}

	extractedText, err := converter.Extract(ctx, filePath)
	if err != nil {
		run.Fail("converting", "extraction failed", err)
		return nil
	}

	var images []llm.RequestImage
	if fileconv.IsVisionMIME(mimeType) {
		if strings.ToLower(strings.TrimSpace(run.LLM.Name())) != "gemini" {
			run.Fail("converting", "image files require LLM_PROVIDER=gemini (vision inline parts)", nil)
			return nil
		}
		limit := maxVisionImageBytes()
		raw, rerr := os.ReadFile(filePath)
		if rerr != nil {
			run.Fail("converting", "read image file", rerr)
			return nil
		}
		if len(raw) > limit {
			run.Fail("converting", fmt.Sprintf("image too large (%d bytes); max %d", len(raw), limit), nil)
			return nil
		}
		images = []llm.RequestImage{{MIMEType: mimeType, Data: raw}}
		run.Event("converting", fmt.Sprintf("loaded image for vision (%d bytes)", len(raw)))
	} else {
		run.Event("converting", fmt.Sprintf("extracted %d characters", len(extractedText)))
	}

	// --- Stage: llm_pending ---
	run.SetStatus(db.StatusLLMPending)
	modelTag := strings.TrimSpace(run.Project.LLMModel)
	if modelTag == "" {
		modelTag = "server default"
	}
	run.Event(db.StatusLLMPending, fmt.Sprintf("sending to LLM (%s, model=%s)", run.LLM.Name(), modelTag))

	systemPrompt, promptName := run.SelectPrompt()
	promptSel := run.SelectPromptSelection()
	if systemPrompt == "" && promptSel.Combined != "" {
		systemPrompt = promptSel.Combined
	}

	responseSchema, err := llm.BuildSchema(run.Output)
	if err != nil {
		run.Fail("llm_pending", "build response schema", err)
		return nil
	}

	userEnvelope := buildUserPrompt(originalName, mimeType, len(extractedText), extractedText)
	if len(images) > 0 {
		userEnvelope = buildUserPromptVision(originalName, mimeType, len(images[0].Data))
	}

	if ctx.Err() != nil {
		run.Fail("llm_pending", "cancelled before LLM", ctx.Err())
		return nil
	}

	resp, err := run.CompleteLLM(ctx, llm.Request{
		SystemPrompt:         systemPrompt,
		UserPrompt:           userEnvelope,
		Images:               images,
		GeminiResponseSchema: responseSchema,
		Model:                strings.TrimSpace(run.Project.LLMModel),
		Log: &llm.RequestLog{
			SourceFile:  originalName,
			PromptFiles: promptSel.PromptFiles,
		},
	})
	if err != nil {
		if isContextCancelled(err) {
			run.Fail("llm_pending", "cancelled during LLM", err)
		} else {
			run.Fail("llm_pending", "LLM request failed", err)
		}
		return nil
	}
	_ = promptName

	// --- Stage: llm_done ---
	run.SetStatus(db.StatusLLMDone)
	run.Event(db.StatusLLMDone, fmt.Sprintf("LLM responded with %d characters", len(resp.Text)))

	// --- Stage: post_processing ---
	run.SetStatus(db.StatusPostProcessing)
	run.Event(db.StatusPostProcessing, "writing result to disk")

	var resultData interface{}
	if resp.JSON != nil {
		resultData = resp.JSON
	} else {
		resultData = map[string]interface{}{"raw_text": resp.Text}
	}

	resultBytes, err := json.MarshalIndent(resultData, "", "  ")
	if err != nil {
		run.Fail("post_processing", "marshal result JSON", err)
		return nil
	}
	if err := os.WriteFile(filePath+".result.json", resultBytes, 0644); err != nil {
		run.Fail("post_processing", "write result file", err)
		return nil
	}

	// --- Stage: complete ---
	run.SetStatus(db.StatusComplete)
	run.Event(db.StatusComplete, "pipeline finished successfully")
	slog.Info("pipeline: complete", "project", run.Project.ID)
	return nil
}

func buildUserPrompt(originalName, mimeType string, charCount int, extractedText string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "### Uploaded file context\n- Original filename: %s\n- MIME type: %s\n- Extracted text length: %d characters\n\n### Extracted content\n%s",
		originalName, mimeType, charCount, strings.TrimSpace(extractedText))
	return b.String()
}

func buildUserPromptVision(originalName, mimeType string, byteSize int) string {
	return fmt.Sprintf("### Uploaded file context\n- Original filename: %s\n- MIME type: %s\n- Image size: %d bytes\n\n### Content\nThe image is attached as inline visual input. Analyze it and respond according to the JSON schema.",
		originalName, mimeType, byteSize)
}

// DefaultProcess exposes the built-in pipeline so custom routes can delegate to it.
// Example: run pre-processing, then hand off to the standard LLM+result flow.
func DefaultProcess(ctx context.Context, run *Run) error {
	return defaultProcess(ctx, run)
}

