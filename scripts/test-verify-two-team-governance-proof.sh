#!/usr/bin/env bash
set -euo pipefail

# Self-test for the two-team governance cross-scope denial proof verifier
# (#1910). It proves the verifier is well-formed, passes a good proof-artifact
# set, and fails closed on each tenant-isolation regression: a leaked cross-scope
# repository, an open cross-scope selector, an API/MCP parity mismatch, an open
# unauthenticated read, and leaked registry token-hash material. This runs
# locally with no Compose stack; the live run that produces real artifacts
# (scripts/run-two-team-governance-proof.sh) is the operator/CI gate.

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
verifier="${repo_root}/scripts/verify-two-team-governance-proof.sh"
fixtures="${repo_root}/tests/fixtures/governance_two_team_proof"

die() {
	printf 'test-verify-two-team-governance-proof: %s\n' "$*" >&2
	exit 1
}

[[ -f "${verifier}" ]] || die "missing verifier: ${verifier}"
bash -n "${verifier}" || die "verifier failed bash syntax check"

# --list names every proof check without running anything.
list_log="$(bash "${verifier}" --list)"
for needle in "unauthenticated:" "admin:" "team-a allowed:" "team-a denied:" \
	"team-b allowed:" "team-b denied:" "parity:" "provenance:" "redaction canary:"; do
	rg --fixed-strings --quiet "${needle}" < <(printf '%s\n' "${list_log}") \
		|| die "--list output missing ${needle}"
done

# Good artifacts pass.
bash "${verifier}" --artifacts "${fixtures}/good" >/dev/null \
	|| die "verifier rejected the good proof artifacts"

# Each bad artifact set must fail closed.
for bad in bad_cross_scope_leak bad_selector_open bad_parity bad_unauth_open bad_leak; do
	if bash "${verifier}" --artifacts "${fixtures}/${bad}" >/dev/null 2>&1; then
		die "verifier accepted bad artifacts: ${bad}"
	fi
done

printf 'two-team governance cross-scope denial proof verifier self-test passed\n'
