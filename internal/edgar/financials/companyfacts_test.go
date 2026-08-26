package financials

import (
	"context"
	"encoding/json"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const (
	accQ2_2024   = "0001213900-24-068559" // clean fill
	accQ3_2024   = "0001213900-24-097126" // clean fill + corroborator
	accQ3_2023   = "0001213900-23-085500" // mis-tagged x1000 in SEC's feed
	accQ1_2024   = "0001213900-24-040628" // prompt 8's withheld filing
	fixtureFacts = "testdata/companyfacts/CIK0001567529.trimmed.json"
)

func loadFixtureFacts(t *testing.T) *CompanyFacts {
	t.Helper()
	b, err := os.ReadFile(fixtureFacts)
	if err != nil {
		t.Fatal(err)
	}
	cf, err := decodeFacts(b, time.Date(2026, 8, 26, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	return cf
}

func fixtureIndex(t *testing.T) *factIndex {
	t.Helper()
	return buildIndex(loadFixtureFacts(t))
}

// --- client -----------------------------------------------------------------

// TestClientRequiresUserAgent: SEC rejects anonymous clients and inventing a
// contact would misrepresent the caller, so the client must refuse *before*
// putting a request on the wire.
func TestClientRequiresUserAgent(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	c := &Client{BaseURL: srv.URL, HTTP: srv.Client(), UserAgent: ""}
	_, err := c.Facts(context.Background(), t.TempDir(), "0001567529", false)
	if err != ErrNoUserAgent {
		t.Errorf("err = %v, want ErrNoUserAgent", err)
	}
	if calls != 0 {
		t.Errorf("made %d request(s) without a User-Agent; want 0", calls)
	}
}

func TestClientSendsUserAgentAndCaches(t *testing.T) {
	body, err := os.ReadFile(fixtureFacts)
	if err != nil {
		t.Fatal(err)
	}
	calls := 0
	var gotUA string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		gotUA = r.Header.Get("User-Agent")
		w.Write(body)
	}))
	defer srv.Close()

	root := t.TempDir()
	c := &Client{BaseURL: srv.URL, HTTP: srv.Client(), UserAgent: "megane-test contact@example.com", TTL: time.Hour}

	if _, err := c.Facts(context.Background(), root, "0001567529", false); err != nil {
		t.Fatal(err)
	}
	if gotUA == "" {
		t.Error("User-Agent header not sent")
	}
	if _, err := os.Stat(CachePath(root, "0001567529")); err != nil {
		t.Fatalf("cache not written: %v", err)
	}
	// Second call must be served from disk.
	if _, err := c.Facts(context.Background(), root, "0001567529", false); err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Errorf("made %d requests; want 1 (second should hit the cache)", calls)
	}
	// An expired cache refetches.
	old := time.Now().Add(-2 * time.Hour)
	os.Chtimes(CachePath(root, "0001567529"), old, old)
	if _, err := c.Facts(context.Background(), root, "0001567529", false); err != nil {
		t.Fatal(err)
	}
	if calls != 2 {
		t.Errorf("expired cache made %d requests; want 2", calls)
	}
}

func TestClientRateLimitedAndNotFound(t *testing.T) {
	for _, tc := range []struct {
		status int
		want   error
	}{
		{http.StatusTooManyRequests, ErrRateLimited},
		{http.StatusNotFound, ErrNoCompanyFacts},
	} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(tc.status)
			w.Write([]byte("Too Many Requests")) // not JSON: must not be parsed
		}))
		c := &Client{BaseURL: srv.URL, HTTP: srv.Client(), UserAgent: "t contact@example.com"}
		_, err := c.Facts(context.Background(), t.TempDir(), "0001567529", false)
		if err != tc.want {
			t.Errorf("status %d: err = %v, want %v", tc.status, err, tc.want)
		}
		srv.Close()
	}
}

func TestPadCIK(t *testing.T) {
	if got := PadCIK("1567529"); got != "0001567529" {
		t.Errorf("PadCIK = %q, want 0001567529", got)
	}
}

// --- index and units --------------------------------------------------------

