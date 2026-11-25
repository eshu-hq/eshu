#!/usr/bin/env bash
#
# test-verify-ci-gates-registry-telemetry-cases.sh — the telemetry-coverage
# trigger-parity cases for scripts/test-verify-ci-gates-registry.sh.
# Sourced by scripts/test-verify-ci-gates-registry.sh; expects repo_root,
# registry, static_contract_workflow, fail(), require(), and require_path_line().

check_telemetry_coverage_trigger_parity() {
	local telemetry_coverage_gate telemetry_trigger telemetry_input telemetry_selection

	telemetry_coverage_gate="$(
		sed -n '/^  - id: telemetry-coverage$/,/^  - id:/p' "${registry}"
	)"
	require "telemetry coverage verifier step" \
		"scripts/verify-telemetry-coverage.sh" \
		"${static_contract_workflow}"

	for telemetry_trigger in \
		'docs/public/observability/telemetry-coverage.md' \
		'scripts/*verify-telemetry-coverage*.sh' \
		'scripts/lib/test-verify-telemetry-coverage-*'; do
		require_path_line "${telemetry_coverage_gate}" "${telemetry_trigger}" \
			"telemetry-coverage registry triggers omit ${telemetry_trigger}"
	done

	for telemetry_input in \
		'docs/public/observability/telemetry-coverage.md' \
		'scripts/verify-telemetry-coverage.sh' \
		'scripts/test-verify-telemetry-coverage.sh'; do
		telemetry_selection="$(
			printf '%s\n' "${telemetry_input}" |
				(cd "${repo_root}/go" && go run ./cmd/ci-gates select \
					--registry "${registry}" --tier pre-pr --paths-from - --explain)
		)"
		printf '%s\n' "${telemetry_selection}" |
			rg '^SELECTED[[:space:]]+telemetry-coverage[[:space:]]' >/dev/null ||
			fail "a change to ${telemetry_input} did not select telemetry-coverage"
	done
}
