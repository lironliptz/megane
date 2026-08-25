package handlers

import "testing"

func TestPipelineStatusPct(t *testing.T) {
	tests := []struct {
		status string
		want   int
	}{
		{"uploaded", 8},
		{"converting", 28},
		{"llm_pending", 45},
		{"complete", 100},
		{"error", 100},
		{"unknown", 5},
	}
	for _, tt := range tests {
		if got := PipelineStatusPct(tt.status); got != tt.want {
			t.Errorf("PipelineStatusPct(%q) = %d, want %d", tt.status, got, tt.want)
		}
	}
}

func TestPipelineStatusLabel(t *testing.T) {
	if got := PipelineStatusLabel("llm_pending"); got != "Analyzing with LLM" {
		t.Errorf("got %q", got)
	}
}
