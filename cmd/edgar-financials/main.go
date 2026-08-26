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

	"megane/internal/edgar/financials"
)

func main() {
	root := flag.String("root", "./fileDB", "fileDB root containing companies/")
	cik := flag.String("cik", "", "restrict to one CIK (default: every company)")
	all := flag.Bool("all", false, "overwrite existing financials.json")
	dryRun := flag.Bool("dry-run", false, "compute everything, write nothing")
	report := flag.Bool("report", false, "print the extracted table and exit non-zero if suspects exceed -max-suspects")
	maxSuspects := flag.Int("max-suspect-filings", 1, "acceptance threshold for -report: filings holding at least one withheld metric")
	flag.Parse()

	opts := financials.Options{Force: *all, DryRun: *dryRun}

	rep, err := financials.ExtractAll(context.Background(), *root, *cik, opts)
	if err != nil {
		fmt.Fprintln(os.Stderr, "edgar-financials:", err)
		os.Exit(2)
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

	if *report && rep.SuspectFilings > *maxSuspects {
		fmt.Fprintf(os.Stderr, "edgar-financials: %d filings with withheld metrics exceeds threshold %d\n",
			rep.SuspectFilings, *maxSuspects)
		os.Exit(1)
	}
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