// TestUnitFilterExcludesILS: the fixture carries an ILS revenue fact of
// 10,000,000,000 for a year whose USD revenue was 160,953,000. Publishing it
// would render "$10.0B", so it must never reach the index.
func TestUnitFilterExcludesILS(t *testing.T) {
	cf := loadFixtureFacts(t)
	var sawILSInDocument bool
	for _, concepts := range cf.Facts {
		for _, node := range concepts {
			if _, ok := node.Units["ILS"]; ok {
				sawILSInDocument = true
			}
		}
	}
	if !sawILSInDocument {
		t.Fatal("fixture no longer carries the ILS entry; the regression is not being exercised")
	}
	idx := buildIndex(cf)
	for _, facts := range idx.byPeriod {
		for _, f := range facts {
			if f.Unit != unitUSDLabel && f.Unit != unitPerShareLabel {
				t.Errorf("non-USD fact entered the index: %+v", f)
			}
			if f.Val == 10000000000 {
				t.Errorf("the ILS revenue fact entered the index: %+v", f)
			}
		}
	}
}

func TestConceptKeyReusesElementRanks(t *testing.T) {
	if k, r, ok := conceptKey("ifrs-full", "Revenue"); !ok || k != KeyTotalRevenues || r != 0 {
		t.Errorf("conceptKey(ifrs-full,Revenue) = %q,%d,%v", k, r, ok)
	}
	if _, _, ok := conceptKey("ifrs-full", "NotAConcept"); ok {
		t.Error("unknown concept mapped")
	}
}

// --- period selection -------------------------------------------------------

// TestPeriodSelectionPicksTheQuarter is the D4 regression: one accession
// reports the quarter, the year to date, the prior-year quarter and the prior
// full year at once, so a naive "first entry" yields prior-year H1 -- a
// plausible number for the wrong period.
func TestPeriodSelectionPicksTheQuarter(t *testing.T) {
	idx := fixtureIndex(t)
	p, ok := selectPeriod(idx, accQ2_2024)
	if !ok {
		t.Fatal("no period selected")
	}
	if p.Label != "Q2 2024" || p.EndDate != "2024-06-30" || p.Duration != durationP3M {
		t.Fatalf("period = %+v, want Q2 2024 / 2024-06-30 / P3M", p)
	}
	f, ok := pickEntry(idx, accQ2_2024, KeyTotalRevenues, p.EndDate, p.Duration, false)
	if !ok {
		t.Fatal("no revenue entry")
	}
	if f.Val != 42472000 {
		t.Errorf("revenue = %g, want 42472000 (68153000 is the prior-year half-year trap)", f.Val)
	}
}

// --- scale reconciliation ---------------------------------------------------

// TestResolveScaleDetectsMisTaggedFiling is the regression this prompt exists
// for. SEC does not normalize scale: it reproduces the filer's tagging, and
// this accession reports every monetary figure at one thousandth.
func TestResolveScaleDetectsMisTaggedFiling(t *testing.T) {
	idx := fixtureIndex(t)
	p, _ := selectPeriod(idx, accQ3_2023)
	res := resolveScale(idx, accQ3_2023, p)
	if res.Irreconcilable {
		t.Fatalf("unexpectedly irreconcilable: %v", res.Evidence)
	}
	if res.Factor != 1000 {
		t.Errorf("factor = %g, want 1000", res.Factor)
	}
	joined := strings.Join(res.Evidence, " ")
	if !strings.Contains(joined, accQ3_2024) {
		t.Errorf("evidence does not name the corroborating accession: %v", res.Evidence)
	}
}

// TestResolveScaleIsPerAccession: the factor is a property of the filing, so
// every monetary metric of the mis-tagged accession is corrected, not just the
// ones that happen to have a corroborator.
func TestResolveScaleIsPerAccession(t *testing.T) {
	idx := fixtureIndex(t)
	p, _ := selectPeriod(idx, accQ3_2023)
	res := resolveScale(idx, accQ3_2023, p)
	for _, key := range []string{KeyTotalRevenues, KeyNetIncome, KeyCash} {
		if res.Corroborated[key] == 0 {
			t.Errorf("%s has no corroborator in the fixture", key)
		}
	}
	if res.Factor != 1000 {
		t.Fatalf("factor = %g", res.Factor)
	}
}

