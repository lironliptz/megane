package financials

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

// qualifyingCategories are the only meta.json categories worth extracting.
var qualifyingCategories = map[string]bool{
	"quarterly_results": true,
	"annual_report":     true,
}

// Options controls a corpus run.
type Options struct {
	Force  bool // re-write financials.json even when it already exists
	DryRun bool // compute everything, write nothing
	LLM    Adjudicator
	Now    func() time.Time
}

func (o Options) now() time.Time {
	if o.Now != nil {
		return o.Now().UTC().Truncate(time.Second)
	}
	return time.Now().UTC().Truncate(time.Second)
}

// Result is one accession's outcome.
type Result struct {
	Dir        string
	Accession  string
	Financials *FilingFinancials
	Written    bool
	Skipped    string
}

// Report summarizes a corpus run.
type Report struct {
	Results []Result
	// Suspects counts withheld metrics; SuspectFilings counts filings holding at
	// least one. The filing count is the meaningful acceptance metric: a filer
	// who mis-tags at source does so for every monetary figure in the filing, so
	// one bad filing produces a whole cluster of withheld metrics.
	Suspects       int
	SuspectFilings int
}

type metaJSON struct {
	Accession string `json:"accession"`
	Category  string `json:"category"`
}

// ExtractAccession extracts one accession without the cross-filing chain check,
// which by definition needs its neighbours. ExtractAll runs this first and then
// grades the chain.
func ExtractAccession(dir string, opts Options) (*FilingFinancials, error) {
	instPath, err := FindInstance(dir)
	if err != nil || instPath == "" {
		return nil, err
	}
	fh, err := os.Open(instPath)
	if err != nil {
		return nil, err
	}
	defer fh.Close()
	doc, err := ParseInstance(fh)
	if err != nil {
		return nil, err
	}
	if len(doc.Facts) == 0 {
		return nil, nil
	}

	period := doc.SelectPeriod()
	curDur, okDur := doc.currentDurationCtx(period)
	if !okDur {
		return nil, nil
	}
	priorDur, okPrior := doc.priorDurationCtx(period, curDur)
	curInst, okInst := doc.currentInstantCtx(period)
	priorInst, okPriorInst := doc.priorInstantCtx(period)

	out := &FilingFinancials{
		Version:     SchemaVersion,
		ExtractedAt: opts.now(),
		Accession:   filepath.Base(dir),
		Currency:    UnitUSD,
		Period:      period,
	}

	// Rendered corroboration. A missing or unresolvable FilingSummary.xml is
	// recorded (detector F) rather than fatal: the instance alone still yields
	// single_source lines.
	roles, err := StatementFiles(dir)
	if err != nil {
		out.ParseNotes = append(out.ParseNotes, fmt.Sprintf("%s: %v", DetectorRoleUnresolved, err))
	}
	tables := map[Role]*renderedTable{}
	for _, role := range []Role{RoleIncome, RoleBalance, RoleCashFlow} {
		file, ok := roles[role]
		if !ok {
			out.ParseNotes = append(out.ParseNotes,
				fmt.Sprintf("%s: no %s statement in FilingSummary.xml", DetectorRoleUnresolved, role))
			continue
		}
		t, err := ReadRendered(filepath.Join(dir, file))
		if err != nil {
			out.ParseNotes = append(out.ParseNotes, fmt.Sprintf("rendered %s: %v", file, err))
			continue
		}
		if t.ScaleAssumed {
			out.ParseNotes = append(out.ParseNotes,
				fmt.Sprintf("%s: no scale in caption, assumed thousands", file))
		}
		tables[role] = t
	}

	values := map[string]float64{}

	buildLine := func(key string, role Role, cur, prior ctx, hasPrior bool, wantYoY bool, priorLabel string) *Line {
		m, ok := metricFor(key)
		if !ok {
			return nil
		}
		cands := doc.pick(key, cur)
		var signals []string
		var instVal *float64
		var element string
		if len(cands) > 1 {
			signals = append(signals, fmt.Sprintf("%s: %d undimensioned candidates for %s",
				DetectorAmbiguousFact, len(cands), key))
		}
		if len(cands) > 0 {
			v := cands[0].Fact.Value
			instVal = &v
			element = cands[0].Fact.Element
			if detectImplausibleTagging(cands[0].Fact, m.Unit) {
				signals = append(signals, fmt.Sprintf("%s: decimals=%q value=%g below plausible magnitude",
					DetectorImplausibleTagging, cands[0].Fact.Decimals, v))
			}
		}
		var rendVal *float64
		if t := tables[role]; t != nil && element != "" {
			if v, ok := t.Value(element, m.Unit == UnitUSDPerShare); ok {
				rendVal = &v
			}
		}
		if instVal == nil && rendVal == nil {
			return nil
		}
		conf, src, notes := Grade(instVal, rendVal, m.Unit, signals)

		val := 0.0
		if instVal != nil {
			val = *instVal
		} else {
			val = *rendVal
		}
		line := Line{
			Key:        key,
			Display:    m.Display,
			Element:    element,
			Value:      val,
			ValueFmt:   FormatValue(val, m.Unit),
			Unit:       m.Unit,
			Confidence: conf,
			Source:     src,
			Notes:      notes,
		}
		if line.Publishable() {
			values[key] = val
		}
		if hasPrior {
			if pc := doc.pick(key, prior); len(pc) > 0 {
				pv := pc[0].Fact.Value
				line.PriorValue = &pv
				line.PriorLabel = priorLabel
				// Year-over-year is computed for duration facts only. A balance
				// sheet's comparative is the prior year-END, not the prior-year
				// same quarter, so a percentage against it would be misleading.
				// A percentage change is only meaningful when the base period is
				// positive. Across a sign flip -- a swing from loss to profit --
				// the arithmetic is defined but the result is nonsense: a real
				// filing in the corpus goes from $(0.04) to $0.04 a share, which
				// this formula would render as "-200.0% YoY" on what was in fact
				// a return to profitability. Omit it and show the figure alone.
				if wantYoY && pv > 0 {
					// One decimal: this is a display figure, and full float
					// precision only makes the JSON noisy to diff.
					y := math.Round((val/pv-1)*1000) / 10
					line.YoYPct = &y
				}
			}
		}
		return &line
	}

	for _, key := range incomeKeys {
		if l := buildLine(key, RoleIncome, curDur, priorDur, okPrior, true, ""); l != nil {
			out.Statements.Income = append(out.Statements.Income, *l)
		}
	}
	if okInst {
		priorLabel := ""
		if okPriorInst {
			priorLabel = "vs " + priorInst.Instant
		}
		for _, key := range balanceKeys {
			if l := buildLine(key, RoleBalance, curInst, priorInst, okPriorInst, false, priorLabel); l != nil {
				out.Statements.Balance = append(out.Statements.Balance, *l)
			}
		}
	}
	for _, key := range cashFlowKeys {
		if l := buildLine(key, RoleCashFlow, curDur, priorDur, okPrior, true, ""); l != nil {
			out.Statements.CashFlow = append(out.Statements.CashFlow, *l)
		}
	}

	out.Statements.Segments = doc.segmentsFor(period, curDur, values[KeyTotalRevenues])
	sort.SliceStable(out.Statements.Segments, func(i, j int) bool {
		return out.Statements.Segments[i].Value > out.Statements.Segments[j].Value
	})

	// Detector C: the filer's own arithmetic.
	if note, bad := checkCalcInvariant(values); bad {
		markSuspect(out, []string{KeyTotalRevenues, KeyCostOfRevenues, KeyGrossProfit}, note)
	}
	// A single member equal to the consolidated total is not a breakdown.
	if len(out.Statements.Segments) < 2 {
		out.Statements.Segments = nil
	}
	if len(out.Statements.Segments) > 0 && !segmentsReconcile(values[KeyTotalRevenues], out.Statements.Segments) {
		out.ParseNotes = append(out.ParseNotes,
			"segment breakdown does not reconcile to total revenues (inter-segment eliminations); breakdown dropped")
		out.Statements.Segments = nil
	}

	sortByRank(out.Statements.Income)
	sortByRank(out.Statements.Balance)
	sortByRank(out.Statements.CashFlow)
	return out, nil
}

