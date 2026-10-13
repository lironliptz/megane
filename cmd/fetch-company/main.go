// Command fetch-company ingests one SEC-registered company to the same
// on-disk depth cmd/fetch-similar gives a peer: filings tree, meta.json,
// financials.json, and (when the prices step runs) stock prices.
//
// Unlike cmd/fetch-similar, this takes a bare --cik — no similar-companies
// JSON, no reference-company framing, no peer-list concept anywhere. Both
// commands share the same step logic via internal/companyfetch so neither
// one's idempotency/subprocess rules can drift from the other's.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"megane/internal/companyfetch"
	"megane/internal/companyview"
	"megane/internal/db"
	"megane/internal/filedb"
	"megane/internal/marketdata"
)

func main() { os.Exit(run(os.Args[1:])) }

// status is this command's fetch-status.json shape — the same field names
// cmd/fetch-similar's PeerStatus uses, minus anything peer-list-specific
// (no company_name/ticker source-of-truth, no reference block), so the two
// tools' output reads consistently side by side.
type status struct {
	CIK             string                `json:"cik"`
	GeneratedAt     string                `json:"generated_at"`
	Status          string                `json:"status"` // complete | partial | error
	Steps           map[string]stepResult `json:"steps"`
	FilingCount     int                   `json:"filing_count"`
	FinancialsCount int                   `json:"financials_count"`
	StockBarCount   int                   `json:"stock_bar_count"`
	DurationMS      int64                 `json:"duration_ms"`
}

type stepResult struct {
	Result string `json:"result"`
	Note   string `json:"note,omitempty"`
}

func run(args []string) int {
	fs := flag.NewFlagSet("fetch-company", flag.ContinueOnError)
	cikFlag := fs.String("cik", "", "SEC CIK to ingest (required)")
	root := fs.String("root", envOr("FILEDB_DIR", "./fileDB"), "fileDB root")
	years := fs.Int("years", 10, "filing history depth")
	stepsFlag := fs.String("steps", "fetch,meta,financials,prices", "comma-separated steps to run")
	force := fs.Bool("force", false, "re-run steps whose output already exists")
	verbose := fs.Bool("v", false, "verbose logging to stdout")
	stockOnly := fs.Bool("stock-only", false, "run only stock prices/volume fetch")
	secOnly := fs.Bool("sec-only", false, "run only SEC filings/meta/financials fetch")
	dbPath := fs.String("db-path", os.Getenv("DB_PATH"), "SQLite path used by the prices step (default: same as cmd/server's DB_PATH)")

	if err := fs.Parse(args); err != nil {
		return 2
	}
	if strings.TrimSpace(*cikFlag) == "" {
		fmt.Fprintln(os.Stderr, "fetch-company: -cik is required")
		return 2
	}
	if *stockOnly && *secOnly {
		fmt.Fprintln(os.Stderr, "fetch-company: -stock-only and -sec-only are mutually exclusive")
		return 2
	}
	cik, err := filedb.NormalizeCIK(*cikFlag)
	if err != nil {
		fmt.Fprintln(os.Stderr, "fetch-company: invalid -cik:", err)
		return 2
	}

	steps := resolveSteps(*stepsFlag, *stockOnly, *secOnly)
	userAgent := strings.TrimSpace(os.Getenv("SEC_EDGAR_USER_AGENT"))

	var svc *companyview.Service
	var database *db.DB
	if steps["prices"] {
		database, err = db.Open(db.ResolvePath(*dbPath))
		if err != nil {
			fmt.Fprintln(os.Stderr, "fetch-company: opening db (prices step disabled):", err)
			database = nil
		} else {
			defer database.Close()
			store := filedb.NewFileDBStore(*root, filedb.Options{TTL: filedb.ParseTTL(os.Getenv("FILEDB_CACHE_TTL"))})
			provider := marketdata.NewFromEnv(os.Getenv("MARKET_DATA_PROVIDER"))
			fallback := marketdata.NewFallbackFromEnv()
			svc = companyview.NewService(store, database, provider, fallback, companyview.Config{
				ChunkYears: getEnvInt("MARKET_DATA_CHUNK_YEARS", marketdata.DefaultChunkYears),
				ChunkDelay: getEnvDuration("MARKET_DATA_CHUNK_DELAY", marketdata.DefaultChunkDelay),
			})
		}
	}

	cfg := companyfetch.Config{
		Root: *root, Years: *years, Force: *force, Steps: steps,
		Runner: companyfetch.DefaultRunner, UserAgent: userAgent, Svc: svc, Database: database,
		Verbose: *verbose,
		Logf: func(format string, args ...any) {
			fmt.Printf(format+"\n", args...)
		},
	}

	res := companyfetch.Run(context.Background(), cfg, cik)

	out := status{
		CIK:             cik,
		GeneratedAt:     time.Now().UTC().Format(time.RFC3339),
		Status:          res.Status,
		Steps:           map[string]stepResult{},
		FilingCount:     res.FilingCount,
		FinancialsCount: res.FinancialsCount,
		StockBarCount:   res.StockBarCount,
		DurationMS:      res.DurationMS,
	}
	for name, s := range res.Steps {
		out.Steps[name] = stepResult{Result: s.Result, Note: s.Note}
	}

	statusPath := filepath.Join(*root, "companies", cik, "fetch-status.json")
	if err := writeJSON(statusPath, out); err != nil {
		fmt.Fprintln(os.Stderr, "fetch-company: writing status:", err)
		return 1
	}

	fmt.Printf("fetch-company  %-10s %-8s filings=%d financials=%d bars=%d -> %s\n",
		cik, out.Status, out.FilingCount, out.FinancialsCount, out.StockBarCount, statusPath)
	for name, s := range out.Steps {
		if s.Note != "" {
			fmt.Printf("  %-10s %-8s %s\n", name, s.Result, s.Note)
		}
	}

	if out.Status == "error" {
		return 1
	}
	return 0
}

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

func resolveSteps(stepsRaw string, stockOnly, secOnly bool) map[string]bool {
	if stockOnly {
		return map[string]bool{"prices": true}
	}
	if secOnly {
		return map[string]bool{"fetch": true, "meta": true, "financials": true}
	}
	return parseSteps(stepsRaw)
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

func writeJSON(path string, v any) error {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	b = append(b, '\n')
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, b, 0o644)
}
