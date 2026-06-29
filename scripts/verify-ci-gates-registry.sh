#!/usr/bin/env bash
# CI gate registry integrity checker (#4213). Verifies that every entry in
# specs/ci-gates.v1.yaml references a script and workflow file that exists on
# disk. Credential-free, Docker-free, network-free.
#
# Usage:
#   scripts/verify-ci-gates-registry.sh
#
# Exit codes:
#   0 — registry is consistent with the repository.
#   1 — one or more integrity errors found; details printed to stderr.
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
registry="${repo_root}/specs/ci-gates.v1.yaml"

(cd "${repo_root}/go" && go run ./cmd/ci-gates validate \
	--registry "${registry}" \
	--repo-root "${repo_root}")
