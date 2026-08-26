package companyview

import (
	"regexp"
	"strconv"
	"strings"

	"megane/internal/edgar/financials"
)

// EventHighlight is a best-effort financial callout attached to
// quarterly_results/annual_report events. Nil when nothing could be parsed.
type EventHighlight struct {
	Metrics []HighlightMetric `json:"metrics"`
	Source  string            `json:"source"` // "financials" | "summary_parse"
}

// HighlightMetric is one label/value pair rendered in the event modal.
type HighlightMetric struct {
	Label string `json:"label"`
	Value string `json:"value"`
}

// numThenWord matches "23% ... growth" (filler words, incl. ones with digits
// like "6-MONTH", up to 4 deep).
var numThenWord = regexp.MustCompile(`(?i)(\d+)%\s+(?:[\w-]+\s+){0,4}?(growth|increase|decrease)`)

// wordOfNum matches "growth of 23%".
var wordOfNum = regexp.MustCompile(`(?i)(growth|increase|decrease)\s+of\s+(\d+)%`)

// decreaseWord flips the metric label when the claim is negative.
var decreaseWord = regexp.MustCompile(`(?i)decrease`)

// BuildHighlights extracts a growth-percentage claim from a filing summary
// when the category is one of the two financial-results categories. Returns
// nil when no percentage is found — callers must not fabricate a metric.
//
// This is intentionally narrow: the corpus's quarterly_results/annual_report
// `summary` field is a press-release headline, not body prose — zero of the 66
// real Kamada financial-results summaries contain a dollar figure, and only 4
// contain a percentage. Only a growth/increase/decrease percentage is ever
// extracted.
//
// Two word orders occur in the corpus ("GROWTH OF 23%" and "11% ... GROWTH"),
// so both patterns are tried and the leftmost match wins.
// HighlightsFromFinancials maps extracted statement figures onto the modal's
// metric list, in MetricRank order.
//
// Only lines that pass Line.Publishable are rendered. A metric the extractor
// could not verify is omitted entirely rather than shown with a caveat: a wrong
// figure of the right magnitude is the failure this whole path exists to
// prevent. Returns nil when nothing publishable survives, so the caller falls
// through to the summary parser and then to "unavailable".
func HighlightsFromFinancials(f *financials.FilingFinancials) *EventHighlight {
	if f == nil {
		return nil
	}
	byKey := map[string]financials.Line{}
	for _, l := range f.Publishable() {
		if _, seen := byKey[l.Key]; !seen {
			byKey[l.Key] = l
		}
	}
	var metrics []HighlightMetric
	for _, key := range financials.MetricRank {
		l, ok := byKey[key]
		if !ok {
			continue
		}
		value := l.ValueFmt
		switch {
		case l.YoYPct != nil:
			value += " (" + signedPct(*l.YoYPct) + " YoY)"
		case l.PriorLabel != "" && l.PriorValue != nil:
			value += " (" + l.PriorLabel + ": " + financials.FormatValue(*l.PriorValue, l.Unit) + ")"
		}
		metrics = append(metrics, HighlightMetric{Label: l.Display, Value: value})
	}
	if len(metrics) == 0 {
		return nil
	}
	return &EventHighlight{Metrics: metrics, Source: "financials"}
}

func signedPct(v float64) string {
	s := strconv.FormatFloat(v, 'f', 1, 64)
	if !strings.HasPrefix(s, "-") {
		s = "+" + s
	}
	return s + "%"
}

func BuildHighlights(category, summary string, fin *financials.FilingFinancials) *EventHighlight {
	if category != "quarterly_results" && category != "annual_report" {
		return nil
	}
	// Extracted figures beat a headline regex whenever they exist and cleared
	// the extractor's gate.
	if h := HighlightsFromFinancials(fin); h != nil {
		return h
	}
	if summary == "" {
		return nil
	}

	loc1 := numThenWord.FindStringSubmatchIndex(summary)
	loc2 := wordOfNum.FindStringSubmatchIndex(summary)

	var pct, word string
	switch {
	case loc1 != nil && (loc2 == nil || loc1[0] <= loc2[0]):
		pct = summary[loc1[2]:loc1[3]]
		word = summary[loc1[4]:loc1[5]]
	case loc2 != nil:
		word = summary[loc2[2]:loc2[3]]
		pct = summary[loc2[4]:loc2[5]]
	default:
		return nil
	}

	label := "Growth"
	if decreaseWord.MatchString(word) {
		label = "Decline"
	}
	return &EventHighlight{
		Metrics: []HighlightMetric{{Label: label, Value: pct + "%"}},
		Source:  "summary_parse",
	}
}
