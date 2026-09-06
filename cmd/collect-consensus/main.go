package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"megane/internal/consensus"
	"megane/internal/db"
	"megane/internal/edgar/financials"
	"megane/internal/filedb"
	"megane/internal/models"
)

func main() { os.Exit(run(os.Args[1:])) }

type target struct {
	CIK           string
	Ticker        string
	FiscalYearEnd string
	Name          string
}

type planRow struct {
	CIK             string `json:"cik"`
	Ticker          string `json:"ticker"`
	ExpectedPeriods int    `json:"expected_periods"`
	ExistingPeriods int    `json:"existing_periods"`
	PlannedCalls    int    `json:"planned_calls"`
}

type dryRunPlan struct {
	GeneratedAt string    `json:"generated_at"`
	SourceFile  string    `json:"source_file,omitempty"`
	Targets     []planRow `json:"targets"`
}

func run(args []string) int {
	fs := flag.NewFlagSet("collect-consensus", flag.ContinueOnError)
	cik := fs.String("cik", "", "target CIK")
	ticker := fs.String("ticker", "", "ticker override")
	similar := fs.String("similar", "", "path to similar-companies JSON; only peers with fetch=true")
	allInCorpus := fs.Bool("all-in-corpus", false, "read all companies under {root}/companies")
	root := fs.String("root", envOr("FILEDB_DIR", "./fileDB"), "fileDB root")
	vendor := fs.String("vendor", envOr("CONSENSUS_PROVIDER", consensus.SourceFinnhub), "vendor slug")
	metrics := fs.String("metrics", "eps,ratings,price_target", "comma-separated metrics")
	fromRaw := fs.String("from", "", "YYYY-MM-DD")
	toRaw := fs.String("to", "today", "YYYY-MM-DD or today")
	snapshot := fs.Bool("snapshot", false, "write analyst snapshot row")
	dryRun := fs.Bool("dry-run", false, "plan only; write no DB rows or sidecars")
	force := fs.Bool("force", false, "ignore min fetch interval")
	dbPath := fs.String("db-path", os.Getenv("DB_PATH"), "SQLite path")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	if countNonEmpty(*cik, *similar) > 1 || ((*allInCorpus) && (strings.TrimSpace(*cik) != "" || strings.TrimSpace(*similar) != "")) {
		fmt.Fprintln(os.Stderr, "collect-consensus: pick one of --cik, --similar, --all-in-corpus")
		return 2
	}
	if strings.TrimSpace(*cik) == "" && strings.TrimSpace(*similar) == "" && !*allInCorpus {
		fmt.Fprintln(os.Stderr, "collect-consensus: one of --cik, --similar, --all-in-corpus is required")
		return 2
	}

	to := time.Now().UTC()
	if strings.TrimSpace(*toRaw) != "" && strings.TrimSpace(*toRaw) != "today" {
		parsed, err := time.Parse("2006-01-02", strings.TrimSpace(*toRaw))
		if err != nil {
			fmt.Fprintln(os.Stderr, "collect-consensus: invalid --to date")
			return 2
		}
		to = parsed
	}

	var from time.Time
	if strings.TrimSpace(*fromRaw) != "" {
		parsed, err := time.Parse("2006-01-02", strings.TrimSpace(*fromRaw))
		if err != nil {
			fmt.Fprintln(os.Stderr, "collect-consensus: invalid --from date")
			return 2
		}
		from = parsed
	}

	database, err := db.Open(db.ResolvePath(*dbPath))
	if err != nil {
		fmt.Fprintln(os.Stderr, "collect-consensus: open db:", err)
		return 1
	}
	defer database.Close()

	targets, err := resolveTargets(*root, strings.TrimSpace(*cik), strings.TrimSpace(*ticker), strings.TrimSpace(*similar), *allInCorpus)
	if err != nil {
		fmt.Fprintln(os.Stderr, "collect-consensus:", err)
		return 2
	}
	if len(targets) == 0 {
		fmt.Fprintln(os.Stderr, "collect-consensus: no targets found")
		return 2
	}

	metricsSet := parseSet(*metrics)

	if *dryRun {
		return runDryRun(database, *similar, targets, strings.ToLower(strings.TrimSpace(*vendor)), metricsSet)
	}

	provider, err := buildProvider(*vendor)
	if err != nil {
		for _, t := range targets {
			_ = database.SetAnalystCoverage(context.Background(), t.CIK, db.AnalystCoverageError, *vendor, err.Error())
		}
		fmt.Fprintln(os.Stderr, "collect-consensus:", err)
		return 1
	}

	var hardErr bool
	ctx := context.Background()
	for _, t := range targets {
		if !*force {
			cov, err := database.AnalystCoverage(ctx, t.CIK)
			if err == nil && cov != nil && cov.LastSuccessAt != "" {
				last, parseErr := time.Parse(time.RFC3339, cov.LastSuccessAt)
				minWait := envDuration("CONSENSUS_MIN_FETCH_INTERVAL", 24*time.Hour)
				if parseErr == nil && time.Since(last) < minWait {
					fmt.Printf("collect-consensus  %-10s skipped  recent fetch (%s)\n", t.CIK, cov.LastSuccessAt)
					continue
				}
			}
		}

		_ = database.SetAnalystCoverage(ctx, t.CIK, db.AnalystCoverageFetching, provider.Slug(), "")
		windowFrom := from
		if windowFrom.IsZero() {
			windowFrom = time.Date(time.Now().UTC().Year()-10, 1, 1, 0, 0, 0, 0, time.UTC)
		}
		rows, err := provider.FetchEarnings(ctx, t.Ticker, windowFrom, to)
		if err != nil {
			status := db.AnalystCoverageError
			if errors.Is(err, consensus.ErrNoData) {
				status = db.AnalystCoverageNoData
			}
			_ = database.SetAnalystCoverage(ctx, t.CIK, status, provider.Slug(), err.Error())
			fmt.Printf("collect-consensus  %-10s error    %v\n", t.CIK, err)
			if status == db.AnalystCoverageError {
				hardErr = true
			}
			continue
		}

		rows = normalizeRows(rows, t)
		filtered, dropped := filterRowsWithPeriod(rows)
		epsMap := loadSidecarEPS(*root, t.CIK)
		for i := range filtered {
			filtered[i].CIK = t.CIK
			filtered[i].Ticker = t.Ticker
			if filtered[i].EPSActual != nil {
				if v, ok := epsMap[keyFor(filtered[i].PeriodEnd, filtered[i].Duration)]; ok {
					filtered[i].Basis, _ = consensus.ClassifyBasis(*filtered[i].EPSActual, v)
				}
			}
		}
		if err := database.UpsertConsensusPeriods(ctx, filtered); err != nil {
			_ = database.SetAnalystCoverage(ctx, t.CIK, db.AnalystCoverageError, provider.Slug(), err.Error())
			fmt.Printf("collect-consensus  %-10s error    %v\n", t.CIK, err)
			hardErr = true
			continue
		}

		if *snapshot || metricsSet["ratings"] || metricsSet["price_target"] {
			s, sErr := provider.FetchSnapshot(ctx, t.Ticker)
			if sErr == nil && s != nil {
				s.CIK = t.CIK
				s.Source = provider.Slug()
				_ = database.UpsertConsensusSnapshot(ctx, *s)
			}
		}

		status := db.AnalystCoverageOK
		note := ""
		if dropped > 0 {
			status = db.AnalystCoveragePartial
			note = fmt.Sprintf("dropped %d rows with underivable period_end", dropped)
		}
		_ = database.SetAnalystCoverage(ctx, t.CIK, status, provider.Slug(), note)
		if err := writeSidecar(*root, t.CIK, t.Ticker, provider.Slug(), filtered); err != nil {
			fmt.Printf("collect-consensus  %-10s warning  sidecar: %v\n", t.CIK, err)
		}
		fmt.Printf("collect-consensus  %-10s %s       rows=%d\n", t.CIK, status, len(filtered))
	}

	if hardErr {
		return 1
	}
	return 0
}

