#!/usr/bin/env bash
# Security preflight (#4217): run the credential-free security gates the changed
# paths select, via the shared gate registry — the local mirror of the
# credential-free security-scan.yml jobs (whole-module gosec, govulncheck,
# nancy, and the optional Trivy filesystem scan). Run it before pushing a
# dependency or deploy change so those failures surface locally instead of on CI.
#
# Usage: scripts/dev/security-preflight.sh [--base <ref>]
#
# Tool installs (gosec/govulncheck/nancy) are handled by precommit-go.sh and
# cached under .git/eshu-precommit. govulncheck/nancy need network for their
# advisory databases. Trivy is optional: if absent, the trivy-fs gate prints
# setup guidance and defers to CI rather than passing silently.
#
# CI remains authoritative for SARIF uploads, the Trivy image scan, and
# release/package security checks — those stay CI-only in the registry.
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"

base="origin/main"
if [[ "${1:-}" == "--base" && -n "${2:-}" ]]; then
	base="$2"
fi

exec bash "${repo_root}/scripts/dev/run-selected-gates.sh" \
	--base "${base}" --tier pre-push --category security
