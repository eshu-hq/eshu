#!/usr/bin/env bash
# shellcheck shell=bash disable=SC2016,SC2154
# Workflow-shape cases for test-verify-ci-gates-registry.sh. Sourced by the
# parent test, which owns the referenced paths and assertion helpers.

check_ci_gate_workflow_shapes() {
	local mirrored_scripts mirrored_count mirrored_script

	# dorny/paths-filter needs pull-request read permission, matrix context is
	# invalid at jobs.<job_id>.if, and main pushes retain the all-gates backstop.
	[[ -f "${static_contract_workflow}" ]] || fail "missing ${static_contract_workflow}"
	require "paths-filter PR permission" "pull-requests: read" "${static_contract_workflow}"
	if rg --quiet '^    if:.*matrix\.' "${static_contract_workflow}"; then
		fail "static-contract-gates.yml must not use matrix context in jobs.<job_id>.if"
	fi
	require "main-push all-gates selector" \
		'[[ "${{ github.event_name }}" == "push" || "${selected}" == "true" ]]' \
		"${static_contract_workflow}"
	require "selected gate matrix" \
		"fromJSON(needs.changes.outputs.matrix)" \
		"${static_contract_workflow}"
	require "empty-selection job guard" \
		"needs.changes.outputs.any == 'true'" \
		"${static_contract_workflow}"

	# Build Test exposes separately timed contract, core, race, and docs/Helm
	# surfaces. A monolithic build job would hide which surface timed out.
	[[ -f "${build_test_workflow}" ]] || fail "missing ${build_test_workflow}"
	require "Build Test read-only token permissions" "permissions:" "${build_test_workflow}"
	require "Build Test contents read permission" "  contents: read" "${build_test_workflow}"
	require "Build Test contract verifier job" "  verify-contracts:" "${build_test_workflow}"
	require "Build Test Go core job" "  go-core:" "${build_test_workflow}"
	require "Build Test Go race job" "  go-race:" "${build_test_workflow}"
	require "Build Test docs/Helm hygiene job" "  docs-helm-hygiene:" "${build_test_workflow}"
	require "Build Test go-core cancellation guards" 'if: ${{ !cancelled() }}' "${build_test_workflow}"
	require "Build Test race Helm setup" "Set up Helm for race tests" "${build_test_workflow}"
	if rg --quiet '^  build:' "${build_test_workflow}"; then
		fail "test.yml must not keep the monolithic build job after #4263 split"
	fi

	# Every test script in the registry gate's test_command must also run in its
	# CI mirror. The minimum count keeps a broken extraction from passing vacuously.
	mirrored_scripts="$(
		printf '%s\n' "${ci_gate_registry_test_command}" |
			rg --only-matching 'scripts/[[:alnum:]_./-]+\.sh' |
			sort -u
	)"
	mirrored_count="$(printf '%s\n' "${mirrored_scripts}" | rg -c . || true)"
	[[ "${mirrored_count:-0}" -ge 2 ]] ||
		fail "ci-gate-registry test_command yielded ${mirrored_count:-0} test scripts; the extraction is broken, not the registry"
	while IFS= read -r mirrored_script; do
		[[ -z "${mirrored_script}" ]] && continue
		require "CI mirror runs ${mirrored_script}" "${mirrored_script}" "${registry_workflow}"
	done <<<"${mirrored_scripts}"
}
