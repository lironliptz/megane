package financials

import (
	"context"
	"encoding/json"
	"fmt"
	"html"
	"log/slog"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"megane/internal/llm"
)

// DefaultAdjudicatorModel is the weakest model sufficient for the task.
// Adjudication is a constrained multiple-choice decision over one short quoted
// passage — no extraction, no arithmetic, no long-context reasoning — so the
// lite tier is the right size rather than a compromise.
const DefaultAdjudicatorModel = "gemini-3.5-flash-lite"

// PromptKey is the pipeline.LoadPrompts key for the adjudicator prompt
// (prompts/edgar_financials_adjudicate.txt).
const PromptKey = "edgar_financials_adjudicate"

// maxEvidenceDefault caps the passage sent per adjudication.
const maxEvidenceDefault = 4000

// LLMAdjudicator implements Adjudicator over the shared llm.Client.
type LLMAdjudicator struct {
	Client      llm.Client
	Model       string
	Prompt      string
	MaxEvidence int
}

var responseSchema = map[string]interface{}{
	"type": "object",
	"properties": map[string]interface{}{
		"choice":        map[string]interface{}{"type": "string"},
		"value":         map[string]interface{}{"type": "number"},
		"evidenceQuote": map[string]interface{}{"type": "string"},
		"reasoning":     map[string]interface{}{"type": "string"},
		"confidence":    map[string]interface{}{"type": "string"},
	},
	"required": []string{"choice", "evidenceQuote", "reasoning", "confidence"},
}

// Adjudicate asks the model to choose among candidates and then verifies the
// answer. Validation is what makes a fabricated number unpublishable rather than
// merely unlikely:
//
//   - choice must be one of the supplied ids, or "none";
//   - value must equal that candidate's value exactly;
//   - evidenceQuote must appear verbatim in the evidence that was sent.
//
// Any violation is treated as "none", i.e. the metric stays withheld.
func (a *LLMAdjudicator) Adjudicate(ctx context.Context, req AdjudicationRequest) (*AdjudicationResult, error) {
	if a.Client == nil || strings.TrimSpace(a.Prompt) == "" {
		return nil, fmt.Errorf("financials: adjudicator not configured")
	}
	model := a.Model
	if model == "" {
		model = DefaultAdjudicatorModel
	}

	payload, err := json.MarshalIndent(map[string]interface{}{
		"metric":     req.Metric,
		"period":     req.Period,
		"candidates": req.Candidates,
		"evidence":   req.Evidence,
	}, "", "  ")
	if err != nil {
		return nil, err
	}

	resp, err := a.Client.Complete(ctx, llm.Request{
		SystemPrompt: a.Prompt,
		UserPrompt:   string(payload),
		Model:        model,
		Schema:       responseSchema,
		Log: &llm.RequestLog{
			SourceFile:  req.Accession,
			PromptFiles: []string{PromptKey},
		},
	})
	if err != nil {
		return nil, err
	}

	var out AdjudicationResult
	raw := resp.Text
	if resp.JSON != nil {
		b, _ := json.Marshal(resp.JSON)
		raw = string(b)
	}
	if err := json.Unmarshal([]byte(strings.TrimSpace(stripFence(raw))), &out); err != nil {
		slog.Warn("financials: adjudicator returned unparseable JSON",
			"accession", req.Accession, "metric", req.Metric, "err", err)
		return &AdjudicationResult{Choice: "none"}, nil
	}

	if !validateAdjudication(&out, req) {
		return &AdjudicationResult{Choice: "none"}, nil
	}
	slog.Info("financials: adjudicated",
		"accession", req.Accession, "metric", req.Metric,
		"choice", out.Choice, "confidence", out.Confidence, "model", model)
	return &out, nil
}

// validateAdjudication enforces the three structural checks above.
func validateAdjudication(out *AdjudicationResult, req AdjudicationRequest) bool {
	if strings.EqualFold(out.Choice, "none") {
		return false
	}
	var chosen *AdjudicationCandidate
	for i := range req.Candidates {
		if req.Candidates[i].ID == out.Choice {
			chosen = &req.Candidates[i]
			break
		}
	}
	if chosen == nil {
		slog.Warn("financials: adjudicator chose an unknown candidate",
			"accession", req.Accession, "choice", out.Choice)
		return false
	}
	if math.Abs(chosen.Value-out.Value) > 1e-9 {
		slog.Warn("financials: adjudicator value does not match its chosen candidate",
			"accession", req.Accession, "choice", out.Choice,
			"candidate", chosen.Value, "returned", out.Value)
		return false
	}
	q := strings.TrimSpace(out.EvidenceQuote)
	if q == "" || !strings.Contains(normalizeWS(req.Evidence), normalizeWS(q)) {
		slog.Warn("financials: adjudicator quote is not verbatim in the supplied evidence",
			"accession", req.Accession, "metric", req.Metric)
		return false
	}
	if strings.EqualFold(out.Confidence, "low") {
		return false
	}
	return true
}

func normalizeWS(s string) string { return wsRe.ReplaceAllString(strings.TrimSpace(s), " ") }

