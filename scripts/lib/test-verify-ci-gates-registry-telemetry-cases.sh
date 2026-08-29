#!/usr/bin/env bash
#
# test-verify-ci-gates-registry-telemetry-cases.sh — the telemetry-coverage
# trigger-parity cases for scripts/test-verify-ci-gates-registry.sh. Split
# out of that script to keep it under the repo's 500-line file cap (#5855),
# the same way scripts/lib/telemetry-coverage-row-check.sh was split out of
# scripts/verify-telemetry-coverage.sh.
#
# Sourced by scripts/test-verify-ci-gates-registry.sh; not meant to run
# standalone. check_telemetry_coverage_trigger_parity expects repo_root,
# registry, and static_contract_workflow to already be set by the caller,
# and the caller's fail() and require_path_line() functions to already be
# defined.
#
# Registered as a trigger of the ci-gate-registry gate in
# specs/ci-gates.v1.yaml, alongside the sibling scripts/lib/ci-gates-doc-parse.awk
# entry, so a change confined to this file re-runs the gate it belongs to.

# #5855 P2 re-review: telemetry-coverage's own X1 contract doc and its
# verify/test scripts were absent from its registry triggers even though the
# CI workflow's `telemetry:` paths-filter (dorny/paths-filter, 14-space
# single-quoted list, distinct from the native `on:.paths:` blocks the caller
# checks) already covered them. A doc/script-only change to this exact surface
# - the surface #5855 itself touches - selected NO local gate, so `make
# pre-pr` silently skipped the gate whose whole purpose is auditing telemetry
# coverage, and only CI would have caught a regression. Assert the registry
# trigger mirrors each CI filter line first (both sources of truth agree),
# then prove concrete real files under that surface actually select the gate
# for the right reason - membership only, since heredoc-budget's
# "scripts/**/*.sh" trigger also legitimately matches the two scripts.
require_workflow_filter_line() {
	local haystack="$1" needle="$2" message="$3"
	printf '%s\n' "${haystack}" |
		rg --fixed-strings --line-regexp -- "              - '${needle}'" >/dev/null ||
		fail "${message}: expected the exact line \`              - '${needle}'\` (14 spaces, single-quoted)"
}

check_telemetry_coverage_trigger_parity() {
	local telemetry_coverage_gate telemetry_workflow_filter
	local telemetry_trigger telemetry_input telemetry_selection

	telemetry_coverage_gate="$(
		sed -n '/^  - id: telemetry-coverage$/,/^  - id:/p' "${registry}"
	)"
	telemetry_workflow_filter="$(
		sed -n '/^            telemetry:$/,/^            dashboard:$/p' "${static_contract_workflow}"
	)"
	[[ -n "${telemetry_workflow_filter}" ]] ||
		fail "could not extract the telemetry: paths-filter block from ${static_contract_workflow}"

	for telemetry_trigger in \
		'docs/public/observability/telemetry-coverage.md' \
		'scripts/*verify-telemetry-coverage*.sh' \
		'scripts/lib/test-verify-telemetry-coverage-*'; do
		require_workflow_filter_line "${telemetry_workflow_filter}" "${telemetry_trigger}" \
			"telemetry: CI paths-filter no longer covers ${telemetry_trigger}"
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
