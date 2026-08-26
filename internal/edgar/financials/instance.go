package financials

import (
	"encoding/xml"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

// linkbaseSuffixes mark the taxonomy files that sit beside a classic instance
// document and must never be mistaken for it.
var linkbaseSuffixes = []string{"_cal.xml", "_def.xml", "_lab.xml", "_pre.xml"}

// FindInstance locates the XBRL instance document in an accession folder.
//
// Two shapes occur in the corpus and both are supported: the inline-XBRL
// extraction "*_htm.xml" (2022 onward) and the classic "{ticker}-{date}.xml"
// (2020-2021). Returns "" with no error when the accession ships no XBRL at all,
// which is the normal case for most filings.
func FindInstance(dir string) (string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", err
	}
	var classic []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if !strings.HasSuffix(name, ".xml") {
			continue
		}
		if strings.HasSuffix(name, "_htm.xml") {
			return filepath.Join(dir, name), nil
		}
		if name == "FilingSummary.xml" {
			continue
		}
		isLinkbase := false
		for _, suf := range linkbaseSuffixes {
			if strings.HasSuffix(name, suf) {
				isLinkbase = true
				break
			}
		}
		if !isLinkbase {
			classic = append(classic, name)
		}
	}
	// Sort for determinism, then take the first file that is actually an XBRL
	// instance. Accession folders also hold ownership.xml (Forms 3/4) and
	// primary_doc.xml, which match the name rules above but are not instances;
	// sniffing the root element rejects them without a filename blocklist.
	sort.Strings(classic)
	for _, name := range classic {
		path := filepath.Join(dir, name)
		if isXBRLInstance(path) {
			return path, nil
		}
	}
	return "", nil
}

// isXBRLInstance reports whether the file's root element is <xbrl> in any
// dialect. Only the opening tags are read.
func isXBRLInstance(path string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()
	dec := xml.NewDecoder(io.LimitReader(f, 64<<10))
	dec.Strict = false
	dec.CharsetReader = passthroughCharset
	for {
		tok, err := dec.Token()
		if err != nil {
			return false
		}
		if se, ok := tok.(xml.StartElement); ok {
			return se.Name.Local == "xbrl"
		}
	}
}

// passthroughCharset accepts the ASCII-compatible encodings that appear in SEC
// filings without pulling in a charset dependency.
func passthroughCharset(charset string, input io.Reader) (io.Reader, error) {
	switch strings.ToLower(charset) {
	case "us-ascii", "ascii", "iso-8859-1", "latin1", "utf-8", "":
		return input, nil
	}
	return nil, fmt.Errorf("financials: unsupported charset %q", charset)
}

// ctx is one XBRL context: who, when, and along which dimensions.
type ctx struct {
	ID      string
	Start   string
	End     string
	Instant string
	// Dimensioned is true when the context carries an explicitMember, i.e. the
	// fact describes a segment or other breakdown rather than the consolidated
	// entity. Such facts are never eligible for a headline metric.
	Dimensioned bool
	Dims        []dimMember
}

// dimMember is one axis/member pair on a dimensioned context. The axis matters:
// a filing may break revenue down along several axes at once (by product and by
// geography), and each breakdown independently sums to the consolidated total.
type dimMember struct {
	Axis   string
	Member string
}

// unitKind is the resolved meaning of a unitRef.
type unitKind int

const (
	unitOther unitKind = iota
	unitUSD
	unitUSDPerShare
)

// fact is one tagged number.
type fact struct {
	Element    string
	Key        string
	ContextRef string
	UnitRef    string
	Decimals   string
	Raw        string
	Value      float64
}

// instanceDoc is everything one pass over the instance collects.
type instanceDoc struct {
	Contexts map[string]ctx
	Units    map[string]unitKind
	Facts    []fact

	DocumentType      string
	PeriodEndDate     string
	FiscalPeriodFocus string
	FiscalYearFocus   string
}

