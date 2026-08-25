#!/usr/bin/env bash
# Start the jump-starter dev server from repo root (CGO required for SQLite/PDF).
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

if [[ ! -f .env ]]; then
  echo "Missing .env in $ROOT" >&2
  echo "  cp .env.example .env   # set PORT, GEMINI_API_KEY, SESSION_SECRET" >&2
  exit 1
fi

export CGO_ENABLED=1

PORT="$(grep -E '^PORT=' .env 2>/dev/null | head -1 | cut -d= -f2- | sed 's/#.*//' | tr -d ' "')"
if [[ -z "$PORT" ]]; then
  echo "Missing PORT in .env (required)" >&2
  exit 1
fi
VERSION_VAL="dev"
if [[ -f VERSION ]]; then
  VERSION_VAL="$(tr -d ' \n\r\t' < VERSION)"
fi
LDFLAGS="-X jump-starter/internal/version.LinkVersion=${VERSION_VAL}"

echo "jump-starter dev server → http://localhost:${PORT}"
echo "  repo: $ROOT"
echo "  stop: Ctrl+C"

exec go run -ldflags "$LDFLAGS" ./cmd/server
