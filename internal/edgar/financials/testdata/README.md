# Fixtures

Copied verbatim from the reference corpus (`fileDB/companies/0001567529`) by the
generator in `prompt_8_extracting_info_from_filings-implementation.md`. Each
directory holds `FilingSummary.xml`, `meta.json`, the XBRL instance, and the
`R*.htm` reports that FilingSummary lists under `MenuCategory=Statements`.

| Fixture | Source accession | Why it exists |
|---|---|---|
| `q3_2025` | 0001213900-25-107836 | Happy path; the Definition-of-Done filing |
| `q1_2024` | 0001213900-24-040628 | Filer mis-tagged the instance (`decimals="0"`); detector B; carries EX-99 for adjudication |
| `fy_2024_20f` | 0001213900-25-020360 | `R2` is *Audit Information* — role resolution; NIS facts; phantom column |
| `fy_2019_20f` | 0001213900-20-004782 | Classic `xbrli:`-prefixed instance, `US-ASCII` encoding |
| `q3_2022` | 0001213900-22-074381 | `ProfitLossFromContinuingOperations` alias rank |
