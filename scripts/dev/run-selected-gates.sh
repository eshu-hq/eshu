#!/usr/bin/env bash
# Thin wrapper: run the CI gates that match the changed paths in this working tree.
# Delegates to `go run ./cmd/ci-gates run` from the go/ directory.
#
# Usage: scripts/dev/run-selected-gates.sh [--base <ref>] [--tier <tier>]
#                                           [--paths-from <file|->]
#
# All flags are passed through to ci-gates run unchanged. See
# docs/public/reference/local-testing.md for the full guide.
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
registry="${repo_root}/specs/ci-gates.v1.yaml"

exec go -C "${repo_root}/go" run ./cmd/ci-gates run \
	--registry "${registry}" \
	"$@"
