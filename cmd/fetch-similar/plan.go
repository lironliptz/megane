package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Estimator constants (LLD §5.3) — measured 2026-09-01 against the reference
// corpus (Kamada, CIK 0001567529): the filing index sums to 464 MB across
// 504 filings while the folder occupies 647 MB, giving the 1.4x disk
// expansion factor (the fetch stores both the full-submission .txt that
// `size` measures AND the extracted documents). 4,422 files across 382
// accession folders on the same corpus gives ~12 files/filing. The Python
// fetcher's 120ms inter-request sleep is comfortably inside SEC's 10 req/s
// per-IP ceiling; peers run serially, so 8.3 req/s is the batch floor.
//
// All three are single-issuer estimates, not measured facts about any given
// peer — every plan value derived from them is rendered with "~" and labeled
// an estimate (never a promise).
const (
	diskExpansionFactor = 1.4
	avgFilesPerFiling   = 12
	requestsPerSecond   = 8.3
)

// PeerPlan is one row of the dry-run survey (LLD §5.2 / §3.3).
type PeerPlan struct {
	CompanyName string   `json:"company_name"`
	CompanyID   string   `json:"company_id,omitempty"`
	Ticker      string   `json:"ticker,omitempty"`
	SIC         string   `json:"sic,omitempty"`
	SICDesc     string   `json:"sic_description,omitempty"`
	Exchanges   []string `json:"exchanges,omitempty"`
	Status      string   `json:"status"` // planned | skipped | error

	FilingsInIndex    int `json:"filings_in_index"`
	FilingsInWindow   int `json:"filings_in_window"`
	FilingsOnDisk     int `json:"filings_on_disk"`
	FilingsToFetch    int `json:"filings_to_fetch"`
	IndexPagesFetched int `json:"index_pages_fetched"`

	FormCounts     map[string]int `json:"form_counts,omitempty"`
	EarliestFiling string         `json:"earliest_filing,omitempty"`
	LatestFiling   string         `json:"latest_filing,omitempty"`

	// XBRLFlagged is an UPPER BOUND on financials.json count: the flag means
	// the filer tagged XBRL, not that EDGAR generated the local instance the
	// extractor needs (prompt 9's gap). Never reported as a prediction.
	XBRLFlagged int `json:"xbrl_flagged"`

	EstDownloadBytes int64  `json:"est_download_bytes"`
	EstDiskBytes     int64  `json:"est_disk_bytes"`
	EstRequests      int    `json:"est_requests"`
	EstSeconds       int    `json:"est_seconds"`
	Note             string `json:"note,omitempty"`
}

// PlanTotals sums every peer row plus outcome counts.
type PlanTotals struct {
	Peers            int   `json:"peers"`
	Planned          int   `json:"planned"`
	Skipped          int   `json:"skipped"`
	Error            int   `json:"error"`
	FilingsToFetch   int   `json:"filings_to_fetch"`
	EstDownloadBytes int64 `json:"est_download_bytes"`
	EstDiskBytes     int64 `json:"est_disk_bytes"`
	EstRequests      int   `json:"est_requests"`
	EstSeconds       int   `json:"est_seconds"`
}

// FetchPlan is the v1 schema for {slug}.fetch-plan.json — dry-run only,
// never written by a real run (D9).
type FetchPlan struct {
	SourceFile  string        `json:"source_file"`
	Reference   ReferenceInfo `json:"reference"`
	GeneratedAt string        `json:"generated_at"`
	Config      struct {
		Root   string `json:"root"`
		Years  int    `json:"years"`
		DryRun bool   `json:"dry_run"`
	} `json:"config"`
	Peers  []PeerPlan `json:"peers"`
	Totals PlanTotals `json:"totals"`
}

// planPeer resolves one peer, fetches ONLY submissions.json (never a filing
// document, never anything under root/companies/), and derives the costed
// estimate (HLD D9 / LLD §5).
func planPeer(ctx context.Context, client *http.Client, baseURL string, p SimilarCompany, tickers tickerMap,
	root string, years int, userAgent string, peerDelay time.Duration) PeerPlan {

	plan := PeerPlan{CompanyName: p.CompanyName, Ticker: p.Ticker}

	cik, note, ok := resolveCIK(p, tickers)
	if !ok {
		plan.Status = "skipped"
		plan.Note = note
		return plan
	}
	plan.CompanyID = cik

	if strings.TrimSpace(userAgent) == "" {
		plan.Status = "error"
		plan.Note = ErrNoUserAgent.Error()
		return plan
	}

	doc, err := fetchSubmissions(ctx, client, baseURL, cik, userAgent)
	if err != nil {
		plan.Status = "error"
		plan.Note = err.Error()
		return plan
	}
	applySubmissions(&plan, doc, root, cik, years)
	plan.EstSeconds = int(float64(plan.EstRequests)/requestsPerSecond) + int(peerDelay.Seconds())
	plan.Status = "planned"
	return plan
}

