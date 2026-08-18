#!/usr/bin/env bash
# shellcheck disable=SC2034,SC2154
# Holder acquisition and transactional setup for the documentation ACK barrier.
# Sourced by ifa_fault_injection_documentation_ack_barrier.sh.

ifa_documentation_validate_ack_identity() {
	local identity="$1"
	# PostgreSQL application_name is 63 bytes; the waiter/holder prefix is 29.
	[[ "${identity}" =~ ^ifa_doc_[0-9]+_[0-9]+_[0-9]+$ && "${#identity}" -le 34 ]]
}

ifa_documentation_prepare_ack_identity() {
	if [[ -z "${ifa_documentation_ack_run_id:-}" ]]; then
		ifa_documentation_ack_run_id="ifa_doc_${BASHPID}_${RANDOM}_${RANDOM}"
	fi
	ifa_documentation_validate_ack_identity "${ifa_documentation_ack_run_id}" || {
		printf 'documentation ACK barrier has invalid run identity %q\n' "${ifa_documentation_ack_run_id}" >&2
		return 2
	}
}

ifa_documentation_install_ack_barrier() {
	local cell="$1" holder_backend_pid="$2"
	ifa_documentation_prepare_ack_identity || return $?
	[[ "${holder_backend_pid}" =~ ^[1-9][0-9]*$ ]] || return 2
	local waiter_app_name="ifa_documentation_ack_waiter_${ifa_documentation_ack_run_id}"
	local holder_app_name="ifa_documentation_ack_holder_${ifa_documentation_ack_run_id}"
	local artifacts artifacts_rc
	if artifacts="$(ifa_det_pg "${FAULT_COMPOSE_PROJECT}" "${use_compose}" "${ESHU_POSTGRES_DSN}" \
		"SELECT ((SELECT count(*) FROM pg_catalog.pg_trigger WHERE tgname = 'ifa_documentation_ack_barrier' AND NOT tgisinternal) + (SELECT count(*) FROM pg_catalog.pg_proc proc JOIN pg_catalog.pg_namespace namespace ON namespace.oid = proc.pronamespace WHERE namespace.nspname = 'public' AND proc.proname = 'ifa_documentation_block_ack'))::text || '|' || (SELECT count(*) FROM pg_catalog.pg_locks held WHERE held.locktype = 'advisory' AND held.classid = 5998 AND held.objid = 5994 AND held.objsubid = 2 AND held.database = (SELECT oid FROM pg_catalog.pg_database WHERE datname = pg_catalog.current_database()))::text || '|' || (SELECT count(*) FROM pg_catalog.pg_stat_activity holder JOIN pg_catalog.pg_locks held ON held.pid = holder.pid WHERE holder.pid = ${holder_backend_pid} AND holder.application_name = '${holder_app_name}' AND held.locktype = 'advisory' AND held.classid = 5998 AND held.objid = 5994 AND held.objsubid = 2 AND held.database = (SELECT oid FROM pg_catalog.pg_database WHERE datname = pg_catalog.current_database()) AND held.granted)::text;" \
		"${compose_file}")"; then
		artifacts="$(printf '%s' "${artifacts}" | tr -d '[:space:]')"
	else
		artifacts_rc=$?
		printf '%s: documentation ACK barrier preflight query FAILED (exit %s)\n' "${cell}" "${artifacts_rc}" >&2
		return "${artifacts_rc}"
	fi
	[[ "${artifacts}" == "0|1|1" ]] || {
		if [[ "${artifacts}" =~ ^0\|[1-9][0-9]*\|[0-9]+$ ]]; then
			printf '%s: documentation ACK barrier key is already in use: %q\n' "${cell}" "${artifacts}" >&2
		else
			printf '%s: documentation ACK barrier preflight expected 0 artifacts, 1 key row, and 1 exact granted owner; got %q\n' "${cell}" "${artifacts}" >&2
		fi
		return 1
	}
	# A successful zero preflight is the ownership boundary. Set it before the
	# DDL so an applied COMMIT followed by a client error is still cleaned up.
	ifa_documentation_ack_barrier_active=1
	ifa_documentation_ack_barrier_cell="${cell}"
	ifa_documentation_ack_ddl_owned=1
	local setup_sql="BEGIN;
CREATE FUNCTION public.ifa_documentation_block_ack() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, pg_temp
AS \$ifa\$
BEGIN
  PERFORM pg_catalog.set_config('application_name', '${waiter_app_name}', true);
  PERFORM pg_catalog.pg_advisory_xact_lock(5998, 5994);
  RETURN NEW;
END
\$ifa\$;
CREATE TRIGGER ifa_documentation_ack_barrier
BEFORE UPDATE ON public.fact_work_items
FOR EACH ROW
WHEN (OLD.stage = 'reducer' AND NEW.stage = 'reducer'
  AND OLD.domain = 'documentation_materialization'
  AND NEW.domain = 'documentation_materialization'
  AND OLD.scope_id = 'scope-ifa-documentation-family'
  AND NEW.scope_id = 'scope-ifa-documentation-family'
  AND OLD.generation_id = 'gen-ifa-documentation-family-1'
  AND NEW.generation_id = 'gen-ifa-documentation-family-1'
  AND OLD.status IN ('claimed', 'running')
  AND OLD.lease_owner IS NOT NULL
  AND NEW.status = 'succeeded'
  AND NEW.lease_owner IS NULL)
EXECUTE FUNCTION public.ifa_documentation_block_ack();
COMMIT;"
	local setup_rc
	if ifa_det_pg "${FAULT_COMPOSE_PROJECT}" "${use_compose}" "${ESHU_POSTGRES_DSN}" \
		"${setup_sql}" "${compose_file}" >/dev/null; then
		return 0
	else
		setup_rc=$?
	fi
	printf '%s: documentation ACK barrier setup query FAILED (exit %s)\n' "${cell}" "${setup_rc}" >&2
	return "${setup_rc}"
}