// TestCleanFilingIsNotRescaled guards the other direction.
func TestCleanFilingIsNotRescaled(t *testing.T) {
	idx := fixtureIndex(t)
	for _, acc := range []string{accQ2_2024, accQ3_2024} {
		p, _ := selectPeriod(idx, acc)
		res := resolveScale(idx, acc, p)
		if res.Factor != 1 || res.Irreconcilable {
			t.Errorf("%s: factor = %g irreconcilable=%v, want 1/false", acc, res.Factor, res.Irreconcilable)
		}
	}
}

// TestCompanyFactsWouldResolvePrompt8Suspect records the HLD D9 finding: the
// filing prompt 8 must withhold is resolvable deterministically from SEC's own
// redundancy, with no model involved. The override path is not wired in this
// prompt; this asserts the evidence exists for it.
func TestCompanyFactsWouldResolvePrompt8Suspect(t *testing.T) {
	idx := fixtureIndex(t)
	p, ok := selectPeriod(idx, accQ1_2024)
	if !ok {
		t.Skip("fixture lacks the prompt 8 suspect accession")
	}
	res := resolveScale(idx, accQ1_2024, p)
	if res.Factor != 1000 {
		t.Errorf("factor = %g, want 1000 — Company Facts should identify this filing as mis-tagged", res.Factor)
	}
}

func TestEPSIsNeverScaled(t *testing.T) {
	idx := fixtureIndex(t)
	g := Gap{Dir: t.TempDir(), Accession: accQ3_2023}
	f, note := fillOne(idx, g, Options{Now: func() time.Time { return time.Unix(0, 0).UTC() }})
	if f == nil {
		t.Fatalf("no artifact: %s", note)
	}
	for _, l := range f.Statements.Income {
		if l.Key != KeyEPSBasic {
			continue
		}
		if math.Abs(l.Value-0.07) > 0.001 {
			t.Errorf("EPS = %g, want 0.07 unscaled", l.Value)
		}
	}
}

// TestMisTaggedFilingPublishesCorrectedValue is the end-to-end statement of the
// governing rule for this prompt.
func TestMisTaggedFilingPublishesCorrectedValue(t *testing.T) {
	idx := fixtureIndex(t)
	g := Gap{Dir: t.TempDir(), Accession: accQ3_2023}
	f, note := fillOne(idx, g, Options{Now: func() time.Time { return time.Unix(0, 0).UTC() }})
	if f == nil {
		t.Fatalf("no artifact: %s", note)
	}
	var rev *Line
	for i := range f.Statements.Income {
		if f.Statements.Income[i].Key == KeyTotalRevenues {
			rev = &f.Statements.Income[i]
		}
	}
	if rev == nil {
		t.Fatal("no revenue line")
	}
	if rev.ValueFmt != "$37.9M" {
		t.Errorf("revenue = %s, want $37.9M (never $37.9K)", rev.ValueFmt)
	}
	if !rev.Publishable() {
		t.Errorf("corrected revenue is not publishable: %v", rev.Notes)
	}
	if !strings.Contains(strings.Join(rev.Notes, " "), "x1000") {
		t.Errorf("correction not recorded in notes: %v", rev.Notes)
	}
}

func TestCleanFillMatchesSEC(t *testing.T) {
	idx := fixtureIndex(t)
	g := Gap{Dir: t.TempDir(), Accession: accQ2_2024}
	f, note := fillOne(idx, g, Options{Now: func() time.Time { return time.Unix(0, 0).UTC() }})
	if f == nil {
		t.Fatalf("no artifact: %s", note)
	}
	want := map[string]string{
		KeyTotalRevenues: "$42.5M",
		KeyEPSBasic:      "$0.08",
		KeyNetIncome:     "$4.4M",
		KeyCash:          "$56.5M",
	}
	got := map[string]string{}
	for _, l := range append(append([]Line{}, f.Statements.Income...), f.Statements.Balance...) {
		got[l.Key] = l.ValueFmt
		if !l.Publishable() {
			t.Errorf("%s not publishable: %v", l.Key, l.Notes)
		}
	}
	for k, w := range want {
		if got[k] != w {
			t.Errorf("%s = %s, want %s", k, got[k], w)
		}
	}
	// A balance line must carry no year-over-year percentage.
	for _, l := range f.Statements.Balance {
		if l.YoYPct != nil {
			t.Errorf("%s carries yoyPct", l.Key)
		}
	}
}

// --- gap classification -----------------------------------------------------

