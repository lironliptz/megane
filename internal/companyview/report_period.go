package companyview

import "megane/internal/edgar/financials"

// reportPeriodFromFinancials maps the on-disk period block onto the timeline
// wire shape. Nil when no artifact or the period is empty.
func reportPeriodFromFinancials(fin *financials.FilingFinancials) *ReportPeriod {
	if fin == nil || fin.Period.Label == "" {
		return nil
	}
	return &ReportPeriod{
		Label:    fin.Period.Label,
		Duration: fin.Period.Duration,
		EndDate:  fin.Period.EndDate,
		Focus:    fin.Period.Focus,
	}
}
