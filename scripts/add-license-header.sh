#!/usr/bin/env bash
#
# Adds the project MIT SPDX header to every .go file in the repository
# that does not already have it. Idempotent: re-running produces no change
# to files that already carry the header.
#
# The generator is deliberately conservative: it never edits files outside
# the repo root and never modifies a file that already has the exact two
# header lines. It runs from the repository root detected via
# `git rev-parse --show-toplevel`, so it must be invoked inside a worktree
# rather than the main checkout per AGENTS.md.
#
# Header (inserted on lines 1-2, followed by a blank line):
#   // SPDX-License-Identifier: MIT
#   // Copyright (c) 2025-2026 eshu-hq
#
# Files with a build constraint directive (//go:build or // +build) keep
# the constraint intact; the header is prepended above it with a blank
# line separator so Go still recognises the constraint as contiguous at
# the top of the file (modulo the SPDX header).
#
# Usage:
#   scripts/add-license-header.sh                # scan repo at HEAD
#   ESHU_LICENSE_HEADER_REPO_ROOT=/tmp/x \
#     scripts/add-license-header.sh              # scan a different tree
#
# Exit code is always 0 when run successfully; the summary line tells the
# caller how many files were added, updated, or already compliant.

set -euo pipefail

HEADER_SPDX='// SPDX-License-Identifier: MIT'
HEADER_COPYRIGHT='// Copyright (c) 2025-2026 eshu-hq'

repo_root="${ESHU_LICENSE_HEADER_REPO_ROOT:-}"
if [ -z "${repo_root}" ]; then
  repo_root="$(git -C "$(dirname "$0")" rev-parse --show-toplevel 2>/dev/null \
    || (cd "$(dirname "$0")/.." && pwd))"
fi
cd "${repo_root}"

# AGENTS.md requires this script run inside a worktree, not the main checkout.
# In a git worktree, the `.git` entry at the worktree root is a regular file
# that points at `<main-repo>/.git/worktrees/<name>`. In the main checkout it
# is a directory. The safeguard applies only when running in default mode;
# the test mirror passes ESHU_LICENSE_HEADER_REPO_ROOT explicitly and is
# exempt so it can exercise the generator on synthetic scratch trees.
if [ -z "${ESHU_LICENSE_HEADER_REPO_ROOT:-}" ] && [ ! -f .git ]; then
  {
    printf 'add-license-header: this script must run inside a git worktree.\n'
    printf 'Per AGENTS.md, do not run it against the main checkout.\n'
    printf 'Create one with: git worktree add -b <branch> ../<repo>-wt/<branch> origin/main\n'
  } >&2
  exit 1
fi

added=0
updated=0
skipped=0
total=0

# AGENTS.md mandates rg --files for file discovery.
while IFS= read -r -d '' f; do
  total=$((total + 1))

  if [ ! -f "${f}" ] || [ ! -w "${f}" ]; then
    skipped=$((skipped + 1))
    continue
  fi

  line1=''
  line2=''
  {
    IFS= read -r line1
    IFS= read -r line2
  } < "${f}" 2>/dev/null || true

  if [ "${line1}" = "${HEADER_SPDX}" ] && [ "${line2}" = "${HEADER_COPYRIGHT}" ]; then
    skipped=$((skipped + 1))
    continue
  fi

  # Strip any pre-existing license header (SPDX, Copyright, "Licensed
  # under", plus the blank line that follows) before prepending the
  # canonical MIT block. Without this step, a stale SPDX-License-
  # Identifier for a non-MIT license stays in the file below the new
  # MIT header, defeating the verifier's presence check.
  stripped_body="$(mktemp -t eshu-license-strip.XXXXXX)"
  trap 'rm -f "${stripped_body}" 2>/dev/null || true' EXIT
  awk '
    BEGIN { in_license = 1 }
    in_license && /^\/\/ SPDX-License-Identifier:/ { next }
    in_license && /^\/\/ Copyright/ { next }
    in_license && /^\/\/ Licensed under/ { next }
    in_license && /^[[:space:]]*$/ { next }
    { in_license = 0; print }
  ' "${f}" > "${stripped_body}"

  tmp="$(mktemp -t eshu-license-header.XXXXXX)"
  trap 'rm -f "${stripped_body}" "${tmp}" 2>/dev/null || true' EXIT

  {
    printf '%s\n' "${HEADER_SPDX}"
    printf '%s\n' "${HEADER_COPYRIGHT}"
    printf '\n'
    if [ -s "${stripped_body}" ]; then
      cat "${stripped_body}"
    fi
  } > "${tmp}"

  mv "${tmp}" "${f}"
  trap - EXIT

  case "${line1}" in
    "// SPDX-License-Identifier:"*|"// Copyright"*|"// Licensed under"*)
      updated=$((updated + 1))
      ;;
    *)
      added=$((added + 1))
      ;;
  esac
done < <(rg --files -g '*.go' -g '!.git' -g '!vendor' -0 . 2>/dev/null)

printf 'add-license-header: %d .go files scanned, %d added, %d updated, %d already had header\n' \
  "${total}" "${added}" "${updated}" "${skipped}"
