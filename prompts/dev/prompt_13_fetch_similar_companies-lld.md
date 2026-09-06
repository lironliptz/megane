# LLD: Batch Peer Company Ingestion (`cmd/fetch-similar`)

Implements `prompt_13_fetch_similar_companies-hld.md` (the contract) and the idea file
`prompt_13_fetch_similar_companies.txt`.

Triage: **STANDARD** — matches the HLD's own triage. One new binary (4 files), one Python
script parameterized, one Makefile target. No migration, no schema change, no new package
under `internal/`.

> **Governing rule, from the HLD:** the curated peer list is never mutated by automation.
> Resolved CIKs go only to `{slug}.fetch-status.json`. Skipping a peer honestly beats
> guessing a CIK.

---

## 1. Scope

**In (P0):** parameterize `fetch_kamada.py`; `cmd/fetch-similar` with parse → resolve →
fetch → meta → financials → status; ticker→CIK map; idempotency; exit codes; the
`--dry-run` costed survey (§5).
**In (P1):** price backfill step; `make fetch-similar`; unit tests.
**Out:** peer discovery, prompt 12 training, Go fetch port, DB changes, admin endpoint.

---

## 2. Three corrections to the HLD contract

Verified against the working tree; each would fail at runtime as written.

**C1 — `build_meta_all.py` takes a positional argument, not `--dir`.**
HLD §2 and §D1 both specify `build_meta_all.py --dir {root}/companies/{cik}`. The script
declares `company_dir` as `nargs="?"` positional plus `--all`. The correct invocation is:

```
python3 python/edgar/build_meta_all.py {root}/companies/{cik} [--all]
```

**C2 — the financials step runs in-process, not via `go run`.**
HLD §D1 step 3 specifies `go run ./cmd/edgar-financials …`. `cmd/fetch-similar` is in the
same module, so it can call `financials.ExtractAll` and `financials.FillGaps` directly.
`go run` in a per-peer loop recompiles the binary every iteration, requires the Go
toolchain on the host at runtime, and reduces typed errors to an exit code and a stderr
scrape. In-process keeps D1's actual intent — reuse the shipped extractor, don't rewrite
it — while dropping a subprocess that buys nothing. The Python steps remain `exec`.

