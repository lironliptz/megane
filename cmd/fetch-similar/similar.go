package main

import (
	"encoding/json"
	"fmt"
	"os"
)

// SimilarCompany is one peer row from the curated similar-companies JSON
// (LLD §3.1). CompanyID is a pointer so a JSON `null` (peer has no known SEC
// CIK) is distinguishable from an empty string.
type SimilarCompany struct {
	CompanyName  string  `json:"company_name"`
	CompanyID    *string `json:"company_id"`
	Ticker       string  `json:"ticker"`
	RelationType string  `json:"relation_type"`
	Description  string  `json:"description"`
	// Fetch marks whether this peer should be ingested. Omitted or false
	// skips the row; lets a curated list hold more peers than one batch run.
	Fetch bool `json:"fetch"`
}

// selectedPeers returns only rows with fetch: true.
func selectedPeers(peers []SimilarCompany) []SimilarCompany {
	out := make([]SimilarCompany, 0, len(peers))
	for _, p := range peers {
		if p.Fetch {
			out = append(out, p)
		}
	}
	return out
}

// SimilarList is the parsed input file: a reference company plus its curated
// peers. The reference block is read for status/plan metadata only — it is
// never itself fetched unless --include-reference adds it to the peer slice.
type SimilarList struct {
	CompanyName      string           `json:"company_name"`
	CompanyID        string           `json:"company_id"`
	Ticker           string           `json:"ticker"`
	SimilarCompanies []SimilarCompany `json:"similar_companies"`
}

// loadSimilarList reads and parses the input JSON. The source file is opened
// read-only — this orchestrator never writes back to it (HLD's governing
// rule: hand-curated input, machine output goes only to the status/plan file).
func loadSimilarList(path string) (*SimilarList, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read similar list: %w", err)
	}
	var list SimilarList
	if err := json.Unmarshal(b, &list); err != nil {
		return nil, fmt.Errorf("parse similar list %s: %w", path, err)
	}
	if len(list.SimilarCompanies) == 0 {
		return nil, fmt.Errorf("similar list %s has no similar_companies", path)
	}
	return &list, nil
}