func runDryRun(database *db.DB, similar string, targets []target, source string, metrics map[string]bool) int {
	rows := make([]planRow, 0, len(targets))
	for _, t := range targets {
		existing := 0
		got, err := database.ConsensusPeriods(context.Background(), t.CIK, source)
		if err == nil {
			existing = len(got)
		}
		calls := 1
		if metrics["ratings"] || metrics["price_target"] {
			calls += 2
		}
		rows = append(rows, planRow{
			CIK:             t.CIK,
			Ticker:          t.Ticker,
			ExpectedPeriods: 4,
			ExistingPeriods: existing,
			PlannedCalls:    calls,
		})
	}
	plan := dryRunPlan{
		GeneratedAt: time.Now().UTC().Format(time.RFC3339),
		SourceFile:  similar,
		Targets:     rows,
	}
	fmt.Printf("collect-consensus dry-run  targets=%d source=%s\n", len(rows), filepath.Base(similar))
	for _, r := range rows {
		fmt.Printf("  %s %s  calls=%d  existing=%d expected=%d\n", r.CIK, r.Ticker, r.PlannedCalls, r.ExistingPeriods, r.ExpectedPeriods)
	}
	if strings.TrimSpace(similar) != "" {
		out := strings.TrimSuffix(similar, filepath.Ext(similar)) + ".consensus-plan.json"
		if err := writeJSON(out, plan); err != nil {
			fmt.Fprintln(os.Stderr, "collect-consensus: write dry-run plan:", err)
			return 1
		}
		fmt.Printf("  -> %s\n", out)
	}
	return 0
}

