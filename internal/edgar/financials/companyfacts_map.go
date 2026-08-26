package financials

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"time"
)

// unitUSDLabel and unitPerShareLabel are the Company Facts unit names.
const (
	unitUSDLabel      = "USD"
	unitPerShareLabel = "USD/shares"
)

// conceptKey maps a Company Facts (namespace, concept) pair onto a canonical
// metric key, reusing the ranked aliases the instance reader already uses.
//
// elements.go stores qualified names ("ifrs-full:Revenue") while Company Facts
// nests namespace and concept separately, so this splits rather than
// introducing a second dictionary with its own precedence rules.
func conceptKey(ns, concept string) (key string, rank int, ok bool) {
	qualified := ns + ":" + concept
	for _, m := range metrics {
		for i, el := range m.Elements {
			if el == qualified {
				return m.Key, i, true
			}
		}
	}
	return "", 0, false
}

// indexedFact is one fact after unit filtering.
type indexedFact struct {
	Key   string
	Accn  string
	Form  string
	FP    string
	FY    int
	Start string
	End   string
	Val   float64
	Unit  string
	Rank  int
}

// periodKey identifies one metric in one reporting period, across all filings.
type periodKey struct{ Key, Start, End string }

func (f indexedFact) periodKey() periodKey {
	return periodKey{Key: f.Key, Start: f.Start, End: f.End}
}

// factIndex holds the document twice: by accession (what one filing reported)
// and by period (what every filing reported about the same span).
//
// The by-period view is what makes scale reconciliation possible without a
// second network call: issuers restate prior periods in every later filing, so
// the redundancy needed to detect a mis-tagged figure is already inside the one
// cached document.
type factIndex struct {
	byAccession map[string]map[string][]indexedFact
	byPeriod    map[periodKey][]indexedFact
	FetchedAt   time.Time
}

// buildIndex filters and indexes the aggregate document.
//
// The unit filter runs first and is absolute: a monetary metric accepts only
// USD and a per-share metric only USD/shares. Facts in any other unit are
// dropped, never converted -- the reference corpus contains an ILS revenue fact
// of 10,000,000,000 for a company whose USD revenue that year was 160,953,000,
// and publishing it would render "$10.0B".
func buildIndex(cf *CompanyFacts) *factIndex {
	idx := &factIndex{
		byAccession: map[string]map[string][]indexedFact{},
		byPeriod:    map[periodKey][]indexedFact{},
		FetchedAt:   cf.FetchedAt,
	}
	for ns, concepts := range cf.Facts {
		for concept, cfacts := range concepts {
			key, rank, ok := conceptKey(ns, concept)
			if !ok {
				continue
			}
			wantUnit := unitUSDLabel
			if m, found := metricFor(key); found && m.Unit == UnitUSDPerShare {
				wantUnit = unitPerShareLabel
			}
			for unit, entries := range cfacts.Units {
				if unit != wantUnit {
					continue
				}
				for _, e := range entries {
					f := indexedFact{
						Key: key, Accn: e.Accn, Form: e.Form, FP: e.FP, FY: e.FY,
						Start: e.Start, End: e.End, Val: e.Val, Unit: unit, Rank: rank,
					}
					if idx.byAccession[f.Accn] == nil {
						idx.byAccession[f.Accn] = map[string][]indexedFact{}
					}
					idx.byAccession[f.Accn][key] = append(idx.byAccession[f.Accn][key], f)
					idx.byPeriod[f.periodKey()] = append(idx.byPeriod[f.periodKey()], f)
				}
			}
		}
	}
	return idx
}

// Accessions lists every accession the document carries facts for.
func (idx *factIndex) Accessions() []string {
	out := make([]string, 0, len(idx.byAccession))
	for a := range idx.byAccession {
		out = append(out, a)
	}
	sort.Strings(out)
	return out
}

// --- period selection -------------------------------------------------------

func factDurationDays(f indexedFact) int {
	if f.Start == "" {
		return -1
	}
	s, ok1 := parseDay(f.Start)
	e, ok2 := parseDay(f.End)
	if !ok1 || !ok2 {
		return -1
	}
	return int(e.Sub(s).Hours() / 24)
}

