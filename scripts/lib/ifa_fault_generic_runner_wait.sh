#!/usr/bin/env bash
# shellcheck disable=SC2034,SC2154
# Exact runner_lease_hold fault mechanism for shared projection reducers.
# Sourced after ifa_determinism_common.sh and ifa_fault_injection_common.sh.

_IFA_RUNNER_LEASE_NAMESPACE='shared_projection_partition_leases'

_ifa_fault_validate_runner_lease_identity() {
	local caller="$1" cell="$2" domain="$3"
	if [[ ! "${cell}" =~ ^[a-z0-9_]+$ ]]; then
		printf '%s: cell must match ^[a-z0-9_]+$, got %q\n' "${caller}" "${cell}" >&2
		return 1
	fi
	if [[ ! "${domain}" =~ ^[a-z0-9_]+$ ]]; then
		printf '%s: domain must match ^[a-z0-9_]+$, got %q\n' "${caller}" "${domain}" >&2
		return 1
	fi
	local app_name="ifa_rlh_${domain}_${cell}"
	if [[ "${#app_name}" -gt 63 ]]; then
		printf '%s: derived application_name exceeds PostgreSQL 63-byte limit: %s\n' \
			"${caller}" "${app_name}" >&2
		return 1
	fi
}

_ifa_fault_runner_lease_app_name() {
	printf 'ifa_rlh_%s_%s' "$2" "$1"
}

_ifa_fault_compact_sql_output() {
	local value="$1"
	value="${value//[[:space:]]/}"
	printf '%s' "${value}"
}

_ifa_fault_runner_lease_key_predicate() {
	local alias="$1" domain="$2"
	printf "%s.locktype = 'advisory' AND %s.database = (SELECT oid FROM pg_catalog.pg_database WHERE datname = pg_catalog.current_database()) AND %s.classid::bigint = ((hashtext('%s')::bigint + 4294967296) %% 4294967296) AND %s.objid::bigint = ((hashtext('%s')::bigint + 4294967296) %% 4294967296) AND %s.objsubid = 2" \
		"${alias}" "${alias}" "${alias}" "${_IFA_RUNNER_LEASE_NAMESPACE}" \
		"${alias}" "${domain}" "${alias}"
}

_ifa_fault_stop_runner_lease_holder_client() {
	local cell="$1" domain="$2" holder_pid="$3"
	local app_name key_predicate
	app_name="$(_ifa_fault_runner_lease_app_name "${cell}" "${domain}")"
	key_predicate="$(_ifa_fault_runner_lease_key_predicate held "${domain}")"
	# Startup rollback may legitimately find no matching backend.
	ifa_det_pg "${FAULT_COMPOSE_PROJECT}" "${use_compose}" "${ESHU_POSTGRES_DSN}" \
		"WITH target AS MATERIALIZED (SELECT holder.pid FROM pg_catalog.pg_stat_activity holder JOIN pg_catalog.pg_locks held ON held.pid = holder.pid WHERE holder.application_name = '${app_name}' AND held.granted AND ${key_predicate}) SELECT pg_catalog.pg_terminate_backend(pid) FROM target;" \
		"${compose_file}" >/dev/null 2>&1 || true
	kill "${holder_pid}" 2>/dev/null || true
	wait "${holder_pid}" 2>/dev/null || true
	ifa_det_untrack_bg_pid "${holder_pid}"
}