func sortByRank(lines []Line) {
	sort.SliceStable(lines, func(i, j int) bool {
		return rankIndex(lines[i].Key) < rankIndex(lines[j].Key)
	})
}

// markSuspect downgrades named lines and records why.
func markSuspect(f *FilingFinancials, keys []string, note string) {
	want := map[string]bool{}
	for _, k := range keys {
		want[k] = true
	}
	for _, set := range [][]Line{f.Statements.Income, f.Statements.Balance, f.Statements.CashFlow} {
		for i := range set {
			if want[set[i].Key] {
				set[i].Confidence = ConfidenceSuspect
				set[i].Notes = append(set[i].Notes, note)
			}
		}
	}
}

// ExtractAll walks one company (or all of them when cik is empty) and writes
// financials.json for every qualifying accession.
//
// The run is two-pass by necessity: detector D compares each filing's prior
// period against the previous comparable filing's current period, so nothing can
// be written until every candidate exists.
func ExtractAll(ctx context.Context, root, cik string, opts Options) (*Report, error) {
	dirs, err := accessionDirs(root, cik)
	if err != nil {
		return nil, err
	}
	rep := &Report{}

	// Pass 1 — candidates.
	var cands []Result
	for _, dir := range dirs {
		var mj metaJSON
		if err := readJSON(filepath.Join(dir, "meta.json"), &mj); err != nil {
			continue
		}
		if !qualifyingCategories[mj.Category] {
			continue
		}
		f, err := ExtractAccession(dir, opts)
		if err != nil {
			cands = append(cands, Result{Dir: dir, Accession: filepath.Base(dir), Skipped: err.Error()})
			continue
		}
		if f == nil {
			continue
		}
		cands = append(cands, Result{Dir: dir, Accession: f.Accession, Financials: f})
	}

	// Pass 2 — detector D across comparable neighbours.
	chainCheck(cands)

	// Pass 3 — optional adjudication of what is still suspect.
	if opts.LLM != nil {
		for _, r := range cands {
			if r.Financials != nil {
				adjudicateFiling(ctx, opts.LLM, r.Dir, r.Financials)
			}
		}
	}

	for i := range cands {
		r := &cands[i]
		if r.Financials == nil {
			rep.Results = append(rep.Results, *r)
			continue
		}
		filingHasSuspect := false
		for _, l := range append(append([]Line{}, r.Financials.Statements.Income...), r.Financials.Statements.Balance...) {
			if l.Confidence == ConfidenceSuspect {
				rep.Suspects++
				filingHasSuspect = true
			}
		}
		if filingHasSuspect {
			rep.SuspectFilings++
		}
		path := filepath.Join(r.Dir, FileName)
		if _, err := os.Stat(path); err == nil && !opts.Force {
			r.Skipped = "exists (use --all to overwrite)"
			rep.Results = append(rep.Results, *r)
			continue
		}
		if !opts.DryRun {
			if err := Write(r.Dir, r.Financials); err != nil {
				return nil, err
			}
			r.Written = true
		}
		rep.Results = append(rep.Results, *r)
	}
	return rep, nil
}

