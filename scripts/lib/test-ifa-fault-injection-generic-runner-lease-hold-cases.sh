#!/usr/bin/env bash
# shellcheck disable=SC1090,SC2034,SC2154
# Hermetic proof for the runner_lease_hold fault mechanism. The parent mirror
# owns strict mode, fail(), repo_root, and the library path variables.

run_ifa_fault_injection_generic_runner_lease_hold_cases() {
	test_ifa_runner_lease_hold_rejects_bad_identifiers_before_sql
	test_ifa_runner_lease_hold_starts_exact_transaction_lock_and_tracks_client
	test_ifa_runner_lease_hold_failed_confirmation_cleans_up
	test_ifa_runner_lease_hold_release_requires_post_kill_waiter
	test_ifa_runner_lease_hold_release_query_failure_cleans_up
	test_ifa_runner_lease_hold_release_orders_terminate_join_then_waiter_drain
	test_ifa_runner_lease_hold_release_timeout_still_untracks_client
	test_ifa_runner_lease_hold_negative_control_requires_holder_without_waiter
	test_ifa_runner_lease_hold_wait_requires_pending_and_exact_waiter
	test_ifa_runner_lease_hold_generic_dispatch_rejects_runner_stage
	test_ifa_runner_lease_hold_wait_uses_fresh_sql_samples
	test_ifa_runner_lease_hold_query_failure_is_unknown
	test_ifa_symbol_runtime_control_query_failure_is_unknown
	test_ifa_runner_lease_hold_durable_reclaim_is_expiry_fenced
}

test_ifa_runner_lease_hold_generic_dispatch_rejects_runner_stage() (
	# shellcheck source=scripts/lib/ifa_fault_generic_cells.sh
	source "${generic_cells_lib}"
	local FAULT_COMPOSE_PROJECT=test-project use_compose=0
	local ESHU_POSTGRES_DSN=test-dsn compose_file=test-compose.yml
	ifa_family_wait_stage() { printf 'runner'; }
	ifa_family_wait_key() { printf 'handles_route'; }
	ifa_fault_wait_for_claimed_projection_intent() { fail "generic dispatch called the custom runner-lease waiter"; }

	local output rc=0
	output="$(_ifa_generic_wait_for_claimed handles_route killworker_handles_route 1 2>&1)" || rc=$?
	[[ "${rc}" -ne 0 && "${output}" == *'requires a custom runner-lease cell'* ]] \
		|| fail "generic runner dispatch did not reject the unsupported runner stage (rc=${rc}, output=${output})"
)

test_ifa_runner_lease_hold_rejects_bad_identifiers_before_sql() (
	# shellcheck source=scripts/lib/ifa_determinism_common.sh
	source "${det_lib}"
	# shellcheck source=scripts/lib/ifa_fault_injection_common.sh
	source "${fault_lib}"
	# shellcheck source=scripts/lib/ifa_fault_generic_runner_wait.sh
	source "${generic_runner_wait_lib}"
	local use_compose=0 FAULT_COMPOSE_PROJECT=test-project ESHU_POSTGRES_DSN=test-dsn
	local compose_file=test-compose.yml log_dir bg_pids=() rc=0 output
	log_dir="$(mktemp -d)"
	# shellcheck disable=SC2064
	trap "rm -rf '${log_dir}'" EXIT

	ifa_det_pg() { fail "ifa_det_pg ran for a rejected identifier"; }
	psql() { fail "psql ran for a rejected identifier"; }

	output="$(ifa_fault_start_runner_lease_hold bad-cell handles_route ignored 2>&1)" || rc=$?
	[[ "${rc}" -eq 1 && "${output}" == *"must match ^[a-z0-9_]+$"* ]] \
		|| fail "runner lease holder accepted an invalid cell (rc=${rc}, output=${output})"

	rc=0
	output="$(ifa_fault_require_no_projection_intent_waiter test-project 0 test-dsn test-compose.yml 'bad domain' goodcell 2>&1)" || rc=$?
	[[ "${rc}" -eq 1 && "${output}" == *"must match ^[a-z0-9_]+$"* ]] \
		|| fail "runner lease negative control accepted an invalid domain (rc=${rc}, output=${output})"

	rc=0
	output="$(ifa_fault_wait_for_claimed_projection_intent test-project 0 test-dsn test-compose.yml 1 handles_route 'bad-cell' 2>&1)" || rc=$?
	[[ "${rc}" -eq 1 && "${output}" == *"must match ^[a-z0-9_]+$"* ]] \
		|| fail "runner lease waiter accepted an invalid cell (rc=${rc}, output=${output})"
)

