package financials

import (
	"encoding/xml"
	"os"
	"path/filepath"
	"strings"
)

// Role identifies a primary financial statement.
type Role string

const (
	RoleIncome   Role = "income"
	RoleBalance  Role = "balance"
	RoleCashFlow Role = "cashFlow"
)

// SummaryFileName is the SEC-generated index of rendered reports.
const SummaryFileName = "FilingSummary.xml"

type filingSummary struct {
	Reports []struct {
		ShortName    string `xml:"ShortName"`
		HTMLFileName string `xml:"HtmlFileName"`
		MenuCategory string `xml:"MenuCategory"`
	} `xml:"MyReports>Report"`
}

// StatementFiles maps each primary statement to the rendered report that holds
// it, by reading FilingSummary.xml.
//
// R-numbers are never hardcoded: on the real corpus R2 is the balance sheet in
// most filings but "Audit Information" in one, where the balance sheet is R3 and
// the income statement is R4. The SEC's own index is the only reliable mapping.
func StatementFiles(dir string) (map[Role]string, error) {
	b, err := os.ReadFile(filepath.Join(dir, SummaryFileName))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var fs filingSummary
	dec := xml.NewDecoder(strings.NewReader(string(b)))
	dec.Strict = false
	dec.CharsetReader = passthroughCharset
	if err := dec.Decode(&fs); err != nil {
		return nil, err
	}

	out := map[Role]string{}
	for _, r := range fs.Reports {
		if !strings.EqualFold(strings.TrimSpace(r.MenuCategory), "Statements") {
			continue
		}
		name := strings.ToLower(r.ShortName)
		if strings.Contains(name, "parenthetical") {
			continue
		}
		file := strings.TrimSpace(r.HTMLFileName)
		if file == "" {
			continue
		}
		var role Role
		switch {
		case strings.Contains(name, "financial position"), strings.Contains(name, "balance sheet"):
			role = RoleBalance
		case strings.Contains(name, "profit or loss"), strings.Contains(name, "operations"):
			role = RoleIncome
		case strings.Contains(name, "cash flow"):
			role = RoleCashFlow
		default:
			continue
		}
		if _, seen := out[role]; !seen {
			out[role] = file
		}
	}
	return out, nil
}
