#!/usr/bin/env bash
# The generic per-family BASELINE fault cell, split out of
# scripts/lib/ifa_fault_generic_cells.sh because that file reached the
# repository's 500-line cap -- the same reason, and the same split, its own
# header records for the per-blocker mechanism files. Keeping the split at a
# whole dispatcher rather than shaving comments keeps each file one coherent
# unit.
#
# Sourced AFTER ifa_fault_generic_cells.sh: this cell calls
# _ifa_generic_drive_family and _ifa_generic_baseline_key, which that file
# defines. Bash resolves function bodies at call time, so the order matters
# only for readability, not correctness -- but keep it, so a reader is never
# asked to hold two files in their head to find a definition.
#
# This file is sourced, never executed.

# cell_baseline_family is the THIRD generic entry point, and the one without
# which the other two cannot work for a self-driving family.
#
# The shared cell_baseline (ifa_fault_injection_cells.sh) never drives a
# FAULT_SHARED_DRIVE=0 family's cassette, so digests[baseline] does not contain
# that family's edges. A recovery cell that drove its own cassette would then
# differ from the shared baseline BY CONSTRUCTION and die reporting a graph
# divergence that is really a fixture difference -- the loudest possible way to
# be wrong about what failed. It also establishes the family's fault-free retry
# count, which _ifa_generic_require_retry_baseline dereferences by name and which
# no other cell sets.
#
# This is the codeowners/deployable_unit trio shape, generalized: those two
# families each hand-wrote this cell, and every family after them gets it from
# the registry instead.
#
# Runs ONLY for FAULT_SHARED_DRIVE=0 families. A family the shared drive covers
# already has its baseline in digests[baseline]; dispatching this for it would
# double-drive its cassette and produce a second, redundant digest.
cell_baseline_family() {
	local family="$1" cell="baseline${1//_/}" cell_start shared_drive retry_var wait_key
	cell_start=$(date +%s)
	shared_drive="$(ifa_family_fault_shared_drive "${family}")" \
		|| die "${cell}: ${family}: ifa_family_fault_shared_drive accessor failed -- refusing to guess whether this family needs its own baseline"
	[[ "${shared_drive}" == "0" ]] \
		|| die "${cell}: ${family} declares FAULT_SHARED_DRIVE=${shared_drive}; a family the shared drive covers must compare against digests[baseline] and must not be given a second baseline cell"

	log "cell generic-baseline (${family}): fresh stack"
	fresh_stack "${cell}"
	if [[ "${use_compose}" -eq 1 ]]; then
		ifa_fault_require_fresh_domain_intents "${cell}" "${family}" \
			"${FAULT_COMPOSE_PROJECT}" "${use_compose}" "${ESHU_POSTGRES_DSN}" "${compose_file}" \
			|| die "${cell}: fresh-stack precondition failed"
	else
		printf '%s: fresh-stack precondition SKIPPED (--no-compose owns the stack; surviving intents are not a leak)\n' "${cell}"
	fi
	drive_all_cassettes "${cell}"
	_ifa_generic_drive_family "${family}" "${cell}"
	local projector_pid reducer_pid
	ifa_det_start_bg "${log_dir}" "projector-${cell}" projector_pid "${bin_dir}/eshu-projector"
	ifa_det_start_bg "${log_dir}" "reducer-${cell}" reducer_pid "${bin_dir}/eshu-reducer"
	run_drain_gate "${cell}"
	assert_no_dead_letters "${cell}"
	# The baseline asserts the SAME exact edge set the recovery cells assert. A
	# baseline that only captured a digest would let a fixture that materializes
	# nothing establish an empty baseline that every recovery cell then matches.
	_ifa_generic_assert_edges "${family}" \
		|| die "${cell}: fault-free graph does not match the expected edge set -- the baseline itself is wrong, so every cell comparing against it would be meaningless"

	# Establish the fault-free retry count the kill cell compares against.
	# Only shared_intent_lock families have a retry-baseline var; for the rest
	# the row is legitimately absent and there is nothing to capture.
	retry_var="$(ifa_family_retry_baseline_var "${family}")" \
		|| die "${cell}: ${family}: ifa_family_retry_baseline_var accessor failed"
	if [[ -n "${retry_var}" ]]; then
		wait_key="$(ifa_family_wait_key "${family}")" \
			|| die "${cell}: ${family}: ifa_family_wait_key accessor failed -- cannot scope the retry baseline to a domain"
		[[ -n "${wait_key}" ]] \
			|| die "${cell}: ${family} declares an empty IFA_FAMILY_WAIT_KEY -- a retry baseline scoped to no domain proves nothing"
		printf -v "${retry_var}" '%s' "$(ifa_fault_count_retried "${FAULT_COMPOSE_PROJECT}" "${use_compose}" "${ESHU_POSTGRES_DSN}" "${compose_file}" "${wait_key}")" \
			|| die "${cell}: could not establish ${retry_var} for ${family}"
		printf '%s: fault-free %s retry baseline (%s): %s\n' "${cell}" "${wait_key}" "${retry_var}" "${!retry_var}"
	fi

	capture_digest "${cell}"
	digests["baseline_${family}"]="${digests[${cell}]}"
	cp "${work_dir}/graph-${cell}.dump" "${work_dir}/graph-baseline_${family}.dump" 2>/dev/null || true
	teardown_cell "${cell}"
	wall_times[$cell]=$(( $(date +%s) - cell_start ))
	printf '%s: cell wall time: %ss\n' "${cell}" "${wall_times[${cell}]}"
}
