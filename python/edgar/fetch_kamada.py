import json
import os
import time
from datetime import datetime
from pathlib import Path

import requests

from build_meta import write_meta

CIK = "0001567529"
CIK_INT = str(int(CIK))
BASE = "https://www.sec.gov"

HEADERS = {
	"User-Agent": "MyResearchApp your-email@example.com"
}

SCRIPT_DIR = os.path.dirname(os.path.abspath(__file__))
PROJECT_ROOT = os.path.abspath(os.path.join(SCRIPT_DIR, "..", ".."))
OUT = os.path.join(PROJECT_ROOT, "fileDB", "companies", CIK)
os.makedirs(OUT, exist_ok=True)


def get_json(url):
	r = requests.get(url, headers=HEADERS)
	r.raise_for_status()
	return r.json()


def download(url, path):
	r = requests.get(url, headers=HEADERS)
	r.raise_for_status()

	parent = os.path.dirname(path)
	if parent:
		os.makedirs(parent, exist_ok=True)

	with open(path, "wb") as f:
		f.write(r.content)

	time.sleep(0.12)  # stay below SEC 10 requests/sec


def filing_archive_url(accession):
	accession_nodashes = accession.replace("-", "")
	return f"{BASE}/Archives/edgar/data/{CIK_INT}/{accession_nodashes}"


def download_filing_files(filing):
	accession = filing["accessionNumber"]
	year = filing["filingDate"][:4]
	archive_url = filing_archive_url(accession)
	local_dir = os.path.join(OUT, year, accession)
	os.makedirs(local_dir, exist_ok=True)

	with open(os.path.join(local_dir, "filing.json"), "w") as f:
		json.dump(filing, f, indent=2)

	index = get_json(f"{archive_url}/index.json")
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
			download(f"{archive_url}/{filename}", dest)
			downloaded += 1
		except Exception as e:
			print("ERROR", accession, filename, e)

	write_meta(Path(local_dir))
	return downloaded


# ------------------------------------------------
# 1. Get Kamada's filing history
# ------------------------------------------------

submissions_url = f"https://data.sec.gov/submissions/CIK{CIK}.json"
data = get_json(submissions_url)

with open(os.path.join(OUT, "submissions.json"), "w") as f:
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
# 3. Keep last 10 years
# ------------------------------------------------

cutoff = datetime.now().year - 10
filings = [
	f for f in filings
	if f["filingDate"] and int(f["filingDate"][:4]) >= cutoff
]

# ------------------------------------------------
# 4. Download all files for each filing
# ------------------------------------------------

for filing in filings:
	accession = filing["accessionNumber"]
	try:
		count = download_filing_files(filing)
		print("Downloaded", accession, f"({count} files)")
	except Exception as e:
		print("ERROR", accession, e)
