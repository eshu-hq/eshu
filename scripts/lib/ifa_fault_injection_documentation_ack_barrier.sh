#!/usr/bin/env bash
# shellcheck disable=SC2034,SC2154
# Exact fact_work_items ACK barrier for the documentation kill/reclaim cell.
# Sourced by scripts/verify-ifa-fault-injection.sh after the shared helpers.

# The documentation handler writes graph records directly; it does not write
# shared_projection_intents. This trigger therefore blocks only the exact
# fact_work_items ACK transition after the handler has committed its graph
# write. The session advisory holder is separate so killing the host reducer
# cannot release the barrier before the orphaned Postgres backend is fenced.
# shellcheck source=scripts/lib/ifa_fault_injection_documentation_ack_setup.sh
source "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/ifa_fault_injection_documentation_ack_setup.sh"

ifa_documentation_census_ack_waiter() {
	local cell="$1" waiter_app_name census query_rc
	ifa_documentation_prepare_ack_identity || return $?
	waiter_app_name="ifa_documentation_ack_waiter_${ifa_documentation_ack_run_id}"
	if census="$(ifa_det_pg "${FAULT_COMPOSE_PROJECT}" "${use_compose}" "${ESHU_POSTGRES_DSN}" \
		"SELECT COALESCE(pg_catalog.string_agg(waiter.pid::text, ',' ORDER BY waiter.pid), '') FROM pg_catalog.pg_stat_activity waiter JOIN pg_catalog.pg_locks barrier ON barrier.pid = waiter.pid WHERE waiter.application_name = '${waiter_app_name}' AND barrier.locktype = 'advisory' AND barrier.classid = 5998 AND barrier.objid = 5994 AND barrier.objsubid = 2 AND barrier.database = (SELECT oid FROM pg_catalog.pg_database WHERE datname = pg_catalog.current_database()) AND NOT barrier.granted;" \
		"${compose_file}")"; then
		census="$(printf '%s' "${census}" | tr -d '[:space:]')"
	else
		query_rc=$?
		printf '%s: documentation ACK waiter census FAILED (exit %s)\n' "${cell}" "${query_rc}" >&2
		return "${query_rc}"
	fi
	[[ -z "${census}" || "${census}" =~ ^[1-9][0-9]*$ ]] || {
		printf '%s: documentation ACK waiter census is ambiguous: %q\n' "${cell}" "${census}" >&2
		return 1
	}
	printf '%s' "${census}"
}

ifa_documentation_census_ack_holder() {
	local cell="$1" holder_app_name census query_rc
	ifa_documentation_prepare_ack_identity || return $?
	holder_app_name="ifa_documentation_ack_holder_${ifa_documentation_ack_run_id}"
	if census="$(ifa_det_pg "${FAULT_COMPOSE_PROJECT}" "${use_compose}" "${ESHU_POSTGRES_DSN}" \
		"SELECT COALESCE(pg_catalog.string_agg(holder.pid::text, ',' ORDER BY holder.pid), '') FROM pg_catalog.pg_stat_activity holder JOIN pg_catalog.pg_locks held ON held.pid = holder.pid WHERE holder.application_name = '${holder_app_name}' AND held.locktype = 'advisory' AND held.classid = 5998 AND held.objid = 5994 AND held.objsubid = 2 AND held.database = (SELECT oid FROM pg_catalog.pg_database WHERE datname = pg_catalog.current_database());" \
		"${compose_file}")"; then
		census="$(printf '%s' "${census}" | tr -d '[:space:]')"
	else
		query_rc=$?
		printf '%s: documentation ACK holder census FAILED (exit %s)\n' "${cell}" "${query_rc}" >&2
		return "${query_rc}"
	fi
	[[ -z "${census}" || "${census}" =~ ^[1-9][0-9]*$ ]] || {
		printf '%s: documentation ACK holder census is ambiguous: %q\n' "${cell}" "${census}" >&2
		return 1
	}
	printf '%s' "${census}"
}