test_ifa_runner_lease_hold_starts_exact_transaction_lock_and_tracks_client() (
	# shellcheck source=scripts/lib/ifa_determinism_common.sh
	source "${det_lib}"
	# shellcheck source=scripts/lib/ifa_fault_injection_common.sh
	source "${fault_lib}"
	# shellcheck source=scripts/lib/ifa_fault_generic_runner_wait.sh
	source "${generic_runner_wait_lib}"
	local use_compose=0 FAULT_COMPOSE_PROJECT=test-project ESHU_POSTGRES_DSN=test-dsn
	local compose_file=test-compose.yml log_dir query_log client_log bg_pids=() holder_pid=""
	log_dir="$(mktemp -d)"
	query_log="${log_dir}/queries.log"
	client_log="${log_dir}/client.log"
	# shellcheck disable=SC2064
	trap "rm -rf '${log_dir}'" EXIT

	psql() {
		printf '%s\n' "$*" >"${client_log}"
		sleep 30
	}
	ifa_det_pg() {
		printf '%s\n' "$4" >>"${query_log}"
		case "$4" in
		*"exact waiter precheck"*) printf '1|1\n' ;;
		*pg_terminate_backend*) printf '1|t\n' ;;
		*"waiter drain"*) printf '0\n' ;;
		*) printf '1\n' ;;
		esac
	}

	ifa_fault_start_runner_lease_hold kill_handles_route handles_route holder_pid \
		|| fail "runner lease holder did not start"
	[[ "${holder_pid}" =~ ^[0-9]+$ ]] || fail "runner lease holder returned a non-PID: ${holder_pid}"
	[[ " ${bg_pids[*]} " == *" ${holder_pid} "* ]] || fail "runner lease holder PID was not tracked"
	for _ in $(seq 1 20); do [[ -s "${client_log}" ]] && break; sleep 0.01; done
	local client_sql held_sql
	client_sql="$(<"${client_log}")"
	held_sql="$(<"${query_log}")"
	[[ "${client_sql}" == *"SET application_name = 'ifa_rlh_handles_route_kill_handles_route'"* ]] \
		|| fail "runner lease holder did not set the derived application_name: ${client_sql}"
	[[ "${client_sql}" == *"BEGIN"* && "${client_sql}" == *"pg_advisory_xact_lock(hashtext('shared_projection_partition_leases'), hashtext('handles_route'))"* && "${client_sql}" == *"pg_sleep(180)"* && "${client_sql}" == *"ROLLBACK"* ]] \
		|| fail "runner lease holder does not hold the exact production key for one transaction: ${client_sql}"
	[[ "${held_sql}" == *"NOT pg_try_advisory_xact_lock(hashtext('shared_projection_partition_leases'), hashtext('handles_route'))"* ]] \
		|| fail "runner lease holder was not independently confirmed with pg_try on the exact key: ${held_sql}"
	[[ "${held_sql}" != *"classid = hashtext"* && "${held_sql}" != *"objid = hashtext"* ]] \
		|| fail "runner lease held check compares signed hashtext values to unsigned pg_locks oid fields"

	ifa_fault_release_runner_lease_hold kill_handles_route handles_route "${holder_pid}" \
		|| fail "runner lease holder did not release cleanly"
	[[ " ${bg_pids[*]} " != *" ${holder_pid} "* ]] || fail "released runner lease holder PID stayed tracked"
	local release_sql
	release_sql="$(<"${query_log}")"
	[[ "${release_sql}" == *"pg_terminate_backend"* && "${release_sql}" == *"ifa_rlh_handles_route_kill_handles_route"* ]] \
		|| fail "runner lease release did not terminate the labeled backend: ${release_sql}"
)

