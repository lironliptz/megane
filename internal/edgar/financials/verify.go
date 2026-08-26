package financials

import (
	"fmt"
	"math"
)

// Detector identifiers, written into Line.Notes when they fire so a withheld
// metric stays diagnosable from financials.json alone.
const (
	DetectorSourceDisagreement = "detector_A_source_disagreement"
	DetectorImplausibleTagging = "detector_B_implausible_tagging"
	DetectorCalcInvariant      = "detector_C_calculation_invariant"
	DetectorChainBreak         = "detector_D_chain_break"
	DetectorAmbiguousFact      = "detector_E_ambiguous_fact"
	DetectorRoleUnresolved     = "detector_F_role_unresolved"
)

// Tolerances.
//
// monetaryTolerance is relative; a scale error is a 1000x disagreement and can
// never pass it. perShareTolerance is absolute, half a cent.
const (
	monetaryTolerance = 0.005
	perShareTolerance = 0.005
)

// implausibleMagnitude is detector B's threshold. A monetary fact tagged with
// decimals >= 0 (i.e. claimed accurate to the unit) whose absolute value is
// below this is almost certainly tagged in thousands by mistake.
//
// MEASURED ON ONE OBSERVATION: exactly one filing in the reference corpus is
// mis-tagged this way, and this threshold isolates it with no false positives
// across the other fourteen. It is the most likely constant in this package to
// need revision against a second issuer.
const implausibleMagnitude = 1e6

// agree reports whether two readings of the same metric match.
func agree(a, b float64, unit string) bool {
	if unit == UnitUSDPerShare {
		return math.Abs(a-b) <= perShareTolerance
	}
	if a == 0 && b == 0 {
		return true
	}
	denom := math.Abs(a)
	if denom == 0 {
		denom = math.Abs(b)
	}
	return math.Abs(a-b)/denom <= monetaryTolerance
}

// detectImplausibleTagging is detector B: the filer tagged the fact itself
// wrongly, so both readers agree on a wrong number and detector A cannot help.
func detectImplausibleTagging(f fact, unit string) bool {
	if unit != UnitUSD {
		return false
	}
	// decimals="-3" means "accurate to the nearest thousand", the conformant
	// tagging for a company reporting in thousands. decimals of 0 or more claims
	// unit accuracy, which for a revenue-scale figure below $1M is implausible.
	var dec int
	if _, err := fmt.Sscanf(f.Decimals, "%d", &dec); err != nil {
		return false // "INF" and friends carry no magnitude claim
	}
	return dec >= 0 && math.Abs(f.Value) < implausibleMagnitude
}

// Grade applies the two-source agreement gate.
//
// Agreement establishes that the two readers parsed the same fact — not that the
// fact is correct. A filer who mis-tagged at source produces two agreeing wrong
// readings, which is why the signals from the detectors are an input here and
// not an afterthought.
func Grade(instVal, rendVal *float64, unit string, signals []string) (confidence, source string, notes []string) {
	notes = append(notes, signals...)
	switch {
	case len(signals) > 0:
		return ConfidenceSuspect, SourceInstance, notes
	case instVal != nil && rendVal != nil:
		if !agree(*instVal, *rendVal, unit) {
			notes = append(notes, DetectorSourceDisagreement,
				fmt.Sprintf("instance=%g rendered=%g", *instVal, *rendVal))
			return ConfidenceSuspect, SourceBoth, notes
		}
		return ConfidenceVerified, SourceBoth, notes
	case instVal != nil:
		return ConfidenceSingleSource, SourceInstance, notes
	case rendVal != nil:
		return ConfidenceSingleSource, SourceRendered, notes
	}
	return ConfidenceSuspect, SourceInstance, notes
}

// checkCalcInvariant is detector C: the filer's own arithmetic must hold.
// revenue - cost = gross profit is the direct structural refutation of a segment
// figure having been picked up as a consolidated total.
func checkCalcInvariant(lines map[string]float64) (string, bool) {
	rev, okR := lines[KeyTotalRevenues]
	cost, okC := lines[KeyCostOfRevenues]
	gp, okG := lines[KeyGrossProfit]
	if !okR || !okC || !okG {
		return "", false
	}
	if math.Abs((rev-cost)-gp) > 1 {
		return fmt.Sprintf("%s: revenue %g - cost %g != gross profit %g",
			DetectorCalcInvariant, rev, cost, gp), true
	}
	return "", false
}

// segmentsReconcile reports whether a segment breakdown adds up to the
// consolidated total.
//
// This is NOT treated as an invariant on the consolidated figure. IFRS segment
// disclosures include inter-segment revenue that eliminates on consolidation, so
// a breakdown legitimately fails to sum to the total (measured: one filing in
// the reference corpus reports 34.9M + 6.5M against a 37.4M total). A breakdown
// that does not reconcile is therefore dropped rather than published, and the
// consolidated revenue — which comes from its own undimensioned fact — keeps the
// grade its own evidence earned.
func segmentsReconcile(total float64, segs []Segment) bool {
	if len(segs) == 0 || total == 0 {
		return false
	}
	var sum float64
	for _, s := range segs {
		sum += s.Value
	}
	return agree(sum, total, UnitUSD)
}
