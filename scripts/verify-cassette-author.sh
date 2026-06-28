#!/usr/bin/env bash
# Fast, offline cassette author-time gate.
#
# Validates every committed cassette under testdata/cassettes against the v1
# cassette-format contract (required fields, schema version, types, and
# additionalProperties:false typo rejection) and exercises the structural and
# unknown-field negative cases. No Docker, no graph, no network — finishes in
# seconds, so a contributor can run it before pushing a hand-authored or
# recorded cassette.
set -euo pipefail

repo_root="$(git -C "$(dirname "$0")" rev-parse --show-toplevel 2>/dev/null \
  || (cd "$(dirname "$0")/.." && pwd))"
go_dir="${repo_root}/go"

cd "$go_dir"
export GOCACHE="${GOCACHE:-${go_dir}/.gocache}"

go test ./internal/replay/schema -count=1 \
  -run 'TestCommittedCassettesValid|TestValidateCassetteBytes'
