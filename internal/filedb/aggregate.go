package filedb

import "sort"

// CategoryUnknown is the category assigned when meta.json carries no category,
// or carries the literal "unknown". Roughly 20% of the sample corpus lands
// here, so it is a first-class value, not an error state.
const CategoryUnknown = "unknown"

// Summarize derives coverage and inventory breakdowns from the filings actually
// present on disk. It is pure over rows.
//
// Per design decision D1, nothing here consults submissions.json: counts and
// dates describe what the corpus holds, not what the SEC index claims.
// indexedNotOnDisk is passed in by the caller purely to be carried through.
//
// Dates are compared as strings, which is correct for the YYYY-MM-DD form used
// throughout filing.json.
func Summarize(rows []FilingRow, indexedNotOnDisk int) (Coverage, FilingsSummary) {
	cov := Coverage{
		YearsOnDisk:      []int{},
		TotalFilings:     len(rows),
		IndexedNotOnDisk: indexedNotOnDisk,
	}
	sum := FilingsSummary{
		ByForm:     map[string]int{},
		ByCategory: map[string]int{},
		ByTier:     map[string]int{},
	}

	yearSeen := map[int]struct{}{}
	for _, r := range rows {
		if r.FilingDate != "" {
			if cov.EarliestFilingDate == "" || r.FilingDate < cov.EarliestFilingDate {
				cov.EarliestFilingDate = r.FilingDate
			}
			if r.FilingDate > cov.LatestFilingDate {
				cov.LatestFilingDate = r.FilingDate
			}
		}
		if r.Year > 0 {
			yearSeen[r.Year] = struct{}{}
		}
		if r.Form != "" {
			sum.ByForm[r.Form]++
		}
		cat := r.Category
		if cat == "" {
			cat = CategoryUnknown
		}
		sum.ByCategory[cat]++
		if r.TierLabel != "" {
			sum.ByTier[r.TierLabel]++
		}
		if r.HasFinancials {
			sum.HasFinancialsCount++
		}
	}

	for y := range yearSeen {
		cov.YearsOnDisk = append(cov.YearsOnDisk, y)
	}
	sort.Ints(cov.YearsOnDisk)

	return cov, sum
}
