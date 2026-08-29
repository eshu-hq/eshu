#!/usr/bin/env bash
# Performance-evidence selector cases for test-verify-ci-gates-registry.sh.
# Sourced by that test; not intended to run standalone.

check_performance_evidence_trigger_parity() {
	local perf_gate perf_input perf_selection

	perf_gate="$(
		sed -n '/^  - id: perf-evidence$/,/^  - id:/p' "${registry}"
	)"
	printf '%s\n' "${perf_gate}" |
		rg --fixed-strings -- \
		'test_command: "bash scripts/test-verify-performance-evidence.sh"' >/dev/null ||
		fail "perf-evidence local.test_command omits its regression suite"

	for perf_input in \
		'scripts/lib/live-gate-lock.sh' \
		'scripts/lib/golden-corpus-keep-marker.sh' \
		'scripts/test-verify-performance-evidence.sh'; do
		require_path_line "${perf_gate}" "${perf_input}" \
			"perf-evidence registry triggers omit ${perf_input}"
		perf_selection="$(
			printf '%s\n' "${perf_input}" |
				bash "${repo_root}/scripts/dev/select-gates.sh" \
					--tier pre-pr --paths-from - --explain
		)"
		printf '%s\n' "${perf_selection}" |
			rg '^SELECTED[[:space:]]+perf-evidence[[:space:]]' >/dev/null ||
			fail "a change to ${perf_input} did not select perf-evidence at pre-pr"
	done
}