ifa_documentation_start_ack_holder() {
	local cell="$1" pid_var="$2" backend_pid_var="$3"
	[[ "${cell}" =~ ^[a-z0-9_]+$ ]] || return 2
	ifa_documentation_prepare_ack_identity || return $?
	local holder_app_name="ifa_documentation_ack_holder_${ifa_documentation_ack_run_id}"
	local lock_sql="SET application_name = '${holder_app_name}'; SELECT CASE WHEN pg_catalog.pg_try_advisory_lock(5998, 5994) THEN pg_catalog.pg_sleep(180) ELSE pg_catalog.pg_advisory_lock(5998, 5994) END;"
	if [[ "${use_compose}" -eq 1 ]]; then
		docker compose -p "${FAULT_COMPOSE_PROJECT}" -f "${compose_file}" exec -T postgres \
			psql -v ON_ERROR_STOP=1 -U eshu -d eshu -c "${lock_sql}" \
			>"${log_dir}/documentation-ack-holder-${cell}.log" 2>&1 &
	else
		psql "${ESHU_POSTGRES_DSN}" -v ON_ERROR_STOP=1 -c "${lock_sql}" \
			>"${log_dir}/documentation-ack-holder-${cell}.log" 2>&1 &
	fi
	local client_pid=$!
	bg_pids+=("${client_pid}")
	ifa_documentation_ack_holder_pid="${client_pid}"
	# The host client now owns a live session attempt even before its backend
	# appears in the first census. Cleanup must retain that ownership across
	# empty, failed, or ambiguous census responses.
	ifa_documentation_ack_holder_owned=1
	printf -v "${pid_var}" '%s' "${client_pid}"

	local i holder_state backend_pid holder_granted holder_rc
	for i in $(seq 1 60); do
		if holder_state="$(ifa_det_pg "${FAULT_COMPOSE_PROJECT}" "${use_compose}" "${ESHU_POSTGRES_DSN}" \
			"SELECT COALESCE(string_agg(holder.pid::text || '|' || held.granted::text, ',' ORDER BY holder.pid), '') FROM pg_catalog.pg_stat_activity holder JOIN pg_catalog.pg_locks held ON held.pid = holder.pid WHERE holder.application_name = '${holder_app_name}' AND held.locktype = 'advisory' AND held.classid = 5998 AND held.objid = 5994 AND held.objsubid = 2 AND held.database = (SELECT oid FROM pg_catalog.pg_database WHERE datname = pg_catalog.current_database());" \
			"${compose_file}")"; then
			holder_state="$(printf '%s' "${holder_state}" | tr -d '[:space:]')"
		else
			holder_rc=$?
			# The session may already own or be waiting on the fixed advisory
			# key. Retain cleanup ownership until an exact tagged census proves
			# otherwise; discarding it here can leak a backend after query loss.
			ifa_documentation_ack_holder_owned=1
			printf '%s: documentation ACK holder query FAILED (exit %s)\n' "${cell}" "${holder_rc}" >&2
			return "${holder_rc}"
		fi
		if [[ "${holder_state}" =~ ^([1-9][0-9]*)\|(true|false)$ ]]; then
			backend_pid="${BASH_REMATCH[1]}"
			holder_granted="${BASH_REMATCH[2]}"
			ifa_documentation_ack_holder_backend_pid="${backend_pid}"
			ifa_documentation_ack_holder_owned=1
			if [[ "${holder_granted}" == "true" ]]; then
				printf -v "${backend_pid_var}" '%s' "${backend_pid}"
				return 0
			fi
			ifa_documentation_terminate_ack_holder "${cell}" "${backend_pid}" \
				&& ifa_documentation_wait_for_ack_backend_gone "${cell}" "${backend_pid}" 5 || return 1
			ifa_documentation_join_ack_holder "${client_pid}" || return $?
			ifa_documentation_ack_holder_backend_pid=""
			ifa_documentation_ack_holder_owned=0
			printf '%s: documentation ACK advisory key is held by another run\n' "${cell}" >&2
			return 1
		fi
		if [[ -n "${holder_state}" ]]; then
			ifa_documentation_ack_holder_owned=1
			printf '%s: documentation ACK holder query returned ambiguous output %q\n' "${cell}" "${holder_state}" >&2
			return 1
		fi
		sleep 0.25
	done
	printf '%s: documentation ACK holder did not acquire its advisory lock\n' "${cell}" >&2
	ifa_documentation_ack_holder_owned=1
	return 1
}

ifa_documentation_start_ack_barrier() {
	local cell="$1" pid_var="$2" backend_pid_var="$3"
	ifa_documentation_ack_producers_safe=1
	# Let the holder starter write the caller's outputs directly. Wrapper-local
	# variables with these names shadow production callers under Bash dynamic
	# scope and leave their holder PID outputs unset.
	ifa_documentation_start_ack_holder "${cell}" "${pid_var}" "${backend_pid_var}" || return $?
	ifa_documentation_install_ack_barrier "${cell}" "${!backend_pid_var}"
}