// chainCheck is detector D: each filing's prior-period figure must equal the
// current figure of the filing covering that same earlier period.
//
// Comparables are matched by period end date, not by position in the list. The
// corpus does not hold a filing for every quarter, so list-adjacent filings are
// usually not a year apart and comparing them would report a break on almost
// every filing. When no filing covers the prior period, the check simply cannot
// run and the line keeps whatever grade the other detectors gave it.
func chainCheck(rs []Result) {
	byPeriod := map[string]*FilingFinancials{}
	var all []*FilingFinancials
	for i := range rs {
		f := rs[i].Financials
		if f == nil || f.Period.EndDate == "" {
			continue
		}
		byPeriod[f.Period.Duration+"|"+f.Period.EndDate] = f
		all = append(all, f)
	}
	sort.SliceStable(all, func(i, j int) bool { return all[i].Period.EndDate < all[j].Period.EndDate })

	for _, cur := range all {
		curLine := findLine(cur, KeyTotalRevenues)
		if curLine == nil || curLine.PriorValue == nil {
			continue
		}
		end, ok := parseDay(cur.Period.EndDate)
		if !ok {
			continue
		}
		// The prior comparable ends about a year earlier; allow a fortnight of
		// drift for 52/53-week fiscal calendars.
		var prev *FilingFinancials
		for delta := -14; delta <= 14 && prev == nil; delta++ {
			key := cur.Period.Duration + "|" + end.AddDate(-1, 0, delta).Format("2006-01-02")
			prev = byPeriod[key]
		}
		if prev == nil {
			continue
		}
		prevLine := findLine(prev, KeyTotalRevenues)
		if prevLine == nil {
			continue
		}
		if agree(*curLine.PriorValue, prevLine.Value, UnitUSD) {
			continue
		}
		note := fmt.Sprintf("%s: prior revenue %g does not match %s (%s) current revenue %g",
			DetectorChainBreak, *curLine.PriorValue, prev.Accession, prev.Period.Label, prevLine.Value)
		// A break says the two filings disagree, not which one is wrong. When one
		// side is already independently explained — detector B has identified it
		// as mis-tagged at source — the fault is located and the other side keeps
		// its own grade rather than being condemned by association.
		curExplained := hasSignal(curLine, DetectorImplausibleTagging)
		prevExplained := hasSignal(prevLine, DetectorImplausibleTagging)
		switch {
		case prevExplained && !curExplained:
			markSuspect(prev, []string{KeyTotalRevenues}, note)
		case curExplained && !prevExplained:
			markSuspect(cur, []string{KeyTotalRevenues}, note)
		default:
			markSuspect(cur, []string{KeyTotalRevenues}, note)
			markSuspect(prev, []string{KeyTotalRevenues}, note)
		}
	}
}

