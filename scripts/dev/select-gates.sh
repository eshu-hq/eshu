#!/usr/bin/env bash
# Thin wrapper: select CI gates for the changed paths in this working tree.
# Delegates to `go run ./cmd/ci-gates select` from the go/ directory.
#
# Usage: scripts/dev/select-gates.sh [--base <ref>] [--tier <tier>]
#                                     [--paths-from <file|->]
#                                     [--explain] [--json]
#
# All flags are passed through to ci-gates select unchanged. See
# docs/public/reference/local-testing.md for the full guide.
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
registry="${repo_root}/specs/ci-gates.v1.yaml"

exec go -C "${repo_root}/go" run ./cmd/ci-gates select \
	--registry "${registry}" \
	"$@"
