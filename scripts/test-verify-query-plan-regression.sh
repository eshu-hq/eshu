#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
verifier="${repo_root}/scripts/verify-query-plan-regression.sh"

"${verifier}" >/tmp/eshu-query-plan-gate.out 2>/tmp/eshu-query-plan-gate.err

if ! rg --quiet 'verify-query-plan-regression: pass' /tmp/eshu-query-plan-gate.out; then
	printf 'expected query-plan verifier pass marker\n' >&2
	sed -n '1,120p' /tmp/eshu-query-plan-gate.out >&2
	sed -n '1,120p' /tmp/eshu-query-plan-gate.err >&2
	exit 1
fi

if rg --quiet 'No such file or directory' /tmp/eshu-query-plan-gate.err; then
	printf 'expected query-plan verifier stderr to stay clean\n' >&2
	sed -n '1,120p' /tmp/eshu-query-plan-gate.err >&2
	exit 1
fi

printf 'test-verify-query-plan-regression: pass\n'
