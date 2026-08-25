#!/usr/bin/env python3
import argparse
import sys
from pathlib import Path

from build_meta import write_meta

SCRIPT_DIR = Path(__file__).resolve().parent
PROJECT_ROOT = SCRIPT_DIR.parent.parent
DEFAULT_COMPANY_DIR = PROJECT_ROOT / "fileDB" / "companies" / "0001567529"


def find_filing_dirs(company_dir: Path) -> list[Path]:
	dirs = []
	for filing_json in sorted(company_dir.rglob("filing.json")):
		dirs.append(filing_json.parent)
	return dirs


def main() -> int:
	parser = argparse.ArgumentParser(description="Build meta.json for downloaded SEC filing folders")
	parser.add_argument(
		"company_dir",
		nargs="?",
		default=str(DEFAULT_COMPANY_DIR),
		help="Company directory under fileDB/companies (default: Kamada CIK 0001567529)",
	)
	parser.add_argument(
		"--all",
		action="store_true",
		help="Rebuild meta.json even when it already exists",
	)
	args = parser.parse_args()

	company_dir = Path(args.company_dir)
	if not company_dir.is_dir():
		print(f"ERROR: not a directory: {company_dir}", file=sys.stderr)
		return 1

	filing_dirs = find_filing_dirs(company_dir)
	if not filing_dirs:
		print(f"No filing folders found under {company_dir}")
		return 0

	created = 0
	skipped = 0
	errors = 0

	for folder in filing_dirs:
		meta_path = folder / "meta.json"
		if meta_path.exists() and not args.all:
			skipped += 1
			continue
		try:
			meta = write_meta(folder)
			created += 1
			print(f"{meta['filingDate']} {meta['form']:16} tier={meta['tier']} {folder.name}")
		except Exception as exc:
			errors += 1
			print(f"ERROR {folder.name}: {exc}", file=sys.stderr)

	print(f"\nDone: {created} written, {skipped} skipped, {errors} errors")
	return 1 if errors else 0


if __name__ == "__main__":
	raise SystemExit(main())