test_ifa_runner_lease_hold_failed_confirmation_cleans_up() (
	# shellcheck source=scripts/lib/ifa_determinism_common.sh
	source "${det_lib}"
	# shellcheck source=scripts/lib/ifa_fault_injection_common.sh
	source "${fault_lib}"
	# shellcheck source=scripts/lib/ifa_fault_generic_runner_wait.sh
	source "${generic_runner_wait_lib}"
	local use_compose=0 FAULT_COMPOSE_PROJECT=test-project ESHU_POSTGRES_DSN=test-dsn
	local compose_file=test-compose.yml log_dir bg_pids=() holder_pid="" rc=0
	log_dir="$(mktemp -d)"
	# shellcheck disable=SC2064
	trap "rm -rf '${log_dir}'" EXIT

	psql() { sleep 30; }
	ifa_det_pg() {
		case "$4" in
		*pg_terminate_backend*) printf '0|f\n' ;;
		*) printf '0\n' ;;
		esac
	}
	sleep() { :; }

	ifa_fault_start_runner_lease_hold timeout_cell handles_route holder_pid >/dev/null 2>&1 || rc=$?
	[[ "${rc}" -ne 0 ]] || fail "runner lease holder passed when the exact-key confirmation never succeeded"
	[[ -n "${holder_pid}" ]] || fail "failed runner lease start did not return its owned PID for attribution"
	[[ " ${bg_pids[*]} " != *" ${holder_pid} "* ]] \
		|| fail "failed runner lease start leaked its holder PID in bg_pids"
)

test_ifa_runner_lease_hold_release_requires_post_kill_waiter() (
	# shellcheck source=scripts/lib/ifa_determinism_common.sh
	source "${det_lib}"
	# shellcheck source=scripts/lib/ifa_fault_injection_common.sh
	source "${fault_lib}"
	# shellcheck source=scripts/lib/ifa_fault_generic_runner_wait.sh
	source "${generic_runner_wait_lib}"
	local use_compose=0 FAULT_COMPOSE_PROJECT=test-project ESHU_POSTGRES_DSN=test-dsn
	local compose_file=test-compose.yml log_dir events output_file bg_pids=() holder_pid="" rc=0 output
	log_dir="$(mktemp -d)"; events="${log_dir}/events"; output_file="${log_dir}/release-output"
	# shellcheck disable=SC2064
	trap "rm -rf '${log_dir}'" EXIT

	psql() { sleep 30; }
	ifa_det_pg() {
		case "$4" in
		*"exact waiter precheck"*) printf '1|0\n' ;;
		*pg_terminate_backend*) printf 'terminate\n' >>"${events}"; printf '1|t\n' ;;
		*"waiter drain"*) printf '0\n' ;;
		*) printf '1\n' ;;
		esac
	}
	sleep() { :; }

	ifa_fault_start_runner_lease_hold no_waiter handles_route holder_pid \
		|| fail "runner lease holder did not start for release non-vacuity proof"
	ifa_fault_release_runner_lease_hold no_waiter handles_route "${holder_pid}" >"${output_file}" 2>&1 || rc=$?
	output="$(<"${output_file}")"
	[[ "${rc}" -ne 0 && "${output}" == *"no exact waiter remained before holder release"* ]] \
		|| fail "runner lease release did not fail closed without a post-kill waiter (rc=${rc}, output=${output})"
	[[ -f "${events}" && "$(<"${events}")" == 'terminate' ]] \
		|| fail "runner lease release did not clean up the holder after the post-kill waiter precheck failed"
	[[ " ${bg_pids[*]} " != *" ${holder_pid} "* ]] \
		|| fail "runner lease release left the holder tracked after the post-kill waiter precheck failed"
)

