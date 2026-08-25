// Package companyview builds the tabbed company view's derived data: filing
// event weights, the curated "special events" set, and timeline assembly.
//
// All classification here keys on meta.json's `category` and nothing else.
// Measured across the sample corpus (381 filings, zero exceptions), `tier`,
// `tierLabel`, and `signals.hasFinancials` are pure functions of `category` —
// python/edgar/build_meta.py:_classify returns (tier, category, tags) as a fixed
// pair at every return site, and hasFinancials is literally
// `category in ("annual_report", "quarterly_results")`.
//
// A rule of the form `tier <= 2 OR category in {...}` therefore double-counts
// the same signal. That is why the design's curation rule keys on category
// alone; see prompt_3_company_view-hld.md D1.
package companyview

import (
	"log/slog"
	"sync"
)

// Weight is an event's visual prominence on the timeline.
type Weight string

const (
	WeightMajor  Weight = "major"
	WeightMedium Weight = "medium"
	WeightMinor  Weight = "minor"
)

// categoryRule is the single source of investor-relevance judgment.
type categoryRule struct {
	Weight  Weight
	IsEvent bool
	Why     string // "Why this matters" template for the special-events tab
}

// categoryRules is the frozen table from the LLD.
//
// Weight reproduces the measured 97 major / 117 medium / 167 minor split.
// IsEvent reproduces the measured 100-filing CORE set (5–13 per calendar year).
//
// annual_guidance exists in the classifier (build_meta.py:132) but has zero
// occurrences in the sample corpus; it is listed so this table is complete
// against the classifier rather than against one company.
var categoryRules = map[string]categoryRule{
	"quarterly_results":    {WeightMajor, true, "Reported quarterly financial results"},
	"business_deal":        {WeightMajor, true, "Announced a commercial agreement or transaction"},
	"annual_report":        {WeightMajor, true, "Filed its annual report"},
	"regulatory_clinical":  {WeightMajor, true, "Regulatory or clinical milestone"},
	"annual_guidance":      {WeightMajor, true, "Issued forward guidance"},
	"corporate_action":     {WeightMedium, true, "Dividend or other corporate action"},
	"current_report":       {WeightMedium, false, ""},
	"governance":           {WeightMedium, false, ""},
	"admin_update":         {WeightMedium, false, ""},
	"earnings_preview":     {WeightMinor, false, ""},
	"insider_trade":        {WeightMinor, false, ""},
	"insider_initial":      {WeightMinor, false, ""},
	"investor_relations":   {WeightMinor, false, ""},
	"ownership_disclosure": {WeightMinor, false, ""},
	"unknown":              {WeightMinor, false, ""},
}

var (
	unknownCategoryOnce sync.Map // category -> *sync.Once, so each new value warns once
)

// ruleFor looks up a category, defaulting unknown values to minor/not-an-event
// and logging each unrecognized value exactly once.
func ruleFor(category string) categoryRule {
	if r, ok := categoryRules[category]; ok {
		return r
	}
	o, _ := unknownCategoryOnce.LoadOrStore(category, &sync.Once{})
	o.(*sync.Once).Do(func() {
		slog.Warn("companyview: unrecognized filing category, defaulting to minor",
			"category", category)
	})
	return categoryRule{Weight: WeightMinor, IsEvent: false}
}

// WeightFor returns the timeline marker weight for a filing category.
func WeightFor(category string) Weight { return ruleFor(category).Weight }

// IsEvent reports whether a category belongs in the curated special-events tab.
func IsEvent(category string) bool { return ruleFor(category).IsEvent }

// WhyItMatters returns the one-line explanation shown on an event card, or ""
// when the category is not a curated event.
func WhyItMatters(category string) string { return ruleFor(category).Why }

// KnownCategories returns every category the table covers. Test/diagnostic use.
func KnownCategories() []string {
	out := make([]string, 0, len(categoryRules))
	for c := range categoryRules {
		out = append(out, c)
	}
	return out
}