**C3 — the price step's outcome cannot be read from an error.**
HLD §D5 says to call `Service.Timeline(...)`. That works — but `pricesFor` deliberately
swallows provider failures (logs and returns empty; "price failures degrade the chart, they
do not fail the request"). So a failed backfill returns `nil` error. The step outcome must
be read from the returned `Timeline.PriceCoverage.Status`:

| `PriceCoverage.Status` | Step result |
|---|---|
| `db.PriceStatusOK` | `ok` |
| `db.PriceStatusNoSymbol` | `skipped` — note "no ticker in submissions.json" |
| `db.PriceStatusNotFound`, `db.PriceStatusError` | `error` |
| coverage `nil` | `error` — note "no coverage row written" |

Also verified: `Config.FetchOnOpen` is declared (`service.go:17`) but **read nowhere in the
package**. `Timeline` calls `pricesFor` unconditionally, so the price step works regardless
of `STOCK_FETCH_ON_COMPANY_OPEN`. D5 is safe with default env.

---

## 3. Current state (verified)

| Thing | Reality |
|---|---|
| `python/edgar/fetch_kamada.py` | 128 lines; `CIK`/`OUT` module constants (lines 11, 21); no `argparse` |
| `python/edgar/build_meta_all.py` | positional `company_dir` + `--all` (C1) |
| `cmd/edgar-financials` | `--root --cik --all --fill-gaps --gaps --dry-run --report` |
| `companyview.NewService` | `(store filedb.CompanyStore, db *db.DB, provider, fallback marketdata.PriceProvider, cfg Config)` |
| `Service.Timeline` | `(ctx, cik string, req Window, filter string) (*Timeline, error)`; `ClampWindow` clamps to filing coverage |
| Fixture `fileDB/similar/kamada.json` | present; 10 peers — 7 with `company_id`, 2 null-with-ticker, 1 null-no-ticker |
| Makefile | `run build test version docker clean` — no fetch target |

The fixture's split (7 / 2 / 1) exercises all three resolution paths, so it doubles as the
integration fixture.

---

## 4. File-by-file

### 4.1 `python/edgar/fetch_kamada.py` — parameterize (D2)

Replace the module constants with `argparse`, preserving zero-arg behaviour:

```python
def parse_args():
    p = argparse.ArgumentParser(description="Fetch SEC filings for one CIK")
    p.add_argument("--cik", default="0001567529")
    p.add_argument("--years", type=int, default=10)
    p.add_argument("--root", default=os.environ.get("FILEDB_DIR", "./fileDB"))
    p.add_argument("--user-agent", default=os.environ.get("SEC_EDGAR_USER_AGENT", ""))
    return p.parse_args()
```

- `CIK`, `CIK_INT`, `OUT`, `HEADERS` become locals derived in `main()`; every function that
  used them takes them as parameters.
- CIK is zero-padded to 10 digits; `CIK_INT` = `str(int(cik))` for the submissions URL.
- `--years` filters filings by `filingDate >= today - years` before download (today the
  script has no depth limit; the flag is what makes `YEARS=1` smoke runs cheap).
- Keep the existing ≥120 ms inter-request sleep. **Do not parallelize**: SEC's limit is
  10 req/s per IP across all EDGAR hosts, and the orchestrator is serial for the same reason.
- Exit non-zero on unrecoverable failure so the orchestrator can mark the step.

No rename in P0 (HLD D2); a `fetch_filings.py` alias is P2.

### 4.2 `cmd/fetch-similar/main.go`

```go
func main() {
    similar  := flag.String("similar", "", "path to the curated peer JSON (required)")
    root     := flag.String("root", envOr("FILEDB_DIR", "./fileDB"), "fileDB root")
    years    := flag.Int("years", 10, "filing history depth")
    steps    := flag.String("steps", "fetch,meta,financials,prices", "comma-separated steps")
    force    := flag.Bool("force", false, "re-run steps whose output already exists")
    dryRun   := flag.Bool("dry-run", false, "survey only: fetch submissions.json, report counts + estimates, download nothing")
    peerDelay := flag.Duration("peer-delay", 5*time.Second, "sleep between peers")
    failFast := flag.Bool("fail-fast", false, "stop after the first peer error")
    includeRef := flag.Bool("include-reference", false, "also ingest the reference company")
}
```

**Open question resolved.** `--include-reference` ships in P0, default `false`. It is
~5 lines — prepend the reference block to the peer slice as a synthetic peer — and it makes
a fresh clone self-sufficient, which is otherwise a manual step the "Done when" does not
cover.

Flow follows HLD §5 exactly. Usage or parse failure → exit 2 before any peer runs.

### 4.3 `cmd/fetch-similar/tickers.go` (D4)

```go
type tickerMap map[string]string // upper(ticker) -> 10-digit CIK

func loadTickerMap(ctx context.Context, ua string) (tickerMap, error)
```

- Fetched **once per run**, and only when at least one peer has `company_id == null` **and**
  a non-empty `ticker`. A list with no null IDs makes zero network calls.
- `https://www.sec.gov/files/company_tickers.json`; `User-Agent` required — empty UA returns
  `ErrNoUserAgent` before any request, mirroring `financials.Client` (prompt 9 D8).
- Payload is a JSON object keyed by index: `{"0":{"cik_str":320193,"ticker":"AAPL",...}}`.
  `cik_str` is an **int** — zero-pad to 10 via `fmt.Sprintf("%010d", …)`.
- Miss → peer `skipped`, note `"ticker not in SEC company_tickers"`. Never guess.

### 4.4 `cmd/fetch-similar/status.go`

Structs mirroring HLD §3.2 verbatim (`FetchStatus`, `PeerStatus`, `StepResults`, `Summary`).

- Output path: input path with `.json` replaced by `.fetch-status.json`.
- Written **wholesale** each run, `MarshalIndent`, trailing newline.
- Written even when the run ends in errors — a failed batch must still leave a diagnosable
  artifact. Only exit 2 (parse/usage) produces no file.
- The source JSON is opened **read-only** and never re-serialized.

### 4.5 `cmd/fetch-similar/steps.go`

```go
type stepFn func(ctx context.Context, p *peer, cfg *config) (result string, note string)
```

| Step | Implementation | Skip when (unless `--force`) |
|---|---|---|
| `fetch` | `exec` python (§4.1) | `submissions.json` exists **and** its newest filing is within the `--years` window |
| `meta` | `exec` python, **positional dir** (C1) | every accession dir already has `meta.json` |
| `financials` | **in-process** `financials.ExtractAll` then `FillGaps` (C2) | every qualifying accession has `financials.json` |
| `prices` | in-process `Service.Timeline` (C3) | `stock_price_coverage.status = ok` and span covers the filing window |

Subprocess contract (D7): `exec.CommandContext`, combined stdout+stderr into a ring buffer,
**last 4 KB** into `note` on failure only, `duration_ms` per peer. Non-zero exit → that step
`error`, continue to the next peer unless `--fail-fast`.

Counts after the steps run, read from disk and DB (not from step output):
`filing_count` = accession dirs; `financials_count` = `financials.json` files;
`stock_bar_count` = `SELECT count(*) FROM stock_prices WHERE cik = ?`.

Peer status rollup: all requested steps `ok` → `complete`; any `error` → `partial` if at
least one step succeeded, else `error`; unresolved CIK → `skipped`.

### 4.6 `Makefile`

```make
fetch-similar:
	CGO_ENABLED=1 go run ./cmd/fetch-similar \
	  --similar $(SIMILAR) $(if $(ROOT),--root $(ROOT),) $(if $(YEARS),--years $(YEARS),)
```

`SIMILAR` required; `ROOT`/`YEARS` optional, matching HLD §10.

---

## 5. `--dry-run` — the costed survey (HLD D9)

New file `cmd/fetch-similar/plan.go`. A dry run short-circuits every step in §4.5: it
resolves the CIK, fetches **only** `submissions.json`, and returns without downloading a
single filing document.

### 5.1 What one request yields

`submissions.json` is a complete filing index. Verified fields in `filings.recent`
(parallel arrays, one entry per filing):

```
accessionNumber  filingDate  form  size  isXBRL  isInlineXBRL  primaryDocument  reportDate
```

plus a header carrying `name`, `tickers`, `exchanges`, `sic`, `sicDescription`,
`fiscalYearEnd`, `category`.

**`filings.files` must also be read.** SEC caps `recent` at ~1000 entries and pushes older
filings into separate JSON files listed there. The reference company has 504 filings and an
**empty** `files` array, so this path is untested locally — implement it, and record
`index_pages_fetched` in the plan so a truncated survey is visible rather than silent.

### 5.2 Plan record

```go
type PeerPlan struct {
    CompanyName string `json:"company_name"`
    CompanyID   string `json:"company_id"`   // resolved, or "" when skipped
    Ticker      string `json:"ticker"`
    SIC         string `json:"sic"`
    SICDesc     string `json:"sic_description"`
    Exchanges   []string `json:"exchanges"`
    Status      string `json:"status"`       // planned | skipped | error

    FilingsInIndex   int `json:"filings_in_index"`
    FilingsInWindow  int `json:"filings_in_window"`
    FilingsOnDisk    int `json:"filings_on_disk"`
    FilingsToFetch   int `json:"filings_to_fetch"`
    IndexPagesFetched int `json:"index_pages_fetched"`

    FormCounts map[string]int `json:"form_counts"`
    EarliestFiling string     `json:"earliest_filing"`
    LatestFiling   string     `json:"latest_filing"`

    XBRLFlagged int `json:"xbrl_flagged"`     // UPPER BOUND on financials.json

    EstDownloadBytes int64 `json:"est_download_bytes"`
    EstDiskBytes     int64 `json:"est_disk_bytes"`
    EstRequests      int   `json:"est_requests"`
    EstSeconds       int   `json:"est_seconds"`
    Note string `json:"note,omitempty"`
}
```

Batch file `{slug}.fetch-plan.json`: `source_file`, `reference`, `generated_at`, `config`
(same block as the status file plus `"dry_run": true`), `peers[]`, and `totals` summing
every numeric field across peers plus `planned / skipped / error` counts.

### 5.3 Estimator constants — measured, and named

```go
const (
    // Measured 2026-09-01 on the reference corpus: the filing index sums to
    // 464 MB across 504 filings while the folder occupies 647 MB. The fetch
    // stores the full-submission .txt (which is what `size` measures) AND the
    // extracted documents, so on-disk runs ~1.4x the indexed bytes.
    diskExpansionFactor = 1.4

    // 4,422 files across 382 accession folders on the same corpus.
    avgFilesPerFiling = 12

    // The Python fetcher sleeps >=120 ms between requests, well inside SEC's
    // 10 req/s per-IP ceiling. Peers run serially, so this is the batch floor.
    requestsPerSecond = 8.3
)
```

```
EstDownloadBytes = Σ size over FilingsToFetch
EstDiskBytes     = EstDownloadBytes * diskExpansionFactor
EstRequests      = 1 + IndexPagesFetched + FilingsToFetch*avgFilesPerFiling
EstSeconds       = EstRequests/requestsPerSecond + peerDelay
```

All three constants are assumptions from **one** issuer and must be labelled as estimates in
output ("~530 MB", not "530 MB"). A filer with many large exhibits will exceed them.

Sanity values for the fixture company: 10-year window → 359 filings, 378 MB indexed,
**≈530 MB** on disk, ~4,300 requests, **~9 min**. 1-year → 73 filings, 66 MB, **≈92 MB**,
~2 min. That per-peer cost is the whole point of the mode: a ten-peer batch is hours, not
minutes, and the survey says so before you start it.

### 5.4 Rules

1. **Never writes `{slug}.fetch-status.json`.** A survey cannot clobber the record of a real
   run. Separate filename, enforced by a test.
2. **Never writes into `{root}/companies/`.** The fetched `submissions.json` is held in
   memory and discarded — writing it would half-create a company folder that the real run's
   idempotency check (§4.5) would then read as "already fetched".
3. **`XBRLFlagged` is an upper bound, and the JSON says so** via a `note` on any peer where
   it is non-zero: the flag means the filer tagged XBRL, not that EDGAR generated the viewer
   bundle the extractor needs. Measured on the reference company: 28 flagged in the 10-year
   window, 15 with a usable local instance.
4. **Unresolved peers still get a row** — `status: "skipped"` with the same note the real
   run would use, so the plan is a complete picture of the list.
5. **Exit codes are the D8 codes**, with `planned` counted as `complete`: a survey where
   everything resolved exits 0.
6. **One request per peer**, and the ticker map at most once per run — a dry run over the
   10-peer fixture makes ≤11 requests total.

### 5.5 Stdout summary

After writing the plan file, print a table to stdout (no extra flags):

```
fetch-similar dry-run  source=kamada.json  years=10  peers=10
  ADMA Biologics (0001368514)  planned  359 filings  ~530 MB  ~9 min
  Grifols (0001438569)         planned  412 filings  ~610 MB  ~10 min
  Kedrion                    skipped  no SEC CIK
  …
TOTAL  7 planned  3 skipped  ~2.9 GB  ~58 min  → see kamada.fetch-plan.json
```

Numbers use `~` prefix. Skipped peers show the note only. Exit code follows D8 with
`planned` counted as success.

### 5.6 Example plan file (truncated)

See HLD §3.3 for the full schema. A real dry run on the fixture with `years=10` should
produce batch totals in the same order of magnitude as 7 × ~530 MB indexed disk per peer.

---

## 6. Wiring the price step (P1)

Construct the service exactly as `cmd/server/main.go:136–142` does:

```go
database, _ := db.Open(envOr("DB_PATH", "./.db/megane.db"))
store := filedb.NewFileDBStore(root, filedb.Options{TTL: filedb.ParseTTL(os.Getenv("FILEDB_CACHE_TTL"))})
svc := companyview.NewService(store, database, marketdata.NewFromEnv(os.Getenv("MARKET_DATA_PROVIDER")),
    marketdata.NewFallbackFromEnv(), companyview.Config{
        ChunkYears: getEnvInt("MARKET_DATA_CHUNK_YEARS", marketdata.DefaultChunkYears),
        ChunkDelay: getEnvDuration("MARKET_DATA_CHUNK_DELAY", marketdata.DefaultChunkDelay),
    })
```

Call `svc.Timeline(ctx, cik, companyview.Window{From: "1900-01-01", To: "2100-01-01"}, "")`.
`ClampWindow` narrows this to the company's own filing coverage, which is the intended
"full history" — no need to compute a window. Read the result per C3.

The DB is opened once for the whole run, not per peer.

---

## 7. Tests

Unit, no network, no subprocess:

| Test | Asserts |
|---|---|
| `TestParseSimilarJSON` | fixture parses; 10 peers; 7/2/1 resolution split |
| `TestResolveCIK` | `company_id` wins; null+ticker hits the map; null+no-ticker → `skipped`; unknown ticker → `skipped` with the documented note |
| `TestTickerMapZeroPads` | `cik_str: 320193` → `"0000320193"`; lookup is case-insensitive |
| `TestTickerMapRequiresUserAgent` | empty UA → error, **and no HTTP request is made** (httptest counter stays 0) |
| `TestStatusPathDerivation` | `kamada.json` → `kamada.fetch-status.json` |
| `TestStatusRoundTrip` | marshal → unmarshal → identical; summary counts match rows |
| `TestSourceJSONNeverWritten` | source file bytes identical after a dry run |
| `TestDryRunPlansAllPeers` | plan lists every peer; zero exec calls |
| `TestPlanFromSubmissionsFixture` | counts, form distribution, date span and `xbrl_flagged` from a checked-in `submissions.json` slice |
| `TestPlanWindowFilter` | `--years 1` vs `--years 10` yield different `filings_in_window` |
| `TestPlanSubtractsOnDisk` | accessions already present drop out of `filings_to_fetch` |
| `TestDryRunNeverWritesStatus` | after a dry run: plan file exists, `.fetch-status.json` untouched, `{root}/companies/` unchanged |
| `TestEstimatorArithmetic` | disk = bytes x 1.4; requests and seconds follow the §5.3 formulas |
| `TestPriceStatusMapping` | the four `db.PriceStatus*` values → the C3 table |
| `TestExitCodes` | all-skipped → 0; one error → 1; bad JSON → 2 |

`steps.go` takes an injectable `runner func(ctx, name string, args ...string) (int, []byte, error)`
so exec paths are tested with a fake rather than a real subprocess.

**Not automated:** a live multi-peer SEC fetch. Verified manually per §8.

---

## 8. Build order

1. Parameterize `fetch_kamada.py`; verify `--cik 0001567529 --years 1` still writes to the
   existing tree and that zero-arg behaviour is unchanged.
2. `status.go` + `main.go` parse/plan + `--dry-run`. Gate: dry run on the fixture lists 10
   peers and makes no network call.
3. `tickers.go` with the UA guard.
3b. `plan.go` — the costed survey (§5). Gate: a dry run on the fixture makes <=11 requests,
   writes only `kamada.fetch-plan.json`, and creates nothing under `{root}/companies/`.
4. `steps.go` fetch + meta (exec) with the injectable runner. **Gate: the meta command uses
   a positional dir (C1).**
5. Financials step in-process (C2).
6. Exit codes + `make fetch-similar`.
7. Price step (P1) + C3 status mapping.
8. Manual smoke per §8.

---

## 9. Done when

```bash
make fetch-similar SIMILAR=./fileDB/similar/kamada.json YEARS=1
```

writes `fileDB/similar/kamada.fetch-status.json` with 10 peer rows and a summary; at least
ADMA Biologics (`0001368514`) has `fileDB/companies/0001368514/submissions.json` and ≥1
accession folder with `meta.json`; the three peers without a US CIK are `skipped` with a
stated reason, not `error`; exit code is 0.

Then open `/companies`, search "ADMA", and the ingested peer appears with its filings —
the same page that today shows only Kamada.

And the negative case: running the same command a second time without `--force` re-writes
the status file with every step `skipped`, downloads nothing, and still exits 0.

---

## 10. Risks

| Risk | Handling |
|---|---|
| Wrong `build_meta_all.py` invocation silently no-ops | C1; asserted in build step 4 |
| A peer's SEC fetch trips the 10 req/s IP limit | Serial peers, 120 ms in-script spacing, `--peer-delay` 5 s. Never parallelize peers |
| `SEC_EDGAR_USER_AGENT` unset | Ticker map and financials gap-fill fail fast as `error` on those steps only; fetch/meta still run |
| Foreign filers yield few `financials.json` | Expected — `partial`, never `error` (HLD §8) |
| Disk: ~650 MB per company as currently fetched | `--years` bounds a smoke run; corpus-wide pruning is prompt 12's §5 concern, not this CLI's |
| Half-written status on crash | Marshal fully in memory, then one `os.WriteFile` |
| Dry-run estimates read as promises | All three constants derive from one issuer; render with `~` and label them estimates (§5.3) |
| `filings.files` overflow untested locally | Reference company has 504 filings and an empty `files` array; `index_pages_fetched` makes a truncated survey visible (§5.1) |

---

## 11. Open question (1)

**Where the ticker-map cache lives.** HLD D4 defers it to P2. If it lands, prefer
`{root}/similar/.company_tickers.cache.json` with a TTL, mirroring prompt 9's
`{root}/companies/{cik}/.sec/` convention — both sit under the gitignored `fileDB/`. Not
required for P0: the map is fetched at most once per run and only when a null-ID peer
carries a ticker.
