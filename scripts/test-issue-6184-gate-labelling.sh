#!/usr/bin/env bash
# Regression test for #6184: the graph-rebuild DR gate's known-bad state and
# the fixture-scale SLO decision must be visible where a gate-list reader
# looks, not only in prose paragraphs.
#
# Asserts:
# 1. The verify-graph-rebuild-from-facts.sh entry in the Compose Gates list in
#    docs/public/reference/local-testing/verification-gates.md is labelled
#    advisory/known-bad at the list itself.
# 2. docs/public/reference/performance-slo-contract.md records an explicit
#    decision that the fixture-scale number is all graph_rebuild_seconds
#    carries, with the reason.
#
# Usage: scripts/test-issue-6184-gate-labelling.sh
# Wired into CI via the docs-helm-hygiene job in .github/workflows/test.yml
# (always-on job), so the labels it protects cannot rot silently.
set -uo pipefail

script_root="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
repo_root="$(cd "${script_root}/.." && pwd)"
gates_doc="${repo_root}/docs/public/reference/local-testing/verification-gates.md"
slo_doc="${repo_root}/docs/public/reference/performance-slo-contract.md"

pass_count=0
fail_count=0

record_pass() { pass_count=$((pass_count + 1)); printf 'PASS: %s\n' "$1"; }
record_fail() {
	fail_count=$((fail_count + 1))
	printf 'FAIL: %s\n' "$1"
	[[ -n "${2:-}" ]] && printf '  %s\n' "$2"
	return 0
}

# 1. The gate-list code fence names the script; the advisory label must sit
# within a few lines of that entry so a reader scanning the list sees it.
gate_entry_line="$(rg -n 'verify-graph-rebuild-from-facts\.sh$' "${gates_doc}" | head -n 1 | cut -d: -f1)"
if [[ -z "${gate_entry_line}" ]]; then
	record_fail "rebuild gate is listed in the Compose Gates list" "no fence entry found"
else
	window="$(sed -n "$((gate_entry_line - 3)),$((gate_entry_line + 3))p" "${gates_doc}")"
	# Canonical affirmative annotation, case-sensitive: a reversed annotation
	# ("not advisory", "passes today") must not match.
	if printf '%s\n' "${window}" | rg -q 'Advisory known-bad'; then
		record_pass "rebuild gate list entry carries an advisory/known-bad label"
	else
		record_fail "rebuild gate list entry carries an advisory/known-bad label" \
			"entry at line ${gate_entry_line} has no canonical 'Advisory known-bad' label in its window"
	fi
fi

# 2. The SLO contract must record the explicit fixture-scale-only decision,
# with its substance (the scale-lab/single-sample reason) and fixture-only
# direction — all scoped to the decision paragraph's window, so a distant
# paragraph's wording cannot satisfy them and a rewritten paragraph fails.
check2_fail=""
decision_line="$(rg -n -i 'recorded decision' "${slo_doc}" | head -n 1 | cut -d: -f1)"
if [[ -z "${decision_line}" ]]; then
	check2_fail="no recorded-decision statement"
else
	paragraph="$(sed -n "$((decision_line - 2)),$((decision_line + 8))p" "${slo_doc}")"
	printf '%s\n' "${paragraph}" | rg -qi 'fixture-scale' || check2_fail="no fixture-scale statement near the decision"
	if [[ -z "${check2_fail}" ]]; then
		printf '%s\n' "${paragraph}" | rg -qi 'scale-lab|single sample|341' || check2_fail="no scale-lab/single-sample reason near the decision"
	fi
	# Direction: the paragraph must read fixture-only/no-bound. A rewrite
	# declaring the sample a production bound keeps the words above but
	# must fail here.
	if [[ -z "${check2_fail}" ]]; then
		printf '%s\n' "${paragraph}" | rg -qi 'is all .* carries|do not read|not .* bound|fixture-only' || check2_fail="no fixture-only/no-bound direction near the decision"
	fi
fi
if [[ -z "${check2_fail}" ]]; then
	record_pass "SLO contract records the explicit fixture-scale decision with reason"
else
	record_fail "SLO contract records the explicit fixture-scale decision with reason" \
		"${check2_fail}"
fi

printf '\n%d passed, %d failed\n' "${pass_count}" "${fail_count}"
[[ "${fail_count}" -eq 0 ]]
