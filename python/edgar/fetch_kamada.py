#!/usr/bin/env python3
"""Fetch SEC filings for one CIK into fileDB/companies/{cik}/.

Originally hardcoded to Kamada (CIK 0001567529); generalized via --cik/--years
(prompt 13, LLD §4.1) so cmd/fetch-similar can shell out to it per peer.
Zero-arg invocation still fetches Kamada with the original 10-year depth, so
nothing that already calls this script with no arguments changes behavior.
"""

import argparse
import json
import os
import time
from datetime import datetime
from pathlib import Path

import requests

from build_meta import write_meta

BASE = "https://www.sec.gov"

SCRIPT_DIR = os.path.dirname(os.path.abspath(__file__))
PROJECT_ROOT = os.path.abspath(os.path.join(SCRIPT_DIR, "..", ".."))

DEFAULT_CIK = "0001567529"


def parse_args():
	p = argparse.ArgumentParser(description="Fetch SEC filings for one CIK")
	p.add_argument("--cik", default=DEFAULT_CIK,
		help="10-digit (or shorter) SEC CIK (default: Kamada, %(default)s)")
	p.add_argument("--years", type=int, default=10,
		help="keep filings from the last N calendar years (default: %(default)s)")
	p.add_argument("--root", default=os.environ.get("FILEDB_DIR", os.path.join(PROJECT_ROOT, "fileDB")),
		help="fileDB root containing companies/ (default: $FILEDB_DIR or ./fileDB)")
	p.add_argument("--user-agent", default=os.environ.get("SEC_EDGAR_USER_AGENT", "MyResearchApp your-email@example.com"),
		help="SEC requires a contact User-Agent on every request")
	return p.parse_args()


def get_json(url, headers):
	r = requests.get(url, headers=headers)
	r.raise_for_status()
	return r.json()


def download(url, path, headers):
	r = requests.get(url, headers=headers)
	r.raise_for_status()

	parent = os.path.dirname(path)
	if parent:
		os.makedirs(parent, exist_ok=True)

	with open(path, "wb") as f:
		f.write(r.content)

	time.sleep(0.12)  # stay below SEC 10 requests/sec


def filing_archive_url(cik_int, accession):
	accession_nodashes = accession.replace("-", "")
	return f"{BASE}/Archives/edgar/data/{cik_int}/{accession_nodashes}"


def download_filing_files(filing, cik_int, out_dir, headers):
	accession = filing["accessionNumber"]
	year = filing["filingDate"][:4]
	archive_url = filing_archive_url(cik_int, accession)
	local_dir = os.path.join(out_dir, year, accession)
	os.makedirs(local_dir, exist_ok=True)

	with open(os.path.join(local_dir, "filing.json"), "w") as f:
		json.dump(filing, f, indent=2)

	index = get_json(f"{archive_url}/index.json", headers)
	with open(os.path.join(local_dir, "index.json"), "w") as f:
		json.dump(index, f, indent=2)

	items = index.get("directory", {}).get("item", [])
	if isinstance(items, dict):
		items = [items]

	downloaded = 0
	for item in items:
		filename = item.get("name")
		if not filename:
			continue

		dest = os.path.join(local_dir, filename)
		if os.path.exists(dest):
			continue

		try:
			download(f"{archive_url}/{filename}", dest, headers)
			downloaded += 1
		except Exception as e:
			print("ERROR", accession, filename, e)

	write_meta(Path(local_dir))
	return downloaded


def main():
	args = parse_args()

	cik = str(args.cik).strip().zfill(10)
	cik_int = str(int(cik))
	headers = {"User-Agent": args.user_agent}
	out_dir = os.path.join(args.root, "companies", cik)
	os.makedirs(out_dir, exist_ok=True)

	# ------------------------------------------------
	# 1. Get the filing history
	# ------------------------------------------------

	submissions_url = f"https://data.sec.gov/submissions/CIK{cik}.json"
	data = get_json(submissions_url, headers)

	with open(os.path.join(out_dir, "submissions.json"), "w") as f:
		json.dump(data, f, indent=2)

	# ------------------------------------------------
	# 2. Build list of filings
	# ------------------------------------------------

	recent = data["filings"]["recent"]
	filings = [
		{key: recent[key][i] for key in recent}
		for i in range(len(recent["accessionNumber"]))
	]

	# ------------------------------------------------
	# 3. Keep last N years
	# ------------------------------------------------

	cutoff = datetime.now().year - args.years
	filings = [
		f for f in filings
		if f["filingDate"] and int(f["filingDate"][:4]) >= cutoff
	]

	# ------------------------------------------------
	# 4. Download all files for each filing
	# ------------------------------------------------

	errors = 0
	for filing in filings:
		accession = filing["accessionNumber"]
		try:
			count = download_filing_files(filing, cik_int, out_dir, headers)
			print("Downloaded", accession, f"({count} files)")
		except Exception as e:
			print("ERROR", accession, e)
			errors += 1

	print(f"\nDone: {len(filings)} filings in window, {errors} errors")
	return 1 if errors else 0


if __name__ == "__main__":
	raise SystemExit(main())
