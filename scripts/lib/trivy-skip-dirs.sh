#!/usr/bin/env bash
# SPDX-License-Identifier: MIT
# Copyright (c) 2025-2026 eshu-hq
#
# Shared Trivy filesystem-scan skip-dirs derivation (#5925 F2).
# specs/trivy-skip-dirs.txt is the single authoritative skip-dirs list (one
# directory per line, per-directory rationale as '#' comments); this is the
# ONE place that turns it into the comma-joined value Trivy's --skip-dirs
# flag wants. scripts/dev/trivy-fs-local.sh and
# .github/workflows/security-scan.yml's trivy-fs job both source this file
# and call trivy_skip_dirs_csv instead of each carrying their own
# `grep | grep | paste` pipeline, so the two cannot silently diverge the way
# two independently-maintained copies of the same pipeline could -- see
# go/internal/cigates/AGENTS.md (checkTrivySkipDirsParity) for the check that
# keeps both sides wired to this file.
#
# Usage (source, then call):
#   source scripts/lib/trivy-skip-dirs.sh
#   skip_dirs="$(trivy_skip_dirs_csv "${repo_root}")"

# trivy_skip_dirs_csv prints specs/trivy-skip-dirs.txt's directory entries --
# one per non-blank, non-comment line -- comma-joined, in file order, to
# stdout. $1 is the repository root (the directory containing specs/).
#
# Each filter below is written to match trivySkipDirsSpecEntries exactly,
# because the two parsers disagreeing is the failure this shared derivation
# exists to prevent -- and a Go test asserts that agreement on every run by
# comparing this function's output to that parser's:
#
#   sed $'s/\r$//'          only a TRAILING CR is a line-ending artifact, which
#                           is what the Go side's strings.TrimSuffix removes. A
#                           bare `tr -d '\r'` would also delete an embedded CR
#                           the Go side keeps.
#   grep -v '^[[:space:]]*#' a '#' after leading whitespace still starts a
#                           WHOLE-LINE comment, which is what
#                           specs/trivy-skip-dirs.txt's own header promises
#                           and what the Go side's
#                           HasPrefix(TrimSpace(line), "#") implements.
#                           Anchoring at column 0 would emit an indented
#                           comment as an entry while Go dropped it. There is
#                           no trailing-comment support on either side: this
#                           filter only drops a line whose first
#                           non-whitespace character is '#', so a '#'
#                           anywhere else on an entry's line is NOT stripped
#                           here -- the Go side rejects any such entry
#                           outright at drift-check time (single-source
#                           review P2-2), so a valid specs file never reaches
#                           this filter with one.
#   grep -v '^[[:space:]]*$' whitespace-only lines are blank lines.
#
# Incidental whitespace AROUND an entry is deliberately not trimmed here: the
# Go side rejects such a line outright (#5925 F5) rather than guessing, so
# trimming it away would hide an error instead of surfacing it.
trivy_skip_dirs_csv() {
	local repo_root="$1"
	local specs_path="${repo_root}/specs/trivy-skip-dirs.txt"
	sed $'s/\r$//' <"${specs_path}" | grep -v '^[[:space:]]*#' | grep -v '^[[:space:]]*$' | paste -sd, -
}