// selectPeriod determines which period a filing is about.
//
// A single accession carries the reporting quarter, the year to date, the prior
// year quarter and the prior full year all at once, so the period is taken from
// the latest in-band end date rather than from the first entry -- which would
// yield the prior-year half-year, a plausible number for the wrong period.
func selectPeriod(idx *factIndex, accn string) (Period, bool) {
	byKey := idx.byAccession[accn]
	if byKey == nil {
		return Period{}, false
	}
	anchor := byKey[KeyTotalRevenues]
	if len(anchor) == 0 {
		for _, key := range incomeKeys {
			if len(byKey[key]) > 0 {
				anchor = byKey[key]
				break
			}
		}
	}
	if len(anchor) == 0 {
		return Period{}, false
	}
	fp, fy := anchor[0].FP, anchor[0].FY
	p := Period{Focus: fp}
	switch {
	case strings.EqualFold(fp, "FY"):
		p.Duration = durationP1Y
		p.Label = fmt.Sprintf("FY %d", fy)
	case len(fp) == 2 && (fp[0] == 'Q' || fp[0] == 'q'):
		p.Duration = durationP3M
		p.Label = fmt.Sprintf("%s %d", strings.ToUpper(fp), fy)
	default:
		return Period{}, false
	}
	best := ""
	for _, f := range anchor {
		if !matchesDuration(factDurationDays(f), p.Duration) {
			continue
		}
		if f.End > best {
			best = f.End
		}
	}
	if best == "" {
		return Period{}, false
	}
	p.EndDate = best
	return p, true
}

// pickEntry selects one accession's fact for a metric at a given period end.
func pickEntry(idx *factIndex, accn, key, end, duration string, instant bool) (indexedFact, bool) {
	cands := idx.byAccession[accn][key]
	var best indexedFact
	found := false
	for _, f := range cands {
		if f.End != end {
			continue
		}
		if instant {
			if f.Start != "" {
				continue
			}
		} else if !matchesDuration(factDurationDays(f), duration) {
			continue
		}
		if !found || f.Rank < best.Rank {
			best, found = f, true
		}
	}
	return best, found
}

// priorEnd returns the period end one year before end, as reported by this
// accession, or "" when it reports no comparable.
func priorEnd(idx *factIndex, accn, key, end, duration string, instant bool) (indexedFact, bool) {
	cur, ok := parseDay(end)
	if !ok {
		return indexedFact{}, false
	}
	var best indexedFact
	bestDelta := 1 << 30
	found := false
	for _, f := range idx.byAccession[accn][key] {
		if instant {
			if f.Start != "" {
				continue
			}
		} else if !matchesDuration(factDurationDays(f), duration) {
			continue
		}
		d, ok := parseDay(f.End)
		if !ok {
			continue
		}
		gap := int(cur.Sub(d).Hours() / 24)
		if gap < 350 || gap > 380 {
			continue
		}
		delta := gap - 365
		if delta < 0 {
			delta = -delta
		}
		if !found || delta < bestDelta {
			best, bestDelta, found = f, delta, true
		}
	}
	return best, found
}

// priorInstant returns the comparative for a balance-sheet metric.
//
// The prior fiscal year-end is preferred over the prior-year same quarter, so a
// gap-filled line shows the same comparative a locally extracted one does -- a
// balance sheet presents its year-end column, and two adjacent timeline events
// disagreeing about which comparative they mean would be a wart the reader
// cannot resolve. Falls back to roughly a year earlier when no year-end is
// reported.
func priorInstant(idx *factIndex, accn, key, end string) (indexedFact, bool) {
	cur, ok := parseDay(end)
	if !ok {
		return indexedFact{}, false
	}
	yearEnd := fmt.Sprintf("%d-12-31", cur.Year()-1)
	for _, f := range idx.byAccession[accn][key] {
		if f.Start == "" && f.End == yearEnd {
			return f, true
		}
	}
	return priorEnd(idx, accn, key, end, "", true)
}

// --- scale reconciliation ---------------------------------------------------

// scaleRatioTolerance is how close a ratio must sit to 1 or 1000 to count.
const scaleRatioTolerance = 0.005

// scaleResolution is the outcome of comparing one filing against its peers.
type scaleResolution struct {
	Factor         float64
	Corroborated   map[string]int // metric key -> number of corroborating accessions
	Evidence       []string
	Irreconcilable bool
}

