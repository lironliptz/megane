package financials

import (
	"html"
	"os"
	"regexp"
	"strconv"
	"strings"
)

var (
	scriptStyleRe = regexp.MustCompile(`(?is)<(script|style)[^>]*>.*?</(script|style)>`)
	trRe          = regexp.MustCompile(`(?is)<tr[^>]*>(.*?)</tr>`)
	cellRe        = regexp.MustCompile(`(?is)<t[dh][^>]*>(.*?)</t[dh]>`)
	tagRe         = regexp.MustCompile(`(?is)<[^>]+>`)
	wsRe          = regexp.MustCompile(`\s+`)
	defrefRe      = regexp.MustCompile(`defref_([A-Za-z0-9_\-]+)`)
	scaleRe       = regexp.MustCompile(`(?i)\$ in (Thousands|Millions|Billions)`)
	numRe         = regexp.MustCompile(`\d`)
)

// renderedRow is one row of a rendered statement.
type renderedRow struct {
	Element string // ifrs-full:Revenue, from the drill-down anchor
	Label   string
	Values  []string // leading non-numeric cells already dropped
}

// renderedTable is one R*.htm report.
type renderedTable struct {
	Caption string
	Scale   float64
	// ScaleAssumed is true when the caption carried no "$ in Thousands" hint and
	// the default was applied. It is the only inferred quantity in the package.
	ScaleAssumed bool
	Rows         []renderedRow
}

// ReadRendered parses one SEC-rendered statement report.
//
// Rows are addressed by the XBRL element name embedded in each label's
// drill-down anchor, not by the English label, so this reader shares the
// instance reader's addressing and differs from it only in how the number is
// scaled. That is deliberate: it isolates scale, which is the one thing the two
// sources can independently disagree about.
func ReadRendered(path string) (*renderedTable, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	s := scriptStyleRe.ReplaceAllString(string(b), "")

	tbl := &renderedTable{Scale: 1}
	for i, m := range trRe.FindAllStringSubmatch(s, -1) {
		cells := cellRe.FindAllStringSubmatch(m[1], -1)
		if len(cells) == 0 {
			continue
		}
		texts := make([]string, 0, len(cells))
		for _, c := range cells {
			texts = append(texts, cellText(c[1]))
		}
		if i == 0 {
			tbl.Caption = texts[0]
			if sm := scaleRe.FindStringSubmatch(tbl.Caption); sm != nil {
				switch strings.ToLower(sm[1]) {
				case "thousands":
					tbl.Scale = 1e3
				case "millions":
					tbl.Scale = 1e6
				case "billions":
					tbl.Scale = 1e9
				}
			} else {
				// Observed on a real filing whose values are thousands despite a
				// caption of plain "USD ($)". Recorded as assumed so the caller
				// can treat the row as unverified rather than trusting it.
				tbl.Scale = 1e3
				tbl.ScaleAssumed = true
			}
			continue
		}
		row := renderedRow{Label: texts[0]}
		if dm := defrefRe.FindStringSubmatch(cells[0][1]); dm != nil {
			row.Element = strings.Replace(dm[1], "_", ":", 1)
		}
		vals := texts[1:]
		// Drop leading cells that hold no digits. A mixed-currency caption
		// ("$ in Thousands, NIS in Billions") emits a phantom empty column that
		// would otherwise shift every subsequent index by one.
		for len(vals) > 0 && !numRe.MatchString(vals[0]) {
			vals = vals[1:]
		}
		row.Values = vals
		if row.Element != "" {
			tbl.Rows = append(tbl.Rows, row)
		}
	}
	return tbl, nil
}

func cellText(s string) string {
	return strings.TrimSpace(wsRe.ReplaceAllString(html.UnescapeString(tagRe.ReplaceAllString(s, "")), " "))
}

// Value returns the first numeric cell of the first row carrying element, scaled
// for display units. perShare rows are never scaled by the table multiplier.
func (t *renderedTable) Value(element string, perShare bool) (float64, bool) {
	for _, r := range t.Rows {
		if r.Element != element || len(r.Values) == 0 {
			continue
		}
		v, ok := parseAmount(r.Values[0])
		if !ok {
			continue
		}
		if perShare {
			return v, true
		}
		return v * t.Scale, true
	}
	return 0, false
}

// parseAmount reads a rendered cell: "$ 47,010", "(14)", "0.09", em dash.
func parseAmount(s string) (float64, bool) {
	s = strings.TrimSpace(s)
	s = strings.NewReplacer("$", "", ",", "", "—", "", "–", "", " ", " ").Replace(s)
	s = strings.TrimSpace(s)
	if s == "" || s == "-" {
		return 0, false
	}
	neg := strings.HasPrefix(s, "(") && strings.HasSuffix(s, ")")
	s = strings.TrimSuffix(strings.TrimPrefix(s, "("), ")")
	s = strings.TrimSpace(s)
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0, false
	}
	if neg {
		v = -v
	}
	return v, true
}
