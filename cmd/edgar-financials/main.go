// Command edgar-financials extracts structured financials from filings on disk
// and writes financials.json beside each accession's meta.json.
//
// It is deliberately standalone: the Go ingest this would otherwise hang off
// (cmd/edgar) does not exist yet. financials.ExtractAll is the seam, so when it
// does land it can call the same function and this command can retire.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"megane/internal/edgar/financials"
)

func main() {
	root := flag.String("root", "./fileDB", "fileDB root containing companies/")
	cik := flag.String("cik", "", "restrict to one CIK (default: every company)")
	all := flag.Bool("all", false, "overwrite existing financials.json")
	dryRun := flag.Bool("dry-run", false, "compute everything, write nothing")
	report := flag.Bool("report", false, "print the extracted table and exit non-zero if suspects exceed -max-suspects")
	maxSuspects := flag.Int("max-suspect-filings", 1, "acceptance threshold for -report: filings holding at least one withheld metric")
	gapsOnly := flag.Bool("gaps", false, "classify qualifying accessions by extraction status and exit (never fetches)")
	companyFacts := flag.Bool("companyfacts", false, "fetch or refresh the SEC Company Facts cache for the CIK")
	fillGaps := flag.Bool("fill-gaps", false, "gap-fill accessions local extraction cannot handle, from SEC Company Facts")
	noFactsRecords := flag.Bool("no-facts-records", true, "with -fill-gaps, also record why unfillable accessions have no figures")
	flag.Parse()

	// -gaps is a filesystem question: answer it and exit without a client.
	if *gapsOnly {
		if err := printGaps(*root, *cik); err != nil {
			fmt.Fprintln(os.Stderr, "edgar-financials:", err)
			os.Exit(2)
		}
		return
	}

	opts := financials.Options{Force: *all, DryRun: *dryRun, WriteNoFacts: *noFactsRecords}
	ctx := context.Background()

	// Local extraction always runs first, so a gap-fill can only ever touch
	// accessions the stricter local path declined.
	rep, err := financials.ExtractAll(ctx, *root, *cik, opts)
	if err != nil {
		fmt.Fprintln(os.Stderr, "edgar-financials:", err)
		os.Exit(2)
	}

	var fill *financials.FillReport
	if *fillGaps || *companyFacts {
		client, err := clientFromEnv()
		if err != nil {
			fmt.Fprintln(os.Stderr, "edgar-financials:", err)
			os.Exit(2)
		}
		if *companyFacts && !*fillGaps {
			cf, err := client.Facts(ctx, *root, *cik, *all)
			if err != nil {
				fmt.Fprintln(os.Stderr, "edgar-financials:", err)
				os.Exit(2)
			}
			fmt.Printf("company facts: %s cik=%d fetchedAt=%s cache=%s\n",
				cf.EntityName, cf.CIK, cf.FetchedAt.Format(time.RFC3339),
				financials.CachePath(*root, *cik))
			return
		}
		fill, err = financials.FillGaps(ctx, *root, *cik, client, opts)
		if err != nil {
			fmt.Fprintln(os.Stderr, "edgar-financials:", err)
			os.Exit(2)
		}
	}

	results := rep.Results
	sort.SliceStable(results, func(i, j int) bool { return results[i].Accession < results[j].Accession })

	written, skipped := 0, 0
	for _, r := range results {
		if r.Written {
			written++
		}
		if r.Skipped != "" {
			skipped++
		}
	}

	if *report {
		printReport(results)
	}
	fmt.Printf("accessions=%d written=%d skipped=%d suspect_metrics=%d suspect_filings=%d\n",
		len(results), written, skipped, rep.Suspects, rep.SuspectFilings)
	if fill != nil {
		fmt.Printf("gap-fill: filled=%d no_facts_records=%d skipped_local=%d withheld_metrics=%d\n",
			fill.Filled, fill.NoFacts, fill.SkippedLocal, fill.Withheld)
	}

	if *report && rep.SuspectFilings > *maxSuspects {
		fmt.Fprintf(os.Stderr, "edgar-financials: %d filings with withheld metrics exceeds threshold %d\n",
			rep.SuspectFilings, *maxSuspects)
		os.Exit(1)
	}
}

// printGaps answers the coverage question from the filesystem alone.
func printGaps(root, cik string) error {
	gaps, err := financials.ClassifyGaps(root, cik)
	if err != nil {
		return err
	}
	counts := map[financials.GapKind]int{}
	fmt.Printf("%-24s %-12s %-6s %-18s %s\n", "accession", "filed", "form", "category", "status")
	for _, g := range gaps {
		counts[g.Kind]++
		fmt.Printf("%-24s %-12s %-6s %-18s %s\n", g.Accession, g.FilingDate, g.Form, g.Category, g.Kind)
	}
	fmt.Printf("\nqualifying=%d local=%d fillable=%d no_xbrl=%d  (no network used)\n",
		len(gaps), counts[financials.GapHasLocal], counts[financials.GapFillable], counts[financials.GapNoXBRL])
	return nil
}

// clientFromEnv builds the SEC client, refusing to run without a real contact.
func clientFromEnv() (*financials.Client, error) {
	ua := strings.TrimSpace(os.Getenv("SEC_EDGAR_USER_AGENT"))
	if ua == "" {
		return nil, fmt.Errorf("SEC_EDGAR_USER_AGENT is required: SEC rejects anonymous clients. " +
			"Set it to \"AppName contact@example.com\" in .env")
	}
	ttl := 7 * 24 * time.Hour
	if v := os.Getenv("SEC_COMPANYFACTS_TTL"); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			ttl = d
		}
	}
	return financials.NewClient(ua, ttl), nil
}

func printReport(results []financials.Result) {
	fmt.Printf("%-24s %-9s %10s %10s %8s %10s %7s %10s  %s\n",
		"accession", "period", "revenue", "prior", "yoy", "net income", "eps", "cash", "notes")
	for _, r := range results {
		f := r.Financials
		if f == nil {
			continue
		}
		get := func(key string) *financials.Line {
			for _, set := range [][]financials.Line{f.Statements.Income, f.Statements.Balance} {
				for i := range set {
					if set[i].Key == key {
						return &set[i]
					}
				}
			}
			return nil
		}
		show := func(l *financials.Line) string {
			if l == nil {
				return "—"
			}
			s := l.ValueFmt
			if !l.Publishable() {
				s = "WITHHELD"
			}
			return s
		}
		rev := get(financials.KeyTotalRevenues)
		prior, yoy := "—", "—"
		if rev != nil && rev.PriorValue != nil {
			prior = financials.FormatValue(*rev.PriorValue, financials.UnitUSD)
		}
		if rev != nil && rev.YoYPct != nil {
			yoy = fmt.Sprintf("%+.1f%%", *rev.YoYPct)
		}
		var notes []string
		for _, set := range [][]financials.Line{f.Statements.Income, f.Statements.Balance} {
			for _, l := range set {
				if l.Confidence == financials.ConfidenceSuspect {
					notes = append(notes, l.Key+":"+strings.Join(l.Notes, "|"))
				}
			}
		}
		fmt.Printf("%-24s %-9s %10s %10s %8s %10s %7s %10s  %s\n",
			r.Accession, f.Period.Label, show(rev), prior, yoy,
			show(get(financials.KeyNetIncome)), show(get(financials.KeyEPSBasic)),
			show(get(financials.KeyCash)), strings.Join(notes, " ; "))
	}
}
