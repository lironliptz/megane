package filedb

import "testing"

func cand(cik, name string, tickers []string, former ...string) SearchCandidate {
	return SearchCandidate{
		CompanySummary: CompanySummary{CIK: cik, Name: name, Tickers: tickers, Exchanges: []string{"Nasdaq"}},
		FormerNames:    former,
	}
}

func corpus() []SearchCandidate {
	return []SearchCandidate{
		cand("0001567529", "KAMADA LTD", []string{"KMDA"}),
		cand("0000320193", "APPLE INC", []string{"AAPL"}),
		cand("0001111111", "ACME KAMADA HOLDINGS", []string{"AKH"}),
		cand("0002222222", "GLOBAL KMDA PARTNERS", []string{"GKP"}),
		cand("0001567000", "ZETA CORP", []string{"ZTA"}, "Kamada Systems Inc"),
	}
}

func names(rows []CompanySummary) []string {
	out := make([]string, len(rows))
	for i, r := range rows {
		out[i] = r.Name
	}
	return out
}

func TestRankCompaniesOrdering(t *testing.T) {
	tests := []struct {
		name  string
		query string
		want  []string
	}{
		{
			name:  "exact ticker outranks name prefix and substring",
			query: "kmda",
			want:  []string{"KAMADA LTD", "GLOBAL KMDA PARTNERS"},
		},
		{
			name:  "name prefix beats token prefix beats former name",
			query: "kamada",
			want:  []string{"KAMADA LTD", "ACME KAMADA HOLDINGS", "ZETA CORP"},
		},
		{
			name:  "case insensitive name match",
			query: "ApPlE",
			want:  []string{"APPLE INC"},
		},
		{
			name:  "cik prefix with leading zeros stripped",
			query: "1567",
			want:  []string{"KAMADA LTD", "ZETA CORP"},
		},
		{
			name:  "full padded cik matches",
			query: "0001567529",
			want:  []string{"KAMADA LTD"},
		},
		{
			name:  "no match returns empty",
			query: "zzzznomatch",
			want:  []string{},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := names(RankCompanies(corpus(), tt.query, 0))
			if len(got) != len(tt.want) {
				t.Fatalf("query %q = %v, want %v", tt.query, got, tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Fatalf("query %q = %v, want %v", tt.query, got, tt.want)
				}
			}
		})
	}
}

func TestRankCompaniesEmptyQuery(t *testing.T) {
	for _, q := range []string{"", "   ", "\t"} {
		if got := RankCompanies(corpus(), q, 0); got != nil {
			t.Errorf("RankCompanies(%q) = %v, want nil", q, got)
		}
	}
}

func TestRankCompaniesLimitClamp(t *testing.T) {
	many := make([]SearchCandidate, 0, 50)
	for i := 0; i < 50; i++ {
		many = append(many, cand("000000000"+string(rune('0'+i%10)), "TESTCO "+string(rune('A'+i%26)), []string{"T"}))
	}
	if got := RankCompanies(many, "testco", 0); len(got) != MaxSearchResults {
		t.Errorf("limit 0 returned %d rows, want clamp to %d", len(got), MaxSearchResults)
	}
	if got := RankCompanies(many, "testco", 999); len(got) != MaxSearchResults {
		t.Errorf("limit 999 returned %d rows, want clamp to %d", len(got), MaxSearchResults)
	}
	if got := RankCompanies(many, "testco", 3); len(got) != 3 {
		t.Errorf("limit 3 returned %d rows, want 3", len(got))
	}
}

func TestRankCompaniesTieBreakByName(t *testing.T) {
	// Both are token-prefix matches at the same score; name asc decides.
	c := []SearchCandidate{
		cand("0000000002", "ZULU BETA CORP", []string{"Z"}),
		cand("0000000001", "ALPHA BETA CORP", []string{"A"}),
	}
	got := names(RankCompanies(c, "beta", 0))
	if len(got) != 2 || got[0] != "ALPHA BETA CORP" {
		t.Errorf("tie-break = %v, want ALPHA first", got)
	}
}

func TestRankCompaniesNumericQuery(t *testing.T) {
	// A numeric query is not CIK-only: a name that starts with those digits is
	// still a legitimate name-prefix match and outranks a CIK-prefix match.
	c := []SearchCandidate{
		cand("0009999999", "3M COMPANY", []string{"MMM"}),
		cand("0000000003", "ALPHA CORP", []string{"ALP"}),
	}
	got := names(RankCompanies(c, "3", 0))
	if len(got) != 2 || got[0] != "3M COMPANY" || got[1] != "ALPHA CORP" {
		t.Errorf("numeric query = %v, want [3M COMPANY ALPHA CORP]", got)
	}
}
