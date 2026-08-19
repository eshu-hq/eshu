#!/usr/bin/env bash
# shellcheck shell=bash disable=SC2154
# Registry-to-workflow lockstep for the Ifa dorny path filter in
# .github/workflows/static-contract-gates.yml. Sourced by
# scripts/test-verify-ci-gates-registry.sh, which owns repo_root, fail(), and
# the ${static_contract_workflow} / ${registry} path variables.
#
# WHY THIS EXISTS. The ifa-materialized-edge-coverage gate declares triggers in
# specs/ci-gates.v1.yaml, and the job that runs it is scheduled by a dorny
# filter in static-contract-gates.yml. Those are two independently hand-edited
# lists, and NOTHING compared them: the registry-subset-of-workflow lockstep in
# scripts/lib/test-ifa-determinism-registry-lockstep-cases.sh only reads
# ifa-determinism-gate.yml. The workflow's own comment says "keep this glob and
# the registry trigger in lockstep" -- an instruction with no gate behind it,
# written from the memory of #5873 where exactly this drifted.
#
# The failure it closes is asymmetric and nasty. Narrow either copy (dorny `*`
# does not cross `/`) and an edit to scripts/lib/ifa_family_registry/rows/*.sh --
# the most common edit in this design -- either selects the gate as BLOCKING
# while the workflow never schedules the job (required check MISSING forever,
# PR unmergeable), or drops the gate silently. Same dark-gate class as #6164.

run_ci_gates_registry_ifa_filter_cases() {
	local trigger

	# The Ifa workflow shape checks, relocated here from
	# scripts/test-verify-ci-gates-registry.sh so every assertion about
	# static-contract-gates.yml's ifa job lives in one module (and to keep that
	# file under the 500-line cap). Unabridged -- only the indentation changed.
	require "Ifa workflow filter" \
		"ifa:" \
		"${static_contract_workflow}"
	require "Ifa workflow path filter" \
		"go/internal/ifa/**" \
		"${static_contract_workflow}"
	# The reducer leg is part of the pinned text on purpose. The registry's
	# local.command for ifa-materialized-edge-coverage runs it, and the Go
	# blocker-shape lockstep this gate's triggers exist for lives in that package --
	# the CI job ran without it until #6147, so the gate's own comment claiming the
	# triggers protected that lockstep in CI was false. Pinning the whole command
	# keeps local and CI reading the same thing; dropping the leg here again should
	# fail loudly.
	require "Ifa workflow matrix entry" \
		'append_gate "${{ steps.filter.outputs.ifa }}" "ifa" "Verify Ifa contract-layer gate" "cd go && go test ./internal/ifa ./cmd/ifa -count=1 && go test ./internal/reducer -count=1" "cd go && go test ./internal/ifa ./cmd/ifa -count=1 && go test ./internal/reducer -count=1"' \
		"${static_contract_workflow}"

	# Every registry trigger the ifa-materialized-edge-coverage gate declares for
	# the shell family registry must also appear in the workflow's ifa filter.
	# Listed literally rather than parsed out of the registry on purpose: a
	# derived list would agree with the registry by construction, which is the
	# thing this check exists not to do.
	for trigger in \
		'scripts/lib/ifa_family_registry.sh' \
		'scripts/lib/ifa_family_registry/**'; do
		rg --fixed-strings --quiet -- "${trigger}" "${registry}" \
			|| fail "specs/ci-gates.v1.yaml no longer triggers on ${trigger}; either this pin is stale or the gate stopped watching the family registry"
		rg --fixed-strings --quiet -- "${trigger}" "${static_contract_workflow}" \
			|| fail "static-contract-gates.yml's ifa filter omits ${trigger}, which specs/ci-gates.v1.yaml triggers on -- the gate would be selected as blocking and the job never scheduled"
	done
}