ifa_documentation_wait_for_blocked_ack() {
	local cell="$1" budget="$2"
	ifa_documentation_prepare_ack_identity || return $?
	local waiter_app_name="ifa_documentation_ack_waiter_${ifa_documentation_ack_run_id}"
	local holder_app_name="ifa_documentation_ack_holder_${ifa_documentation_ack_run_id}" i pair query_rc
	[[ "${budget}" =~ ^[1-9][0-9]*$ ]] || return 2
	for i in $(seq 1 $((budget * 4))); do
		if pair="$(ifa_det_pg "${FAULT_COMPOSE_PROJECT}" "${use_compose}" "${ESHU_POSTGRES_DSN}" \
			"SELECT COALESCE(string_agg(waiter.pid::text || '|' || holder.pid::text, ',' ORDER BY waiter.pid), '') FROM pg_catalog.pg_stat_activity waiter JOIN pg_catalog.pg_locks barrier ON barrier.pid = waiter.pid JOIN pg_catalog.pg_stat_activity holder ON holder.pid = ANY(pg_catalog.pg_blocking_pids(waiter.pid)) JOIN pg_catalog.pg_locks held ON held.pid = holder.pid WHERE waiter.application_name = '${waiter_app_name}' AND holder.application_name = '${holder_app_name}' AND barrier.locktype = 'advisory' AND barrier.classid = 5998 AND barrier.objid = 5994 AND barrier.objsubid = 2 AND barrier.database = (SELECT oid FROM pg_catalog.pg_database WHERE datname = pg_catalog.current_database()) AND NOT barrier.granted AND held.locktype = 'advisory' AND held.classid = 5998 AND held.objid = 5994 AND held.objsubid = 2 AND held.database = (SELECT oid FROM pg_catalog.pg_database WHERE datname = pg_catalog.current_database()) AND held.granted;" \
			"${compose_file}")"; then
			pair="$(printf '%s' "${pair}" | tr -d '[:space:]')"
		else
			query_rc=$?
			printf '%s: documentation blocked-ACK query FAILED (exit %s)\n' "${cell}" "${query_rc}" >&2
			return "${query_rc}"
		fi
		if [[ "${pair}" =~ ^[1-9][0-9]*\|[1-9][0-9]*$ ]]; then
			printf '%s' "${pair}"
			return 0
		fi
		[[ -z "${pair}" ]] || { printf '%s: documentation blocked-ACK query returned ambiguous output %q\n' "${cell}" "${pair}" >&2; return 1; }
		sleep 0.25
	done
	printf '%s: documentation ACK backend did not block on its exact advisory holder\n' "${cell}" >&2
	return 1
}

ifa_documentation_claim_snapshot() {
	local cell="$1" snapshot snapshot_rc
	local snapshot_re='^[^|,]+\|(claimed|running)\|1\|[^|,]+\|[0-9]+\|[0-9]+$'
	if snapshot="$(ifa_det_pg "${FAULT_COMPOSE_PROJECT}" "${use_compose}" "${ESHU_POSTGRES_DSN}" \
		"SELECT COALESCE(string_agg(work_item_id || '|' || status || '|' || attempt_count::text || '|' || lease_owner || '|' || EXTRACT(epoch FROM claim_until)::bigint::text || '|' || EXTRACT(epoch FROM updated_at)::bigint::text, ',' ORDER BY work_item_id), '') FROM public.fact_work_items WHERE stage = 'reducer' AND domain = 'documentation_materialization' AND scope_id = 'scope-ifa-documentation-family' AND generation_id = 'gen-ifa-documentation-family-1' AND status IN ('claimed', 'running');" \
		"${compose_file}")"; then
		snapshot="$(printf '%s' "${snapshot}" | tr -d '[:space:]')"
	else
		snapshot_rc=$?
		printf '%s: documentation claim snapshot query FAILED (exit %s)\n' "${cell}" "${snapshot_rc}" >&2
		return "${snapshot_rc}"
	fi
	[[ "${snapshot}" =~ ${snapshot_re} ]] || {
		printf '%s: documentation claim snapshot is not one exact attempt-1 leased row: %q\n' "${cell}" "${snapshot}" >&2
		return 1
	}
	printf '%s' "${snapshot}"
}