// ParseInstance reads an instance document.
//
// Matching is on XML local names only. The two dialects in the corpus differ in
// namespace prefix — inline extractions put xbrli in the default namespace
// (<context>) while classic instances prefix it (<xbrli:context>) — so
// struct-tag unmarshalling would have to enumerate both. A token walk keyed on
// local name is dialect-agnostic by construction.
func ParseInstance(r io.Reader) (*instanceDoc, error) {
	doc := &instanceDoc{
		Contexts: map[string]ctx{},
		Units:    map[string]unitKind{},
	}
	known := allElements()
	dec := xml.NewDecoder(r)
	dec.Strict = false
	// Classic (pre-2022) instances declare encoding="US-ASCII", which the stdlib
	// decoder refuses without an explicit reader. ASCII and Latin-1 are byte-
	// compatible with what we read (dates, numbers, element names), so pass the
	// stream through rather than pulling in a charset dependency.
	dec.CharsetReader = passthroughCharset

	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("financials: parse instance: %w", err)
		}
		se, ok := tok.(xml.StartElement)
		if !ok {
			continue
		}
		switch se.Name.Local {
		case "context":
			c, err := parseContext(dec, se)
			if err != nil {
				return nil, err
			}
			if c.ID != "" {
				doc.Contexts[c.ID] = c
			}
		case "unit":
			id, kind, err := parseUnit(dec, se)
			if err != nil {
				return nil, err
			}
			if id != "" {
				doc.Units[id] = kind
			}
		default:
			qname := qualify(se.Name)
			if strings.HasPrefix(qname, "dei:") {
				val, err := elementText(dec, se)
				if err != nil {
					return nil, err
				}
				switch se.Name.Local {
				case "DocumentType":
					doc.DocumentType = val
				case "DocumentPeriodEndDate":
					doc.PeriodEndDate = val
				case "DocumentFiscalPeriodFocus":
					doc.FiscalPeriodFocus = val
				case "DocumentFiscalYearFocus":
					doc.FiscalYearFocus = val
				}
				continue
			}
			key, want := known[qname]
			if !want {
				continue
			}
			f := fact{Element: qname, Key: key}
			for _, a := range se.Attr {
				switch a.Name.Local {
				case "contextRef":
					f.ContextRef = a.Value
				case "unitRef":
					f.UnitRef = a.Value
				case "decimals":
					f.Decimals = a.Value
				}
			}
			raw, err := elementText(dec, se)
			if err != nil {
				return nil, err
			}
			f.Raw = strings.TrimSpace(raw)
			v, err := strconv.ParseFloat(strings.ReplaceAll(f.Raw, ",", ""), 64)
			if err != nil {
				continue // non-numeric fact (a text block); not a metric
			}
			f.Value = v
			doc.Facts = append(doc.Facts, f)
		}
	}
	return doc, nil
}

// qualify rebuilds "prefix:LocalName" from a decoded name. Go resolves the
// prefix to a namespace URI, so the well-known financial-taxonomy URIs are
// mapped back to their conventional prefixes.
func qualify(n xml.Name) string {
	switch {
	case strings.Contains(n.Space, "xbrl.ifrs.org"):
		return "ifrs-full:" + n.Local
	case strings.Contains(n.Space, "fasb.org/us-gaap"), strings.Contains(n.Space, "xbrl.us/us-gaap"):
		return "us-gaap:" + n.Local
	case strings.Contains(n.Space, "xbrl.sec.gov/dei"), strings.Contains(n.Space, "xbrl.us/dei"):
		return "dei:" + n.Local
	}
	if n.Space == "" {
		return n.Local
	}
	return n.Space + ":" + n.Local
}

func parseContext(dec *xml.Decoder, se xml.StartElement) (ctx, error) {
	var c ctx
	for _, a := range se.Attr {
		if a.Name.Local == "id" {
			c.ID = a.Value
		}
	}
	depth := 1
	var cur string
	for depth > 0 {
		tok, err := dec.Token()
		if err != nil {
			return c, err
		}
		switch t := tok.(type) {
		case xml.StartElement:
			depth++
			cur = t.Name.Local
			if cur == "explicitMember" {
				c.Dimensioned = true
				axis := ""
				for _, a := range t.Attr {
					if a.Name.Local == "dimension" {
						axis = a.Value
					}
				}
				c.Dims = append(c.Dims, dimMember{Axis: axis})
			}
		case xml.CharData:
			v := strings.TrimSpace(string(t))
			if v == "" {
				continue
			}
			switch cur {
			case "startDate":
				c.Start = v
			case "endDate":
				c.End = v
			case "instant":
				c.Instant = v
			case "explicitMember":
				if n := len(c.Dims); n > 0 {
					c.Dims[n-1].Member = v
				}
			}
		case xml.EndElement:
			depth--
			cur = ""
		}
	}
	return c, nil
}

