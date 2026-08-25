package pipeline

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	"megane/internal/llm"
)

const (
	llmEventStage   = "llm_call"
	llmEventRequest = "llm_request"
)

func effectiveLLMModel(provider, reqModel string) string {
	if m := strings.TrimSpace(reqModel); m != "" {
		return m
	}
	if strings.EqualFold(strings.TrimSpace(provider), "gemini") {
		return llm.EffectiveGeminiModelFromEnv(os.Getenv("LLM_MODEL"))
	}
	return "(server default)"
}

type llmAuditInfo struct {
	Provider         string
	Model            string
	SourceFile       string
	PromptFiles      string
	SystemChars      int
	UserChars        int
	TotalPromptChars int
	ImageCount       int
}

func buildLLMAudit(req llm.Request, provider string) llmAuditInfo {
	model := effectiveLLMModel(provider, req.Model)
	sysLen := len(req.SystemPrompt)
	userLen := len(req.UserPrompt)
	promptFiles := ""
	sourceFile := ""
	if req.Log != nil {
		if len(req.Log.PromptFiles) > 0 {
			promptFiles = strings.Join(req.Log.PromptFiles, ", ")
		}
		sourceFile = strings.TrimSpace(req.Log.SourceFile)
	}
	return llmAuditInfo{
		Provider:         provider,
		Model:            model,
		SourceFile:       sourceFile,
		PromptFiles:      promptFiles,
		SystemChars:      sysLen,
		UserChars:        userLen,
		TotalPromptChars: sysLen + userLen,
		ImageCount:       len(req.Images),
	}
}

func (a llmAuditInfo) jsonSnippet() string {
	payload := map[string]any{
		"model":              a.Model,
		"provider":           a.Provider,
		"prompt_files":       a.PromptFiles,
		"source_file":        a.SourceFile,
		"system_chars":       a.SystemChars,
		"user_chars":         a.UserChars,
		"total_prompt_chars": a.TotalPromptChars,
		"image_count":        a.ImageCount,
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return ""
	}
	return string(raw)
}

func (a llmAuditInfo) doneJSONSnippet(responseChars int, dur time.Duration) string {
	payload := map[string]any{
		"model":                 a.Model,
		"provider":              a.Provider,
		"prompt_files":          a.PromptFiles,
		"source_file":           a.SourceFile,
		"system_chars":          a.SystemChars,
		"user_chars":            a.UserChars,
		"total_prompt_chars":    a.TotalPromptChars,
		"image_count":           a.ImageCount,
		"response_chars":        responseChars,
		"response_duration_sec": dur.Seconds(),
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return fmt.Sprintf("%d chars", responseChars)
	}
	return string(raw)
}

func (a llmAuditInfo) slogAttrs() []any {
	return []any{
		slog.String("provider", a.Provider),
		slog.String("model", a.Model),
		slog.String("source_file", a.SourceFile),
		slog.String("prompt_files", a.PromptFiles),
		slog.Int("system_chars", a.SystemChars),
		slog.Int("user_chars", a.UserChars),
		slog.Int("total_prompt_chars", a.TotalPromptChars),
		slog.Int("image_count", a.ImageCount),
	}
}

// CompleteLLM runs the LLM with concurrency limiting, structured logging, and DB audit events.
func (r *Run) CompleteLLM(ctx context.Context, req llm.Request) (*llm.Response, error) {
	if r == nil || r.LLM == nil {
		return nil, fmt.Errorf("pipeline: LLM client not configured")
	}

	provider := r.LLM.Name()
	audit := buildLLMAudit(req, provider)
	meta := r.llmLogMeta()

	waitAttrs := append(append(meta, audit.slogAttrs()...),
		slog.Int("in_flight", llm.InFlight()),
		slog.Int("max_concurrent", llm.MaxConcurrent()),
	)
	slog.Info("llm waiting for slot", waitAttrs...)

	wait, err := llm.AcquireSlot(ctx)
	if err != nil {
		r.logLLMDB(llmEventRequest, fmt.Sprintf("cancelled waiting for slot: %s", llm.SafeErr(err)), "error", llm.SafeErr(err))
		return nil, err
	}
	defer llm.ReleaseSlot()

	if wait > 200*time.Millisecond {
		slog.Info("llm acquired slot after wait", append(meta, slog.Duration("queue_wait", wait))...)
	}

	startMsg := fmt.Sprintf("LLM start | model=%s | provider=%s | queue_wait=%s",
		audit.Model, audit.Provider, wait.Round(time.Millisecond))
	slog.Info("llm request", append(append(meta, audit.slogAttrs()...), slog.Duration("queue_wait", wait))...)
	r.logLLMDB(llmEventRequest, startMsg, "ok", audit.jsonSnippet())

	callStart := time.Now()
	resp, callErr := r.LLM.Complete(ctx, req)
	callDur := time.Since(callStart)

	if callErr != nil {
		errMsg := fmt.Sprintf("LLM failed | model=%s | after %s: %s",
			audit.Model, callDur.Round(time.Millisecond), llm.SafeErr(callErr))
		slog.Error("llm call failed", append(append(meta, audit.slogAttrs()...),
			slog.Duration("duration", callDur), slog.String("err", llm.SafeErr(callErr)))...)
		r.logLLMDB(llmEventStage, errMsg, "error", llm.SafeErr(callErr))
		return nil, callErr
	}

	chars := 0
	jsonFields := 0
	if resp != nil {
		chars = len(resp.Text)
		if resp.JSON != nil {
			jsonFields = len(resp.JSON)
		}
	}
	doneMsg := fmt.Sprintf("LLM ok | model=%s | duration=%s | response_chars=%d | json_fields=%d",
		audit.Model, callDur.Round(time.Millisecond), chars, jsonFields)
	slog.Info("llm call done", append(append(meta, audit.slogAttrs()...),
		slog.Duration("duration", callDur), slog.Int("response_chars", chars))...)
	r.logLLMDB(llmEventStage, doneMsg, "ok", audit.doneJSONSnippet(chars, callDur))
	return resp, nil
}

func (r *Run) llmLogMeta() []any {
	if r.Project != nil && r.Project.ID > 0 {
		return []any{slog.Int64("project_id", r.Project.ID)}
	}
	return nil
}

func (r *Run) logLLMDB(stage, msg, outcome, snippet string) {
	if r.Project != nil && r.Project.ID > 0 {
		r.EventEx(stage, msg, outcome, snippet)
	}
}
