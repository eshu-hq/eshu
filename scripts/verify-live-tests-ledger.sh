#!/usr/bin/env bash
# Live-test ledger integrity checker (#6784). Verifies that every
# *_live_test.go file under go/ is classified in specs/live-tests.v1.yaml
# with a valid class and a non-blank reason, and that every ledger row
# names a tracked or present file exactly once.
#
# Credential-free, Docker-free, network-free. REPO_ROOT overrides the
# repository root so the seeded RED/GREEN test can point it at a fixture
# tree; LEDGER_PATH overrides the ledger location for the same reason.
set -euo pipefail

repo_root="${REPO_ROOT:-$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)}"
ledger="${LEDGER_PATH:-${repo_root}/specs/live-tests.v1.yaml}"

fail() {
	printf 'verify-live-tests-ledger: %s\n' "$*" >&2
	exit 1
}

[[ -f "${ledger}" ]] || fail "ledger not found: ${ledger}"

# The check lives in scripts/lib as a file, not a bash heredoc: the
# heredoc-budget gate fails new scripts with heredoc bodies over 512
# bytes (Homebrew bash >= 5.1 deadlock), and this body is ~4 KB. The
# helper resolves against this script's own location, not REPO_ROOT, so
# fixture trees (which override REPO_ROOT) still exercise the real check.
script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
python3 "${script_dir}/lib/verify-live-tests-ledger.py" "${ledger}" "${repo_root}"