// parseUnit resolves a unit by its measure content, never by its id: ids are not
// stable across dialects ("usd"/"usdPershares" inline vs "USD"/"Shares" classic),
// so keying on them would silently misclassify per-share facts.
func parseUnit(dec *xml.Decoder, se xml.StartElement) (string, unitKind, error) {
	var id string
	for _, a := range se.Attr {
		if a.Name.Local == "id" {
			id = a.Value
		}
	}
	var measures []string
	var hasDivide bool
	var numerator, denominator []string
	section := ""
	depth := 1
	for depth > 0 {
		tok, err := dec.Token()
		if err != nil {
			return "", unitOther, err
		}
		switch t := tok.(type) {
		case xml.StartElement:
			depth++
			switch t.Name.Local {
			case "divide":
				hasDivide = true
			case "unitNumerator":
				section = "num"
			case "unitDenominator":
				section = "den"
			}
		case xml.CharData:
			v := strings.TrimSpace(string(t))
			if v == "" {
				continue
			}
			switch section {
			case "num":
				numerator = append(numerator, v)
			case "den":
				denominator = append(denominator, v)
			default:
				measures = append(measures, v)
			}
		case xml.EndElement:
			depth--
			if t.Name.Local == "unitNumerator" || t.Name.Local == "unitDenominator" {
				section = ""
			}
		}
	}
	isUSD := func(vals []string) bool {
		for _, v := range vals {
			if strings.EqualFold(v, "iso4217:USD") {
				return true
			}
		}
		return false
	}
	isShares := func(vals []string) bool {
		for _, v := range vals {
			if strings.HasSuffix(strings.ToLower(v), "shares") {
				return true
			}
		}
		return false
	}
	switch {
	case hasDivide && isUSD(numerator) && isShares(denominator):
		return id, unitUSDPerShare, nil
	case !hasDivide && isUSD(measures):
		return id, unitUSD, nil
	}
	return id, unitOther, nil
}

func elementText(dec *xml.Decoder, se xml.StartElement) (string, error) {
	var sb strings.Builder
	depth := 1
	for depth > 0 {
		tok, err := dec.Token()
		if err != nil {
			return "", err
		}
		switch t := tok.(type) {
		case xml.StartElement:
			depth++
		case xml.CharData:
			if depth == 1 {
				sb.WriteString(string(t))
			}
		case xml.EndElement:
			depth--
		}
	}
	return strings.TrimSpace(sb.String()), nil
}

// --- period selection -------------------------------------------------------

const (
	durationP3M = "P3M"
	durationP1Y = "P1Y"
)

// SelectPeriod reads the period the filing declares for itself. The dei facts
// state the period end, the fiscal quarter and the fiscal year outright, so no
// table header is parsed and no column position is guessed.
func (d *instanceDoc) SelectPeriod() Period {
	p := Period{
		EndDate: d.PeriodEndDate,
		Focus:   d.FiscalPeriodFocus,
	}
	switch {
	case strings.EqualFold(d.FiscalPeriodFocus, "FY"):
		p.Duration = durationP1Y
		p.Label = "FY " + d.FiscalYearFocus
	case len(d.FiscalPeriodFocus) == 2 && (d.FiscalPeriodFocus[0] == 'Q' || d.FiscalPeriodFocus[0] == 'q'):
		p.Duration = durationP3M
		p.Label = strings.ToUpper(d.FiscalPeriodFocus) + " " + d.FiscalYearFocus
	default:
		p.Duration = durationP3M
		p.Label = strings.TrimSpace(d.FiscalPeriodFocus + " " + d.FiscalYearFocus)
	}
	return p
}