# ifa_fault_start_runner_lease_hold starts one labeled transaction whose
# two-int advisory key is the production runner lease key. A separate
# connection must prove the key unavailable before this returns.
ifa_fault_start_runner_lease_hold() {
	local cell="$1" domain="$2" pid_var="$3"
	_ifa_fault_validate_runner_lease_identity "ifa_fault_start_runner_lease_hold" "${cell}" "${domain}" || return $?
	if [[ ! "${pid_var}" =~ ^[a-zA-Z_][a-zA-Z0-9_]*$ ]]; then
		printf 'ifa_fault_start_runner_lease_hold: out-pid-var is not a shell identifier: %q\n' "${pid_var}" >&2
		return 1
	fi
	local app_name lock_sql holder_client_pid
	app_name="$(_ifa_fault_runner_lease_app_name "${cell}" "${domain}")"
	lock_sql="SET application_name = '${app_name}'; BEGIN; SELECT pg_advisory_xact_lock(hashtext('${_IFA_RUNNER_LEASE_NAMESPACE}'), hashtext('${domain}')); SELECT pg_sleep(180); ROLLBACK;"
	if [[ "${use_compose}" -eq 1 ]]; then
		docker compose -p "${FAULT_COMPOSE_PROJECT}" -f "${compose_file}" exec -T postgres \
			psql -v ON_ERROR_STOP=1 -U eshu -d eshu -c "${lock_sql}" \
			>"${log_dir}/runner-lease-hold-${domain}-${cell}.log" 2>&1 &
	else
		psql "${ESHU_POSTGRES_DSN}" -v ON_ERROR_STOP=1 -c "${lock_sql}" \
			>"${log_dir}/runner-lease-hold-${domain}-${cell}.log" 2>&1 &
	fi
	holder_client_pid=$!
	bg_pids+=("${holder_client_pid}")
	printf -v "${pid_var}" '%s' "${holder_client_pid}"

	local held_sql key_predicate sample sample_rc i
	key_predicate="$(_ifa_fault_runner_lease_key_predicate held "${domain}")"
	held_sql="SELECT CASE WHEN NOT pg_try_advisory_xact_lock(hashtext('${_IFA_RUNNER_LEASE_NAMESPACE}'), hashtext('${domain}')) AND EXISTS (SELECT 1 FROM pg_catalog.pg_stat_activity holder JOIN pg_catalog.pg_locks held ON held.pid = holder.pid WHERE holder.application_name = '${app_name}' AND held.granted AND ${key_predicate}) THEN 1 ELSE 0 END;"
	for i in $(seq 1 60); do
		if sample="$(ifa_det_pg "${FAULT_COMPOSE_PROJECT}" "${use_compose}" "${ESHU_POSTGRES_DSN}" "${held_sql}" "${compose_file}")"; then
			sample="$(_ifa_fault_compact_sql_output "${sample}")"
			if [[ "${sample}" == "1" ]]; then return 0; fi
			if [[ ! "${sample}" =~ ^[01]$ ]]; then
				printf 'ifa_fault_start_runner_lease_hold: held check returned ambiguous output %q\n' "${sample}" >&2
				_ifa_fault_stop_runner_lease_holder_client "${cell}" "${domain}" "${holder_client_pid}"
				return 1
			fi
		else
			sample_rc=$?
			printf 'ifa_fault_start_runner_lease_hold: held check FAILED (exit %s); state is unknown\n' "${sample_rc}" >&2
			_ifa_fault_stop_runner_lease_holder_client "${cell}" "${domain}" "${holder_client_pid}"
			return "${sample_rc}"
		fi
		sleep 0.25
	done
	printf 'ifa_fault_start_runner_lease_hold: exact advisory key was not confirmed held for domain=%s\n' "${domain}" >&2
	_ifa_fault_stop_runner_lease_holder_client "${cell}" "${domain}" "${holder_client_pid}"
	return 1
}

_ifa_fault_count_exact_runner_lease_waiters() {
	local compose_project="$1" use_compose_arg="$2" dsn="$3" compose_file_arg="$4"
	local domain="$5" cell="$6" phase="$7" app_name held_key output query_rc
	app_name="$(_ifa_fault_runner_lease_app_name "${cell}" "${domain}")"
	held_key="$(_ifa_fault_runner_lease_key_predicate held "${domain}")"
	if output="$(ifa_det_pg "${compose_project}" "${use_compose_arg}" "${dsn}" \
		"/* runner_lease_hold exact waiter ${phase} */ SELECT count(DISTINCT holder.pid)::text || '|' || count(DISTINCT waiter.pid)::text FROM pg_catalog.pg_stat_activity holder JOIN pg_catalog.pg_locks held ON held.pid = holder.pid JOIN pg_catalog.pg_locks lock_row ON lock_row.locktype = held.locktype AND lock_row.database = held.database AND lock_row.classid = held.classid AND lock_row.objid = held.objid AND lock_row.objsubid = held.objsubid JOIN pg_catalog.pg_stat_activity waiter ON waiter.pid = lock_row.pid WHERE holder.application_name = '${app_name}' AND held.granted AND ${held_key} AND NOT lock_row.granted AND waiter.wait_event_type = 'Lock' AND waiter.pid <> holder.pid;" \
		"${compose_file_arg}")"; then
		output="$(_ifa_fault_compact_sql_output "${output}")"
	else
		query_rc=$?
		printf 'runner_lease_hold %s waiter query FAILED (exit %s); state is unknown\n' "${phase}" "${query_rc}" >&2
		return "${query_rc}"
	fi
	if [[ ! "${output}" =~ ^[0-9]+\|[0-9]+$ ]]; then
		printf 'runner_lease_hold %s waiter query returned ambiguous output %q; state is unknown\n' "${phase}" "${output}" >&2
		return 1
	fi
	printf '%s' "${output}"
}

