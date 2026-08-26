// Package financials extracts structured financial figures from SEC filings that
// ship XBRL data, and writes them next to the filing as financials.json.
//
// The governing rule of this package: a wrong number of the right order of
// magnitude is worse than no number. Every metric is read twice from independent
// sources, screened by six anomaly detectors, and withheld unless it clears the
// gate in verify.go. See prompts/dev/prompt_8_extracting_info_from_filings-lld.md.
package financials

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"time"
)

// SchemaVersion is written into every financials.json so readers can detect a
// future schema bump without guessing.
const SchemaVersion = 1

// FileName is the artifact written beside meta.json in an accession folder.
const FileName = "financials.json"

// Confidence levels. Only ConfidenceVerified and ConfidenceSingleSource are ever
// rendered; ConfidenceSuspect is persisted so a withheld metric stays auditable
// from the JSON alone.
const (
	ConfidenceVerified     = "verified"
	ConfidenceSingleSource = "single_source"
	ConfidenceSuspect      = "suspect"
)

// Source values for a Line.
const (
	SourceInstance    = "xbrl_instance"
	SourceRendered    = "rendered_html"
	SourceBoth        = "both"
	SourceAdjudicated = "llm_adjudicated"
	// SourceCompanyFacts marks a line gap-filled from SEC's Company Facts API.
	SourceCompanyFacts = "sec_companyfacts"
	// SourceNone marks an artifact recording that no source holds facts for this
	// accession: its statements are empty and nothing is publishable. It exists so
	// an absent figure is diagnosable rather than merely missing.
	SourceNone = "none"
)

// Unit values. Facts in any other unit (for example the NIS-denominated facts in
// some 20-F filings) are excluded rather than converted.
const (
	UnitUSD         = "USD"
	UnitUSDPerShare = "USD/share"
)

// FilingFinancials is the whole artifact.
type FilingFinancials struct {
	Version     int           `json:"version"`
	ExtractedAt time.Time     `json:"extractedAt"`
	Accession   string        `json:"accession"`
	Currency    string        `json:"currency,omitempty"`
	Period      Period        `json:"period"`
	Statements  StatementSets `json:"statements"`
	ParseNotes  []string      `json:"parseNotes,omitempty"`
}

// Period is the reporting period the filing declares for itself via the dei
// facts, rather than anything inferred from a table header.
type Period struct {
	EndDate  string `json:"endDate"`
	Label    string `json:"label"`
	Duration string `json:"duration"` // P3M | P9M | P1Y
	Focus    string `json:"focus"`    // Q1..Q4 | FY
}

// StatementSets groups extracted lines by statement.
type StatementSets struct {
	Income   []Line    `json:"income,omitempty"`
	Balance  []Line    `json:"balance,omitempty"`
	CashFlow []Line    `json:"cashFlow,omitempty"`
	Segments []Segment `json:"segments,omitempty"`
}

// Line is one extracted metric.
type Line struct {
	Key        string   `json:"key"`
	Display    string   `json:"display"`
	Element    string   `json:"element"`
	Value      float64  `json:"value"`
	ValueFmt   string   `json:"valueFmt"`
	Unit       string   `json:"unit"`
	PriorValue *float64 `json:"priorValue,omitempty"`
	PriorLabel string   `json:"priorLabel,omitempty"`
	YoYPct     *float64 `json:"yoyPct,omitempty"`
	Confidence string   `json:"confidence"`
	Source     string   `json:"source"`
	Notes      []string `json:"notes,omitempty"`
}

// Segment is one reportable segment's revenue, read from a dimensioned context.
type Segment struct {
	Name     string  `json:"name"`
	Member   string  `json:"member"`
	Value    float64 `json:"value"`
	ValueFmt string  `json:"valueFmt"`
}

// Publishable reports whether a line may be rendered to a user. This is the one
// place that decision is made; callers must not compare Confidence themselves.
func (l Line) Publishable() bool {
	return l.Confidence == ConfidenceVerified || l.Confidence == ConfidenceSingleSource
}

// Publishable returns the income+balance lines that may be rendered, in the
// order they were stored (MetricRank order, applied by extract.go).
func (f *FilingFinancials) Publishable() []Line {
	if f == nil {
		return nil
	}
	var out []Line
	for _, set := range [][]Line{f.Statements.Income, f.Statements.Balance, f.Statements.CashFlow} {
		for _, l := range set {
			if l.Publishable() {
				out = append(out, l)
			}
		}
	}
	return out
}

// FormatValue renders a value for display: SI shorthand at or above one million,
// two decimals for per-share amounts.
func FormatValue(v float64, unit string) string {
	if unit == UnitUSDPerShare {
		return fmt.Sprintf("$%.2f", v)
	}
	abs := math.Abs(v)
	sign := ""
	if v < 0 {
		sign = "-"
	}
	switch {
	case abs >= 1e9:
		return fmt.Sprintf("%s$%.1fB", sign, abs/1e9)
	case abs >= 1e6:
		return fmt.Sprintf("%s$%.1fM", sign, abs/1e6)
	case abs >= 1e3:
		return fmt.Sprintf("%s$%.1fK", sign, abs/1e3)
	default:
		return fmt.Sprintf("%s$%.0f", sign, abs)
	}
}

// Write marshals f to dir/financials.json. Field order is fixed by the struct
// declaration and slice order is fixed by the caller, so output is byte-stable
// across runs — a re-extract produces a diffable file.
func Write(dir string, f *FilingFinancials) error {
	b, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return fmt.Errorf("financials: marshal %s: %w", f.Accession, err)
	}
	b = append(b, '\n')
	path := filepath.Join(dir, FileName)
	if err := os.WriteFile(path, b, 0o644); err != nil {
		return fmt.Errorf("financials: write %s: %w", path, err)
	}
	return nil
}

// Load reads dir/financials.json. A missing file is not an error: it is the
// normal case for the overwhelming majority of accessions, so Load returns
// (nil, nil) and callers treat that as "no financials".
func Load(dir string) (*FilingFinancials, error) {
	b, err := os.ReadFile(filepath.Join(dir, FileName))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var f FilingFinancials
	if err := json.Unmarshal(b, &f); err != nil {
		return nil, fmt.Errorf("financials: parse %s: %w", dir, err)
	}
	return &f, nil
}
