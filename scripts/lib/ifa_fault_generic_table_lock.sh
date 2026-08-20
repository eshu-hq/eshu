#!/usr/bin/env bash
# shellcheck disable=SC2034,SC2154
# The table_lock generic blocker mechanism (split out of
# ifa_fault_generic_cells.sh -- see that file's header for why the split, and
# scripts/lib/ifa_family_registry.sh's IFA_FAMILY_CELL_KIND comment for the
# scoping call this mechanism is currently affected by).
#
# UNEXERCISED IN THIS PR: no family is registered cell_kind=generic with a
# table_lock blocker today. Two declare one -- deployable_unit_edges
# (admission_decisions) and codeowners_ownership_edges (fact_records) -- and
# BOTH keep bespoke cells. Read the precondition below with that second family
# in mind: _ifa_generic_require_table_domain_written assumes the locked table
# carries a `domain` column, which is true of admission_decisions and is NOT
# true of fact_records. Flipping codeowners to generic without addressing that
# fails loudly (the query errors, ifa_det_pg propagates the rc, the precondition
# returns it and the cell dies) -- loud, but only after a live shard has been
# spent. deployable_unit_edges keeps its own already-proven
# cell_killworker_deployable_unit / cell_failgraphwrite_deployable_unit
# (ifa_fault_injection_deployable_unit_cells.sh) rather than migrating onto
# this mechanism, so nothing in this file runs against a live stack in this
# PR. Built and unit-tested (mocked ifa_det_pg) for the next family that
# needs this shape; do not read its presence as proof it has been exercised
# live. Flip a family's registry row to cell_kind=generic only once you have
# proven this mechanism against that family's live cell.
#
# Sourced by ifa_fault_generic_cells.sh, which also supplies the driver-owned
# globals this file reads (bg_pids, log_dir, bin_dir, use_compose,
# compose_file, FAULT_COMPOSE_PROJECT, ESHU_POSTGRES_DSN, plus the
# fresh_stack / drive_all_cassettes / run_drain_gate / teardown_cell / log /
# die helpers from ifa_fault_injection_driver.sh).

# _ifa_generic_table_lock_start / _ifa_generic_table_lock_release generalize
# ifa_deployable_unit_start_admission_decisions_lock /
# ..._release_admission_decisions_lock (ifa_fault_injection_deployable_unit_lock.sh)
# over an arbitrary table name instead of a hardcoded admission_decisions.
_ifa_generic_table_lock_start() {
	local cell="$1" table="$2" pid_var="$3"
	if [[ ! "${cell}" =~ ^[a-z0-9_]+$ || ! "${table}" =~ ^[a-z0-9_]+$ ]]; then
		printf '_ifa_generic_table_lock_start: cell and table must match ^[a-z0-9_]+$\n' >&2
		return 1
	fi
	local app_name="ifa_generic_lock_${table}_${cell}"
	local lock_sql="SET application_name = '${app_name}'; BEGIN; LOCK TABLE ${table} IN ACCESS EXCLUSIVE MODE; SELECT pg_sleep(180); ROLLBACK;"
	if [[ "${use_compose}" -eq 1 ]]; then
		docker compose -p "${FAULT_COMPOSE_PROJECT}" -f "${compose_file}" exec -T postgres \
			psql -v ON_ERROR_STOP=1 -U eshu -d eshu -c "${lock_sql}" \
			>"${log_dir}/generic-${table}-lock-${cell}.log" 2>&1 &
	else
		psql "${ESHU_POSTGRES_DSN}" -v ON_ERROR_STOP=1 -c "${lock_sql}" \
			>"${log_dir}/generic-${table}-lock-${cell}.log" 2>&1 &
	fi
	local holder_pid=$!
	bg_pids+=("${holder_pid}")
	printf -v "${pid_var}" '%s' "${holder_pid}"

	local i lock_count
	for i in $(seq 1 60); do
		lock_count="$(ifa_det_pg "${FAULT_COMPOSE_PROJECT}" "${use_compose}" "${ESHU_POSTGRES_DSN}" \
			"SELECT count(*) FROM pg_locks l JOIN pg_stat_activity a ON a.pid = l.pid WHERE a.application_name = '${app_name}' AND l.relation = '${table}'::regclass AND l.mode = 'AccessExclusiveLock' AND l.granted;" \
			"${compose_file}" | tr -d '[:space:]')"
		if [[ "${lock_count}" == "1" ]]; then
			return 0
		fi
		sleep 0.25
	done
	return 1
}

_ifa_generic_table_lock_release() {
	local cell="$1" table="$2" holder_pid="$3"
	local app_name="ifa_generic_lock_${table}_${cell}"
	ifa_det_pg "${FAULT_COMPOSE_PROJECT}" "${use_compose}" "${ESHU_POSTGRES_DSN}" \
		"SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE application_name = '${app_name}';" \
		"${compose_file}" >/dev/null
	wait "${holder_pid}" 2>/dev/null || true
	ifa_det_untrack_bg_pid "${holder_pid}"
}