// hasSignal reports whether a detector already fired on a line.
func hasSignal(l *Line, detector string) bool {
	for _, n := range l.Notes {
		if strings.HasPrefix(n, detector) {
			return true
		}
	}
	return false
}

func findLine(f *FilingFinancials, key string) *Line {
	for i := range f.Statements.Income {
		if f.Statements.Income[i].Key == key {
			return &f.Statements.Income[i]
		}
	}
	return nil
}

func accessionDirs(root, cik string) ([]string, error) {
	base := filepath.Join(root, "companies")
	var ciks []string
	if cik != "" {
		ciks = []string{cik}
	} else {
		entries, err := os.ReadDir(base)
		if err != nil {
			return nil, err
		}
		for _, e := range entries {
			if e.IsDir() {
				ciks = append(ciks, e.Name())
			}
		}
	}
	var out []string
	for _, c := range ciks {
		years, err := os.ReadDir(filepath.Join(base, c))
		if err != nil {
			return nil, err
		}
		for _, y := range years {
			if !y.IsDir() {
				continue
			}
			if n, err := strconv.Atoi(y.Name()); err != nil || n < 1000 || n > 9999 {
				continue
			}
			accs, err := os.ReadDir(filepath.Join(base, c, y.Name()))
			if err != nil {
				continue
			}
			for _, a := range accs {
				if a.IsDir() {
					out = append(out, filepath.Join(base, c, y.Name(), a.Name()))
				}
			}
		}
	}
	sort.Strings(out)
	return out, nil
}

func readJSON(path string, v any) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, v)
}