_ifa_fault_count_exact_runner_lease_rows() {
	local compose_project="$1" use_compose_arg="$2" dsn="$3" compose_file_arg="$4" domain="$5"
	local lock_key output query_rc
	lock_key="$(_ifa_fault_runner_lease_key_predicate lock_row "${domain}")"
	if output="$(ifa_det_pg "${compose_project}" "${use_compose_arg}" "${dsn}" \
		"/* runner_lease_hold waiter drain */ SELECT count(*) FROM pg_catalog.pg_locks lock_row WHERE ${lock_key};" \
		"${compose_file_arg}")"; then
		output="$(_ifa_fault_compact_sql_output "${output}")"
	else
		query_rc=$?
		printf 'runner_lease_hold drain query FAILED (exit %s); state is unknown\n' "${query_rc}" >&2
		return "${query_rc}"
	fi
	if [[ ! "${output}" =~ ^[0-9]+$ ]]; then
		printf 'runner_lease_hold drain query returned ambiguous output %q; state is unknown\n' "${output}" >&2
		return 1
	fi
	printf '%s' "${output}"
}

# Called after the reducer client has been killed and joined. Prove an
# orphaned exact-key request still exists before holder termination, then
# prove every such request gone before a replacement reducer may start.
ifa_fault_release_runner_lease_hold() {
	local cell="$1" domain="$2" holder_pid="$3"
	_ifa_fault_validate_runner_lease_identity "ifa_fault_release_runner_lease_hold" "${cell}" "${domain}" || return $?
	if [[ ! "${holder_pid}" =~ ^[1-9][0-9]*$ ]]; then
		printf 'ifa_fault_release_runner_lease_hold: holder-pid must be positive, got %q\n' "${holder_pid}" >&2
		return 1
	fi
	local holder_count waiter_count waiter_rc holder_waiter_pair
	if holder_waiter_pair="$(_ifa_fault_count_exact_runner_lease_waiters "${FAULT_COMPOSE_PROJECT}" "${use_compose}" "${ESHU_POSTGRES_DSN}" "${compose_file}" "${domain}" "${cell}" 'precheck')"; then :; else waiter_rc=$?; return "${waiter_rc}"; fi
	IFS='|' read -r holder_count waiter_count <<<"${holder_waiter_pair}"
	if [[ "${holder_count}" != "1" ]]; then
		printf 'ifa_fault_release_runner_lease_hold: expected one exact labeled holder before release, observed %s\n' "${holder_count}" >&2
		return 1
	fi
	if [[ "${waiter_count}" == "0" ]]; then
		printf 'ifa_fault_release_runner_lease_hold: no exact waiter remained before holder release for domain=%s; refusing vacuous recovery\n' "${domain}" >&2
		return 1
	fi

	local app_name held_key terminated termination_rc=0
	app_name="$(_ifa_fault_runner_lease_app_name "${cell}" "${domain}")"
	held_key="$(_ifa_fault_runner_lease_key_predicate held "${domain}")"
	if terminated="$(ifa_det_pg "${FAULT_COMPOSE_PROJECT}" "${use_compose}" "${ESHU_POSTGRES_DSN}" \
		"WITH target AS MATERIALIZED (SELECT DISTINCT holder.pid FROM pg_catalog.pg_stat_activity holder JOIN pg_catalog.pg_locks held ON held.pid = holder.pid WHERE holder.application_name = '${app_name}' AND held.granted AND ${held_key}), terminated AS MATERIALIZED (SELECT pg_catalog.pg_terminate_backend(pid) AS stopped FROM target) SELECT count(*)::text || '|' || COALESCE(pg_catalog.bool_and(stopped), false)::text FROM terminated;" \
		"${compose_file}")"; then
		terminated="$(_ifa_fault_compact_sql_output "${terminated}")"
		if [[ "${terminated}" != "1|true" && "${terminated}" != "1|t" ]]; then
			printf 'ifa_fault_release_runner_lease_hold: exact holder termination returned %q, want 1|true\n' "${terminated}" >&2
			termination_rc=1
		fi
	else
		termination_rc=$?
		printf 'ifa_fault_release_runner_lease_hold: holder termination FAILED (exit %s); state is unknown\n' "${termination_rc}" >&2
	fi

	kill "${holder_pid}" 2>/dev/null || true
	wait "${holder_pid}" 2>/dev/null || true
	ifa_det_untrack_bg_pid "${holder_pid}"

	local i remaining drain_rc
	for i in $(seq 1 60); do
		if remaining="$(_ifa_fault_count_exact_runner_lease_rows "${FAULT_COMPOSE_PROJECT}" "${use_compose}" "${ESHU_POSTGRES_DSN}" "${compose_file}" "${domain}")"; then
			if [[ "${remaining}" == "0" ]]; then
				[[ "${termination_rc}" -eq 0 ]] && return 0
				return "${termination_rc}"
			fi
		else
			drain_rc=$?
			return "${drain_rc}"
		fi
		sleep 0.25
	done
	printf 'ifa_fault_release_runner_lease_hold: exact advisory waiters did not drain for domain=%s\n' "${domain}" >&2
	return 1
}