func parseDay(s string) (time.Time, bool) {
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		return time.Time{}, false
	}
	return t, true
}

// durationDays returns the context's span in days, or -1 for an instant.
func (c ctx) durationDays() int {
	s, ok1 := parseDay(c.Start)
	e, ok2 := parseDay(c.End)
	if !ok1 || !ok2 {
		return -1
	}
	return int(e.Sub(s).Hours() / 24)
}

// matchesDuration reports whether the context spans roughly the wanted period.
// The bands are wide because fiscal quarters and years are not uniform.
func matchesDuration(days int, duration string) bool {
	switch duration {
	case durationP3M:
		return days >= 80 && days <= 100
	case durationP1Y:
		return days >= 350 && days <= 380
	}
	return false
}

// currentDurationCtx finds the consolidated context for the filing's own period.
func (d *instanceDoc) currentDurationCtx(p Period) (ctx, bool) {
	for _, c := range d.Contexts {
		if c.Dimensioned || c.End != p.EndDate {
			continue
		}
		if matchesDuration(c.durationDays(), p.Duration) {
			return c, true
		}
	}
	return ctx{}, false
}

// priorDurationCtx finds the same period one year earlier: same span, ending
// within a 350-380 day window before the current period end, closest to exactly
// one year.
func (d *instanceDoc) priorDurationCtx(p Period, cur ctx) (ctx, bool) {
	curEnd, ok := parseDay(cur.End)
	if !ok {
		return ctx{}, false
	}
	var best ctx
	var bestDelta = 1 << 30
	found := false
	for _, c := range d.Contexts {
		if c.Dimensioned || !matchesDuration(c.durationDays(), p.Duration) {
			continue
		}
		end, ok := parseDay(c.End)
		if !ok {
			continue
		}
		gap := int(curEnd.Sub(end).Hours() / 24)
		if gap < 350 || gap > 380 {
			continue
		}
		delta := gap - 365
		if delta < 0 {
			delta = -delta
		}
		if !found || delta < bestDelta {
			best, bestDelta, found = c, delta, true
		}
	}
	return best, found
}

// currentInstantCtx finds the balance-sheet date for the filing's own period.
func (d *instanceDoc) currentInstantCtx(p Period) (ctx, bool) {
	for _, c := range d.Contexts {
		if c.Dimensioned || c.Instant == "" {
			continue
		}
		if c.Instant == p.EndDate {
			return c, true
		}
	}
	return ctx{}, false
}

// priorInstantCtx finds the prior fiscal year-end.
//
// This is deliberately the prior year-END and not the prior-year same quarter,
// because that is the comparative a balance sheet actually presents. Callers
// must label it accordingly and must not compute a year-over-year percentage
// from it.
func (d *instanceDoc) priorInstantCtx(p Period) (ctx, bool) {
	end, ok := parseDay(p.EndDate)
	if !ok {
		return ctx{}, false
	}
	priorYearEnd := fmt.Sprintf("%d-12-31", end.Year()-1)
	if p.Duration == durationP1Y && end.Format("01-02") == "12-31" {
		priorYearEnd = fmt.Sprintf("%d-12-31", end.Year()-1)
	}
	for _, c := range d.Contexts {
		if c.Dimensioned || c.Instant == "" {
			continue
		}
		if c.Instant == priorYearEnd {
			return c, true
		}
	}
	return ctx{}, false
}

// --- fact selection ---------------------------------------------------------

// candidate is one selected fact plus the context it came from.
type candidate struct {
	Fact fact
	Ctx  ctx
}