test_ifa_runner_lease_hold_release_query_failure_cleans_up() (
	# shellcheck source=scripts/lib/ifa_determinism_common.sh
	source "${det_lib}"
	# shellcheck source=scripts/lib/ifa_fault_injection_common.sh
	source "${fault_lib}"
	# shellcheck source=scripts/lib/ifa_fault_generic_runner_wait.sh
	source "${generic_runner_wait_lib}"
	local use_compose=0 FAULT_COMPOSE_PROJECT=test-project ESHU_POSTGRES_DSN=test-dsn
	local compose_file=test-compose.yml log_dir events output_file bg_pids=() holder_pid="" rc=0 output
	log_dir="$(mktemp -d)"; events="${log_dir}/events"; output_file="${log_dir}/release-output"
	# shellcheck disable=SC2064
	trap "rm -rf '${log_dir}'" EXIT

	psql() { sleep 30; }
	ifa_det_pg() {
		case "$4" in
		*"exact waiter precheck"*) return 9 ;;
		*pg_terminate_backend*) printf 'terminate\n' >>"${events}"; return 0 ;;
		*) printf '1\n' ;;
		esac
	}
	sleep() { :; }

	ifa_fault_start_runner_lease_hold query_error handles_route holder_pid \
		|| fail "runner lease holder did not start for release query-failure proof"
	ifa_fault_release_runner_lease_hold query_error handles_route "${holder_pid}" >"${output_file}" 2>&1 || rc=$?
	output="$(<"${output_file}")"
	[[ "${rc}" -eq 9 && "${output}" == *"FAILED (exit 9)"* && "${output}" == *"unknown"* ]] \
		|| fail "runner lease release did not preserve the precheck query failure (rc=${rc}, output=${output})"
	[[ -f "${events}" && "$(<"${events}")" == 'terminate' ]] \
		|| fail "runner lease release did not attempt holder cleanup after a precheck query failure"
	[[ " ${bg_pids[*]} " != *" ${holder_pid} "* ]] \
		|| fail "runner lease release left the holder tracked after a precheck query failure"
)

test_ifa_runner_lease_hold_release_orders_terminate_join_then_waiter_drain() (
	# shellcheck source=scripts/lib/ifa_determinism_common.sh
	source "${det_lib}"
	# shellcheck source=scripts/lib/ifa_fault_injection_common.sh
	source "${fault_lib}"
	# shellcheck source=scripts/lib/ifa_fault_generic_runner_wait.sh
	source "${generic_runner_wait_lib}"
	local use_compose=0 FAULT_COMPOSE_PROJECT=test-project ESHU_POSTGRES_DSN=test-dsn
	local compose_file=test-compose.yml log_dir events drain_count_file drain_query_log bg_pids=() holder_pid=""
	log_dir="$(mktemp -d)"; events="${log_dir}/events"; drain_count_file="${log_dir}/drain-count"; drain_query_log="${log_dir}/drain-query"
	printf '0\n' >"${drain_count_file}"
	# shellcheck disable=SC2064
	trap "rm -rf '${log_dir}'" EXIT

	psql() { sleep 30; }
	ifa_det_pg() {
		case "$4" in
		*"exact waiter precheck"*) printf 'prewait\n' >>"${events}"; printf '1|2\n' ;;
		*pg_terminate_backend*) printf 'terminate\n' >>"${events}"; printf '1|t\n' ;;
		*"waiter drain"*)
			local count; count="$(<"${drain_count_file}")"; count=$((count + 1)); printf '%s\n' "${count}" >"${drain_count_file}"
			printf '%s\n' "$4" >"${drain_query_log}"
			printf 'drain\n' >>"${events}"
			if [[ "${count}" -lt 3 ]]; then printf '2\n'; else printf '0\n'; fi
			;;
		*) printf '1\n' ;;
		esac
	}
	ifa_det_untrack_bg_pid() {
		local pid="$1"
		kill -0 "${pid}" 2>/dev/null && fail "runner lease client was still live when untracked"
		printf 'untrack\n' >>"${events}"
		bg_pids=()
	}
	sleep() { :; }

	ifa_fault_start_runner_lease_hold ordered_cell handles_route holder_pid \
		|| fail "runner lease holder did not start for release-order proof"
	ifa_fault_release_runner_lease_hold ordered_cell handles_route "${holder_pid}" \
		|| fail "runner lease release did not drain its orphaned waiters"
	local sequence
	sequence="$(<"${events}")"
	[[ "${sequence}" == $'prewait\nterminate\nuntrack\ndrain\ndrain\ndrain' ]] \
		|| fail "runner lease release order was not precheck -> terminate -> join/untrack -> bounded waiter drain: ${sequence}"
	local drain_sql
	drain_sql="$(<"${drain_query_log}")"
	[[ "${drain_sql}" == *"FROM pg_catalog.pg_locks lock_row"* && "${drain_sql}" == *"classid::bigint"* && "${drain_sql}" == *"4294967296"* ]] \
		|| fail "runner lease drain did not count the normalized exact advisory tag: ${drain_sql}"
	[[ "${drain_sql}" != *"holder.application_name"* && "${drain_sql}" != *"NOT lock_row.granted"* ]] \
		|| fail "runner lease drain stayed dependent on the vanished holder or ignored granted orphan rows: ${drain_sql}"
)