ifa_documentation_terminate_blocked_ack() {
	local cell="$1" waiter_pid="$2"
	ifa_documentation_prepare_ack_identity || return $?
	local waiter_app_name="ifa_documentation_ack_waiter_${ifa_documentation_ack_run_id}"
	local holder_app_name="ifa_documentation_ack_holder_${ifa_documentation_ack_run_id}" terminated query_rc
	[[ "${waiter_pid}" =~ ^[1-9][0-9]*$ ]] || return 2
	if terminated="$(ifa_det_pg "${FAULT_COMPOSE_PROJECT}" "${use_compose}" "${ESHU_POSTGRES_DSN}" \
		"WITH target AS MATERIALIZED (SELECT waiter.pid FROM pg_catalog.pg_stat_activity waiter JOIN pg_catalog.pg_locks barrier ON barrier.pid = waiter.pid JOIN pg_catalog.pg_stat_activity holder ON holder.pid = ANY(pg_catalog.pg_blocking_pids(waiter.pid)) JOIN pg_catalog.pg_locks held ON held.pid = holder.pid WHERE waiter.pid = ${waiter_pid} AND waiter.application_name = '${waiter_app_name}' AND holder.application_name = '${holder_app_name}' AND barrier.locktype = 'advisory' AND barrier.classid = 5998 AND barrier.objid = 5994 AND barrier.objsubid = 2 AND barrier.database = (SELECT oid FROM pg_catalog.pg_database WHERE datname = pg_catalog.current_database()) AND NOT barrier.granted AND held.locktype = 'advisory' AND held.classid = 5998 AND held.objid = 5994 AND held.objsubid = 2 AND held.database = (SELECT oid FROM pg_catalog.pg_database WHERE datname = pg_catalog.current_database()) AND held.granted), terminated AS MATERIALIZED (SELECT pg_catalog.pg_terminate_backend(pid) AS stopped FROM target) SELECT count(*)::text || '|' || COALESCE(pg_catalog.bool_and(stopped), false)::text FROM terminated;" \
		"${compose_file}")"; then
		terminated="$(printf '%s' "${terminated}" | tr -d '[:space:]')"
	else
		query_rc=$?
		printf '%s: documentation blocked-ACK termination query FAILED (exit %s)\n' "${cell}" "${query_rc}" >&2
		return "${query_rc}"
	fi
	[[ "${terminated}" == "1|true" ]] || { printf '%s: exact documentation ACK termination returned %q, want 1|true\n' "${cell}" "${terminated}" >&2; return 1; }
}

ifa_documentation_wait_for_ack_backend_gone() {
	local cell="$1" waiter_pid="$2" budget="$3" i remaining query_rc
	[[ "${waiter_pid}" =~ ^[1-9][0-9]*$ && "${budget}" =~ ^[1-9][0-9]*$ ]] || return 2
	for i in $(seq 1 $((budget * 4))); do
		if remaining="$(ifa_det_pg "${FAULT_COMPOSE_PROJECT}" "${use_compose}" "${ESHU_POSTGRES_DSN}" \
			"SELECT (SELECT count(*) FROM pg_catalog.pg_stat_activity WHERE pid = ${waiter_pid}) + (SELECT count(*) FROM pg_catalog.pg_locks WHERE pid = ${waiter_pid} AND locktype = 'advisory' AND classid = 5998 AND objid = 5994 AND objsubid = 2 AND database = (SELECT oid FROM pg_catalog.pg_database WHERE datname = pg_catalog.current_database()));" \
			"${compose_file}")"; then
			remaining="$(printf '%s' "${remaining}" | tr -d '[:space:]')"
		else
			query_rc=$?
			printf '%s: documentation waiter-gone query FAILED (exit %s)\n' "${cell}" "${query_rc}" >&2
			return "${query_rc}"
		fi
		[[ "${remaining}" =~ ^[0-9]+$ ]] || return 1
		[[ "${remaining}" == "0" ]] && return 0
		sleep 0.25
	done
	return 1
}

