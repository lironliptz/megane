# Company Facts fixture

`CIK0001567529.trimmed.json` is the live SEC document
(`https://data.sec.gov/api/xbrl/companyfacts/CIK0001567529.json`, fetched 2026-08-26)
reduced to four concepts and five accessions. Regenerate with the script in
`prompt_9_extra_source_for_financial_reports-implementation.md`.

Kept deliberately:

| Kept | Why the tests need it |
|---|---|
| `0001213900-24-068559` | Clean fill; the Definition-of-Done accession; also carries the multi-period trap (prior-year H1 alongside the quarter) |
| `0001213900-24-097126` | Clean fill **and** the corroborator that proves `23-085500` is mis-tagged |
| `0001213900-23-085500` | Mis-tagged x1000 in SEC's own feed — the scale-reconciliation regression |
| `0001213900-25-042940` | Corroborator proving prompt 8's `24-040628` is mis-tagged (groundwork for HLD D9) |
| `0001213900-24-040628` | The filing prompt 8 withholds |
| the `ILS` `Revenue` entry | Unit-filter regression: 10,000,000,000 must never enter the index |

No test in this package performs a live network request.
