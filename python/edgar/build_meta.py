import json
import re
import xml.etree.ElementTree as ET
from html import unescape
from pathlib import Path

TIER_LABELS = {1: "MAJOR", 2: "MODERATE", 3: "MINOR", 4: "ROUTINE"}


def _strip_html(text: str) -> str:
	text = re.sub(r"<[^>]+>", " ", text)
	text = unescape(text)
	return re.sub(r"\s+", " ", text).strip()


def _xml_text(root: ET.Element, tag: str) -> str | None:
	for elem in root.iter():
		if not elem.tag.endswith(tag):
			continue
		for child in elem:
			if child.tag.endswith("value") and child.text:
				return child.text.strip()
		if elem.text and elem.text.strip():
			return elem.text.strip()
	return None


def _load_filing(folder: Path) -> dict:
	path = folder / "filing.json"
	if not path.exists():
		raise FileNotFoundError(f"missing filing.json in {folder}")
	return json.loads(path.read_text())


def _extract_descriptions(path: Path) -> list[str]:
	content = path.read_text(errors="ignore")
	descriptions = []
	for match in re.finditer(r"<DESCRIPTION>([^<\n]+)", content, re.I):
		desc = match.group(1).strip()
		if desc and desc not in descriptions:
			descriptions.append(desc)
	return descriptions


def _document_type(path: Path) -> str | None:
	# As-filed documents are served with their original SGML wrapper
	# (<DOCUMENT>/<TYPE>/<FILENAME>/<DESCRIPTION>/<TEXT>) still intact, so the
	# real EDGAR document type is a tag inside the file itself, not something
	# encoded in the filename. Filenames are chosen by whichever filing agent
	# assembled the submission and vary freely between them and across time.
	head = path.read_text(errors="ignore")[:2000]
	match = re.search(r"<TYPE>([^\n\r<]+)", head, re.I)
	return match.group(1).strip() if match else None


def _extract_exhibit_titles(folder: Path) -> list[str]:
	titles = []
	for path in sorted(folder.glob("*.htm")) + sorted(folder.glob("*.html")):
		if path.name.endswith("-index.html"):
			continue
		doc_type = _document_type(path)
		if doc_type and doc_type.upper().startswith("EX-99"):
			titles.extend(_extract_descriptions(path))
	return titles


def _extract_body_snippet(folder: Path, filing: dict) -> str:
	primary = filing.get("primaryDocument")
	candidates = []
	if primary:
		candidates.append(folder / primary)
	candidates.extend(sorted(folder.glob("*.htm")))
	candidates.extend(sorted(folder.glob("*.html")))

	for path in candidates:
		if not path.exists() or path.name.endswith("-index.html"):
			continue
		content = path.read_text(errors="ignore")
		text = _strip_html(content)
		if len(text) > 80:
			return text[:2000].lower()
	return ""


def _find_ownership_xml(folder: Path) -> Path | None:
	for path in sorted(folder.rglob("*.xml")):
		if path.name in ("ownership.xml", "primary_doc.xml") or "ownership" in path.name:
			return path
	return next(iter(folder.rglob("*.xml")), None)


def _parse_ownership(folder: Path) -> dict:
	xml_path = _find_ownership_xml(folder)
	if not xml_path:
		return {}

	try:
		root = ET.parse(xml_path).getroot()
	except ET.ParseError:
		return {}

	person = _xml_text(root, "rptOwnerName") or _xml_text(root, "reportingPersonName")
	title = _xml_text(root, "officerTitle")
	txn_code = _xml_text(root, "transactionAcquiredDisposedCode") or _xml_text(root, "transactionCode")
	shares = _xml_text(root, "transactionShares") or _xml_text(root, "sharesOwnedFollowingTransaction")
	txn_date = _xml_text(root, "transactionDate") or _xml_text(root, "periodOfReport")

	return {
		"person": person,
		"title": title,
		"transactionCode": txn_code,
		"shares": shares,
		"transactionDate": txn_date,
	}


