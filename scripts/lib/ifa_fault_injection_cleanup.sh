#!/usr/bin/env bash
# EXIT-trap cleanup for scripts/verify-ifa-fault-injection.sh.
# This is a plain library, not an executable: it deliberately does not change
# the caller's shell options. The block lived inline in the gate script until
# that script hit its 500-line cap; the gate sources this file up top with the
# other libs, so the function is defined before `trap cleanup EXIT` can fire.
# Runs in the gate's shell, so it reads the gate's globals (keep, work_dir,
# log_dir, compose project/file, DSN, background pids, barrier state).
# shellcheck disable=SC2154  # gate globals, set by the gate before the trap.
cleanup() {
	local status=$?
	local barrier_cleanup_rc=0
	if [[ "${status}" -ne 0 && -d "${log_dir}" ]]; then
		if [[ "${keep}" -eq 1 ]]; then
			if ifa_fault_capture_failure_preserving_status "${status}" \
				"${work_dir}" "${log_dir}" "${FAULT_COMPOSE_PROJECT:-}" "${compose_file:-}" \
				"${use_compose:-0}" "${ESHU_POSTGRES_DSN:-}" "${bin_dir:-}"; then
				:
			fi
		fi
		# A dead-lettered row's failure_message exists ONLY in Postgres, and the
		# tail below is routinely flooded by INFO chatter (one real CI failure
		# spent all 60 of its lines on "drift finding admitted"), so a red cell
		# could name its own cause nowhere. Print the durable rows first and
		# unelided. Never allowed to change the exit status: status was captured
		# above, and the dump cannot fail the trap.
		printf '\n=== durable work-item failures (Postgres) ===\n' >&2
		ifa_det_pg "${FAULT_COMPOSE_PROJECT:-}" "${use_compose:-0}" "${ESHU_POSTGRES_DSN:-}" \
			"SELECT stage, domain, scope_id, status, attempt_count, failure_class, failure_message FROM fact_work_items WHERE status NOT IN ('succeeded', 'superseded') ORDER BY status, domain;" \
			"${compose_file:-}" >&2 || printf '(durable work-item dump unavailable)\n' >&2
		printf '\n=== host binary logs (failure) ===\n' >&2
		for logf in "${log_dir}"/*.log; do
			[[ -f "${logf}" ]] || continue
			printf '\n--- %s ---\n' "$(basename "${logf}")" >&2
			tail -60 "${logf}" >&2 || true
		done
	fi
	if declare -F ifa_documentation_stop_ack_producers >/dev/null; then
		ifa_documentation_stop_ack_producers || barrier_cleanup_rc=$?
	else
		for pid in "${bg_pids[@]:-}"; do
			[[ -n "${pid}" ]] && kill "${pid}" >/dev/null 2>&1 || true
		done
	fi
	if declare -F ifa_documentation_cleanup_ack_barrier >/dev/null; then
		ifa_documentation_cleanup_ack_barrier "${ifa_documentation_ack_barrier_cell:-killworkerdocumentation}" \
			|| { local cleanup_rc=$?; [[ "${barrier_cleanup_rc}" -ne 0 ]] || barrier_cleanup_rc="${cleanup_rc}"; }
	fi
	if declare -F ifa_fault_cleanup_runner_lease_audit >/dev/null; then
		ifa_fault_cleanup_runner_lease_audit \
			|| { local cleanup_rc=$?; [[ "${barrier_cleanup_rc}" -ne 0 ]] || barrier_cleanup_rc="${cleanup_rc}"; }
	fi
	if [[ "${barrier_cleanup_rc}" -ne 0 ]]; then
		printf 'verify-ifa-fault-injection: database fixture EXIT cleanup failed\n' >&2
		[[ "${status}" -ne 0 ]] || status="${barrier_cleanup_rc}"
	fi
	if [[ "${keep}" -eq 1 ]]; then
		printf '\n[--keep] work dir retained: %s\n' "${work_dir}" >&2
	else
		if [[ "${use_compose}" -eq 1 ]]; then
			bash scripts/ci/compose.sh -p "${FAULT_COMPOSE_PROJECT}" -f "${compose_file}" -- down -v >/dev/null 2>&1 || true
		fi
		rm -rf "${work_dir}"
	fi
	if [[ "${keep}" -eq 0 ]] && declare -F eshu_ci_compose_cleanup >/dev/null; then eshu_ci_compose_cleanup; fi
	exit "${status}"
}