test_ifa_runner_lease_hold_release_timeout_still_untracks_client() (
	# shellcheck source=scripts/lib/ifa_determinism_common.sh
	source "${det_lib}"
	# shellcheck source=scripts/lib/ifa_fault_injection_common.sh
	source "${fault_lib}"
	# shellcheck source=scripts/lib/ifa_fault_generic_runner_wait.sh
	source "${generic_runner_wait_lib}"
	local use_compose=0 FAULT_COMPOSE_PROJECT=test-project ESHU_POSTGRES_DSN=test-dsn
	local compose_file=test-compose.yml log_dir output_file bg_pids=() holder_pid="" rc=0 output
	log_dir="$(mktemp -d)"
	output_file="${log_dir}/release.stderr"
	# shellcheck disable=SC2064
	trap "rm -rf '${log_dir}'" EXIT

	psql() { sleep 30; }
	ifa_det_pg() {
		case "$4" in
		*"exact waiter precheck"*) printf '1|1\n' ;;
		*pg_terminate_backend*) printf '1|t\n' ;;
		*"waiter drain"*) printf '1\n' ;;
		*) printf '1\n' ;;
		esac
	}
	sleep() { :; }

	ifa_fault_start_runner_lease_hold timeout_release handles_route holder_pid \
		|| fail "runner lease holder did not start for release-timeout proof"
	ifa_fault_release_runner_lease_hold timeout_release handles_route "${holder_pid}" 2>"${output_file}" || rc=$?
	output="$(<"${output_file}")"
	[[ "${rc}" -ne 0 && "${output}" == *"waiters did not drain"* ]] \
		|| fail "runner lease release did not fail closed on waiter-drain timeout (rc=${rc}, output=${output})"
	[[ " ${bg_pids[*]} " != *" ${holder_pid} "* ]] \
		|| fail "runner lease release timeout left its stopped holder client tracked"
)