def _classify_6k(titles: list[str], body: str, is_xbrl: bool) -> tuple[int, str, list[str]]:
	text = " ".join(titles).lower() + " " + body
	tags = []

	if is_xbrl or ("financial results" in text and "announce" not in text and "to announce" not in text):
		if "to announce" in text or "will announce" in text:
			return 3, "earnings_preview", tags
		return 1, "quarterly_results", ["financials"]

	if "to announce" in text and "financial results" in text:
		return 3, "earnings_preview", ["financials"]

	if any(k in text for k in ("annual guidance", "guidance of $", "revenue", "ebitda")) and "reports" not in text:
		if "guidance" in text:
			tags.append("guidance")
			return 1, "annual_guidance", tags

	if "dividend" in text:
		tags.append("dividend")
		if "withholding tax" in text or "payment date has been changed" in text:
			return 2, "admin_update" if "changed" in text else "corporate_action", tags
		return 2, "corporate_action", tags

	if any(k in text for k in ("annual general meeting", "proxy statement", "agm")):
		tags.append("governance")
		if "held its annual general meeting" in text or "shareholders approved" in text:
			return 2, "governance", tags
		return 2, "governance", tags

	if any(k in text for k in ("fda approval", "fda approved")):
		tags.append("regulatory")
		return 1, "regulatory_clinical", tags

	if any(k in text for k in ("agreement", "million sales", "supply", "partnership", "acquisition", "merger")):
		tags.append("business")
		return 1, "business_deal", tags

	if any(k in text for k in ("conference", "presented at", "investor", "deck")):
		return 3, "investor_relations", tags

	if titles:
		return 2, "current_report", tags

	return 2, "current_report", tags


def _classify(form: str, titles: list[str], body: str, is_xbrl: bool, ownership: dict) -> tuple[int, str, list[str]]:
	form = form.upper().replace("SCHEDULE ", "")

	if form == "20-F":
		return 1, "annual_report", ["financials", "xbrl" if is_xbrl else "annual"]

	if form == "13G/A" or form == "13G":
		return 4, "ownership_disclosure", ["ownership"]

	if form == "3":
		return 3, "insider_initial", ["insider"]

	if form == "4":
		return 3, "insider_trade", ["insider"]

	if form == "6-K":
		return _classify_6k(titles, body, is_xbrl)

	return 3, "unknown", []


def _build_summary(form: str, category: str, titles: list[str], body: str, ownership: dict) -> str:
	if titles:
		summary = titles[0]
		if category == "quarterly_results" and len(titles) > 1:
			for title in titles:
				if "financial results" in title.lower():
					summary = title
					break
		return summary[:240]

	if ownership.get("person"):
		person = ownership["person"]
		role = ownership.get("title") or "insider"
		if form == "3":
			return f"Initial insider ownership report: {person} ({role})"
		if form == "4":
			code = ownership.get("transactionCode") or "?"
			shares = ownership.get("shares") or "?"
			return f"Insider transaction: {person} ({role}) — code {code}, {shares} shares"

	if form == "20-F":
		return "Annual report with audited financials and MD&A"

	if form in ("13G/A", "13G"):
		person = ownership.get("person")
		if person:
			return f"Large shareholder ownership update: {person}"
		return "Large shareholder ownership update"

	if category == "governance" and "shareholders approved" in body:
		return "Annual general meeting results — shareholder proposals approved"

	if category == "admin_update" and "payment date has been changed" in body:
		return "Dividend payment date change"

	return form


def build_meta(folder: Path | str) -> dict:
	folder = Path(folder)
	filing = _load_filing(folder)

	form = filing.get("form", "")
	is_xbrl = bool(filing.get("isXBRL") or filing.get("isInlineXBRL"))
	titles = _extract_exhibit_titles(folder)
	body = _extract_body_snippet(folder, filing)
	ownership = _parse_ownership(folder)

	tier, category, tags = _classify(form, titles, body, is_xbrl, ownership)
	summary = _build_summary(form, category, titles, body, ownership)

	file_count = sum(1 for p in folder.iterdir() if p.is_file())
	exhibit_count = len(titles)

	return {
		"accession": filing.get("accessionNumber", folder.name),
		"form": form,
		"filingDate": filing.get("filingDate"),
		"reportDate": filing.get("reportDate") or None,
		"tier": tier,
		"tierLabel": TIER_LABELS[tier],
		"category": category,
		"summary": summary,
		"signals": {
			"isXBRL": is_xbrl,
			"hasFinancials": category in ("annual_report", "quarterly_results"),
			"fileCount": file_count,
			"exhibitCount": exhibit_count,
		},
		"tags": tags,
	}


def write_meta(folder: Path | str) -> dict:
	folder = Path(folder)
	meta = build_meta(folder)
	path = folder / "meta.json"
	path.write_text(json.dumps(meta, indent=2) + "\n")
	return meta
