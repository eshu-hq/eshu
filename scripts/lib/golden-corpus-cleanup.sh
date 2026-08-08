#!/usr/bin/env bash
#
# golden-corpus-cleanup.sh — EXIT trap for the golden corpus gate orchestrator
# (scripts/verify-golden-corpus-gate.sh). Extracted into a lib chunk so the
# orchestrator stays under the 500-line cap.
#
# cleanup() dumps every host-binary log on failure, reaps every background pid
# the orchestrator recorded, tears down (or retains, on --keep) the compose
# stack and work dir, then releases (or retains) the cross-run mutex from
# scripts/lib/live-gate-lock.sh. It MUST run last in the trap chain: it exits
# with the captured status, so anything the caller wants to run on exit must be
# installed before `trap cleanup EXIT`.
#
# Requires (set by the orchestrator before `trap cleanup EXIT`): bg_pids
# (array), log_dir, keep, use_compose, compose_file, work_dir, and the
# retain_live_gate_lock/release_live_gate_lock functions from
# scripts/lib/live-gate-lock.sh (sourced separately; this chunk only calls
# them, it does not source the lock lib itself).

cleanup() {
	local status=$?
	# On failure, dump every host-binary log to stdout BEFORE the work dir is
	# removed, so a CI failure (which captures this script's stdout) preserves the
	# api/collector/projector/reducer logs that explain the break — not just the
	# Docker logs the workflow dumps separately.
	if [[ "${status}" -ne 0 && -d "${log_dir}" ]]; then
		printf '\n=== host binary logs (failure) ===\n' >&2
		for logf in "${log_dir}"/*.log; do
			[[ -f "${logf}" ]] || continue
			printf '\n--- %s ---\n' "$(basename "${logf}")" >&2
			tail -40 "${logf}" >&2 || true
		done
	fi
	for pid in "${bg_pids[@]:-}"; do
		[[ -n "${pid}" ]] && kill "${pid}" >/dev/null 2>&1 || true
	done
	if [[ "${keep}" -eq 1 ]]; then
		printf '\n[--keep] work dir retained: %s\n' "${work_dir}" >&2
	else
		if [[ "${use_compose}" -eq 1 ]]; then
			docker compose "${compose_args[@]}" down -v >/dev/null 2>&1 || true
		fi
		rm -rf "${work_dir}"
	fi
	# Last, and only while still the recorded holder (see the lib). A --keep run
	# retains the stack on the fixed ports, so it retains the mutex with it.
	if [[ "${keep}" -eq 1 ]]; then
		retain_live_gate_lock
	else
		release_live_gate_lock
	fi
	exit "${status}"
}