ifa_documentation_drop_ack_trigger() {
	local cell="$1" cleanup_rc
	if ifa_det_pg "${FAULT_COMPOSE_PROJECT}" "${use_compose}" "${ESHU_POSTGRES_DSN}" \
		"BEGIN; SET LOCAL lock_timeout = '2s'; SET LOCAL statement_timeout = '5s'; DROP TRIGGER IF EXISTS ifa_documentation_ack_barrier ON public.fact_work_items; COMMIT;" \
		"${compose_file}" >/dev/null; then return 0; else cleanup_rc=$?; fi
	printf '%s: documentation ACK trigger cleanup query FAILED (exit %s)\n' "${cell}" "${cleanup_rc}" >&2
	return "${cleanup_rc}"
}

ifa_documentation_drop_ack_function() {
	local cell="$1" cleanup_rc
	if ifa_det_pg "${FAULT_COMPOSE_PROJECT}" "${use_compose}" "${ESHU_POSTGRES_DSN}" \
		"BEGIN; SET LOCAL lock_timeout = '2s'; SET LOCAL statement_timeout = '5s'; DROP FUNCTION IF EXISTS public.ifa_documentation_block_ack(); COMMIT;" \
		"${compose_file}" >/dev/null; then return 0; else cleanup_rc=$?; fi
	printf '%s: documentation ACK function cleanup query FAILED (exit %s)\n' "${cell}" "${cleanup_rc}" >&2
	return "${cleanup_rc}"
}

ifa_documentation_drop_ack_barrier() {
	local cell="$1" first_rc=0 cleanup_rc
	ifa_documentation_drop_ack_trigger "${cell}" || {
		cleanup_rc=$?
		first_rc="${cleanup_rc}"
	}
	ifa_documentation_drop_ack_function "${cell}" || {
		cleanup_rc=$?
		[[ "${first_rc}" -ne 0 ]] || first_rc="${cleanup_rc}"
	}
	return "${first_rc}"
}

ifa_documentation_terminate_ack_holder() {
	local cell="$1" holder_backend_pid="$2" holder_app_name terminated query_rc
	ifa_documentation_prepare_ack_identity || return $?
	holder_app_name="ifa_documentation_ack_holder_${ifa_documentation_ack_run_id}"
	[[ "${holder_backend_pid}" =~ ^[1-9][0-9]*$ ]] || return 2
	if terminated="$(ifa_det_pg "${FAULT_COMPOSE_PROJECT}" "${use_compose}" "${ESHU_POSTGRES_DSN}" \
		"WITH target AS MATERIALIZED (SELECT holder.pid FROM pg_catalog.pg_stat_activity holder JOIN pg_catalog.pg_locks held ON held.pid = holder.pid WHERE holder.pid = ${holder_backend_pid} AND holder.application_name = '${holder_app_name}' AND held.locktype = 'advisory' AND held.classid = 5998 AND held.objid = 5994 AND held.objsubid = 2 AND held.database = (SELECT oid FROM pg_catalog.pg_database WHERE datname = pg_catalog.current_database())), terminated AS MATERIALIZED (SELECT pg_catalog.pg_terminate_backend(pid) AS stopped FROM target) SELECT count(*)::text || '|' || COALESCE(pg_catalog.bool_and(stopped), false)::text FROM terminated;" \
		"${compose_file}")"; then
		terminated="$(printf '%s' "${terminated}" | tr -d '[:space:]')"
	else
		query_rc=$?
		printf '%s: documentation ACK holder termination query FAILED (exit %s)\n' "${cell}" "${query_rc}" >&2
		return "${query_rc}"
	fi
	[[ "${terminated}" == "1|true" ]] || {
		printf '%s: exact documentation ACK holder termination returned %q, want 1|true\n' "${cell}" "${terminated}" >&2
		return 1
	}
}

ifa_documentation_join_ack_holder() {
	local holder_pid="$1"
	[[ "${holder_pid}" =~ ^[1-9][0-9]*$ ]] || return 2
	ifa_det_stop_join_untrack_bg_pid "${holder_pid}" TERM || return $?
	ifa_documentation_ack_holder_pid=""
	ifa_documentation_ack_holder_owned=0
}

