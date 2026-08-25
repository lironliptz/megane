"""
One-off remediation: fetch primary documents that are referenced by
filing.json (from SEC submissions.json) but were never downloaded into
fileDB/companies/<cik>/<year>/<accession>/.

Skips filings whose primaryDocument path is an EDGAR XSL-viewer path
(e.g. "xslF345X02/ownership.xml") — those are expected to differ from
the on-disk filename and are already handled by build_meta.py's own
ownership-XML lookup, so they are not real gaps.
"""

import json
import time
from pathlib import Path

import requests

CIK = "0001567529"
CIK_INT = str(int(CIK))
BASE_URL = "https://www.sec.gov"

HEADERS = {
    "User-Agent": "megane-edgar-backfill lironliptz@gmail.com"
}

FILE_DB = Path(__file__).resolve().parents[2] / "fileDB" / "companies" / CIK


def find_real_gaps():
    gaps = []
    for year_dir in sorted(FILE_DB.iterdir()):
        if not year_dir.is_dir():
            continue
        for folder in sorted(year_dir.iterdir()):
            if not folder.is_dir():
                continue
            filing_path = folder / "filing.json"
            if not filing_path.exists():
                continue
            filing = json.loads(filing_path.read_text())
            primary = filing.get("primaryDocument")
            if not primary:
                continue
            if (folder / primary).exists():
                continue
            if "/" in primary or primary.endswith(".xml"):
                continue  # benign XSL-viewer path, not a real gap
            gaps.append((folder, filing["accessionNumber"], primary))
    return gaps


def download(url, path):
    r = requests.get(url, headers=HEADERS, timeout=30)
    r.raise_for_status()
    path.write_bytes(r.content)


def main():
    gaps = find_real_gaps()
    print(f"Found {len(gaps)} filings missing their primary document.\n")

    ok, failed = [], []

    for folder, accession, primary in gaps:
        accession_nodashes = accession.replace("-", "")
        url = f"{BASE_URL}/Archives/edgar/data/{CIK_INT}/{accession_nodashes}/{primary}"
        dest = folder / primary

        try:
            download(url, dest)
            ok.append((accession, primary))
            print("OK  ", accession, primary)
        except Exception as e:
            failed.append((accession, primary, repr(e)))
            print("FAIL", accession, primary, "->", e)

        time.sleep(0.12)  # stay below SEC 10 requests/sec

    print(f"\nDone. {len(ok)} downloaded, {len(failed)} failed.")
    if failed:
        print("\nFailures:")
        for accession, primary, err in failed:
            print(" ", accession, primary, err)


if __name__ == "__main__":
    main()
