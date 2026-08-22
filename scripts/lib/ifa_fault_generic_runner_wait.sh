#!/usr/bin/env bash
# shellcheck disable=SC2034,SC2154
# Exact runner_lease_hold fault mechanism for shared projection reducers.
# Sourced after ifa_determinism_common.sh and ifa_fault_injection_common.sh.

_IFA_RUNNER_LEASE_NAMESPACE='shared_projection_partition_leases'
_IFA_RUNNER_LEASE_AUDIT_TABLE="ifa_runner_lease_audit_$$"
_IFA_RUNNER_LEASE_AUDIT_FUNCTION="ifa_runner_lease_audit_capture_$$"
_IFA_RUNNER_LEASE_AUDIT_ATTEMPT_TRIGGER="ifa_runner_lease_audit_attempt_$$"
_IFA_RUNNER_LEASE_AUDIT_TRANSITION_TRIGGER="ifa_runner_lease_audit_transition_$$"
ifa_runner_lease_audit_owned=0
ifa_runner_lease_audit_cell=""

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

_ifa_fault_validate_runner_partition_lease_snapshot() {
	local reducer_pid="$1" snapshot="$2" row partition_id partition_count owner expires_epoch updated_epoch
	local owner_re epoch_re='^[0-9]+([.][0-9]+)?$' seen=$'\n'
	[[ "${reducer_pid}" =~ ^[1-9][0-9]*$ && -n "${snapshot}" ]] || return 1
	owner_re="^[A-Za-z0-9._-]+:[A-Za-z0-9._-]+:${reducer_pid}:[0-9a-f]{16,32}$"
	while IFS='|' read -r partition_id partition_count owner expires_epoch updated_epoch; do
		row="${partition_id}|${partition_count}|${owner}|${expires_epoch}|${updated_epoch}"
		[[ "${partition_id}" =~ ^[0-9]+$ && "${partition_count}" =~ ^[1-9][0-9]*$ ]] || return 1
		(( 10#${partition_id} < 10#${partition_count} )) || return 1
		[[ "${owner}" =~ ${owner_re} && "${expires_epoch}" =~ ${epoch_re} && "${updated_epoch}" =~ ${epoch_re} ]] || return 1
		[[ "${seen}" != *$'\n'"${row}"$'\n'* ]] || return 1
		seen+="${row}"$'\n'
	done <<<"${snapshot}"
}

_ifa_fault_runner_partition_lease_values() {
	local snapshot="$1" output_var="$2" count_var="$3"
	local partition_id partition_count owner expires_epoch updated_epoch rendered="" row_count=0
	while IFS='|' read -r partition_id partition_count owner expires_epoch updated_epoch; do
		[[ -z "${rendered}" ]] || rendered+=","
		rendered+="(${partition_id},${partition_count},'${owner}',to_timestamp(${expires_epoch}),to_timestamp(${updated_epoch}))"
		row_count=$((row_count + 1))
	done <<<"${snapshot}"
	printf -v "${output_var}" '%s' "${rendered}"
	printf -v "${count_var}" '%s' "${row_count}"
}

ifa_fault_capture_runner_partition_leases() {
	local cell="$1" domain="$2" reducer_pid="$3" minimum_remaining="$4" output_var="$5"
	local budget="${6:-${CLAIMED_ROW_WAIT_TIMEOUT:-30}}" snapshot query_rc owner_re i
	_ifa_fault_validate_runner_lease_identity "runner durable lease capture" "${cell}" "${domain}" || return $?
	[[ "${reducer_pid}" =~ ^[1-9][0-9]*$ && "${minimum_remaining}" =~ ^[1-9][0-9]*$ && "${output_var}" =~ ^[a-zA-Z_][a-zA-Z0-9_]*$ && "${budget}" =~ ^[1-9][0-9]*$ ]] || return 2
	owner_re="^[A-Za-z0-9._-]+:[A-Za-z0-9._-]+:${reducer_pid}:[0-9a-f]{16,32}$"
	for i in $(seq 1 "$((budget * 4))"); do
		if snapshot="$(ifa_det_pg "${FAULT_COMPOSE_PROJECT}" "${use_compose}" "${ESHU_POSTGRES_DSN}" \
			"/* runner_lease_hold capture durable leases */ SELECT COALESCE(string_agg(partition_id::text || '|' || partition_count::text || '|' || lease_owner || '|' || extract(epoch FROM lease_expires_at)::numeric(20,6)::text || '|' || extract(epoch FROM updated_at)::numeric(20,6)::text, E'\\n' ORDER BY partition_id, partition_count, lease_owner), '') FROM shared_projection_partition_leases WHERE projection_domain = '${domain}' AND lease_expires_at > clock_timestamp() + INTERVAL '${minimum_remaining} seconds' AND lease_owner ~ '${owner_re}';" \
			"${compose_file}")"; then
			if [[ -n "${snapshot}" ]]; then
				_ifa_fault_validate_runner_partition_lease_snapshot "${reducer_pid}" "${snapshot}" || return 1
				printf -v "${output_var}" '%s' "${snapshot}"
				return 0
			fi
		else
			query_rc=$?
			printf '%s: durable runner lease capture FAILED (exit %s); state is unknown\n' "${cell}" "${query_rc}" >&2
			return "${query_rc}"
		fi
		sleep 0.25
	done
	printf '%s: expected nonzero active %s partition leases owned by reducer PID %s\n' "${cell}" "${domain}" "${reducer_pid}" >&2
	return 1
}

ifa_fault_wait_for_runner_lease_attempt_fenced() {
	local cell="$1" domain="$2" replacement_pid="$3" captured="$4" budget="$5"
	local values expected result query_rc owner_re i
	_ifa_fault_validate_runner_lease_identity "runner durable lease fence" "${cell}" "${domain}" || return $?
	[[ "${replacement_pid}" =~ ^[1-9][0-9]*$ && "${budget}" =~ ^[1-9][0-9]*$ ]] || return 2
	local dead_owner="${captured#*|}"; dead_owner="${dead_owner#*|}"; dead_owner="${dead_owner%%|*}"
	local dead_pid="${dead_owner%:*}"; dead_pid="${dead_pid##*:}"
	_ifa_fault_validate_runner_partition_lease_snapshot "${dead_pid}" "${captured}" || return 2
	[[ "${replacement_pid}" != "${dead_pid}" ]] || return 2
	_ifa_fault_runner_partition_lease_values "${captured}" values expected
	owner_re="^[A-Za-z0-9._-]+:[A-Za-z0-9._-]+:${replacement_pid}:[0-9a-f]{16,32}$"
	for i in $(seq 1 "$((budget * 4))"); do
		if result="$(ifa_det_pg "${FAULT_COMPOSE_PROJECT}" "${use_compose}" "${ESHU_POSTGRES_DSN}" \
			"/* runner_lease_hold pre-expiry replacement attempt fence */ WITH captured(partition_id, partition_count, dead_owner, dead_expiry, dead_updated) AS (VALUES ${values}) SELECT (SELECT count(lease.partition_id) FROM captured JOIN shared_projection_partition_leases AS lease ON lease.projection_domain = '${domain}' AND lease.partition_id = captured.partition_id AND lease.partition_count = captured.partition_count)::text || '|' || (SELECT count(*) FROM captured JOIN shared_projection_partition_leases AS lease ON lease.projection_domain = '${domain}' AND lease.partition_id = captured.partition_id AND lease.partition_count = captured.partition_count WHERE lease.lease_owner = captured.dead_owner AND lease.lease_expires_at > clock_timestamp())::text || '|' || (SELECT count(DISTINCT (audit.partition_id, audit.partition_count)) FROM captured JOIN ${_IFA_RUNNER_LEASE_AUDIT_TABLE} AS audit ON audit.projection_domain = '${domain}' AND audit.partition_id = captured.partition_id AND audit.partition_count = captured.partition_count WHERE audit.event_kind = 'attempt' AND audit.lease_owner ~ '${owner_re}' AND audit.observed_at < captured.dead_expiry)::text;" \
			"${compose_file}")"; then :; else
			query_rc=$?
			printf '%s: pre-expiry replacement attempt query FAILED (exit %s); state is unknown\n' "${cell}" "${query_rc}" >&2
			return "${query_rc}"
		fi
		result="$(_ifa_fault_compact_sql_output "${result}")"
		[[ "${result}" == "${expected}|${expected}|${expected}" ]] && return 0
		sleep 0.25
	done
	printf '%s: replacement did not attempt every captured lease while the dead-owner rows remained active (observed %s)\n' "${cell}" "${result}" >&2
	return 1
}

ifa_fault_install_runner_lease_audit() {
	local cell="$1" domain="$2" captured="$3" query_rc
	_ifa_fault_validate_runner_lease_identity "runner durable lease audit" "${cell}" "${domain}" || return $?
	local dead_owner="${captured#*|}"; dead_owner="${dead_owner#*|}"; dead_owner="${dead_owner%%|*}"
	local dead_pid="${dead_owner%:*}"; dead_pid="${dead_pid##*:}"
	_ifa_fault_validate_runner_partition_lease_snapshot "${dead_pid}" "${captured}" || return 2
	ifa_runner_lease_audit_owned=1; ifa_runner_lease_audit_cell="${cell}"
	if ifa_det_pg "${FAULT_COMPOSE_PROJECT}" "${use_compose}" "${ESHU_POSTGRES_DSN}" \
		"/* runner_lease_hold install durable lease audit */ DROP TRIGGER IF EXISTS ${_IFA_RUNNER_LEASE_AUDIT_ATTEMPT_TRIGGER} ON shared_projection_partition_leases; DROP TRIGGER IF EXISTS ${_IFA_RUNNER_LEASE_AUDIT_TRANSITION_TRIGGER} ON shared_projection_partition_leases; DROP FUNCTION IF EXISTS ${_IFA_RUNNER_LEASE_AUDIT_FUNCTION}(); DROP TABLE IF EXISTS ${_IFA_RUNNER_LEASE_AUDIT_TABLE}; CREATE TABLE ${_IFA_RUNNER_LEASE_AUDIT_TABLE} (event_kind TEXT NOT NULL, projection_domain TEXT NOT NULL, partition_id INTEGER NOT NULL, partition_count INTEGER NOT NULL, lease_owner TEXT NOT NULL, observed_at TIMESTAMPTZ NOT NULL, lease_expires_at TIMESTAMPTZ NOT NULL); CREATE FUNCTION ${_IFA_RUNNER_LEASE_AUDIT_FUNCTION}() RETURNS trigger LANGUAGE plpgsql AS \$\$ BEGIN IF TG_WHEN = 'BEFORE' THEN INSERT INTO ${_IFA_RUNNER_LEASE_AUDIT_TABLE} VALUES ('attempt', NEW.projection_domain, NEW.partition_id, NEW.partition_count, NEW.lease_owner, clock_timestamp(), NEW.lease_expires_at); RETURN NEW; END IF; IF NEW.lease_owner IS NOT NULL AND NEW.lease_expires_at IS NOT NULL THEN INSERT INTO ${_IFA_RUNNER_LEASE_AUDIT_TABLE} VALUES ('transition', NEW.projection_domain, NEW.partition_id, NEW.partition_count, NEW.lease_owner, clock_timestamp(), NEW.lease_expires_at); END IF; RETURN NEW; END \$\$; CREATE TRIGGER ${_IFA_RUNNER_LEASE_AUDIT_ATTEMPT_TRIGGER} BEFORE INSERT ON shared_projection_partition_leases FOR EACH ROW EXECUTE FUNCTION ${_IFA_RUNNER_LEASE_AUDIT_FUNCTION}(); CREATE TRIGGER ${_IFA_RUNNER_LEASE_AUDIT_TRANSITION_TRIGGER} AFTER INSERT OR UPDATE ON shared_projection_partition_leases FOR EACH ROW EXECUTE FUNCTION ${_IFA_RUNNER_LEASE_AUDIT_FUNCTION}();" \
		"${compose_file}" >/dev/null; then
		return 0
	fi
	query_rc=$?
	printf '%s: durable lease audit installation FAILED (exit %s); state is unknown\n' "${cell}" "${query_rc}" >&2
	return "${query_rc}"
}

ifa_fault_wait_for_runner_lease_expiry() {
	local cell="$1" captured="$2" budget="$3" values expected result query_rc i
	[[ "${budget}" =~ ^[1-9][0-9]*$ ]] || return 2
	local dead_owner="${captured#*|}"; dead_owner="${dead_owner#*|}"; dead_owner="${dead_owner%%|*}"
	local dead_pid="${dead_owner%:*}"; dead_pid="${dead_pid##*:}"
	_ifa_fault_validate_runner_partition_lease_snapshot "${dead_pid}" "${captured}" || return 2
	_ifa_fault_runner_partition_lease_values "${captured}" values expected
	for i in $(seq 1 "$((budget * 4))"); do
		if result="$(ifa_det_pg "${FAULT_COMPOSE_PROJECT}" "${use_compose}" "${ESHU_POSTGRES_DSN}" \
			"/* runner_lease_hold wait captured expiry */ WITH captured(partition_id, partition_count, dead_owner, dead_expiry, dead_updated) AS (VALUES ${values}) SELECT CASE WHEN MAX(captured.dead_expiry) <= clock_timestamp() THEN count(*) ELSE 0 END FROM captured;" \
			"${compose_file}")"; then :; else query_rc=$?; return "${query_rc}"; fi
		result="$(_ifa_fault_compact_sql_output "${result}")"
		[[ "${result}" == "${expected}" ]] && return 0
		sleep 0.25
	done
	printf '%s: captured dead-owner runner leases did not reach their expiry boundary\n' "${cell}" >&2
	return 1
}

ifa_fault_wait_for_replacement_runner_lease_audit() {
	local cell="$1" domain="$2" replacement_pid="$3" captured="$4" budget="$5"
	local values expected result query_rc owner_re i
	_ifa_fault_validate_runner_lease_identity "replacement durable lease audit" "${cell}" "${domain}" || return $?
	[[ "${replacement_pid}" =~ ^[1-9][0-9]*$ && "${budget}" =~ ^[1-9][0-9]*$ ]] || return 2
	local dead_owner="${captured#*|}"; dead_owner="${dead_owner#*|}"; dead_owner="${dead_owner%%|*}"
	local dead_pid="${dead_owner%:*}"; dead_pid="${dead_pid##*:}"
	_ifa_fault_validate_runner_partition_lease_snapshot "${dead_pid}" "${captured}" || return 2
	[[ "${replacement_pid}" != "${dead_pid}" ]] || return 2
	_ifa_fault_runner_partition_lease_values "${captured}" values expected
	owner_re="^[A-Za-z0-9._-]+:[A-Za-z0-9._-]+:${replacement_pid}:[0-9a-f]{16,32}$"
	for i in $(seq 1 "$((budget * 4))"); do
		if result="$(ifa_det_pg "${FAULT_COMPOSE_PROJECT}" "${use_compose}" "${ESHU_POSTGRES_DSN}" \
			"/* runner_lease_hold replacement durable lease audit */ WITH captured(partition_id, partition_count, dead_owner, dead_expiry, dead_updated) AS (VALUES ${values}) SELECT count(DISTINCT (audit.partition_id, audit.partition_count)) FROM captured JOIN ${_IFA_RUNNER_LEASE_AUDIT_TABLE} AS audit ON audit.projection_domain = '${domain}' AND audit.partition_id = captured.partition_id AND audit.partition_count = captured.partition_count WHERE audit.event_kind = 'transition' AND audit.lease_owner ~ '${owner_re}';" \
			"${compose_file}")"; then :; else query_rc=$?; return "${query_rc}"; fi
		result="$(_ifa_fault_compact_sql_output "${result}")"
		[[ "${result}" == "${expected}" ]] && return 0
		sleep 0.25
	done
	printf '%s: replacement audit covered %s of %s captured %s leases\n' "${cell}" "${result}" "${expected}" "${domain}" >&2
	return 1
}

ifa_fault_drop_runner_lease_audit() {
	local cell="$1" query_rc
	if ifa_det_pg "${FAULT_COMPOSE_PROJECT}" "${use_compose}" "${ESHU_POSTGRES_DSN}" \
		"/* runner_lease_hold drop durable lease audit */ DROP TRIGGER IF EXISTS ${_IFA_RUNNER_LEASE_AUDIT_ATTEMPT_TRIGGER} ON shared_projection_partition_leases; DROP TRIGGER IF EXISTS ${_IFA_RUNNER_LEASE_AUDIT_TRANSITION_TRIGGER} ON shared_projection_partition_leases; DROP FUNCTION IF EXISTS ${_IFA_RUNNER_LEASE_AUDIT_FUNCTION}(); DROP TABLE IF EXISTS ${_IFA_RUNNER_LEASE_AUDIT_TABLE};" \
		"${compose_file}" >/dev/null; then
		ifa_runner_lease_audit_owned=0; ifa_runner_lease_audit_cell=""
		return 0
	fi
	query_rc=$?
	printf '%s: durable lease audit cleanup FAILED (exit %s)\n' "${cell}" "${query_rc}" >&2
	return "${query_rc}"
}

ifa_fault_cleanup_runner_lease_audit() {
	[[ "${ifa_runner_lease_audit_owned:-0}" -eq 1 ]] || return 0
	ifa_fault_drop_runner_lease_audit "${ifa_runner_lease_audit_cell:-runner_lease_audit}"
}

ifa_fault_require_runner_leases_reclaimed() {
	local cell="$1" domain="$2" captured="$3" values expected result query_rc
	_ifa_fault_validate_runner_lease_identity "post-reclaim durable lease release" "${cell}" "${domain}" || return $?
	local dead_owner="${captured#*|}"; dead_owner="${dead_owner#*|}"; dead_owner="${dead_owner%%|*}"
	local dead_pid="${dead_owner%:*}"; dead_pid="${dead_pid##*:}"
	_ifa_fault_validate_runner_partition_lease_snapshot "${dead_pid}" "${captured}" || return 2
	_ifa_fault_runner_partition_lease_values "${captured}" values expected
	if result="$(ifa_det_pg "${FAULT_COMPOSE_PROJECT}" "${use_compose}" "${ESHU_POSTGRES_DSN}" \
		"/* runner_lease_hold post-reclaim durable lease release */ WITH captured(partition_id, partition_count, dead_owner, dead_expiry, dead_updated) AS (VALUES ${values}) SELECT count(*)::text || '|' || count(*) FILTER (WHERE lease.lease_owner IS NULL AND lease.lease_expires_at IS NULL)::text || '|' || count(*) FILTER (WHERE lease.updated_at > captured.dead_updated)::text FROM captured LEFT JOIN shared_projection_partition_leases AS lease ON lease.projection_domain = '${domain}' AND lease.partition_id = captured.partition_id AND lease.partition_count = captured.partition_count;" \
		"${compose_file}")"; then :; else
		query_rc=$?
		printf '%s: post-reclaim durable lease query FAILED (exit %s); state is unknown\n' "${cell}" "${query_rc}" >&2
		return "${query_rc}"
	fi
	result="$(_ifa_fault_compact_sql_output "${result}")"
	[[ "${result}" == "${expected}|${expected}|${expected}" ]] || {
		printf '%s: captured %s durable leases were not all claimed after expiry and released (observed %s)\n' "${cell}" "${domain}" "${result}" >&2
		return 1
	}
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
	if holder_waiter_pair="$(_ifa_fault_count_exact_runner_lease_waiters "${FAULT_COMPOSE_PROJECT}" "${use_compose}" "${ESHU_POSTGRES_DSN}" "${compose_file}" "${domain}" "${cell}" 'precheck')"; then
		:
	else
		waiter_rc=$?
		_ifa_fault_stop_runner_lease_holder_client "${cell}" "${domain}" "${holder_pid}"
		return "${waiter_rc}"
	fi
	IFS='|' read -r holder_count waiter_count <<<"${holder_waiter_pair}"
	if [[ "${holder_count}" != "1" ]]; then
		printf 'ifa_fault_release_runner_lease_hold: expected one exact labeled holder before release, observed %s\n' "${holder_count}" >&2
		_ifa_fault_stop_runner_lease_holder_client "${cell}" "${domain}" "${holder_pid}"
		return 1
	fi
	if [[ "${waiter_count}" == "0" ]]; then
		printf 'ifa_fault_release_runner_lease_hold: no exact waiter remained before holder release for domain=%s; refusing vacuous recovery\n' "${domain}" >&2
		_ifa_fault_stop_runner_lease_holder_client "${cell}" "${domain}" "${holder_pid}"
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