func buildProvider(vendor string) (consensus.Provider, error) {
	switch strings.ToLower(strings.TrimSpace(vendor)) {
	case "", consensus.SourceFinnhub:
		return consensus.NewFinnhub(os.Getenv("FINNHUB_API_KEY"), &http.Client{Timeout: 20 * time.Second})
	default:
		return nil, fmt.Errorf("unsupported --vendor %q", vendor)
	}
}

func resolveTargets(root, cik, ticker, similar string, allInCorpus bool) ([]target, error) {
	if cik != "" {
		norm, err := filedb.NormalizeCIK(cik)
		if err != nil {
			return nil, err
		}
		sub, err := readSubmissions(filepath.Join(root, "companies", norm, "submissions.json"))
		if err != nil {
			return nil, err
		}
		t := strings.TrimSpace(ticker)
		if t == "" && len(sub.Tickers) > 0 {
			t = strings.TrimSpace(sub.Tickers[0])
		}
		if t == "" {
			return nil, fmt.Errorf("%s has no ticker", norm)
		}
		return []target{{CIK: norm, Ticker: t, FiscalYearEnd: sub.FiscalYearEnd, Name: sub.Name}}, nil
	}
	if similar != "" {
		list, err := readSimilarList(similar)
		if err != nil {
			return nil, err
		}
		out := make([]target, 0)
		for _, p := range list.SimilarCompanies {
			if !p.Fetch {
				continue
			}
			if p.CompanyID == nil || strings.TrimSpace(*p.CompanyID) == "" {
				continue
			}
			norm, err := filedb.NormalizeCIK(*p.CompanyID)
			if err != nil {
				continue
			}
			sub, _ := readSubmissions(filepath.Join(root, "companies", norm, "submissions.json"))
			t := strings.TrimSpace(p.Ticker)
			if t == "" && sub != nil && len(sub.Tickers) > 0 {
				t = strings.TrimSpace(sub.Tickers[0])
			}
			if t == "" {
				continue
			}
			fye := "1231"
			if sub != nil && strings.TrimSpace(sub.FiscalYearEnd) != "" {
				fye = strings.TrimSpace(sub.FiscalYearEnd)
			}
			out = append(out, target{CIK: norm, Ticker: t, FiscalYearEnd: fye, Name: p.CompanyName})
		}
		return out, nil
	}
	if allInCorpus {
		entries, err := os.ReadDir(filepath.Join(root, "companies"))
		if err != nil {
			return nil, err
		}
		out := make([]target, 0)
		for _, e := range entries {
			if !e.IsDir() {
				continue
			}
			norm, err := filedb.NormalizeCIK(e.Name())
			if err != nil {
				continue
			}
			sub, err := readSubmissions(filepath.Join(root, "companies", norm, "submissions.json"))
			if err != nil {
				continue
			}
			t := ""
			if len(sub.Tickers) > 0 {
				t = strings.TrimSpace(sub.Tickers[0])
			}
			if t == "" {
				continue
			}
			out = append(out, target{CIK: norm, Ticker: t, FiscalYearEnd: sub.FiscalYearEnd, Name: sub.Name})
		}
		sort.Slice(out, func(i, j int) bool { return out[i].CIK < out[j].CIK })
		return out, nil
	}
	return nil, fmt.Errorf("no targets")
}

