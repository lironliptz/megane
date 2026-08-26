package financials

import "context"

// AdjudicationCandidate is one deterministic reading offered to the adjudicator.
type AdjudicationCandidate struct {
	ID        string  `json:"id"`
	Value     float64 `json:"value"`
	UnitBasis string  `json:"unitBasis"`
	Source    string  `json:"source"`
}

// AdjudicationRequest is one disputed metric.
type AdjudicationRequest struct {
	Accession  string
	Metric     string
	Period     string
	Unit       string
	Candidates []AdjudicationCandidate
	Evidence   string
}

// AdjudicationResult is the model's decision, after validation.
type AdjudicationResult struct {
	Choice        string  `json:"choice"`
	Value         float64 `json:"value"`
	EvidenceQuote string  `json:"evidenceQuote"`
	Reasoning     string  `json:"reasoning"`
	Confidence    string  `json:"confidence"`
}

// Adjudicator resolves a metric the deterministic readers could not settle.
//
// An implementation must never author a value: it chooses among the supplied
// candidates or declines. See adjudicate_llm.go for the enforcement.
type Adjudicator interface {
	Adjudicate(ctx context.Context, req AdjudicationRequest) (*AdjudicationResult, error)
}

// adjudicateFiling is a placeholder wired in ExtractAll; the LLM implementation
// and its evidence gathering live in adjudicate_llm.go.
func adjudicateFiling(ctx context.Context, a Adjudicator, dir string, f *FilingFinancials) {
	adjudicateWithEvidence(ctx, a, dir, f)
}