test_ifa_runner_lease_hold_negative_control_requires_holder_without_waiter() (
	# shellcheck source=scripts/lib/ifa_fault_generic_runner_wait.sh
	source "${generic_runner_wait_lib}"
	local sample query rc=0 output
	ifa_det_pg() { query="$4"; printf '%s\n' "${sample}"; }

	sample='0|1|0'
	output="$(ifa_fault_require_no_projection_intent_waiter test-project 0 test-dsn test-compose.yml handles_route control_cell)" || rc=$?
	[[ "${rc}" -eq 0 && "${output}" == *"pending=0 holder=1 waiter=0"* ]] \
		|| fail "valid pre-reducer negative control did not pass (rc=${rc}, output=${output})"

	# Capture the SQL in a file because the helper samples in a command
	# substitution and shell variables mutated there do not escape.
	local tmp_dir query_log
	tmp_dir="$(mktemp -d)"; query_log="${tmp_dir}/query.log"
	# shellcheck disable=SC2064
	trap "rm -rf '${tmp_dir}'" EXIT
	ifa_det_pg() { printf '%s\n' "$4" >"${query_log}"; printf '0|1|0\n'; }
	ifa_fault_require_no_projection_intent_waiter test-project 0 test-dsn test-compose.yml handles_route control_cell >/dev/null \
		|| fail "negative control failed while capturing its SQL"
	query="$(<"${query_log}")"
	[[ "${query}" == *"FROM shared_projection_intents"* && "${query}" == *"completed_at IS NULL"* ]] \
		|| fail "runner observation does not count pending target intents: ${query}"
	[[ "${query}" == *"holder.application_name = 'ifa_rlh_handles_route_control_cell'"* && "${query}" == *"NOT waiting.granted"* && "${query}" == *"waiter.wait_event_type = 'Lock'"* ]] \
		|| fail "runner observation does not identify the labeled holder and ungranted lock waiter: ${query}"
	for column in locktype database classid objid objsubid; do
		[[ "${query}" == *"waiting.${column} = held.${column}"* ]] \
			|| fail "runner observation does not join waiter to holder on ${column}: ${query}"
	done
	[[ "${query}" != *"classid = hashtext"* && "${query}" != *"objid = hashtext"* ]] \
		|| fail "runner observation compares signed hashtext values to unsigned pg_locks oid fields"

	for sample in '0|0|0' '0|1|1' 'not-a-sample'; do
		rc=0
		ifa_det_pg() { printf '%s\n' "${sample}"; }
		ifa_fault_require_no_projection_intent_waiter test-project 0 test-dsn test-compose.yml handles_route control_cell >/dev/null 2>&1 || rc=$?
		[[ "${rc}" -ne 0 ]] || fail "negative control accepted invalid observation ${sample}"
	done
)

test_ifa_runner_lease_hold_wait_requires_pending_and_exact_waiter() (
	# shellcheck source=scripts/lib/ifa_fault_generic_runner_wait.sh
	source "${generic_runner_wait_lib}"
	local sample rc output
	sleep() { :; }
	ifa_det_pg() { printf '%s\n' "${sample}"; }

	for sample in '3|1|0' '0|1|4' '3|0|4'; do
		rc=0
		ifa_fault_wait_for_claimed_projection_intent test-project 0 test-dsn test-compose.yml 1 handles_route wait_cell >/dev/null 2>&1 || rc=$?
		[[ "${rc}" -ne 0 ]] || fail "runner wait accepted incomplete observation ${sample}"
	done

	sample='3|1|4'; rc=0
	output="$(ifa_fault_wait_for_claimed_projection_intent test-project 0 test-dsn test-compose.yml 1 handles_route wait_cell)" || rc=$?
	[[ "${rc}" -eq 0 && "${output}" == '3|4' ]] \
		|| fail "runner wait rejected pending+exact-waiter observation (rc=${rc}, output=${output})"
)

