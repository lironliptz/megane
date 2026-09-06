package main

import (
	"path/filepath"
	"testing"
)

// TestParseSimilarJSON verifies the bundled fixture parses into the expected
// 7 / 2 / 1 CIK-resolution split (LLD §3 "current state").
func TestParseSimilarJSON(t *testing.T) {
	path := filepath.Join("..", "..", "fileDB", "similar", "kamada.json")
	list, err := loadSimilarList(path)
	if err != nil {
		t.Fatalf("loadSimilarList: %v", err)
	}
	if list.CompanyName != "Kamada Ltd." {
		t.Errorf("company_name = %q, want Kamada Ltd.", list.CompanyName)
	}
	if len(list.SimilarCompanies) != 10 {
		t.Fatalf("len(similar_companies) = %d, want 10", len(list.SimilarCompanies))
	}

	var withID, nullWithTicker, nullNoTicker int
	for _, p := range list.SimilarCompanies {
		switch {
		case p.CompanyID != nil && *p.CompanyID != "":
			withID++
		case p.Ticker != "":
			nullWithTicker++
		default:
			nullNoTicker++
		}
	}
	if withID != 7 || nullWithTicker != 2 || nullNoTicker != 1 {
		t.Errorf("resolution split = %d/%d/%d, want 7/2/1", withID, nullWithTicker, nullNoTicker)
	}
}

func TestSelectedPeers(t *testing.T) {
	peers := []SimilarCompany{
		{CompanyName: "A", Fetch: true},
		{CompanyName: "B", Fetch: false},
		{CompanyName: "C", Fetch: true},
	}
	got := selectedPeers(peers)
	if len(got) != 2 || got[0].CompanyName != "A" || got[1].CompanyName != "C" {
		t.Fatalf("selectedPeers = %+v, want [A C]", got)
	}
}

func TestParseSimilarJSONFetchFlags(t *testing.T) {
	path := filepath.Join("..", "..", "fileDB", "similar", "kamada.json")
	list, err := loadSimilarList(path)
	if err != nil {
		t.Fatalf("loadSimilarList: %v", err)
	}
	var fetchTrue int
	for _, p := range list.SimilarCompanies {
		if p.Fetch {
			fetchTrue++
		}
	}
	if fetchTrue != 3 {
		t.Errorf("fetch: true count = %d, want 3 (Grifols, Emergent, Protalix)", fetchTrue)
	}
}

func TestLoadSimilarListMissingFile(t *testing.T) {
	if _, err := loadSimilarList("does-not-exist.json"); err == nil {
		t.Fatal("expected an error for a missing file")
	}
}

func TestLoadSimilarListEmptyPeers(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "empty.json")
	writeFile(t, path, `{"company_name":"X","company_id":"1","similar_companies":[]}`)
	if _, err := loadSimilarList(path); err == nil {
		t.Fatal("expected an error for a list with no peers")
	}
}