func normalizeRows(rows []models.ConsensusPeriod, t target) []models.ConsensusPeriod {
	out := make([]models.ConsensusPeriod, 0, len(rows))
	for _, r := range rows {
		end, dur, ok := consensus.DerivePeriodEnd(t.FiscalYearEnd, r.FiscalYear, r.FiscalPeriod)
		if ok {
			r.PeriodEnd = end
			r.Duration = dur
		}
		if r.ConsensusDate == "" {
			r.ConsensusDate = r.PeriodEnd
		}
		if r.Currency == "" {
			r.Currency = "USD"
		}
		r.CIK = t.CIK
		r.Ticker = t.Ticker
		out = append(out, r)
	}
	return out
}

func filterRowsWithPeriod(rows []models.ConsensusPeriod) ([]models.ConsensusPeriod, int) {
	out := make([]models.ConsensusPeriod, 0, len(rows))
	dropped := 0
	for _, r := range rows {
		if strings.TrimSpace(r.PeriodEnd) == "" || strings.TrimSpace(r.Duration) == "" {
			dropped++
			continue
		}
		if strings.TrimSpace(r.ConsensusDate) == "" {
			dropped++
			continue
		}
		out = append(out, r)
	}
	return out, dropped
}

func loadSidecarEPS(root, cik string) map[string]*float64 {
	out := map[string]*float64{}
	base := filepath.Join(root, "companies", cik)
	_ = filepath.WalkDir(base, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || d.Name() != financials.FileName {
			return nil
		}
		f, e := financials.Load(filepath.Dir(path))
		if e != nil || f == nil {
			return nil
		}
		var eps *float64
		for _, line := range f.Publishable() {
			if line.Key == "eps_basic" {
				v := line.Value
				eps = &v
				break
			}
		}
		if eps == nil {
			return nil
		}
		k := keyFor(f.Period.EndDate, f.Period.Duration)
		if k != "|" {
			out[k] = eps
		}
		return nil
	})
	return out
}

func writeSidecar(root, cik, ticker, source string, rows []models.ConsensusPeriod) error {
	type sidecar struct {
		CIK           string                   `json:"cik"`
		Ticker        string                   `json:"ticker"`
		PrimarySource string                   `json:"primary_source"`
		FetchedAt     string                   `json:"fetched_at"`
		Periods       []models.ConsensusPeriod `json:"periods"`
	}
	doc := sidecar{
		CIK:           cik,
		Ticker:        ticker,
		PrimarySource: source,
		FetchedAt:     time.Now().UTC().Format(time.RFC3339),
		Periods:       rows,
	}
	path := filepath.Join(root, "companies", cik, "consensus.json")
	return writeJSON(path, doc)
}

func readSubmissions(path string) (*struct {
	Name          string   `json:"name"`
	Tickers       []string `json:"tickers"`
	FiscalYearEnd string   `json:"fiscalYearEnd"`
}, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var s struct {
		Name          string   `json:"name"`
		Tickers       []string `json:"tickers"`
		FiscalYearEnd string   `json:"fiscalYearEnd"`
	}
	if err := json.Unmarshal(b, &s); err != nil {
		return nil, err
	}
	return &s, nil
}

type similarPeer struct {
	CompanyName string  `json:"company_name"`
	CompanyID   *string `json:"company_id"`
	Ticker      string  `json:"ticker"`
	Fetch       bool    `json:"fetch"`
}

type similarList struct {
	SimilarCompanies []similarPeer `json:"similar_companies"`
}

func readSimilarList(path string) (*similarList, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var l similarList
	if err := json.Unmarshal(b, &l); err != nil {
		return nil, err
	}
	return &l, nil
}

func parseSet(v string) map[string]bool {
	out := map[string]bool{}
	for _, part := range strings.Split(v, ",") {
		part = strings.TrimSpace(part)
		if part != "" {
			out[part] = true
		}
	}
	return out
}

func envOr(k, def string) string {
	if v := strings.TrimSpace(os.Getenv(k)); v != "" {
		return v
	}
	return def
}

func envDuration(k string, def time.Duration) time.Duration {
	raw := strings.TrimSpace(os.Getenv(k))
	if raw == "" {
		return def
	}
	d, err := time.ParseDuration(raw)
	if err != nil || d <= 0 {
		return def
	}
	return d
}

func countNonEmpty(vals ...string) int {
	n := 0
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			n++
		}
	}
	return n
}

func keyFor(end, duration string) string { return end + "|" + duration }

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