test_ifa_runner_lease_hold_wait_uses_fresh_sql_samples() (
	# shellcheck source=scripts/lib/ifa_fault_generic_runner_wait.sh
	source "${generic_runner_wait_lib}"
	local tmp_dir count_file rc=0 output calls
	tmp_dir="$(mktemp -d)"; count_file="${tmp_dir}/calls"
	printf '0\n' >"${count_file}"
	# shellcheck disable=SC2064
	trap "rm -rf '${tmp_dir}'" EXIT
	sleep() { :; }
	ifa_det_pg() {
		local count
		count="$(<"${count_file}")"; count=$((count + 1)); printf '%s\n' "${count}" >"${count_file}"
		if [[ "${count}" -eq 1 ]]; then printf '0|1|0\n'; else printf '2|1|4\n'; fi
	}

	output="$(ifa_fault_wait_for_claimed_projection_intent test-project 0 test-dsn test-compose.yml 2 handles_route fresh_cell)" || rc=$?
	calls="$(<"${count_file}")"
	[[ "${rc}" -eq 0 && "${output}" == '2|4' && "${calls}" -eq 2 ]] \
		|| fail "runner wait did not resample through fresh SQL calls (rc=${rc}, output=${output}, calls=${calls})"
)

test_ifa_runner_lease_hold_query_failure_is_unknown() (
	# shellcheck source=scripts/lib/ifa_fault_generic_runner_wait.sh
	source "${generic_runner_wait_lib}"
	local rc=0 output
	ifa_det_pg() { return 9; }

	output="$(ifa_fault_require_no_projection_intent_waiter test-project 0 test-dsn test-compose.yml handles_route error_cell 2>&1)" || rc=$?
	[[ "${rc}" -eq 9 && "${output}" == *"FAILED (exit 9)"* && "${output}" == *"unknown"* ]] \
		|| fail "negative control did not propagate an indeterminate query failure (rc=${rc}, output=${output})"
)

test_ifa_symbol_runtime_control_query_failure_is_unknown() (
	# shellcheck source=scripts/lib/ifa_fault_injection_symbol_runtime_cells.sh
	source "${repo_root}/scripts/lib/ifa_fault_injection_symbol_runtime_cells.sh"
	local use_compose=0 FAULT_COMPOSE_PROJECT=test-project ESHU_POSTGRES_DSN=test-dsn
	local compose_file=test-compose.yml rc=0 output
	set +o pipefail
	ifa_det_pg() { return 9; }
	sleep() { :; }

	output="$(_ifa_symbol_runtime_wait_for_code_calls_control 1 2>&1)" || rc=$?
	[[ "${rc}" -eq 9 && "${output}" == *"FAILED (exit 9)"* && "${output}" == *"unknown"* ]] \
		|| fail "symbol-runtime control did not preserve an indeterminate query failure (rc=${rc}, output=${output})"
)

