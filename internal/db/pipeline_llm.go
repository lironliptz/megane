package db

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// LLMTiming holds LLM stage duration derived from pipeline_events.
type LLMTiming struct {
	DurationSec float64 `json:"duration_sec"`
	InProgress  bool    `json:"in_progress"`
}

// LLMInfo is parsed metadata from llm_call result_snippet JSON.
type LLMInfo struct {
	Model            string  `json:"model,omitempty"`
	Provider         string  `json:"provider,omitempty"`
	PromptFiles      string  `json:"prompt_files,omitempty"`
	TotalPromptChars int     `json:"total_prompt_chars,omitempty"`
	ResponseChars    int     `json:"response_chars,omitempty"`
	ResponseDuration float64 `json:"response_duration_sec,omitempty"`
}

// LLMTimingForProject returns duration from llm_request → llm_call (ok) or in-flight from llm_request.
func (d *DB) LLMTimingForProject(projectID int64) (LLMTiming, error) {
	events, err := d.GetEvents(projectID)
	if err != nil {
		return LLMTiming{}, err
	}
	return llmTimingFromPipelineEvents(events, time.Now().UTC()), nil
}

func llmTimingFromPipelineEvents(events []*PipelineEvent, now time.Time) LLMTiming {
	var startAt time.Time
	var haveStart bool
	var endAt time.Time
	var haveEnd bool
	for _, e := range events {
		if e == nil {
			continue
		}
		switch e.Stage {
		case "llm_request":
			if strings.TrimSpace(e.Outcome) != "error" {
				startAt = e.CreatedAt
				haveStart = true
				haveEnd = false
			}
		case "llm_call":
			if haveStart && e.CreatedAt.After(startAt) {
				endAt = e.CreatedAt
				haveEnd = true
			}
		case "llm_done":
			if haveStart && e.CreatedAt.After(startAt) {
				endAt = e.CreatedAt
				haveEnd = true
			}
		}
	}
	if haveStart && haveEnd {
		dur := endAt.Sub(startAt)
		if dur < 0 {
			dur = 0
		}
		return LLMTiming{DurationSec: dur.Seconds(), InProgress: false}
	}
	if haveStart {
		dur := now.Sub(startAt)
		if dur < 0 {
			dur = 0
		}
		return LLMTiming{DurationSec: dur.Seconds(), InProgress: true}
	}
	return LLMTiming{}
}

// LLMInfoForProject parses the latest successful llm_call snippet.
func (d *DB) LLMInfoForProject(projectID int64) (*LLMInfo, error) {
	events, err := d.GetEvents(projectID)
	if err != nil {
		return nil, err
	}
	var snippet string
	for i := len(events) - 1; i >= 0; i-- {
		e := events[i]
		if e == nil || e.Stage != "llm_call" {
			continue
		}
		if strings.TrimSpace(e.Outcome) == "error" {
			continue
		}
		snippet = strings.TrimSpace(e.ResultSnippet)
		if snippet != "" {
			break
		}
	}
	if snippet == "" {
		return nil, nil
	}
	var info LLMInfo
	if err := json.Unmarshal([]byte(snippet), &info); err != nil {
		return &LLMInfo{Model: snippet}, nil
	}
	return &info, nil
}

// LastPipelineError returns the message from the most recent error outcome event.
func (d *DB) LastPipelineError(projectID int64) (string, error) {
	events, err := d.GetEvents(projectID)
	if err != nil {
		return "", err
	}
	for i := len(events) - 1; i >= 0; i-- {
		e := events[i]
		if e == nil {
			continue
		}
		if strings.TrimSpace(e.Outcome) == "error" {
			return e.Message, nil
		}
		if e.Stage == "error" || strings.Contains(strings.ToLower(e.Message), "cancelled") {
			return e.Message, nil
		}
	}
	return "", nil
}

// CancelProjectProcessing marks a project as error with a cancellation message.
func (d *DB) CancelProjectProcessing(ctx context.Context, projectID int64, reason string) error {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		reason = "cancelled by user"
	}
	if err := d.AddEventEx(projectID, "error", reason, "error", ""); err != nil {
		return fmt.Errorf("cancel event: %w", err)
	}
	return d.UpdateProjectStatus(projectID, "error")
}