func TestClassifyGapsOffline(t *testing.T) {
	root := t.TempDir()
	mk := func(year, acc, cat string, withZip, withLocal bool) {
		dir := filepath.Join(root, "companies", "0001567529", year, acc)
		os.MkdirAll(dir, 0o755)
		meta := map[string]any{"accession": acc, "category": cat, "form": "6-K", "filingDate": year + "-01-01"}
		b, _ := json.Marshal(meta)
		os.WriteFile(filepath.Join(dir, "meta.json"), b, 0o644)
		if withZip {
			os.WriteFile(filepath.Join(dir, acc+"-xbrl.zip"), []byte("x"), 0o644)
		}
		if withLocal {
			Write(dir, &FilingFinancials{
				Accession: acc,
				Statements: StatementSets{Income: []Line{{
					Key: KeyTotalRevenues, Source: SourceBoth, Confidence: ConfidenceVerified,
				}}},
			})
		}
	}
	mk("2024", "acc-local", "quarterly_results", true, true)
	mk("2024", "acc-fillable", "quarterly_results", true, false)
	mk("2024", "acc-noxbrl", "quarterly_results", false, false)
	mk("2024", "acc-ignored", "governance", false, false)

	gaps, err := ClassifyGaps(root, "0001567529")
	if err != nil {
		t.Fatal(err)
	}
	if len(gaps) != 3 {
		t.Fatalf("got %d qualifying accessions, want 3 (governance must be ignored)", len(gaps))
	}
	want := map[string]GapKind{
		"acc-local": GapHasLocal, "acc-fillable": GapFillable, "acc-noxbrl": GapNoXBRL,
	}
	for _, g := range gaps {
		if want[g.Accession] != g.Kind {
			t.Errorf("%s = %s, want %s", g.Accession, g.Kind, want[g.Accession])
		}
	}
}

// TestFillGapsNeverOverwritesLocal: local extraction is stricter, and the API
// is not a superset of it.
func TestFillGapsNeverOverwritesLocal(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "companies", "0001567529", "2024", accQ2_2024)
	os.MkdirAll(dir, 0o755)
	b, _ := json.Marshal(map[string]any{
		"accession": accQ2_2024, "category": "quarterly_results", "form": "6-K", "filingDate": "2024-08-14",
	})
	os.WriteFile(filepath.Join(dir, "meta.json"), b, 0o644)
	os.WriteFile(filepath.Join(dir, accQ2_2024+"-xbrl.zip"), []byte("x"), 0o644)
	Write(dir, &FilingFinancials{
		Accession: accQ2_2024,
		Statements: StatementSets{Income: []Line{{
			Key: KeyTotalRevenues, Value: 1, ValueFmt: "$1", Unit: UnitUSD,
			Source: SourceBoth, Confidence: ConfidenceVerified,
		}}},
	})
	before, _ := os.ReadFile(filepath.Join(dir, FileName))

	rep, err := FillGaps(context.Background(), root, "0001567529", nil, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if rep.SkippedLocal != 1 || rep.Filled != 0 {
		t.Errorf("report = %+v, want SkippedLocal=1 Filled=0", rep)
	}
	after, _ := os.ReadFile(filepath.Join(dir, FileName))
	if string(before) != string(after) {
		t.Error("a locally-sourced artifact was modified")
	}
}

func TestNoFactsRecordIsNotPublishable(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "companies", "0001567529", "2024", "acc-noxbrl")
	os.MkdirAll(dir, 0o755)
	b, _ := json.Marshal(map[string]any{
		"accession": "acc-noxbrl", "category": "quarterly_results", "form": "6-K", "filingDate": "2024-03-06",
	})
	os.WriteFile(filepath.Join(dir, "meta.json"), b, 0o644)

	rep, err := FillGaps(context.Background(), root, "0001567529", nil, Options{WriteNoFacts: true})
	if err != nil {
		t.Fatal(err)
	}
	if rep.NoFacts != 1 {
		t.Fatalf("NoFacts = %d, want 1", rep.NoFacts)
	}
	f, err := Load(dir)
	if err != nil || f == nil {
		t.Fatalf("Load = (%v,%v)", f, err)
	}
	if len(f.Publishable()) != 0 {
		t.Error("a no-facts record published something")
	}
	if !strings.Contains(strings.Join(f.ParseNotes, " "), SourceNone) {
		t.Errorf("reason not recorded: %v", f.ParseNotes)
	}
}