test_ifa_runner_lease_hold_durable_reclaim_is_expiry_fenced() (
	# shellcheck source=scripts/lib/ifa_fault_generic_runner_wait.sh
	source "${generic_runner_wait_lib}"
	local FAULT_COMPOSE_PROJECT=test-project use_compose=0 ESHU_POSTGRES_DSN=test-dsn
	local compose_file=test-compose.yml mode=capture captured=""
	local dead_pid=111 replacement_pid=222 nonce=0123456789abcdef0123456789abcdef
	local dead_owner="shared-projection-runner:test-host:${dead_pid}:${nonce}"
	local replacement_owner="shared-projection-runner:test-host:${replacement_pid}:${nonce}"

	ifa_det_pg() {
		local sql="$4"
		case "${mode}" in
		capture)
			[[ "${sql}" == *"runner_lease_hold capture durable leases"* &&
				"${sql}" == *"lease_expires_at > clock_timestamp() + INTERVAL '2 seconds'"* &&
				"${sql}" == *":${dead_pid}:[0-9a-f]{16,32}"* ]] || return 1
			printf '0|8|%s|200.000000|100.000000' "${dead_owner}"
			;;
		preexpiry)
			[[ "${sql}" == *"runner_lease_hold pre-expiry durable lease fence"* &&
				"${sql}" == *"(0,8,'${dead_owner}',to_timestamp(200.000000),to_timestamp(100.000000))"* &&
				"${sql}" == *":${replacement_pid}:[0-9a-f]{16,32}"* ]] || return 1
			printf '1|1|0'
			;;
		install)
			[[ "${sql}" == *"CREATE TRIGGER ${_IFA_RUNNER_LEASE_AUDIT_TRIGGER}"* && "${sql}" == *"CREATE TABLE ${_IFA_RUNNER_LEASE_AUDIT_TABLE}"* ]] || return 1
			;;
		expiry)
			[[ "${sql}" == *"runner_lease_hold wait captured expiry"* && "${sql}" == *"MAX(captured.dead_expiry)"* ]] || return 1
			printf '1'
			;;
		audit)
			[[ "${sql}" == *"runner_lease_hold replacement durable lease audit"* &&
				"${sql}" == *"JOIN ${_IFA_RUNNER_LEASE_AUDIT_TABLE} AS audit"* &&
				"${sql}" == *":${replacement_pid}:[0-9a-f]{16,32}"* ]] || return 1
			printf '1'
			;;
		drop) [[ "${sql}" == *"DROP TRIGGER IF EXISTS ${_IFA_RUNNER_LEASE_AUDIT_TRIGGER}"* ]] || return 1 ;;
		reclaimed)
			[[ "${sql}" == *"runner_lease_hold post-reclaim durable lease release"* ]] || return 1
			printf '1|1|1'
			;;
		esac
	}

	ifa_fault_capture_runner_partition_leases proof handles_route "${dead_pid}" 2 captured || return 1
	[[ "${captured}" == "0|8|${dead_owner}|200.000000|100.000000" ]] || return 1
	mode=preexpiry
	ifa_fault_require_runner_leases_expiry_fenced proof handles_route "${replacement_pid}" "${captured}" || return 1
	mode=install; ifa_fault_install_runner_lease_audit proof handles_route "${captured}" || return 1
	[[ "${ifa_runner_lease_audit_owned}" -eq 1 ]] || fail "runner lease audit installation did not register cleanup ownership"
	mode=expiry; ifa_fault_wait_for_runner_lease_expiry proof "${captured}" 1 || return 1
	mode=audit; ifa_fault_require_replacement_runner_lease_audit proof handles_route "${replacement_pid}" "${captured}" || return 1
	mode=reclaimed
	ifa_fault_require_runner_leases_reclaimed proof handles_route "${captured}" || return 1
	mode=drop; ifa_fault_cleanup_runner_lease_audit || return 1
	[[ "${ifa_runner_lease_audit_owned}" -eq 0 ]] || fail "runner lease audit cleanup retained ownership"
	local cell_source
	cell_source="$(<"${repo_root}/scripts/lib/ifa_fault_injection_symbol_runtime_cells.sh")"
	[[ "${cell_source}" == *'ESHU_SHARED_PROJECTION_LEASE_TTL="${_IFA_SYMBOL_RUNTIME_RECLAIM_LEASE_TTL}"'* &&
		"${cell_source}" == *'ifa_fault_capture_runner_partition_leases'* &&
		"${cell_source}" == *'ifa_fault_require_runner_leases_expiry_fenced'* &&
		"${cell_source}" == *'ifa_fault_require_replacement_runner_lease_audit'* &&
		"${cell_source}" == *'ifa_fault_require_runner_leases_reclaimed'* ]] \
		|| fail "symbol-runtime runner cells do not prove durable dead-owner expiry and distinct-owner reclaim"
	[[ "${cell_source}" == *'ifa_fault_release_runner_lease_hold "${cell}" "${family}" "${holder_before}"'*'ifa_fault_capture_runner_partition_leases "${cell}" "${family}" "${reducer_before}"'* ]] \
		|| fail "symbol-runtime runner cell does not release the holder before capturing the killed waiter's committed durable lease"
	[[ "$(<"${repo_root}/scripts/verify-ifa-fault-injection.sh")" == *'ifa_fault_cleanup_runner_lease_audit'* ]] \
		|| fail "top-level EXIT cleanup does not remove a caller-owned runner lease audit"
)