// applySubmissions fills in every count/estimate field from a fetched (or
// fixture, in tests) submissions document — split out from planPeer so tests
// can drive it directly against a checked-in submissions.json slice without
// any network (LLD §7: TestPlanFromSubmissionsFixture).
func applySubmissions(plan *PeerPlan, doc *submissionsDoc, root, cik string, years int) {
	plan.IndexPagesFetched = 1
	if len(doc.Filings.Files) > 0 {
		appendNote(plan, fmt.Sprintf("submissions has %d older-filing index page(s) not fetched by the survey", len(doc.Filings.Files)))
	}
	plan.SIC = doc.SIC
	plan.SICDesc = doc.SICDesc
	plan.Exchanges = doc.Exchanges

	cutoff := time.Now().UTC().Year() - years
	forms := map[string]int{}
	var totalBytes int64
	xbrl, toFetch, onDisk := 0, 0, 0
	var earliest, latest string

	recent := doc.Filings.Recent
	plan.FilingsInIndex = len(recent.AccessionNumber)
	for i, accession := range recent.AccessionNumber {
		fd := safeIndex(recent.FilingDate, i)
		if len(fd) < 4 {
			continue
		}
		year, err := strconv.Atoi(fd[:4])
		if err != nil || year < cutoff {
			continue
		}
		plan.FilingsInWindow++
		forms[safeIndex(recent.Form, i)]++
		totalBytes += safeIndexInt64(recent.Size, i)
		if safeIndexInt(recent.IsXBRL, i) == 1 || safeIndexInt(recent.IsInlineXBRL, i) == 1 {
			xbrl++
		}
		if earliest == "" || fd < earliest {
			earliest = fd
		}
		if latest == "" || fd > latest {
			latest = fd
		}
		dir := filepath.Join(root, "companies", cik, fd[:4], accession)
		if _, err := os.Stat(filepath.Join(dir, "filing.json")); err == nil {
			onDisk++
		} else {
			toFetch++
		}
	}

	plan.FormCounts = forms
	plan.EarliestFiling = earliest
	plan.LatestFiling = latest
	plan.FilingsOnDisk = onDisk
	plan.FilingsToFetch = toFetch
	plan.XBRLFlagged = xbrl
	if xbrl > 0 {
		appendNote(plan, "xbrl_flagged is an upper bound on financials.json count")
	}

	plan.EstDownloadBytes = totalBytes
	plan.EstDiskBytes = int64(float64(totalBytes) * diskExpansionFactor)
	plan.EstRequests = 1 + plan.IndexPagesFetched + toFetch*avgFilesPerFiling
	plan.EstSeconds = 0 // filled by caller once peerDelay is known — see buildTotals
}

func appendNote(plan *PeerPlan, note string) {
	if plan.Note == "" {
		plan.Note = note
		return
	}
	plan.Note += "; " + note
}

func buildTotals(peers []PeerPlan) PlanTotals {
	t := PlanTotals{Peers: len(peers)}
	for _, p := range peers {
		switch p.Status {
		case "planned":
			t.Planned++
		case "skipped":
			t.Skipped++
		case "error":
			t.Error++
		}
		t.FilingsToFetch += p.FilingsToFetch
		t.EstDownloadBytes += p.EstDownloadBytes
		t.EstDiskBytes += p.EstDiskBytes
		t.EstRequests += p.EstRequests
		t.EstSeconds += p.EstSeconds
	}
	return t
}

// runDryRun performs the whole D9 survey: resolve every peer, fetch only
// submissions.json, write {slug}.fetch-plan.json, print a batch summary, and
// exit per D8 (planned counts as success). baseURL is SEC's submissions host
// in production ("https://data.sec.gov") and an httptest fixture server in
// tests.
func runDryRun(ctx context.Context, client *http.Client, baseURL string, list *SimilarList, peers []SimilarCompany, tickers tickerMap,
	root string, years int, similarPath, userAgent string, peerDelay time.Duration) int {

	plans := make([]PeerPlan, 0, len(peers))
	for _, p := range peers {
		plans = append(plans, planPeer(ctx, client, baseURL, p, tickers, root, years, userAgent, peerDelay))
	}

	fp := &FetchPlan{
		SourceFile:  similarPath,
		Reference:   ReferenceInfo{CompanyName: list.CompanyName, CompanyID: list.CompanyID, Ticker: list.Ticker},
		GeneratedAt: time.Now().UTC().Format(time.RFC3339),
	}
	fp.Config.Root = root
	fp.Config.Years = years
	fp.Config.DryRun = true
	fp.Peers = plans
	fp.Totals = buildTotals(plans)

	if err := writeJSONFile(planPath(similarPath), fp); err != nil {
		fmt.Fprintln(os.Stderr, "fetch-similar: writing plan:", err)
		return 1
	}

	printPlanSummary(fp)
	if fp.Totals.Error > 0 {
		return 1
	}
	return 0
}

func printPlanSummary(fp *FetchPlan) {
	fmt.Printf("fetch-similar dry-run  source=%s years=%d peers=%d\n",
		filepath.Base(fp.SourceFile), fp.Config.Years, fp.Totals.Peers)
	for _, p := range fp.Peers {
		if p.Status != "planned" {
			fmt.Printf("  %-32s %-8s %s\n", p.CompanyName, p.Status, p.Note)
			continue
		}
		fmt.Printf("  %-32s %-8s %d filings  ~%s  ~%s\n",
			p.CompanyName, p.Status, p.FilingsToFetch, humanBytes(p.EstDiskBytes), humanDuration(p.EstSeconds))
	}
	fmt.Printf("TOTAL  %d planned  %d skipped  ~%s  ~%s  -> %s\n",
		fp.Totals.Planned, fp.Totals.Skipped, humanBytes(fp.Totals.EstDiskBytes),
		humanDuration(fp.Totals.EstSeconds), planPath(fp.SourceFile))
}

func humanBytes(n int64) string {
	const mb = 1024 * 1024
	const gb = 1024 * mb
	if n >= gb {
		return fmt.Sprintf("%.1f GB", float64(n)/float64(gb))
	}
	return fmt.Sprintf("%.0f MB", float64(n)/float64(mb))
}

func humanDuration(seconds int) string {
	if seconds >= 60 {
		return fmt.Sprintf("%d min", (seconds+30)/60)
	}
	return fmt.Sprintf("%ds", seconds)
}