// resolveScale determines the multiplier for one accession's monetary facts.
//
// Mis-tagging is a property of the filing, not of a metric: in the reference
// corpus the affected accession reports revenue, profit and cash each at
// exactly one thousandth of the same periods as reported by a later filing. So
// the factor is decided once from whichever metrics have corroborators and then
// applied filing-wide, rather than being re-derived per metric where a metric
// with no corroborator would have nothing to go on.
//
// Per-share metrics never participate: they are not mis-scaled by thousands.
func resolveScale(idx *factIndex, accn string, p Period) scaleResolution {
	res := scaleResolution{Factor: 1, Corroborated: map[string]int{}}
	votes1, votes1000 := 0, 0

	for _, key := range append(append([]string{}, incomeKeys...), balanceKeys...) {
		m, ok := metricFor(key)
		if !ok || m.Unit != UnitUSD {
			continue
		}
		instant := isInstantKey(key)
		own, ok := pickEntry(idx, accn, key, p.EndDate, p.Duration, instant)
		if !ok || own.Val == 0 {
			continue
		}
		peers := 0
		for _, other := range idx.byPeriod[own.periodKey()] {
			if other.Accn == accn || other.Val == 0 {
				continue
			}
			peers++
			ratio := other.Val / own.Val
			switch {
			case closeTo(ratio, 1):
				votes1++
			case closeTo(ratio, 1000):
				votes1000++
				res.Evidence = append(res.Evidence, fmt.Sprintf(
					"%s: %s reports %g for the same period (x1000)", key, other.Accn, other.Val))
			case closeTo(ratio, 0.001):
				// The peer is the mis-tagged one; this filing needs no change.
				votes1++
			default:
				res.Irreconcilable = true
				res.Evidence = append(res.Evidence, fmt.Sprintf(
					"%s: %s reports %g against %g (ratio %.4f)", key, other.Accn, other.Val, own.Val, ratio))
			}
		}
		if peers > 0 {
			res.Corroborated[key] = peers
		}
	}

	switch {
	case res.Irreconcilable:
		return res
	case votes1000 > 0 && votes1 > 0:
		// The filing cannot be both correctly and incorrectly tagged; something
		// in the comparison is wrong, so nothing from it is trusted.
		res.Irreconcilable = true
		res.Evidence = append(res.Evidence, "mixed scale votes across metrics")
	case votes1000 > 0:
		res.Factor = 1000
	}
	return res
}

func closeTo(v, want float64) bool {
	if want == 0 {
		return v == 0
	}
	return math.Abs(v-want)/want <= scaleRatioTolerance
}

func isInstantKey(key string) bool {
	for _, k := range balanceKeys {
		if k == key {
			return true
		}
	}
	return false
}

// plausibleMagnitude is the fallback used only when a metric has no
// corroborating accession.
//
// It is self-calibrating rather than a fixed floor: a fixed floor would reject
// genuinely small figures, and the reference corpus contains a real half-year
// profit of 3,000. Comparing against the median magnitude of the same metric
// across the whole document is issuer-independent and catches a value that is
// three orders below everything else the company reported.
//
// This is the weakest rule in the package and is deliberately confined to the
// uncorroborated path.
func plausibleMagnitude(idx *factIndex, key string, val float64) bool {
	var mags []float64
	for pk, facts := range idx.byPeriod {
		if pk.Key != key {
			continue
		}
		for _, f := range facts {
			if f.Val != 0 {
				mags = append(mags, math.Abs(f.Val))
			}
		}
	}
	if len(mags) < 3 {
		return true // not enough history to judge
	}
	sort.Float64s(mags)
	median := mags[len(mags)/2]
	if median == 0 {
		return true
	}
	return math.Abs(val) >= median/100
}

// gradeEntry assigns the publication grade for one Company Facts line.
func gradeEntry(key string, corroborators int, scaled bool, plausible bool) (confidence, source string, notes []string) {
	switch {
	case corroborators > 0:
		if scaled {
			notes = append(notes, fmt.Sprintf(
				"scale corrected x1000 against %d corroborating accession(s)", corroborators))
		} else {
			notes = append(notes, fmt.Sprintf("corroborated by %d other accession(s)", corroborators))
		}
		return ConfidenceVerified, SourceCompanyFacts, notes
	case !plausible:
		notes = append(notes, "magnitude implausible against the company's other periods; withheld")
		return ConfidenceSuspect, SourceCompanyFacts, notes
	default:
		notes = append(notes, "single accession reports this period; no corroboration available")
		return ConfidenceSingleSource, SourceCompanyFacts, notes
	}
}
