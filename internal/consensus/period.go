package consensus

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// DerivePeriodEnd converts fiscal year metadata into a concrete period end date.
// fiscalYearEnd format is MMDD from SEC submissions.json.
func DerivePeriodEnd(fiscalYearEnd, fiscalYear, fiscalPeriod string) (end, duration string, ok bool) {
	if len(strings.TrimSpace(fiscalYearEnd)) != 4 {
		return "", "", false
	}
	year, err := strconv.Atoi(strings.TrimSpace(fiscalYear))
	if err != nil || year < 1900 {
		return "", "", false
	}
	month, err := strconv.Atoi(fiscalYearEnd[:2])
	if err != nil || month < 1 || month > 12 {
		return "", "", false
	}
	day, err := strconv.Atoi(fiscalYearEnd[2:])
	if err != nil || day < 1 || day > 31 {
		return "", "", false
	}

	fyEnd := time.Date(year, time.Month(month), day, 0, 0, 0, 0, time.UTC)
	switch strings.ToUpper(strings.TrimSpace(fiscalPeriod)) {
	case "FY":
		return fyEnd.Format("2006-01-02"), "P1Y", true
	case "Q4":
		return fyEnd.Format("2006-01-02"), "P3M", true
	case "Q3":
		return shiftQuarter(fyEnd, -1), "P3M", true
	case "Q2":
		return shiftQuarter(fyEnd, -2), "P3M", true
	case "Q1":
		return shiftQuarter(fyEnd, -3), "P3M", true
	default:
		return "", "", false
	}
}

func shiftQuarter(fyEnd time.Time, delta int) string {
	t := fyEnd.AddDate(0, 3*delta, 0)
	// keep month end semantics for fiscal-year-end dates such as 0630, 0930, 1231
	lastDay := time.Date(t.Year(), t.Month()+1, 0, 0, 0, 0, 0, time.UTC).Day()
	if t.Day() > lastDay {
		t = time.Date(t.Year(), t.Month(), lastDay, 0, 0, 0, 0, time.UTC)
	}
	return t.Format("2006-01-02")
}

func deriveQuarterLabel(year int, q int) string {
	if q < 1 || q > 4 {
		return ""
	}
	return fmt.Sprintf("Q%d", q)
}
