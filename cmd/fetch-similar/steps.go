package main

import (
	"context"
	"strings"
	"time"

	"megane/internal/companyfetch"
)

// setStep writes a step's result into the schema's step map (see
// StepResults' doc comment for the submissions/filings duplication).
func setStep(s *StepResults, name, result string) {
	switch name {
	case "fetch":
		s.Submissions = result
		s.Filings = result
	case "meta":
		s.Meta = result
	case "financials":
		s.Financials = result
	case "prices":
		s.StockPrices = result
	}
}

// runPeer resolves one peer's CIK and delegates the actual ingestion to
// companyfetch.Run (shared with cmd/fetch-company) — this function's only
// job is to translate a SimilarCompany + companyfetch.Result into this
// package's peer-list-shaped PeerStatus row.
func runPeer(ctx context.Context, cfg companyfetch.Config, p SimilarCompany, tickers tickerMap) PeerStatus {
	start := time.Now()
	ps := PeerStatus{CompanyName: p.CompanyName, Ticker: p.Ticker}

	cik, note, ok := resolveCIK(p, tickers)
	if !ok {
		ps.Status = "skipped"
		ps.Steps.Resolve = "skipped"
		ps.Note = note
		ps.DurationMS = time.Since(start).Milliseconds()
		return ps
	}
	ps.CompanyID = cik
	ps.Steps.Resolve = "ok"

	res := companyfetch.Run(ctx, cfg, cik)

	var notes []string
	for _, name := range []string{"fetch", "meta", "financials", "prices"} {
		step, ok := res.Steps[name]
		if !ok {
			continue
		}
		setStep(&ps.Steps, name, step.Result)
		if step.Note != "" {
			notes = append(notes, name+": "+step.Note)
		}
	}

	ps.Status = res.Status
	ps.FilingCount = res.FilingCount
	ps.FinancialsCount = res.FinancialsCount
	ps.StockBarCount = res.StockBarCount
	ps.Note = strings.Join(notes, "; ")
	ps.DurationMS = time.Since(start).Milliseconds()
	return ps
}