# _ifa_generic_require_table_domain_written is THE MANDATORY PRECONDITION
# ASSERT for this mechanism: assumes the locked table exposes a "domain"
# column (true for admission_decisions -- a future table_lock family whose
# table lacks that column needs its own variant, not a silent pass here).
# Modeled directly on ifa_deployable_unit_require_admission_decisions_written's
# fail-closed shape: query-failure, empty output, and non-numeric output are
# all treated as UNKNOWN, never as a pass or a fail in either direction; only
# a confirmed literal "0" fails the precondition. A blocker aimed at a table
# the handler never writes must fail loudly here, never pass vacuously.
_ifa_generic_require_table_domain_written() {
	local family="$1" table="$2" domain="$3" count count_rc
	if [[ ! "${table}" =~ ^[a-z0-9_]+$ || ! "${domain}" =~ ^[a-z0-9_]+$ ]]; then
		printf 'cell_killworker_family (%s): _ifa_generic_require_table_domain_written: table and domain must match ^[a-z0-9_]+$, got table=%q domain=%q\n' \
			"${family}" "${table}" "${domain}" >&2
		return 1
	fi
	if count="$(ifa_det_pg "${FAULT_COMPOSE_PROJECT}" "${use_compose}" "${ESHU_POSTGRES_DSN}" \
		"SELECT count(*) FROM ${table} WHERE domain = '${domain}';" "${compose_file}")"; then
		count_rc=0
	else
		count_rc=$?
	fi
	if [[ "${count_rc}" -ne 0 ]]; then
		printf 'cell_killworker_family (%s): PRECONDITION query on %s FAILED (exit %s); treat as unknown, not as a verdict\n' "${family}" "${table}" "${count_rc}" >&2
		return "${count_rc}"
	fi
	count="$(printf '%s' "${count}" | tr -d '[:space:]')"
	if [[ -z "${count}" || ! "${count}" =~ ^[0-9]+$ ]]; then
		printf 'cell_killworker_family (%s): PRECONDITION query on %s returned non-numeric output %q; treat as unknown, not as zero\n' "${family}" "${table}" "${count}" >&2
		return 1
	fi
	if [[ "${count}" == "0" ]]; then
		printf 'cell_killworker_family (%s): PRECONDITION FAILED: expected at least one %s row for domain=%s after an unblocked drain, got 0 -- this table_lock:%s target is a table this family'"'"'s handler never wrote for this fixture; the lock would never engage and a kill would prove ordinary baseline recovery, not domain-scoped reclaim\n' \
			"${family}" "${table}" "${domain}" "${table}" >&2
		return 1
	fi
	printf 'cell_killworker_family (%s): precondition confirmed: %s %s row(s) for domain=%s -- this table_lock target is a real write, not a race\n' "${family}" "${count}" "${table}" "${domain}"
}

# _ifa_generic_table_lock_precheck proves _ifa_generic_require_table_domain_written
# against a live, UNBLOCKED pass before the real (blocked) cell ever runs: a
# table_lock blocker cannot be checked while it is itself blocking (that is
# the whole point of blocking), so this runs its own throwaway fresh_stack,
# drive, and drain first. Costs one extra Compose cycle per table_lock family;
# accepted because "before the kill fires" (the mandatory-precondition
# mandate) rules out checking the write after the blocked pass has already
# committed to a kill.
_ifa_generic_table_lock_precheck() {
	local family="$1" table="$2" domain cassette_var cassette precheck_cell
	domain="$(ifa_family_wait_key "${family}")" || return 1
	cassette_var="$(ifa_family_cassette_var "${family}")" || return 1
	if [[ -z "${cassette_var}" ]]; then
		printf 'cell_killworker_family (%s): no cassette_var registered for the table_lock precheck\n' "${family}" >&2
		return 1
	fi
	cassette="${!cassette_var}"
	precheck_cell="genprecheck${family//_/}"
	log "generic table_lock precondition pass (${family}): fresh stack, unblocked drive+drain, verify ${table} is written for domain=${domain}"
	fresh_stack "${precheck_cell}"
	drive_all_cassettes "${precheck_cell}"
	local projector_pid reducer_pid
	ifa_det_start_bg "${log_dir}" "projector-${precheck_cell}" projector_pid "${bin_dir}/eshu-projector"
	ifa_det_start_bg "${log_dir}" "reducer-${precheck_cell}" reducer_pid "${bin_dir}/eshu-reducer"
	run_drain_gate "${precheck_cell}"
	if ! _ifa_generic_require_table_domain_written "${family}" "${table}" "${domain}"; then
		teardown_cell "${precheck_cell}"
		return 1
	fi
	ifa_det_stop_join_untrack_bg_pid "${projector_pid}" TERM || true
	ifa_det_stop_join_untrack_bg_pid "${reducer_pid}" TERM || true
	teardown_cell "${precheck_cell}"
}

# _ifa_generic_cell_killworker_table_lock is the mechanism's thin wrapper:
# run the live precheck BEFORE calling the shared skeleton
# (ifa_fault_generic_cells.sh's _ifa_generic_cell_killworker_body).
_ifa_generic_cell_killworker_table_lock() {
	local family="$1" table="$2" cell="genkillworker${1//_/}"
	_ifa_generic_table_lock_precheck "${family}" "${table}" \
		|| die "${cell}: mandatory precondition failed (see above) -- refusing to run a kill cell whose blocker cannot engage"
	_ifa_generic_cell_killworker_body "${family}" "${cell}" table_lock "${table}"
}
