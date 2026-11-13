#!/usr/bin/env bash
# Download and verify the third-party dictionary sources into data-src/.
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"
mkdir -p data-src

ECDICT_URL="https://raw.githubusercontent.com/skywind3000/ECDICT/9c46923fb81a2eb6c264a3a1689562ea7bc56b6a/ecdict.csv"
ECDICT_SHA="d1ecbdf9ee2b920afb6f430ca6b6bb9f559588cf0dff86e04bd255f9390843cc"
SENTENCES_URL="https://www.manythings.org/anki/cmn-eng.zip"
SENTENCES_SHA="d606361aa7312dcdfb15ef4fc51cb1e7ed8bc2dc608a7b192626cd36e8403106"

sha256_of() {
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "$1" | awk '{print $1}'
  elif command -v shasum >/dev/null 2>&1; then
    shasum -a 256 "$1" | awk '{print $1}'
  else
    echo "need sha256sum or shasum to verify downloads" >&2
    return 1
  fi
}

fetch() {
  local name="$1" url="$2" sum="$3"
  local dest="data-src/$name"

  if [[ -f "$dest" ]] && [[ "$(sha256_of "$dest")" == "$sum" ]]; then
    echo "reuse $dest"
    return 0
  fi

  echo "download $url"
  curl -fsSL "$url" -o "$dest.tmp"
  if [[ "$(sha256_of "$dest.tmp")" != "$sum" ]]; then
    rm -f "$dest.tmp"
    echo "checksum mismatch for $url" >&2
    exit 1
  fi
  mv "$dest.tmp" "$dest"
  echo "saved $dest"
}

fetch ecdict.csv "$ECDICT_URL" "$ECDICT_SHA"
fetch cmn-eng.zip "$SENTENCES_URL" "$SENTENCES_SHA"
