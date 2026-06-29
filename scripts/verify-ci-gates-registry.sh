#!/usr/bin/env bash
# CI gate registry integrity checker (#4213, drift extension #4220). Verifies
# that every entry in specs/ci-gates.v1.yaml references a script and workflow
# file that exists on disk. With --drift it ALSO checks hook/preflight/workflow
# registry completeness against .pre-commit-config.yaml and .github/workflows/.
# Credential-free, Docker-free, network-free.
#
# Usage:
#   scripts/verify-ci-gates-registry.sh           # integrity only
#   scripts/verify-ci-gates-registry.sh --drift   # integrity + hook/workflow drift
#
# Exit codes:
#   0 — registry is consistent with the repository.
#   1 — one or more integrity/drift errors found; details printed to stderr.
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
registry="${repo_root}/specs/ci-gates.v1.yaml"

if [[ "${1:-}" == "--drift" ]]; then
	(cd "${repo_root}/go" && go run ./cmd/ci-gates validate \
		--registry "${registry}" --repo-root "${repo_root}" --drift)
else
	(cd "${repo_root}/go" && go run ./cmd/ci-gates validate \
		--registry "${registry}" --repo-root "${repo_root}")
fi
