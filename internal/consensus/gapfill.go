package consensus

import "context"

// GapFillResult describes whether a nullable field was filled from an allowlisted web source.
type GapFillResult struct {
	Filled bool
	Source string
	URL    string
	Note   string
}

// GapFill is a placeholder for Tier 3 allowlisted web gap-fill (P1).
func GapFill(_ context.Context, _ string, _ string) (*GapFillResult, error) {
	return &GapFillResult{Filled: false, Note: "gap-fill not implemented"}, nil
}
