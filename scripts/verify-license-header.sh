#!/usr/bin/env bash
#
# Verifies every .go file in the repository carries the project MIT SPDX
# header on lines 1-2, matching the repo-root LICENSE. The check is run as
# a pre-merge CI gate from .github/workflows/test.yml so a missing header
# fails PR builds rather than landing silently.
#
# Lines 1-2 must be exactly:
#   // SPDX-License-Identifier: MIT
#   // Copyright (c) 2025-2026 eshu-hq
#
# A blank line is required between the header and any subsequent content
# (build constraints, package clause) per Go convention, but that is
# enforced by gofmt/lint rather than this script. This script is a
# presence check; structure below the header is the responsibility of
# add-license-header.sh and the Go toolchain.
#
# Usage:
#   scripts/verify-license-header.sh                # scan repo at HEAD
#   ESHU_LICENSE_HEADER_REPO_ROOT=/tmp/x \
#     scripts/verify-license-header.sh              # scan a different tree
#
# Exit code 0 = all .go files carry the header. Non-zero = at least one
# missing or wrong, with the offending paths printed to stderr.

set -euo pipefail

HEADER_SPDX='// SPDX-License-Identifier: MIT'
HEADER_COPYRIGHT='// Copyright (c) 2025-2026 eshu-hq'

repo_root="${ESHU_LICENSE_HEADER_REPO_ROOT:-}"
if [ -z "${repo_root}" ]; then
  repo_root="$(git -C "$(dirname "$0")" rev-parse --show-toplevel 2>/dev/null \
    || (cd "$(dirname "$0")/.." && pwd))"
fi
cd "${repo_root}"

if ! command -v rg >/dev/null 2>&1; then
  printf 'verify-license-header: rg not available in PATH\n' >&2
  exit 1
fi

missing=0
checked=0
missing_files=()

# AGENTS.md mandates rg --files / globbing for file discovery and forbids
# find. Use rg --files with negative globs for the same exclusions the
# previous find-based walker applied (the .git worktree link and vendor/).
while IFS= read -r -d '' f; do
  checked=$((checked + 1))
  # Read first two lines via bash builtins (no per-file fork).
  line1=''
  line2=''
  {
    IFS= read -r line1
    IFS= read -r line2
  } < "${f}" 2>/dev/null || true
  if [ "${line1}" != "${HEADER_SPDX}" ] || [ "${line2}" != "${HEADER_COPYRIGHT}" ]; then
    missing=$((missing + 1))
    missing_files+=("${f}")
  fi
done < <(rg --files -g '*.go' -g '!.git' -g '!vendor' -0 . 2>/dev/null)

if [ "${missing}" -ne 0 ]; then
  {
    printf 'verify-license-header: %d of %d .go files missing or wrong SPDX header\n' \
      "${missing}" "${checked}"
    printf '\nFirst 20 offenders:\n'
    for f in "${missing_files[@]:0:20}"; do
      printf '  %s\n' "${f}"
    done
    if [ "${missing}" -gt 20 ]; then
      printf '  ... and %d more\n' "$((missing - 20))"
    fi
    printf '\nRun scripts/add-license-header.sh inside a worktree to repair.\n'
    printf 'Required header (lines 1-2 of every .go file):\n'
    printf '  %s\n' "${HEADER_SPDX}"
    printf '  %s\n' "${HEADER_COPYRIGHT}"
  } >&2
  exit 1
fi

printf 'verify-license-header: all %d .go files carry the SPDX header\n' "${checked}"
