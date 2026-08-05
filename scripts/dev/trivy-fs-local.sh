#!/usr/bin/env bash
# Optional local Trivy filesystem scan (#4217), mirroring the security-scan.yml
# trivy-fs job (vuln + secret + config) at the HIGH,CRITICAL threshold. Trivy is
# not a required local tool, so this is intentionally a soft gate: if `trivy` is
# not installed it prints setup guidance and reports that CI remains
# authoritative — it does NOT silently pass as if the scan ran.
#
# Usage: scripts/dev/trivy-fs-local.sh
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"

if ! command -v trivy >/dev/null 2>&1; then
	printf 'trivy-fs: trivy is not installed locally — skipping the local filesystem scan.\n'
	printf 'trivy-fs: install it (https://aquasecurity.github.io/trivy) to run this gate locally;\n'
	printf 'trivy-fs: CI (.github/workflows/security-scan.yml, job "Trivy filesystem scan") remains authoritative.\n'
	exit 0
fi

printf 'trivy-fs: scanning working tree (vuln + secret + config, CRITICAL/HIGH)...\n'
# Mirror .github/workflows/security-scan.yml trivy-fs: CRITICAL,HIGH only,
# ignore-unfixed, and the same skip-dirs so local findings match CI rather
# than reporting noise CI suppresses.
#
# scripts/lib/trivy-skip-dirs.sh is the single shared derivation of the
# skip-dirs list from specs/trivy-skip-dirs.txt; both this script and the CI
# workflow invoke it, so the two sides cannot drift apart the way two
# separately-maintained derivation pipelines could. go/internal/cigates
# (checkTrivySkipDirsParity) asserts both sides are wired to it -- see
# go/internal/cigates/AGENTS.md.
# shellcheck source=scripts/lib/trivy-skip-dirs.sh
source "${repo_root}/scripts/lib/trivy-skip-dirs.sh"
skip_dirs="$(trivy_skip_dirs_csv "${repo_root}")"
exec trivy fs \
	--scanners vuln,secret,misconfig \
	--severity CRITICAL,HIGH \
	--ignore-unfixed \
	--skip-dirs "${skip_dirs}" \
	--exit-code 1 \
	"${repo_root}"