ifa_documentation_release_ack_barrier() {
	local cell="$1" holder_pid="$2" holder_backend_pid="$3" census
	ifa_documentation_terminate_ack_holder "${cell}" "${holder_backend_pid}" || return $?
	ifa_documentation_wait_for_ack_backend_gone "${cell}" "${holder_backend_pid}" 5 || return $?
	census="$(ifa_documentation_census_ack_holder "${cell}")" || return $?
	[[ -z "${census}" ]] || return 1
	ifa_documentation_join_ack_holder "${holder_pid}" || return $?
	ifa_documentation_ack_holder_backend_pid=""
	if [[ "${ifa_documentation_ack_ddl_owned:-${ifa_documentation_ack_barrier_active:-0}}" == "1" ]]; then
		ifa_documentation_drop_ack_barrier "${cell}" || return $?
	fi
	ifa_documentation_ack_barrier_active=0
	ifa_documentation_ack_ddl_owned=0
	ifa_documentation_ack_barrier_cell=""
	ifa_documentation_ack_waiter_pid=""
	ifa_documentation_ack_run_id=""
}

# Signal every host ACK producer before joining any producer. This preserves
# parallel shutdown while retaining the advisory holder until DB cleanup is safe.
ifa_documentation_signal_ack_producers() {
	local failed=0 pid
	ifa_documentation_ack_producer_pids=()
	for pid in "${bg_pids[@]:-}"; do
		[[ -n "${pid}" && "${pid}" != "${ifa_documentation_ack_holder_pid:-}" ]] || continue
		ifa_documentation_ack_producer_pids+=("${pid}")
	done
	for pid in "${ifa_documentation_ack_producer_pids[@]:-}"; do
		[[ -n "${pid}" ]] || continue
		if kill -0 "${pid}" 2>/dev/null && ! kill -s TERM "${pid}" 2>/dev/null; then
			failed=1
		fi
	done
	[[ "${failed}" -eq 0 ]] || ifa_documentation_ack_producers_safe=0
	return "${failed}"
}

ifa_documentation_join_ack_producers() {
	local failed=0 pid wait_rc
	for pid in "${ifa_documentation_ack_producer_pids[@]:-}"; do
		[[ -n "${pid}" ]] || continue
		wait_rc=0
		wait "${pid}" 2>/dev/null || wait_rc=$?
		if [[ "${wait_rc}" -eq 127 ]]; then
			failed=1
			continue
		fi
		ifa_det_untrack_bg_pid "${pid}" || failed=1
	done
	ifa_documentation_ack_producer_pids=()
	[[ "${failed}" -eq 0 ]] || ifa_documentation_ack_producers_safe=0
	return "${failed}"
}

ifa_documentation_stop_ack_producers() {
	local signal_rc=0 join_rc=0
	ifa_documentation_signal_ack_producers || signal_rc=$?
	ifa_documentation_join_ack_producers || join_rc=$?
	[[ "${signal_rc}" -eq 0 && "${join_rc}" -eq 0 ]]
}

