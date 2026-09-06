// Command fetch-similar batch-ingests a curated similar-companies JSON file
// (prompt 13) to the same on-disk depth the reference company already has:
// filings tree, meta.json, financials.json, and (when the prices step runs)
// stock prices. See prompts/dev/prompt_13_fetch_similar_companies-{hld,lld}.md.
package main

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"megane/internal/companyview"
	"megane/internal/db"
	"megane/internal/filedb"
	"megane/internal/marketdata"
)

func main() {
	os.Exit(run(os.Args[1:]))
}

var allSteps = []string{"fetch", "meta", "financials", "prices"}

func parseSteps(s string) map[string]bool {
	out := map[string]bool{}
	for _, part := range strings.Split(s, ",") {
		part = strings.TrimSpace(part)
		if part != "" {
			out[part] = true
		}
	}
	return out
}

func sortedSteps(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for _, k := range allSteps {
		if m[k] {
			out = append(out, k)
		}
	}
	sort.Strings(out)
	return out
}

func envOr(key, def string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return def
}

func getEnvInt(key string, def int) int {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil || n <= 0 {
		return def
	}
	return n
}

func getEnvDuration(key string, def time.Duration) time.Duration {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return def
	}
	d, err := time.ParseDuration(v)
	if err != nil || d <= 0 {
		return def
	}
	return d
}

// run is the whole CLI, factored out of main so tests can drive it with
// argv-style args and check the returned exit code (D8) without os.Exit.
func run(args []string) int {
	fs := flag.NewFlagSet("fetch-similar", flag.ContinueOnError)
	similarPath := fs.String("similar", "", "path to the curated peer JSON (required)")
	root := fs.String("root", envOr("FILEDB_DIR", "./fileDB"), "fileDB root")
	years := fs.Int("years", 10, "filing history depth")
	stepsFlag := fs.String("steps", "fetch,meta,financials,prices", "comma-separated steps to run")
	force := fs.Bool("force", false, "re-run steps whose output already exists")
	dryRun := fs.Bool("dry-run", false, "survey only: fetch submissions.json, report counts + estimates, download nothing")
	peerDelay := fs.Duration("peer-delay", 5*time.Second, "sleep between peers")
	failFast := fs.Bool("fail-fast", false, "stop after the first peer error")
	includeRef := fs.Bool("include-reference", false, "also ingest the reference company")
	// No literal default here: db.ResolvePath applies the SAME fallback
	// cmd/server uses (DefaultSQLitePathRelative) when DB_PATH is unset, so
	// this tool's prices step always lands in the one database the running
	// app actually reads — a hardcoded default of our own would silently
	// write into a second, invisible SQLite file whenever DB_PATH is unset.
	dbPath := fs.String("db-path", os.Getenv("DB_PATH"), "SQLite path used by the prices step (default: same as cmd/server's DB_PATH)")

	if err := fs.Parse(args); err != nil {
		return 2
	}
	if strings.TrimSpace(*similarPath) == "" {
		fmt.Fprintln(os.Stderr, "fetch-similar: -similar is required")
		return 2
	}

	list, err := loadSimilarList(*similarPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, "fetch-similar:", err)
		return 2
	}

	steps := parseSteps(*stepsFlag)
	userAgent := strings.TrimSpace(os.Getenv("SEC_EDGAR_USER_AGENT"))

	peers := selectedPeers(list.SimilarCompanies)
	if len(peers) == 0 && !*includeRef {
		fmt.Fprintln(os.Stderr, "fetch-similar: no peers with fetch: true")
		return 2
	}
	if *includeRef {
		refID := list.CompanyID
		ref := SimilarCompany{CompanyName: list.CompanyName, CompanyID: &refID, Ticker: list.Ticker, Fetch: true}
		peers = append([]SimilarCompany{ref}, peers...)
	}

	ctx := context.Background()
	httpClient := &http.Client{Timeout: 30 * time.Second}

	var tickers tickerMap
	if needsTickerMap(peers) {
		tickers, err = loadTickerMap(ctx, httpClient, "https://www.sec.gov", userAgent)
		if err != nil {
			// Not a CLI-usage failure (D8 exit 2 is parse/usage only): peers
			// that needed the map simply cannot resolve below and come back
			// "skipped" with a note; peers resolved via company_id are
			// unaffected.
			fmt.Fprintln(os.Stderr, "fetch-similar: ticker map unavailable:", err)
			tickers = nil
		}
	}

	if *dryRun {
		return runDryRun(ctx, httpClient, "https://data.sec.gov", list, peers, tickers, *root, *years, *similarPath, userAgent, *peerDelay)
	}

	var svc *companyview.Service
	var database *db.DB
	if steps["prices"] {
		database, err = db.Open(db.ResolvePath(*dbPath))
		if err != nil {
			fmt.Fprintln(os.Stderr, "fetch-similar: opening db (prices step disabled):", err)
			database = nil
		} else {
			store := filedb.NewFileDBStore(*root, filedb.Options{TTL: filedb.ParseTTL(os.Getenv("FILEDB_CACHE_TTL"))})
			provider := marketdata.NewFromEnv(os.Getenv("MARKET_DATA_PROVIDER"))
			fallback := marketdata.NewFallbackFromEnv()
			svc = companyview.NewService(store, database, provider, fallback, companyview.Config{
				ChunkYears: getEnvInt("MARKET_DATA_CHUNK_YEARS", marketdata.DefaultChunkYears),
				ChunkDelay: getEnvDuration("MARKET_DATA_CHUNK_DELAY", marketdata.DefaultChunkDelay),
			})
		}
	}

	orch := &orchestrator{
		root: *root, years: *years, force: *force, steps: steps,
		runner: defaultRunner, userAgent: userAgent, svc: svc, database: database,
	}

	status := &FetchStatus{
		SourceFile:  *similarPath,
		Reference:   ReferenceInfo{CompanyName: list.CompanyName, CompanyID: list.CompanyID, Ticker: list.Ticker},
		GeneratedAt: time.Now().UTC().Format(time.RFC3339),
		Config:      RunConfig{Root: *root, Years: *years, Steps: sortedSteps(steps), Force: *force},
	}

	anyError := false
	for i, p := range peers {
		ps := orch.runPeer(ctx, p, tickers)
		status.Peers = append(status.Peers, ps)
		if ps.Status == "error" {
			anyError = true
			if *failFast {
				break
			}
		}
		if i < len(peers)-1 {
			time.Sleep(*peerDelay)
		}
	}
	status.Summary = summarize(status.Peers)

	if err := writeStatus(statusPath(*similarPath), status); err != nil {
		fmt.Fprintln(os.Stderr, "fetch-similar: writing status:", err)
		return 1
	}

	printSummary(status, *similarPath)
	if anyError {
		return 1
	}
	return 0
}

func printSummary(status *FetchStatus, similarPath string) {
	fmt.Printf("fetch-similar  peers=%d complete=%d partial=%d skipped=%d error=%d -> %s\n",
		status.Summary.Total, status.Summary.Complete, status.Summary.Partial,
		status.Summary.Skipped, status.Summary.Error, statusPath(similarPath))
	for _, p := range status.Peers {
		fmt.Printf("  %-32s %-8s %s\n", p.CompanyName, p.Status, p.Note)
	}
}