_ifa_fault_runner_lease_observation() {
	local compose_project="$1" use_compose_arg="$2" dsn="$3" compose_file_arg="$4"
	local domain="$5" cell="$6"
	_ifa_fault_validate_runner_lease_identity "runner_lease_hold observation" "${cell}" "${domain}" || return $?
	local app_name held_key observation query_rc
	app_name="$(_ifa_fault_runner_lease_app_name "${cell}" "${domain}")"
	held_key="$(_ifa_fault_runner_lease_key_predicate held "${domain}")"
	if observation="$(ifa_det_pg "${compose_project}" "${use_compose_arg}" "${dsn}" \
		"WITH pending AS (SELECT count(*) AS n FROM shared_projection_intents WHERE projection_domain = '${domain}' AND completed_at IS NULL), lock_state AS (SELECT count(DISTINCT holder.pid) AS holders, count(DISTINCT waiter.pid) AS waiters FROM pg_catalog.pg_stat_activity holder JOIN pg_catalog.pg_locks held ON held.pid = holder.pid LEFT JOIN pg_catalog.pg_locks waiting ON waiting.locktype = held.locktype AND waiting.database = held.database AND waiting.classid = held.classid AND waiting.objid = held.objid AND waiting.objsubid = held.objsubid AND NOT waiting.granted LEFT JOIN pg_catalog.pg_stat_activity waiter ON waiter.pid = waiting.pid AND waiter.wait_event_type = 'Lock' AND waiter.pid <> holder.pid WHERE holder.application_name = '${app_name}' AND held.granted AND ${held_key}) SELECT pending.n::text || '|' || lock_state.holders::text || '|' || lock_state.waiters::text FROM pending CROSS JOIN lock_state;" \
		"${compose_file_arg}")"; then
		observation="$(_ifa_fault_compact_sql_output "${observation}")"
	else
		query_rc=$?
		printf 'runner_lease_hold observation query FAILED (exit %s); state is unknown\n' "${query_rc}" >&2
		return "${query_rc}"
	fi
	if [[ ! "${observation}" =~ ^[0-9]+\|[0-9]+\|[0-9]+$ ]]; then
		printf 'runner_lease_hold observation returned ambiguous output %q; state is unknown\n' "${observation}" >&2
		return 1
	fi
	printf '%s' "${observation}"
}

ifa_fault_require_no_projection_intent_waiter() {
	local compose_project="$1" use_compose_arg="$2" dsn="$3" compose_file_arg="$4"
	local domain="$5" cell="$6" observation rc pending holders waiters
	_ifa_fault_validate_runner_lease_identity "ifa_fault_require_no_projection_intent_waiter" "${cell}" "${domain}" || return $?
	if observation="$(_ifa_fault_runner_lease_observation "${compose_project}" "${use_compose_arg}" "${dsn}" "${compose_file_arg}" "${domain}" "${cell}")"; then :; else rc=$?; return "${rc}"; fi
	IFS='|' read -r pending holders waiters <<<"${observation}"
	if [[ "${holders}" != "1" || "${waiters}" != "0" ]]; then
		printf 'ifa_fault_require_no_projection_intent_waiter: want exactly one holder and zero waiters, observed pending=%s holder=%s waiter=%s\n' "${pending}" "${holders}" "${waiters}" >&2
		return 1
	fi
	printf 'pending=%s holder=%s waiter=%s' "${pending}" "${holders}" "${waiters}"
}

ifa_fault_wait_for_claimed_projection_intent() {
	local compose_project="$1" use_compose_arg="$2" dsn="$3" compose_file_arg="$4"
	local budget="${5:-60}" domain="$6" cell="$7"
	if [[ ! "${budget}" =~ ^[1-9][0-9]*$ ]]; then
		printf 'ifa_fault_wait_for_claimed_projection_intent: budget must be a positive integer, got %q\n' "${budget}" >&2
		return 1
	fi
	_ifa_fault_validate_runner_lease_identity "ifa_fault_wait_for_claimed_projection_intent" "${cell}" "${domain}" || return $?
	local observation rc pending holders waiters i
	for i in $(seq 1 $((budget * 4))); do
		if observation="$(_ifa_fault_runner_lease_observation "${compose_project}" "${use_compose_arg}" "${dsn}" "${compose_file_arg}" "${domain}" "${cell}")"; then :; else rc=$?; return "${rc}"; fi
		IFS='|' read -r pending holders waiters <<<"${observation}"
		if (( pending > 0 && holders == 1 && waiters > 0 )); then
			printf '%s|%s' "${pending}" "${waiters}"
			return 0
		fi
		sleep 0.25
	done
	printf 'ifa_fault_wait_for_claimed_projection_intent: no pending intent plus exact advisory waiter appeared for domain=%s within %ss\n' "${domain}" "${budget}" >&2
	return 1
}
