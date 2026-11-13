#!/usr/bin/env bash
# Build dict.db from the verified ECDICT and Tatoeba sources.
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

OUT="${OUT:-/out/dict.db}"
mkdir -p "$(dirname "$OUT")"

bash scripts/fetch-data.sh
go run ./cmd/dictbuild \
  -ecdict data-src/ecdict.csv \
  -sentences data-src/cmn-eng.zip \
  -out "$OUT"
