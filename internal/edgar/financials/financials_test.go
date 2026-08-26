package financials

import (
	"context"
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func fixture(t *testing.T, name string) string {
	t.Helper()
	dir := filepath.Join("testdata", name)
	if _, err := os.Stat(dir); err != nil {
		t.Fatalf("missing fixture %s: %v", name, err)
	}
	return dir
}

func extract(t *testing.T, name string) *FilingFinancials {
	t.Helper()
	f, err := ExtractAccession(fixture(t, name), Options{Now: func() time.Time {
		return time.Date(2026, 8, 26, 12, 0, 0, 0, time.UTC)
	}})
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	if f == nil {
		t.Fatalf("%s: no financials extracted", name)
	}
	return f
}

func line(t *testing.T, f *FilingFinancials, key string) *Line {
	t.Helper()
	for _, set := range [][]Line{f.Statements.Income, f.Statements.Balance, f.Statements.CashFlow} {
		for i := range set {
			if set[i].Key == key {
				return &set[i]
			}
		}
	}
	return nil
}

// TestFindInstanceBothDialects covers the inline extraction and the classic
// instance, and confirms linkbases are never mistaken for one.
func TestFindInstanceBothDialects(t *testing.T) {
	for _, tc := range []struct{ fixture, want string }{
		{"q3_2025", "ea0262621-6k_kamada_htm.xml"},
		{"fy_2019_20f", "kmda-20191231.xml"},
	} {
		got, err := FindInstance(fixture(t, tc.fixture))
		if err != nil {
			t.Fatalf("%s: %v", tc.fixture, err)
		}
		if filepath.Base(got) != tc.want {
			t.Errorf("%s: FindInstance = %q, want %q", tc.fixture, filepath.Base(got), tc.want)
		}
	}
}

// TestParseInstanceDialects proves the US-ASCII-declared, xbrli-prefixed classic
// instance parses to the same shape as a default-namespace inline one.
func TestParseInstanceDialects(t *testing.T) {
	for _, name := range []string{"q3_2025", "fy_2019_20f"} {
		p, _ := FindInstance(fixture(t, name))
		fh, err := os.Open(p)
		if err != nil {
			t.Fatal(err)
		}
		doc, err := ParseInstance(fh)
		fh.Close()
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if len(doc.Contexts) == 0 || len(doc.Units) == 0 || len(doc.Facts) == 0 {
			t.Errorf("%s: empty parse (ctx=%d units=%d facts=%d)",
				name, len(doc.Contexts), len(doc.Units), len(doc.Facts))
		}
		if doc.PeriodEndDate == "" || doc.FiscalPeriodFocus == "" {
			t.Errorf("%s: dei facts not captured", name)
		}
	}
}

// TestUnitResolution pins the fact that units are resolved by measure, not id:
// the two dialects spell the same units differently.
func TestUnitResolution(t *testing.T) {
	for _, tc := range []struct {
		fixture            string
		wantUSD, wantPerSh bool
	}{
		{"q3_2025", true, true},
		{"fy_2019_20f", true, true},
	} {
		p, _ := FindInstance(fixture(t, tc.fixture))
		fh, _ := os.Open(p)
		doc, err := ParseInstance(fh)
		fh.Close()
		if err != nil {
			t.Fatal(err)
		}
		var sawUSD, sawPerShare bool
		for _, k := range doc.Units {
			switch k {
			case unitUSD:
				sawUSD = true
			case unitUSDPerShare:
				sawPerShare = true
			}
		}
		if sawUSD != tc.wantUSD || sawPerShare != tc.wantPerSh {
			t.Errorf("%s: USD=%v perShare=%v", tc.fixture, sawUSD, sawPerShare)
		}
	}
}

// TestSelectPeriod checks the filing declares its own period.
func TestSelectPeriod(t *testing.T) {
	for _, tc := range []struct{ fixture, label, duration, end string }{
		{"q3_2025", "Q3 2025", durationP3M, "2025-09-30"},
		{"q1_2024", "Q1 2024", durationP3M, "2024-03-31"},
		{"fy_2019_20f", "FY 2019", durationP1Y, "2019-12-31"},
		{"fy_2024_20f", "FY 2024", durationP1Y, "2024-12-31"},
	} {
		f := extract(t, tc.fixture)
		if f.Period.Label != tc.label || f.Period.Duration != tc.duration || f.Period.EndDate != tc.end {
			t.Errorf("%s: got %+v, want %s/%s/%s", tc.fixture, f.Period, tc.label, tc.duration, tc.end)
		}
	}
}

// TestConsolidatedNotSegment is the golden regression for the defect this whole
// package is shaped around: the label "Total revenues" appears three times in
// the Q3 2025 income statement — once consolidated and twice inside segment
// sections — and the consolidated figure must win.
func TestConsolidatedNotSegment(t *testing.T) {
	f := extract(t, "q3_2025")
	rev := line(t, f, KeyTotalRevenues)
	if rev == nil {
		t.Fatal("no revenue line")
	}
	const consolidated = 47010000.0
	if rev.Value != consolidated {
		t.Errorf("revenue = %g, want %g (segment values 39523000 / 7487000 must not win)", rev.Value, consolidated)
	}
	for _, seg := range []float64{39523000, 7487000} {
		if rev.Value == seg {
			t.Fatalf("revenue picked up segment figure %g", seg)
		}
	}
}

// TestStatementFilesNotHardcodedRNumbers uses the filing where R2 is "Audit
// Information" and hardcoded R-numbers break.
func TestStatementFilesNotHardcodedRNumbers(t *testing.T) {
	roles, err := StatementFiles(fixture(t, "fy_2024_20f"))
	if err != nil {
		t.Fatal(err)
	}
	if roles[RoleBalance] != "R3.htm" || roles[RoleIncome] != "R4.htm" {
		t.Errorf("got balance=%q income=%q, want R3.htm/R4.htm", roles[RoleBalance], roles[RoleIncome])
	}
	roles2, _ := StatementFiles(fixture(t, "q3_2025"))
	if roles2[RoleBalance] != "R2.htm" || roles2[RoleIncome] != "R3.htm" {
		t.Errorf("got balance=%q income=%q, want R2.htm/R3.htm", roles2[RoleBalance], roles2[RoleIncome])
	}
}

// TestRenderedPhantomColumn: the mixed-currency caption emits an empty leading
// cell that would shift every value by one column.
func TestRenderedPhantomColumn(t *testing.T) {
	dir := fixture(t, "fy_2024_20f")
	roles, _ := StatementFiles(dir)
	tbl, err := ReadRendered(filepath.Join(dir, roles[RoleIncome]))
	if err != nil {
		t.Fatal(err)
	}
	v, ok := tbl.Value("ifrs-full:Revenue", false)
	if !ok {
		t.Fatal("no revenue row")
	}
	if math.Abs(v-160953000) > 1 {
		t.Errorf("revenue = %g, want 160953000 (phantom NIS column not dropped?)", v)
	}
}

// TestRenderedScaleFallback: one filing's caption carries no scale hint.
func TestRenderedScaleFallback(t *testing.T) {
	dir := fixture(t, "q1_2024")
	roles, _ := StatementFiles(dir)
	tbl, _ := ReadRendered(filepath.Join(dir, roles[RoleIncome]))
	if !tbl.ScaleAssumed {
		t.Error("expected ScaleAssumed for a caption with no '$ in Thousands'")
	}
	dir2 := fixture(t, "q3_2025")
	roles2, _ := StatementFiles(dir2)
	tbl2, _ := ReadRendered(filepath.Join(dir2, roles2[RoleIncome]))
	if tbl2.ScaleAssumed || tbl2.Scale != 1e3 {
		t.Errorf("q3_2025: ScaleAssumed=%v scale=%g, want false/1000", tbl2.ScaleAssumed, tbl2.Scale)
	}
}

// TestDetectorBIsolatesMisTaggedFiling is the core safety test: the filer tagged
// this filing in thousands with unit USD, both readers agree on the wrong
// number, and the metric must still be withheld.
func TestDetectorBIsolatesMisTaggedFiling(t *testing.T) {
	bad := extract(t, "q1_2024")
	rev := line(t, bad, KeyTotalRevenues)
	if rev == nil {
		t.Fatal("no revenue line")
	}
	if rev.Publishable() {
		t.Errorf("mis-tagged revenue %g is publishable; it must be withheld", rev.Value)
	}
	if rev.Confidence != ConfidenceSuspect {
		t.Errorf("confidence = %q, want %q", rev.Confidence, ConfidenceSuspect)
	}
	if !hasSignal(rev, DetectorImplausibleTagging) {
		t.Errorf("detector B did not fire; notes=%v", rev.Notes)
	}

	// ...and must stay silent on the clean filings.
	for _, name := range []string{"q3_2025", "fy_2019_20f", "fy_2024_20f", "q3_2022"} {
		f := extract(t, name)
		for _, set := range [][]Line{f.Statements.Income, f.Statements.Balance} {
			for i := range set {
				if hasSignal(&set[i], DetectorImplausibleTagging) {
					t.Errorf("%s/%s: detector B false positive", name, set[i].Key)
				}
			}
		}
	}
}

// TestHappyPathVerified is the Definition-of-Done data.
func TestHappyPathVerified(t *testing.T) {
	f := extract(t, "q3_2025")
	want := map[string]struct {
		fmtd string
		yoy  float64
	}{
		KeyTotalRevenues: {"$47.0M", 12.6},
		KeyEPSBasic:      {"$0.09", 28.6},
		KeyNetIncome:     {"$5.3M", 37.1},
	}
	for key, w := range want {
		l := line(t, f, key)
		if l == nil {
			t.Errorf("%s: missing", key)
			continue
		}
		if l.ValueFmt != w.fmtd {
			t.Errorf("%s: valueFmt = %q, want %q", key, l.ValueFmt, w.fmtd)
		}
		if l.Confidence != ConfidenceVerified {
			t.Errorf("%s: confidence = %q, want verified", key, l.Confidence)
		}
		if l.YoYPct == nil || math.Abs(*l.YoYPct-w.yoy) > 0.05 {
			t.Errorf("%s: yoy = %v, want %v", key, l.YoYPct, w.yoy)
		}
	}
	cash := line(t, f, KeyCash)
	if cash == nil || cash.ValueFmt != "$72.0M" {
		t.Fatalf("cash = %v, want $72.0M", cash)
	}
	// A balance-sheet comparative is the prior year-END, so a YoY percentage
	// against it would be misleading and must not be emitted.
	if cash.YoYPct != nil {
		t.Errorf("cash carries yoyPct = %v; balance lines must not", *cash.YoYPct)
	}
	if cash.PriorLabel == "" {
		t.Error("cash missing PriorLabel")
	}
}

// TestElementAliasRank covers the filing that uses ProfitLossFromContinuingOperations.
func TestElementAliasRank(t *testing.T) {
	f := extract(t, "q3_2022")
	ni := line(t, f, KeyNetIncome)
	if ni == nil {
		t.Fatal("net income not found via alias rank")
	}
	if math.Abs(ni.Value-484000) > 1 {
		t.Errorf("net income = %g, want 484000", ni.Value)
	}
}

// TestGradeTruthTable pins the publication gate.
func TestGradeTruthTable(t *testing.T) {
	v1, v2, v3 := 100.0, 100.0, 200.0
	for _, tc := range []struct {
		name       string
		inst, rend *float64
		signals    []string
		wantConf   string
		wantPub    bool
	}{
		{"both agree", &v1, &v2, nil, ConfidenceVerified, true},
		{"both disagree", &v1, &v3, nil, ConfidenceSuspect, false},
		{"instance only", &v1, nil, nil, ConfidenceSingleSource, true},
		{"rendered only", nil, &v2, nil, ConfidenceSingleSource, true},
		{"signal beats agreement", &v1, &v2, []string{DetectorImplausibleTagging}, ConfidenceSuspect, false},
		{"neither", nil, nil, nil, ConfidenceSuspect, false},
	} {
		conf, _, _ := Grade(tc.inst, tc.rend, UnitUSD, tc.signals)
		if conf != tc.wantConf {
			t.Errorf("%s: confidence = %q, want %q", tc.name, conf, tc.wantConf)
		}
		if got := (Line{Confidence: conf}).Publishable(); got != tc.wantPub {
			t.Errorf("%s: publishable = %v, want %v", tc.name, got, tc.wantPub)
		}
	}
}

// TestChainCheckMatchesByPeriod: comparables are matched by date, not by
// position, because the corpus has gaps between quarters.
func TestChainCheckMatchesByPeriod(t *testing.T) {
	mk := func(acc, end, dur string, cur, prior float64) Result {
		p := prior
		return Result{Accession: acc, Financials: &FilingFinancials{
			Accession: acc,
			Period:    Period{EndDate: end, Duration: dur},
			Statements: StatementSets{Income: []Line{{
				Key: KeyTotalRevenues, Value: cur, PriorValue: &p,
				Confidence: ConfidenceVerified, Unit: UnitUSD,
			}}},
		}}
	}
	// Two filings a year apart that agree, plus an unrelated quarter between
	// them that must not be treated as the comparable.
	rs := []Result{
		mk("old", "2024-09-30", durationP3M, 100, 90),
		mk("mid", "2025-03-31", durationP3M, 55, 50),
		mk("new", "2025-09-30", durationP3M, 120, 100),
	}
	chainCheck(rs)
	for _, r := range rs {
		if l := findLine(r.Financials, KeyTotalRevenues); l.Confidence != ConfidenceVerified {
			t.Errorf("%s: unexpectedly suspect: %v", r.Accession, l.Notes)
		}
	}

	// Now break the chain: "new" claims a prior that does not match "old".
	broken := []Result{
		mk("old", "2024-09-30", durationP3M, 100, 90),
		mk("new", "2025-09-30", durationP3M, 120, 999),
	}
	chainCheck(broken)
	for _, r := range broken {
		if l := findLine(r.Financials, KeyTotalRevenues); l.Confidence != ConfidenceSuspect {
			t.Errorf("%s: chain break not flagged", r.Accession)
		}
	}
}

// TestWriteLoadRoundTrip also covers the absent-file contract.
func TestWriteLoadRoundTrip(t *testing.T) {
	dir := t.TempDir()
	if got, err := Load(dir); got != nil || err != nil {
		t.Fatalf("Load(empty) = (%v, %v), want (nil, nil)", got, err)
	}
	f := extract(t, "q3_2025")
	if err := Write(dir, f); err != nil {
		t.Fatal(err)
	}
	got, err := Load(dir)
	if err != nil || got == nil {
		t.Fatalf("Load = (%v, %v)", got, err)
	}
	if got.Accession != f.Accession || len(got.Statements.Income) != len(f.Statements.Income) {
		t.Errorf("round trip mismatch")
	}
	// Byte stability: a second write of the same struct is identical.
	b1, _ := os.ReadFile(filepath.Join(dir, FileName))
	if err := Write(dir, f); err != nil {
		t.Fatal(err)
	}
	b2, _ := os.ReadFile(filepath.Join(dir, FileName))
	if string(b1) != string(b2) {
		t.Error("write is not byte-stable")
	}
}

// TestNoYoYAcrossSignFlip: Q1 2024 swung from a $(0.04) loss per share to a
// $0.04 profit. The arithmetic yields -200%, which reads as a collapse rather
// than a return to profitability, so no percentage may be emitted.
func TestNoYoYAcrossSignFlip(t *testing.T) {
	f := extract(t, "q1_2024")
	eps := line(t, f, KeyEPSBasic)
	if eps == nil {
		t.Fatal("no EPS line")
	}
	if eps.PriorValue == nil || *eps.PriorValue >= 0 {
		t.Skipf("fixture no longer has a negative prior EPS (%v)", eps.PriorValue)
	}
	if eps.YoYPct != nil {
		t.Errorf("YoYPct = %v across a sign flip; want nil", *eps.YoYPct)
	}
	if !eps.Publishable() || eps.ValueFmt != "$0.04" {
		t.Errorf("EPS should still publish its own value, got %q (%s)", eps.ValueFmt, eps.Confidence)
	}
}

// --- adjudicator ------------------------------------------------------------

type fakeAdjudicator struct {
	res  *AdjudicationResult
	seen AdjudicationRequest
}

func (f *fakeAdjudicator) Adjudicate(_ context.Context, req AdjudicationRequest) (*AdjudicationResult, error) {
	f.seen = req
	return f.res, nil
}

// TestAdjudicationValidation is the guarantee that a fabricated number cannot be
// published: the model's answer must name a supplied candidate, carry that
// candidate's exact value, and quote the evidence verbatim.
func TestAdjudicationValidation(t *testing.T) {
	req := AdjudicationRequest{
		Metric:   "Total revenues",
		Evidence: "Total revenues were $37.7 million in the first quarter of 2024.",
		Candidates: []AdjudicationCandidate{
			{ID: "A", Value: 37736},
			{ID: "B", Value: 37736000},
		},
	}
	quote := "Total revenues were $37.7 million in the first quarter of 2024."
	for _, tc := range []struct {
		name string
		res  AdjudicationResult
		want bool
	}{
		{"valid choice", AdjudicationResult{Choice: "B", Value: 37736000, EvidenceQuote: quote, Confidence: "high"}, true},
		{"declines", AdjudicationResult{Choice: "none"}, false},
		{"unknown candidate id", AdjudicationResult{Choice: "Z", Value: 37736000, EvidenceQuote: quote, Confidence: "high"}, false},
		{"invented value", AdjudicationResult{Choice: "B", Value: 42000000, EvidenceQuote: quote, Confidence: "high"}, false},
		{"quote not in evidence", AdjudicationResult{Choice: "B", Value: 37736000, EvidenceQuote: "Revenues were $99 million.", Confidence: "high"}, false},
		{"empty quote", AdjudicationResult{Choice: "B", Value: 37736000, EvidenceQuote: "", Confidence: "high"}, false},
		{"low confidence", AdjudicationResult{Choice: "B", Value: 37736000, EvidenceQuote: quote, Confidence: "low"}, false},
	} {
		got := validateAdjudication(&tc.res, req)
		if got != tc.want {
			t.Errorf("%s: validateAdjudication = %v, want %v", tc.name, got, tc.want)
		}
	}
}

// TestGatherEvidenceIsProseOnly proves the flattened statement table in the same
// exhibit is not sent: it holds the very ambiguity under dispute.
func TestGatherEvidenceIsProseOnly(t *testing.T) {
	ev := gatherEvidence(fixture(t, "q1_2024"), KeyTotalRevenues, 4000)
	if ev == "" {
		t.Fatal("no evidence gathered from EX-99")
	}
	if !contains(ev, "$37.7 million") {
		t.Errorf("evidence lacks the unit-explicit sentence: %q", ev)
	}
	if contains(ev, "37,736 30,710") {
		t.Error("evidence includes the flattened statement table")
	}
}

func contains(h, n string) bool {
	return len(h) >= len(n) && (func() bool {
		for i := 0; i+len(n) <= len(h); i++ {
			if h[i:i+len(n)] == n {
				return true
			}
		}
		return false
	}())
}

// TestAdjudicationRepairsMisTaggedFiling: with a valid decision the withheld
// metric publishes; without one it stays withheld.
func TestAdjudicationRepairsMisTaggedFiling(t *testing.T) {
	f := extract(t, "q1_2024")
	rev := line(t, f, KeyTotalRevenues)
	if rev.Publishable() {
		t.Fatal("precondition: revenue should start withheld")
	}
	quote := "Total revenues were $37.7 million in the first quarter of 2024."
	fake := &fakeAdjudicator{res: &AdjudicationResult{
		Choice: "B", Value: 37736000, EvidenceQuote: quote, Confidence: "high",
	}}
	adjudicateWithEvidence(context.Background(), fake, fixture(t, "q1_2024"), f)

	rev = line(t, f, KeyTotalRevenues)
	if !rev.Publishable() {
		t.Errorf("after adjudication revenue is still withheld: %v", rev.Notes)
	}
	if rev.ValueFmt != "$37.7M" || rev.Source != SourceAdjudicated {
		t.Errorf("got %s/%s, want $37.7M/%s", rev.ValueFmt, rev.Source, SourceAdjudicated)
	}

	// A declining adjudicator must leave the metric withheld.
	f2 := extract(t, "q1_2024")
	decline := &fakeAdjudicator{res: &AdjudicationResult{Choice: "none"}}
	adjudicateWithEvidence(context.Background(), decline, fixture(t, "q1_2024"), f2)
	if line(t, f2, KeyTotalRevenues).Publishable() {
		t.Error("declined adjudication must leave the metric withheld")
	}
}
