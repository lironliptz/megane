package financials

import (
	"context"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// GapKind classifies a qualifying accession's extraction status.
type GapKind string

const (
	// GapHasLocal: prompt 8's local extractor already produced an artifact.
	GapHasLocal GapKind = "local"
	// GapFillable: no local artifact, but the accession shipped an XBRL bundle,
	// so SEC's aggregate feed will hold facts for it.
	GapFillable GapKind = "fillable"
	// GapNoXBRL: the accession carries no XBRL at all. No source can fill it --
	// these are press releases filed without structured data.
	GapNoXBRL GapKind = "no_xbrl"
)

// Gap is one qualifying accession and what can be done about it.
type Gap struct {
	Dir        string
	Accession  string
	FilingDate string
	Form       string
	Category   string
	Kind       GapKind
}

// ClassifyGaps reports the coverage picture for a company without touching the
// network.
//
// The presence of an XBRL bundle in the accession folder predicts whether SEC
// holds facts for it: measured across the reference corpus's 51 unfilled
// qualifying accessions, the predicate agreed with the API in every case. So a
// user can see the whole gap -- and what it would cost to close -- before a
// single request leaves the machine.
func ClassifyGaps(root, cik string) ([]Gap, error) {
	dirs, err := accessionDirs(root, cik)
	if err != nil {
		return nil, err
	}
	var out []Gap
	for _, dir := range dirs {
		var mj metaJSON
		if err := readJSON(filepath.Join(dir, "meta.json"), &mj); err != nil {
			continue
		}
		if !qualifyingCategories[mj.Category] {
			continue
		}
		acc := mj.Accession
		if acc == "" {
			acc = filepath.Base(dir)
		}
		g := Gap{
			Dir: dir, Accession: acc, FilingDate: mj.FilingDate,
			Form: mj.Form, Category: mj.Category,
		}
		switch {
		case hasLocalArtifact(dir):
			g.Kind = GapHasLocal
		case hasXBRLBundle(dir):
			g.Kind = GapFillable
		default:
			g.Kind = GapNoXBRL
		}
		out = append(out, g)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Accession < out[j].Accession })
	return out, nil
}

// hasLocalArtifact reports whether a trusted, locally-sourced financials.json
// already exists. An artifact this pass wrote itself does not count as local.
func hasLocalArtifact(dir string) bool {
	f, err := Load(dir)
	if err != nil || f == nil {
		return false
	}
	// If the local artifact contains suspect/withheld metrics, we do not treat
	// it as a trusted/complete local artifact, allowing gap-fill to resolve it.
	for _, l := range append(append([]Line{}, f.Statements.Income...), f.Statements.Balance...) {
		if l.Confidence == ConfidenceSuspect {
			return false
		}
	}
	for _, l := range append(append([]Line{}, f.Statements.Income...), f.Statements.Balance...) {
		switch l.Source {
		case SourceInstance, SourceRendered, SourceBoth, SourceAdjudicated:
			return true
		}
	}
	// A no-facts or API-sourced artifact is not a local extraction.
	return false
}

func hasXBRLBundle(dir string) bool {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), "-xbrl.zip") {
			return true
		}
	}
	return false
}

// FillReport summarizes a gap-fill run.
type FillReport struct {
	Filled       int
	NoFacts      int
	SkippedLocal int
	Withheld     int
	Results      []Result
}

// FillGaps writes financials.json for qualifying accessions the local extractor
// could not handle, using SEC's aggregate facts.
//
// Local extraction always wins: an accession that already has a locally-sourced
// artifact is never touched. The API is not a superset of local extraction --
// the aggregate feed lags recent filings, and the reference corpus has two
// locally-extracted filings absent from it -- so the two sources are
// complementary rather than ranked.
func FillGaps(ctx context.Context, root, cik string, c *Client, opts Options) (*FillReport, error) {
	gaps, err := ClassifyGaps(root, cik)
	if err != nil {
		return nil, err
	}
	rep := &FillReport{}

	// One Company Facts fetch per CIK — never per accession, and never with an
	// empty CIK when --cik is omitted and the root holds multiple companies.
	indices := map[string]*factIndex{}
	for _, g := range gaps {
		if g.Kind != GapFillable {
			continue
		}
		companyCIK, err := cikFromAccessionDir(root, g.Dir)
		if err != nil {
			return nil, err
		}
		if _, ok := indices[companyCIK]; ok {
			continue
		}
		if c == nil {
			return nil, fmt.Errorf("financials: fillable gaps found but no Company Facts client configured")
		}
		cf, err := c.Facts(ctx, root, companyCIK, opts.Force)
		if err != nil {
			return nil, err
		}
		indices[companyCIK] = buildIndex(cf)
	}

	for _, g := range gaps {
		switch g.Kind {
		case GapHasLocal:
			rep.SkippedLocal++
		case GapFillable:
			companyCIK, err := cikFromAccessionDir(root, g.Dir)
			if err != nil {
				return nil, err
			}
			f, note := fillOne(indices[companyCIK], g, opts)
			if f == nil {
				if opts.WriteNoFacts {
					f = noFactsArtifact(g, note, opts)
				} else {
					continue
				}
			}
			for _, l := range append(append([]Line{}, f.Statements.Income...), f.Statements.Balance...) {
				if l.Confidence == ConfidenceSuspect {
					rep.Withheld++
				}
			}
			if err := writeArtifact(g.Dir, f, opts); err != nil {
				return nil, err
			}
			rep.Filled++
			rep.Results = append(rep.Results, Result{Dir: g.Dir, Accession: g.Accession, Financials: f})
		case GapNoXBRL:
			if !opts.WriteNoFacts {
				continue
			}
			f := noFactsArtifact(g, "no XBRL in the accession folder; SEC Company Facts holds no entries for it", opts)
			if err := writeArtifact(g.Dir, f, opts); err != nil {
				return nil, err
			}
			rep.NoFacts++
			rep.Results = append(rep.Results, Result{Dir: g.Dir, Accession: g.Accession, Financials: f})
		}
	}
	return rep, nil
}

