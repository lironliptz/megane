package companyview

import (
	"go/ast"
	"go/parser"
	"go/token"
	"testing"
)

func TestCategoryRulesTable(t *testing.T) {
	tests := []struct {
		category string
		weight   Weight
		isEvent  bool
	}{
		{"quarterly_results", WeightMajor, true},
		{"business_deal", WeightMajor, true},
		{"annual_report", WeightMajor, true},
		{"regulatory_clinical", WeightMajor, true},
		{"annual_guidance", WeightMajor, true},
		{"corporate_action", WeightMedium, true},
		{"current_report", WeightMedium, false},
		{"governance", WeightMedium, false},
		{"admin_update", WeightMedium, false},
		{"earnings_preview", WeightMinor, false},
		{"insider_trade", WeightMinor, false},
		{"insider_initial", WeightMinor, false},
		{"investor_relations", WeightMinor, false},
		{"ownership_disclosure", WeightMinor, false},
		{"unknown", WeightMinor, false},
	}
	if len(tests) != len(categoryRules) {
		t.Fatalf("table has %d categories, test covers %d — keep them in sync", len(categoryRules), len(tests))
	}
	for _, tt := range tests {
		if got := WeightFor(tt.category); got != tt.weight {
			t.Errorf("WeightFor(%q) = %q, want %q", tt.category, got, tt.weight)
		}
		if got := IsEvent(tt.category); got != tt.isEvent {
			t.Errorf("IsEvent(%q) = %v, want %v", tt.category, got, tt.isEvent)
		}
	}
}

// The measured corpus distribution. If the table drifts, these break.
func TestWeightsReproduceMeasuredSplit(t *testing.T) {
	corpus := map[string]int{
		"current_report": 80, "unknown": 78, "quarterly_results": 55, "governance": 33,
		"earnings_preview": 30, "insider_trade": 26, "business_deal": 23, "insider_initial": 20,
		"annual_report": 11, "investor_relations": 9, "regulatory_clinical": 8,
		"ownership_disclosure": 4, "corporate_action": 3, "admin_update": 1,
	}
	byWeight := map[Weight]int{}
	events, total := 0, 0
	for cat, n := range corpus {
		byWeight[WeightFor(cat)] += n
		total += n
		if IsEvent(cat) {
			events += n
		}
	}
	if total != 381 {
		t.Fatalf("fixture total = %d, want 381", total)
	}
	if byWeight[WeightMajor] != 97 || byWeight[WeightMedium] != 117 || byWeight[WeightMinor] != 167 {
		t.Errorf("weights = major %d / medium %d / minor %d, want 97/117/167",
			byWeight[WeightMajor], byWeight[WeightMedium], byWeight[WeightMinor])
	}
	// The whole point of D2: the curated set must be a small fraction of filings.
	if events != 100 {
		t.Errorf("IsEvent count = %d, want 100 (CORE); the idea file's rule matched 244", events)
	}
	if events*100/total > 30 {
		t.Errorf("curated events are %d%% of filings — not \"much less than total\"", events*100/total)
	}
}

func TestUnknownCategoryDefaults(t *testing.T) {
	if got := WeightFor("some_future_category"); got != WeightMinor {
		t.Errorf("unknown category weight = %q, want minor", got)
	}
	if IsEvent("some_future_category") {
		t.Error("unknown category must not be a curated event")
	}
	if got := WhyItMatters("some_future_category"); got != "" {
		t.Errorf("unknown category why = %q, want empty", got)
	}
}

func TestCuratedEventsHaveWhyText(t *testing.T) {
	for cat := range categoryRules {
		if IsEvent(cat) && WhyItMatters(cat) == "" {
			t.Errorf("curated event %q has no \"why this matters\" text", cat)
		}
		if !IsEvent(cat) && WhyItMatters(cat) != "" {
			t.Errorf("non-event %q carries why text %q", cat, WhyItMatters(cat))
		}
	}
}

// Guard for design decision D1: tier is a function of category, so no
// classification rule may read it as an independent signal.
func TestNoClassificationRuleReadsTier(t *testing.T) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "classify.go", nil, 0)
	if err != nil {
		t.Fatalf("parsing classify.go: %v", err)
	}
	ast.Inspect(f, func(n ast.Node) bool {
		sel, ok := n.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		name := sel.Sel.Name
		if name == "Tier" || name == "TierLabel" || name == "HasFinancials" {
			t.Errorf("classify.go reads %q — tier and hasFinancials are functions of category (D1); "+
				"using them as a separate term double-counts the same signal", name)
		}
		return true
	})
	// Note: only the AST is inspected, never the raw source — the package doc
	// deliberately quotes the bad rule ("tier <= 2 OR category in {...}") to
	// explain why it is wrong, and a text scan would flag that comment.
}
