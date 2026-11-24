#!/usr/bin/env bash
# Queue-contract coverage ledger checker (#11354). Verifies every row of
# specs/queue-contract-coverage.v1.yaml names an existing proving Test func
# in an existing file with a matching build tag, every covered SQL symbol
# still exists in go/, runners and classes come from the closed allowlists,
# live claim/lease rows use live-Postgres runners, and every row maps to at
# least one covered symbol.
#
# Credential-free, Docker-free, network-free. REPO_ROOT overrides the
# repository root so the seeded RED/GREEN test can point it at a fixture
# tree; LEDGER_PATH overrides the ledger location for the same reason.
set -euo pipefail

repo_root="${REPO_ROOT:-$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)}"
ledger="${LEDGER_PATH:-${repo_root}/specs/queue-contract-coverage.v1.yaml}"

fail() {
	printf 'verify-queue-contract-coverage: %s\n' "$*" >&2
	exit 1
}

[[ -f "${ledger}" ]] || fail "ledger not found: ${ledger}"

# The check lives in scripts/lib as a file, not a bash heredoc: the
# heredoc-budget gate fails new scripts with heredoc bodies over 512
# bytes, and this body is ~11 KB. The helper resolves against this
# script's own location, not REPO_ROOT, so fixture trees (which override
# REPO_ROOT) still exercise the real check.
script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
python3 "${script_dir}/lib/verify-queue-contract-coverage.py" "${ledger}" "${repo_root}"