// fillOne builds one accession's artifact from the aggregate facts. A nil
// return means nothing usable was found; note says why.
func fillOne(idx *factIndex, g Gap, opts Options) (*FilingFinancials, string) {
	if idx == nil {
		return nil, "no Company Facts document available"
	}
	if _, ok := idx.byAccession[g.Accession]; !ok {
		return nil, "SEC Company Facts holds no entries for this accession"
	}
	period, ok := selectPeriod(idx, g.Accession)
	if !ok {
		return nil, "could not determine the reporting period from SEC facts"
	}
	scale := resolveScale(idx, g.Accession, period)

	out := &FilingFinancials{
		Version:     SchemaVersion,
		ExtractedAt: opts.now(),
		Accession:   g.Accession,
		Currency:    UnitUSD,
		Period:      period,
		ParseNotes: []string{
			"gap-filled from SEC Company Facts (" + idx.FetchedAt.Format("2006-01-02") + ")",
		},
	}
	if scale.Irreconcilable {
		out.ParseNotes = append(out.ParseNotes,
			"scale could not be reconciled across accessions; all monetary metrics withheld")
		out.ParseNotes = append(out.ParseNotes, scale.Evidence...)
	}
	if scale.Factor != 1 {
		out.ParseNotes = append(out.ParseNotes, scale.Evidence...)
	}

	build := func(key string, instant bool) *Line {
		m, ok := metricFor(key)
		if !ok {
			return nil
		}
		f, ok := pickEntry(idx, g.Accession, key, period.EndDate, period.Duration, instant)
		if !ok {
			return nil
		}
		factor := 1.0
		if m.Unit == UnitUSD {
			factor = scale.Factor
		}
		val := f.Val * factor

		var conf, src string
		var notes []string
		if scale.Irreconcilable && m.Unit == UnitUSD {
			conf, src = ConfidenceSuspect, SourceCompanyFacts
			notes = []string{"scale irreconcilable across accessions; withheld"}
		} else {
			conf, src, notes = gradeEntry(key, scale.Corroborated[key], factor != 1,
				plausibleMagnitude(idx, key, f.Val))
		}
		line := Line{
			Key: key, Display: m.Display, Element: "sec:" + key,
			Value: val, ValueFmt: FormatValue(val, m.Unit), Unit: m.Unit,
			Confidence: conf, Source: src,
			Notes: append(notes, "concept period "+f.Start+".."+f.End),
		}
		prior, hasPrior := indexedFact{}, false
		if instant {
			prior, hasPrior = priorInstant(idx, g.Accession, key, period.EndDate)
		} else {
			prior, hasPrior = priorEnd(idx, g.Accession, key, period.EndDate, period.Duration, false)
		}
		if p, ok := prior, hasPrior; ok {
			pv := p.Val * factor
			line.PriorValue = &pv
			if instant {
				line.PriorLabel = "vs " + p.End
			} else if pv > 0 {
				y := math.Round((val/pv-1)*1000) / 10
				line.YoYPct = &y
			}
		}
		return &line
	}

	for _, key := range incomeKeys {
		if l := build(key, false); l != nil {
			out.Statements.Income = append(out.Statements.Income, *l)
		}
	}
	for _, key := range balanceKeys {
		if l := build(key, true); l != nil {
			out.Statements.Balance = append(out.Statements.Balance, *l)
		}
	}
	if len(out.Statements.Income) == 0 && len(out.Statements.Balance) == 0 {
		return nil, "SEC facts held no headline metrics for this period"
	}
	sortByRank(out.Statements.Income)
	sortByRank(out.Statements.Balance)
	return out, ""
}

// noFactsArtifact records that nothing can be extracted, and why.
func noFactsArtifact(g Gap, reason string, opts Options) *FilingFinancials {
	return &FilingFinancials{
		Version:     SchemaVersion,
		ExtractedAt: opts.now(),
		Accession:   g.Accession,
		Period:      Period{EndDate: g.FilingDate},
		ParseNotes:  []string{"source: " + SourceNone, reason},
	}
}

func writeArtifact(dir string, f *FilingFinancials, opts Options) error {
	if opts.DryRun {
		return nil
	}
	path := filepath.Join(dir, FileName)
	if _, err := os.Stat(path); err == nil && !opts.Force {
		return nil
	}
	return Write(dir, f)
}

func cikFromAccessionDir(root, dir string) (string, error) {
	rel, err := filepath.Rel(filepath.Join(root, companiesDirName), dir)
	if err != nil {
		return "", fmt.Errorf("financials: accession dir outside companies root: %s", dir)
	}
	parts := strings.Split(filepath.ToSlash(rel), "/")
	if len(parts) == 0 || parts[0] == "" || parts[0] == "." {
		return "", fmt.Errorf("financials: cannot determine CIK from %s", dir)
	}
	return parts[0], nil
}