# Cleanup always discovers the exact run-tagged database sessions. Captured
# PIDs are reconciliation evidence, never authority to skip the census.
ifa_documentation_cleanup_ack_barrier() {
	local cell="$1" failed=0 waiter_safe=1 holder_safe=1 waiter holder
	local holder_owned="${ifa_documentation_ack_holder_owned:-0}"
	local ddl_owned="${ifa_documentation_ack_ddl_owned:-${ifa_documentation_ack_barrier_active:-0}}"
	[[ "${holder_owned}" == "1" || "${ddl_owned}" == "1" ]] || return 0
	ifa_documentation_prepare_ack_identity || return $?
	[[ "${ifa_documentation_ack_producers_safe:-1}" == "1" ]] || holder_safe=0
	ifa_documentation_stop_ack_producers || { failed=1; holder_safe=0; }

	if waiter="$(ifa_documentation_census_ack_waiter "${cell}")"; then
		if [[ -n "${waiter}" && -n "${ifa_documentation_ack_waiter_pid:-}" && "${waiter}" != "${ifa_documentation_ack_waiter_pid}" ]]; then
			failed=1; waiter_safe=0
		elif [[ -n "${waiter}" ]]; then
			ifa_documentation_ack_waiter_pid="${waiter}"
			if ! ifa_documentation_terminate_blocked_ack "${cell}" "${waiter}" \
				|| ! ifa_documentation_wait_for_ack_backend_gone "${cell}" "${waiter}" 5; then
				failed=1; waiter_safe=0
			fi
		elif [[ "${ifa_documentation_ack_waiter_pid:-}" =~ ^[1-9][0-9]*$ ]]; then
			ifa_documentation_wait_for_ack_backend_gone "${cell}" "${ifa_documentation_ack_waiter_pid}" 5 \
				|| { failed=1; waiter_safe=0; }
		fi
	else
		failed=1; waiter_safe=0
	fi
	if [[ "${waiter_safe}" -eq 1 ]]; then
		waiter="$(ifa_documentation_census_ack_waiter "${cell}")" || { failed=1; waiter_safe=0; }
		[[ -z "${waiter}" ]] || { failed=1; waiter_safe=0; }
	fi
	[[ "${waiter_safe}" -eq 1 ]] || holder_safe=0
	[[ "${waiter_safe}" -ne 1 ]] || ifa_documentation_ack_waiter_pid=""

	if holder="$(ifa_documentation_census_ack_holder "${cell}")"; then
		if [[ -n "${holder}" && -n "${ifa_documentation_ack_holder_backend_pid:-}" && "${holder}" != "${ifa_documentation_ack_holder_backend_pid}" ]]; then
			failed=1; holder_safe=0
		elif [[ -n "${holder}" ]]; then
			ifa_documentation_ack_holder_backend_pid="${holder}"
		elif [[ "${ifa_documentation_ack_holder_backend_pid:-}" =~ ^[1-9][0-9]*$ ]]; then
			if ifa_documentation_wait_for_ack_backend_gone "${cell}" "${ifa_documentation_ack_holder_backend_pid}" 5; then
				ifa_documentation_ack_holder_backend_pid=""
			else
				failed=1; holder_safe=0
			fi
		fi
	else
		failed=1; holder_safe=0
	fi
	if [[ "${holder_safe}" -eq 1 && -n "${ifa_documentation_ack_holder_backend_pid:-}" ]]; then
		holder="${ifa_documentation_ack_holder_backend_pid}"
		if ! ifa_documentation_terminate_ack_holder "${cell}" "${holder}" \
			|| ! ifa_documentation_wait_for_ack_backend_gone "${cell}" "${holder}" 5; then
			failed=1; holder_safe=0
		fi
	fi
	if [[ "${holder_safe}" -eq 1 ]]; then
		holder="$(ifa_documentation_census_ack_holder "${cell}")" || { failed=1; holder_safe=0; }
		[[ -z "${holder}" ]] || { failed=1; holder_safe=0; }
	fi
	if [[ "${holder_safe}" -eq 1 && -n "${ifa_documentation_ack_holder_pid:-}" ]]; then
		ifa_documentation_join_ack_holder "${ifa_documentation_ack_holder_pid}" || { failed=1; holder_safe=0; }
	fi
	[[ "${holder_safe}" -eq 1 ]] && ifa_documentation_ack_holder_backend_pid=""

	if [[ "${ddl_owned}" == "1" ]]; then
		ifa_documentation_drop_ack_trigger "${cell}" || failed=1
		ifa_documentation_drop_ack_function "${cell}" || failed=1
	fi
	if [[ "${failed}" -eq 0 ]]; then
		ifa_documentation_ack_barrier_active=0
		ifa_documentation_ack_holder_owned=0
		ifa_documentation_ack_ddl_owned=0
		ifa_documentation_ack_barrier_cell=""
		ifa_documentation_ack_run_id=""
		ifa_documentation_ack_producers_safe=1
	fi
	return "${failed}"
}
