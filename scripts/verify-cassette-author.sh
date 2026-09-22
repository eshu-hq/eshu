#!/usr/bin/env bash
# Fast, offline cassette author-time gate.
#
# Two checks over every committed cassette under testdata/cassettes, no
# Docker, no graph, no network, so a contributor can run it before pushing a
# hand-authored or recorded cassette:
#
#   1. A private-data scan (#6965 Phase 2): IP literals, 12-digit account ids,
#      ARNs, hostnames and organisation identifiers, with a fail-closed
#      allowlist of documentation values. The pattern, its allowlist and the
#      controls that prove them live in scripts/lib/cassette_private_data_pattern.sh.
#      A finding names file:line:alternative and never the value.
#   2. The v1 cassette-format contract (required fields, schema version, types,
#      and additionalProperties:false typo rejection), plus the structural and
#      unknown-field negative cases, through the focused go test.
set -euo pipefail

repo_root="$(git -C "$(dirname "$0")" rev-parse --show-toplevel 2>/dev/null \
  || (cd "$(dirname "$0")/.." && pwd))"
go_dir="${repo_root}/go"
private_data_lib="${repo_root}/scripts/lib/cassette_private_data_pattern.sh"

fail() { printf 'verify-cassette-author: %s\n' "$*" >&2; exit 1; }

# The scan uses namerefs and associative arrays (bash >= 4.3). macOS ships
# 3.2, which fails on `local -n` in a way that is easy to misread as a scan
# result, so the precondition is stated up front.
[[ "${BASH_VERSINFO[0]}" -gt 4 || ("${BASH_VERSINFO[0]}" -eq 4 && "${BASH_VERSINFO[1]}" -ge 3) ]] \
  || fail "requires bash >= 4.3 (found ${BASH_VERSION}); on macOS run it under Homebrew bash"

# shellcheck source=scripts/lib/cassette_private_data_pattern.sh
source "${private_data_lib}"
cassette_private_data_scan "${repo_root}/testdata/cassettes"

cd "$go_dir"
export GOCACHE="${GOCACHE:-${go_dir}/.gocache}"

go test ./internal/replay/schema -count=1 \
  -run 'TestCommittedCassettesValid|TestValidateCassetteBytes'