func stripFence(s string) string {
	s = strings.TrimSpace(s)
	if !strings.HasPrefix(s, "```") {
		return s
	}
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[i+1:]
	}
	return strings.TrimSuffix(strings.TrimSpace(s), "```")
}

// --- evidence ---------------------------------------------------------------

var (
	sentenceSplit = regexp.MustCompile(`(?:[.!?])\s+`)
	explicitUnit  = regexp.MustCompile(`(?i)\$\s?[\d.,]+\s*(million|billion)|per share`)
	bareNumberRun = regexp.MustCompile(`\d[\d,.]*\s+\d[\d,.]*\s+\d[\d,.]*`)
)

var metricKeywords = map[string][]string{
	KeyTotalRevenues: {"total revenues", "revenues", "revenue"},
	KeyNetIncome:     {"net income", "net loss", "net earnings"},
	KeyEPSBasic:      {"per share"},
	KeyCash:          {"cash and cash equivalents", "cash position"},
}

// gatherEvidence pulls unit-explicit prose sentences about one metric from the
// accession's EX-99 exhibits.
//
// Prose only, deliberately. The same exhibits contain flattened statement tables
// whose numbers are unscaled and unit-less — exactly the ambiguity under dispute
// — so a passage with a run of bare numbers is rejected rather than sent.
func gatherEvidence(dir, key string, limit int) string {
	if limit <= 0 {
		limit = maxEvidenceDefault
	}
	kws, ok := metricKeywords[key]
	if !ok {
		return ""
	}
	paths, _ := filepath.Glob(filepath.Join(dir, "*ex99*.htm"))
	more, _ := filepath.Glob(filepath.Join(dir, "*EX99*.htm"))
	paths = append(paths, more...)

	var kept []string
	total := 0
	for _, p := range paths {
		b, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		text := cellText(scriptStyleRe.ReplaceAllString(string(b), ""))
		text = html.UnescapeString(text)
		for _, sent := range sentenceSplit.Split(text, -1) {
			sent = strings.TrimSpace(sent)
			if sent == "" || !explicitUnit.MatchString(sent) {
				continue
			}
			if bareNumberRun.MatchString(sent) {
				continue // a flattened table, not prose
			}
			low := strings.ToLower(sent)
			hit := false
			for _, kw := range kws {
				if strings.Contains(low, kw) {
					hit = true
					break
				}
			}
			if !hit {
				continue
			}
			if total+len(sent) > limit {
				return strings.Join(kept, " ")
			}
			kept = append(kept, sent+".")
			total += len(sent)
		}
	}
	return strings.Join(kept, " ")
}

// adjudicateWithEvidence offers every suspect line to the adjudicator.
func adjudicateWithEvidence(ctx context.Context, a Adjudicator, dir string, f *FilingFinancials) {
	limit := maxEvidenceDefault
	if v := os.Getenv("EDGAR_FINANCIALS_LLM_MAX_EVIDENCE"); v != "" {
		fmt.Sscanf(v, "%d", &limit)
	}
	for _, set := range [][]Line{f.Statements.Income, f.Statements.Balance, f.Statements.CashFlow} {
		for i := range set {
			l := &set[i]
			if l.Confidence != ConfidenceSuspect {
				continue
			}
			cands := candidatesFor(*l)
			if len(cands) < 2 {
				continue
			}
			ev := gatherEvidence(dir, l.Key, limit)
			if ev == "" {
				// No qualifying passage: withhold rather than ask the model to
				// choose without a source.
				l.Notes = append(l.Notes, "adjudication skipped: no unit-explicit evidence")
				continue
			}
			res, err := a.Adjudicate(ctx, AdjudicationRequest{
				Accession:  f.Accession,
				Metric:     l.Display,
				Period:     f.Period.Label,
				Unit:       l.Unit,
				Candidates: cands,
				Evidence:   ev,
			})
			if err != nil {
				l.Notes = append(l.Notes, fmt.Sprintf("adjudication failed: %v", err))
				continue
			}
			if res == nil || strings.EqualFold(res.Choice, "none") {
				l.Notes = append(l.Notes, "adjudication declined; metric withheld")
				continue
			}
			l.Value = res.Value
			l.ValueFmt = FormatValue(res.Value, l.Unit)
			l.Confidence = ConfidenceVerified
			l.Source = SourceAdjudicated
			l.Notes = append(l.Notes,
				"adjudicated: "+res.EvidenceQuote,
				"model: "+DefaultAdjudicatorModel)
		}
	}
}

// candidatesFor rebuilds the competing readings for a suspect line. The
// as-tagged value and the thousands-scaled reading are the two that a
// mis-tagged filing puts in dispute.
func candidatesFor(l Line) []AdjudicationCandidate {
	if l.Unit != UnitUSD {
		return nil
	}
	return []AdjudicationCandidate{
		{ID: "A", Value: l.Value, UnitBasis: "as_tagged_usd", Source: l.Source},
		{ID: "B", Value: l.Value * 1000, UnitBasis: "assumed_thousands", Source: l.Source},
	}
}
