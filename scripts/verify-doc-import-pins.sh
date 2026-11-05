#!/usr/bin/env bash
# verify-doc-import-pins.sh - fail when a document-import tesseract pin file
# is malformed (#10205 P2: PR builds prove only the amd64 digest, so a static
# guard must fail fast on a malformed arm64 pin file). Scope is structure
# only: digest correctness is proven by the main/tag dual-arch image build,
# which checksums both files; this guard proves both files are well-formed
# portable records before that build runs.
#
# What it checks, for tesseract-5.5.0.<arch>.sha256 in the pin directory:
#   1. Both arch files (amd64, arm64) exist and are readable.
#   2. Every line is a portable `sha256sum -c` record: 64 lowercase hex
#      digits, two spaces, then the file path (BusyBox sha256sum -c takes
#      only this shape, which is all the pin files contain).
#   3. Each file holds exactly EXPECTED_LINES records. The count moves only
#      with a reviewed re-pin that changes the file set; a silent drift
#      fails here instead of passing an incomplete check into the build.
#
# Usage:
#   scripts/verify-doc-import-pins.sh [--pins-dir PATH]
#
# Exit codes:
#   0 - both pin files are well-formed.
#   1 - a pin file has a malformed line or the wrong record count. The
#       failure names the file and the offending line.
#   2 - fail closed: bad usage, an unreadable pin directory, or a missing
#       arch file. A missing file never passes and never reports corruption.
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
pins_dir="${repo_root}/specs/doc-import-pins"
expected_lines=17

die() {
  printf 'verify-doc-import-pins: %s\n' "$1" >&2
  exit "$2"
}

while (($# > 0)); do
  case "$1" in
    --pins-dir)
      (($# >= 2)) || die "--pins-dir needs a name" 2
      pins_dir="$2"
      shift 2
      ;;
    -h | --help)
      sed -n '2,22p' "$0"
      exit 0
      ;;
    *)
      die "unknown option: $1" 2
      ;;
  esac
done

[[ -d "${pins_dir}" ]] || die "pin directory '${pins_dir}' is not a directory" 2

for arch in amd64 arm64; do
  pin="${pins_dir}/tesseract-5.5.0.${arch}.sha256"
  [[ -r "${pin}" ]] || die "missing pin file '${pin}'" 2
  count=0
  while IFS= read -r line || [[ -n "${line}" ]]; do
    count=$((count + 1))
    if [[ ! "${line}" =~ ^[0-9a-f]{64}"  "[^[:space:]]+$ ]]; then
      die "'${pin}' line ${count} is not a portable sha256sum record: ${line}" 1
    fi
  done <"${pin}"
  if ((count != expected_lines)); then
    die "'${pin}' holds ${count} records, expected ${expected_lines}" 1
  fi
done

printf 'verify-doc-import-pins: OK: amd64 and arm64 pin files are well-formed (%d records each)\n' "${expected_lines}"
