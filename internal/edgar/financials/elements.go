package financials

// Canonical metric keys.
const (
	KeyTotalRevenues   = "total_revenues"
	KeyCostOfRevenues  = "cost_of_revenues"
	KeyGrossProfit     = "gross_profit"
	KeyOperatingIncome = "operating_income"
	KeyNetIncome       = "net_income"
	KeyEPSBasic        = "eps_basic"
	KeyEPSDiluted      = "eps_diluted"
	KeyCash            = "cash_and_equivalents"
	KeyTotalAssets     = "total_assets"
	KeyOperatingCF     = "operating_cash_flow"
)

// metric describes one canonical figure and the XBRL elements that carry it.
//
// Elements are matched by exact "prefix:LocalName", case-sensitive, in rank
// order — never by substring, prefix, or edit distance. English row labels are
// not used for selection at all. Both rules exist because label matching
// silently returned a segment's revenue as the consolidated total on the real
// corpus (see the LLD's defect 3).
type metric struct {
	Key      string
	Display  string
	Unit     string
	Elements []string
}

// metrics is the whole dictionary.
//
// The us-gaap:* entries are UNMEASURED: the reference corpus is a single IFRS
// foreign private issuer, so they are seeded from the standard taxonomy and must
// be validated against a domestic filer before multi-issuer support is claimed.
var metrics = []metric{
	{KeyTotalRevenues, "Total revenues", UnitUSD, []string{
		"ifrs-full:Revenue",
		"ifrs-full:RevenueFromContractsWithCustomers",
		"us-gaap:Revenues",
		"us-gaap:RevenueFromContractWithCustomerExcludingAssessedTax",
	}},
	{KeyCostOfRevenues, "Total cost of revenues", UnitUSD, []string{
		"ifrs-full:CostOfSales",
		"us-gaap:CostOfRevenue",
	}},
	{KeyGrossProfit, "Gross profit", UnitUSD, []string{
		"ifrs-full:GrossProfit",
		"us-gaap:GrossProfit",
	}},
	{KeyOperatingIncome, "Operating income", UnitUSD, []string{
		"ifrs-full:ProfitLossFromOperatingActivities",
		"us-gaap:OperatingIncomeLoss",
	}},
	{KeyNetIncome, "Net income", UnitUSD, []string{
		"ifrs-full:ProfitLoss",
		"ifrs-full:ProfitLossFromContinuingOperations",
		"us-gaap:NetIncomeLoss",
	}},
	{KeyEPSBasic, "Basic EPS", UnitUSDPerShare, []string{
		"ifrs-full:BasicEarningsLossPerShare",
		"us-gaap:EarningsPerShareBasic",
	}},
	{KeyEPSDiluted, "Diluted EPS", UnitUSDPerShare, []string{
		"ifrs-full:DilutedEarningsLossPerShare",
		"us-gaap:EarningsPerShareDiluted",
	}},
	{KeyCash, "Cash and cash equivalents", UnitUSD, []string{
		"ifrs-full:CashAndCashEquivalents",
		"us-gaap:CashAndCashEquivalentsAtCarryingValue",
	}},
	{KeyTotalAssets, "Total assets", UnitUSD, []string{
		"ifrs-full:Assets",
		"us-gaap:Assets",
	}},
	{KeyOperatingCF, "Operating cash flow", UnitUSD, []string{
		"ifrs-full:CashFlowsFromUsedInOperatingActivities",
		"us-gaap:NetCashProvidedByUsedInOperatingActivities",
	}},
}

// Statement membership. A key belongs to exactly one statement set.
var (
	incomeKeys   = []string{KeyTotalRevenues, KeyCostOfRevenues, KeyGrossProfit, KeyOperatingIncome, KeyNetIncome, KeyEPSBasic, KeyEPSDiluted}
	balanceKeys  = []string{KeyCash, KeyTotalAssets}
	cashFlowKeys = []string{KeyOperatingCF}
)

// MetricRank is the display order used for the timeline modal, highest value to
// a reader first. Keys outside this list are extracted but never ranked ahead of
// it.
var MetricRank = []string{KeyTotalRevenues, KeyEPSBasic, KeyNetIncome, KeyCash}

// metricFor returns the dictionary entry for a canonical key.
func metricFor(key string) (metric, bool) {
	for _, m := range metrics {
		if m.Key == key {
			return m, true
		}
	}
	return metric{}, false
}

// allElements maps every known element name to its canonical key, for the
// single-pass instance reader.
func allElements() map[string]string {
	out := make(map[string]string, len(metrics)*3)
	for _, m := range metrics {
		for _, el := range m.Elements {
			out[el] = m.Key
		}
	}
	return out
}

// rankIndex returns the position of key in MetricRank, or a large number so
// unranked keys sort last but stay in a deterministic order.
func rankIndex(key string) int {
	for i, k := range MetricRank {
		if k == key {
			return i
		}
	}
	return len(MetricRank) + 1
}
