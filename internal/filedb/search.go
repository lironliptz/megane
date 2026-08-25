package filedb

import (
	"sort"
	"strings"
)

// MaxSearchResults caps what SearchCompanies and RankCompanies will return.
const MaxSearchResults = 20

// SearchCandidate is one company as seen by the ranker. It carries FormerNames,
// which are matchable but not part of the returned CompanySummary.
type SearchCandidate struct {
	CompanySummary
	FormerNames []string
}

// Score ladder. First rule that matches wins; a candidate matching no rule is
// excluded from results entirely.
const (
	scoreExactTicker     = 100
	scoreNamePrefix      = 90
	scoreNameTokenPrefix = 80
	scoreNameSubstring   = 70
	scoreFormerName      = 60
	scoreCIKPrefix       = 50
)

// RankCompanies scores candidates against a free-text query and returns the
// best matches, highest score first, ties broken by name then CIK.
//
// It is pure and exported so ranking can be table-tested without a filesystem.
// An empty query returns nil. limit <= 0 or > MaxSearchResults clamps to
// MaxSearchResults.
func RankCompanies(cands []SearchCandidate, query string, limit int) []CompanySummary {
	q := strings.ToLower(strings.TrimSpace(query))
	if q == "" {
		return nil
	}
	if limit <= 0 || limit > MaxSearchResults {
		limit = MaxSearchResults
	}

	type scored struct {
		c CompanySummary
		s int
	}
	matches := make([]scored, 0, len(cands))
	for _, cand := range cands {
		if s := scoreCandidate(cand, q); s > 0 {
			matches = append(matches, scored{c: cand.CompanySummary, s: s})
		}
	}

	sort.SliceStable(matches, func(i, j int) bool {
		if matches[i].s != matches[j].s {
			return matches[i].s > matches[j].s
		}
		if matches[i].c.Name != matches[j].c.Name {
			return matches[i].c.Name < matches[j].c.Name
		}
		return matches[i].c.CIK < matches[j].c.CIK
	})

	if len(matches) > limit {
		matches = matches[:limit]
	}
	out := make([]CompanySummary, 0, len(matches))
	for _, m := range matches {
		out = append(out, m.c)
	}
	return out
}

// scoreCandidate returns the best (highest) rule score for one candidate, or 0
// when nothing matches. q is already lowercased and trimmed.
func scoreCandidate(cand SearchCandidate, q string) int {
	for _, tk := range cand.Tickers {
		if strings.EqualFold(tk, q) {
			return scoreExactTicker
		}
	}

	name := strings.ToLower(cand.Name)
	switch {
	case strings.HasPrefix(name, q):
		return scoreNamePrefix
	case hasTokenPrefix(name, q):
		return scoreNameTokenPrefix
	case strings.Contains(name, q):
		return scoreNameSubstring
	}

	for _, fn := range cand.FormerNames {
		if strings.Contains(strings.ToLower(fn), q) {
			return scoreFormerName
		}
	}

	if isAllDigits(q) && strings.HasPrefix(trimCIKZeros(cand.CIK), trimCIKZeros(q)) {
		return scoreCIKPrefix
	}
	return 0
}

// hasTokenPrefix reports whether any whitespace-delimited token of name starts
// with q, so "kamada" matches "ACME KAMADA HOLDINGS".
func hasTokenPrefix(name, q string) bool {
	for _, tok := range strings.Fields(name) {
		if strings.HasPrefix(tok, q) {
			return true
		}
	}
	return false
}

func isAllDigits(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}