// pick returns the consolidated fact for a metric in a given context.
//
// Two exclusions do the real work here:
//
//   - facts in a dimensioned context are dropped, so a segment's revenue can
//     never be returned as the consolidated total;
//   - facts whose unit is neither USD nor USD-per-share are dropped, so
//     non-USD facts are excluded by unit rather than by column position.
//
// More than one surviving fact is not resolved arbitrarily: all are returned so
// the caller can flag the ambiguity.
func (d *instanceDoc) pick(key string, c ctx) []candidate {
	m, ok := metricFor(key)
	if !ok {
		return nil
	}
	wantUnit := unitUSD
	if m.Unit == UnitUSDPerShare {
		wantUnit = unitUSDPerShare
	}
	for _, el := range m.Elements {
		var out []candidate
		for _, f := range d.Facts {
			if f.Element != el || f.ContextRef != c.ID {
				continue
			}
			fctx, ok := d.Contexts[f.ContextRef]
			if !ok || fctx.Dimensioned {
				continue
			}
			if d.Units[f.UnitRef] != wantUnit {
				continue
			}
			out = append(out, candidate{Fact: f, Ctx: fctx})
		}
		if len(out) > 0 {
			return dedupeCandidates(out)
		}
	}
	return nil
}

// dedupeCandidates collapses repeated taggings of the same number.
//
// Inline XBRL routinely tags one fact more than once within a filing (the
// statements and an exhibit carry the same value in the same context). Those
// duplicates are not ambiguity, so only genuinely differing values are left for
// detector E to flag.
func dedupeCandidates(in []candidate) []candidate {
	var out []candidate
	for _, c := range in {
		dup := false
		for _, seen := range out {
			if seen.Fact.Value == c.Fact.Value {
				dup = true
				break
			}
		}
		if !dup {
			out = append(out, c)
		}
	}
	return out
}

// segmentsFor returns revenue facts from dimensioned contexts covering the same
// period — the same facts pick() excludes, now selected deliberately.
//
// Facts are grouped by axis and only one axis is returned: a filer may present
// several breakdowns of the same revenue (by product line and by geography), and
// emitting them together would double-count. The axis whose members reconcile
// most closely to the consolidated total wins.
func (d *instanceDoc) segmentsFor(p Period, cur ctx, total float64) []Segment {
	m, _ := metricFor(KeyTotalRevenues)
	byAxis := map[string][]Segment{}
	for _, el := range m.Elements {
		for _, f := range d.Facts {
			if f.Element != el {
				continue
			}
			c, ok := d.Contexts[f.ContextRef]
			if !ok || !c.Dimensioned || len(c.Dims) != 1 {
				continue // multi-axis intersections are not a top-level breakdown
			}
			if c.Start != cur.Start || c.End != cur.End {
				continue
			}
			if d.Units[f.UnitRef] != unitUSD {
				continue
			}
			dim := c.Dims[0]
			dup := false
			for _, s := range byAxis[dim.Axis] {
				if s.Member == dim.Member {
					dup = true
					break
				}
			}
			if dup {
				continue
			}
			byAxis[dim.Axis] = append(byAxis[dim.Axis], Segment{
				Name:     memberDisplay(dim.Member),
				Member:   dim.Member,
				Value:    f.Value,
				ValueFmt: FormatValue(f.Value, UnitUSD),
			})
		}
		if len(byAxis) > 0 {
			break
		}
	}
	if len(byAxis) == 0 {
		return nil
	}
	axes := make([]string, 0, len(byAxis))
	for a := range byAxis {
		axes = append(axes, a)
	}
	sort.Strings(axes) // determinism before scoring
	best, bestDelta := "", math.Inf(1)
	for _, a := range axes {
		var sum float64
		for _, s := range byAxis[a] {
			sum += s.Value
		}
		delta := math.Abs(sum - total)
		if delta < bestDelta {
			best, bestDelta = a, delta
		}
	}
	return byAxis[best]
}

// memberDisplay turns "kmda:ProprietaryProductsMember" into "Proprietary Products".
func memberDisplay(member string) string {
	name := member
	if i := strings.IndexByte(name, ':'); i >= 0 {
		name = name[i+1:]
	}
	name = strings.TrimSuffix(name, "Member")
	var sb strings.Builder
	for i, r := range name {
		if i > 0 && r >= 'A' && r <= 'Z' {
			sb.WriteByte(' ')
		}
		sb.WriteRune(r)
	}
	return strings.TrimSpace(sb.String())
}
