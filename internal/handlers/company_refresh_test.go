package handlers

import "testing"

func TestParseProgressLine(t *testing.T) {
	stage, done, total, ok := parseProgressLine("MEGANE_PROGRESS sec 3 12")
	if !ok {
		t.Fatal("expected progress line to parse")
	}
	if stage != "sec" || done != 3 || total != 12 {
		t.Fatalf("unexpected parse: stage=%q done=%d total=%d", stage, done, total)
	}
}

func TestParseProgressLineRejectsInvalid(t *testing.T) {
	cases := []string{
		"",
		"MEGANE_PROGRESS sec foo 12",
		"MEGANE_PROGRESS sec 3 0",
		"PROGRESS sec 3 12",
	}
	for _, tc := range cases {
		if _, _, _, ok := parseProgressLine(tc); ok {
			t.Fatalf("expected parse failure for %q", tc)
		}
	}
}

func TestWeightedProgress(t *testing.T) {
	got := weightedProgress(10, 45, 9, 10)
	if got != 50 {
		t.Fatalf("weightedProgress = %d, want 50", got)
	}
}
