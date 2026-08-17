#!/usr/bin/env bash
# shellcheck disable=SC1090,SC2030,SC2031,SC2034,SC2154,SC2329
# Hermetic query and claim cases for the documentation fact_work_items ACK barrier.
# Sourced by scripts/test-verify-ifa-fault-injection.sh.

test_ifa_documentation_ack_barrier_queries_are_typed_and_fail_closed() (
	source "${det_lib}"
	source "${documentation_barrier_lib}"
	local query_output stub_rc query_log setup_output output rc fail_setup=0
	query_log="$(mktemp -t ifa-documentation-ack-query.XXXXXX)"
	setup_output="$(mktemp -t ifa-documentation-ack-setup.XXXXXX)"
	trap 'rm -f "${query_log}" "${setup_output}"' EXIT
	FAULT_COMPOSE_PROJECT="test"
	use_compose=0
	ESHU_POSTGRES_DSN="postgresql://unused"
	compose_file="docker-compose.yaml"
	ifa_documentation_ack_run_id="ifa_doc_123_456_789"
	ifa_det_pg() {
		printf '%s\n' "$4" >>"${query_log}"
		if [[ "${fail_setup}" -eq 1 && "$4" == *"CREATE FUNCTION"* ]]; then
			return 9
		fi
		printf '%s' "${query_output}"
		return "${stub_rc}"
	}
	sleep() { :; }

	query_output="ignored"
	stub_rc=7
	rc=0
	output="$(ifa_documentation_install_ack_barrier test 42002 2>&1)" || rc=$?
	[[ "${rc}" -eq 7 ]] || fail "ACK barrier preflight failure returned ${rc}, want original exit 7"
	[[ "${output}" == *"preflight query FAILED (exit 7)"* ]] \
		|| fail "ACK barrier preflight failure did not preserve its query error: ${output}"

	query_output="0|1|1"
	stub_rc=0
	ifa_documentation_install_ack_barrier test 42002 \
		|| fail "valid ACK barrier setup query failed"
	rg --fixed-strings --quiet -- "BEFORE UPDATE ON public.fact_work_items" "${query_log}" \
		|| fail "ACK barrier setup did not install a fact_work_items BEFORE UPDATE trigger"
	rg --fixed-strings --quiet -- "NEW.domain = 'documentation_materialization'" "${query_log}" \
		|| fail "ACK barrier setup did not scope its trigger to documentation_materialization"
	rg --fixed-strings --quiet -- "NEW.status = 'succeeded'" "${query_log}" \
		|| fail "ACK barrier setup did not scope its trigger to succeeded ACKs"

	query_output="1|1|1"
	if ifa_documentation_install_ack_barrier test 42002 >/dev/null 2>&1; then
		fail "ACK barrier setup accepted a leaked trigger/function artifact"
	fi
	query_output="0|2|1"
	if ifa_documentation_install_ack_barrier test 42002 >/dev/null 2>&1; then
		fail "ACK barrier setup accepted an advisory key already in use"
	fi
	ifa_documentation_ack_run_id="ifa_doc_12345678901234567890_123_456"
	if ifa_documentation_install_ack_barrier test 42002 >/dev/null 2>&1; then
		fail "ACK barrier setup accepted an invalid run identity"
	fi
	ifa_documentation_ack_run_id="ifa_doc_123_456_789"
	query_output="0|1|1"
	fail_setup=1
	ifa_documentation_ack_barrier_active=0
	rc=0
	ifa_documentation_install_ack_barrier test 42002 >"${setup_output}" 2>&1 || rc=$?
	output="$(<"${setup_output}")"
	[[ "${rc}" -eq 9 ]] || fail "ACK barrier CREATE failure returned ${rc}, want original exit 9"
	[[ "${output}" == *"setup query FAILED (exit 9)"* ]] \
		|| fail "ACK barrier CREATE failure was not diagnosed distinctly: ${output}"
	[[ "${ifa_documentation_ack_barrier_active:-0}" == "1" ]] \
		|| fail "ACK barrier setup failure did not retain cleanup ownership after a clean preflight"
	fail_setup=0
	query_output=""
	bg_pids=()
	ifa_documentation_ack_producers_safe=1
	ifa_documentation_cleanup_ack_barrier test \
		|| fail "ACK barrier cleanup rejected a commit-ambiguous setup failure"
	rg --fixed-strings --quiet -- "DROP TRIGGER IF EXISTS ifa_documentation_ack_barrier" "${query_log}" \
		|| fail "commit-ambiguous setup cleanup did not attempt its trigger drop"
	rg --fixed-strings --quiet -- "DROP FUNCTION IF EXISTS public.ifa_documentation_block_ack" "${query_log}" \
		|| fail "commit-ambiguous setup cleanup did not attempt its function drop"

	query_output="ignored"
	stub_rc=8
	rc=0
	output="$(ifa_documentation_wait_for_blocked_ack test 1 2>&1)" || rc=$?
	[[ "${rc}" -eq 8 ]] || fail "blocked-ACK query failure returned ${rc}, want original exit 8"
	[[ "${output}" == *"blocked-ACK query FAILED (exit 8)"* ]] \
		|| fail "blocked-ACK query failure was not diagnosed distinctly: ${output}"

	stub_rc=0
	for query_output in "" "not-a-pid" "0" $'42001\n42002' "42001|42002|42003"; do
		rc=0
		ifa_documentation_wait_for_blocked_ack test 1 >/dev/null 2>&1 || rc=$?
		[[ "${rc}" -ne 0 ]] \
			|| fail "blocked-ACK proof accepted invalid PID pair ${query_output@Q}; want one waiter and one holder"
	done
	query_output=$' 42001|42002\n'
	[[ "$(ifa_documentation_wait_for_blocked_ack test 1)" == "42001|42002" ]] \
		|| fail "blocked-ACK proof did not return the exact waiter and holder PIDs"
	rg --fixed-strings --quiet -- "NOT barrier.granted" "${query_log}" \
		|| fail "blocked-ACK proof did not query an ungranted advisory lock"
	rg --fixed-strings --quiet -- "barrier.objsubid = 2" "${query_log}" \
		|| fail "blocked-ACK proof did not bind the two-key advisory lock identity"
	rg --fixed-strings --quiet -- "pg_catalog.pg_blocking_pids(waiter.pid)" "${query_log}" \
		|| fail "blocked-ACK proof did not verify the returned holder blocks the waiter"
)

test_ifa_documentation_waiter_cleanup_preserves_exact_claim() (
	source "${documentation_barrier_lib}"
	local query_output stub_rc query_log rc snapshot
	query_log="$(mktemp -t ifa-documentation-waiter-cleanup.XXXXXX)"
	trap 'rm -f "${query_log}"' EXIT
	FAULT_COMPOSE_PROJECT="test"
	use_compose=0
	ESHU_POSTGRES_DSN="postgresql://unused"
	compose_file="docker-compose.yaml"
	ifa_documentation_ack_run_id="ifa_doc_123_456_789"
	ifa_det_pg() {
		printf '%s\n' "$4" >>"${query_log}"
		printf '%s' "${query_output}"
		return "${stub_rc}"
	}
	sleep() { :; }

	query_output="ignored"
	stub_rc=6
	rc=0
	ifa_documentation_terminate_blocked_ack test 42001 >/dev/null 2>&1 || rc=$?
	[[ "${rc}" -eq 6 ]] || fail "blocked-ACK termination query failure returned ${rc}, want original exit 6"

	stub_rc=0
	for query_output in "" "0|false" "1|false" "2|true"; do
		rc=0
		ifa_documentation_terminate_blocked_ack test 42001 >/dev/null 2>&1 || rc=$?
		[[ "${rc}" -ne 0 ]] \
			|| fail "blocked-ACK termination accepted result ${query_output@Q}; want one exact backend"
	done
	query_output="1|true"
	ifa_documentation_terminate_blocked_ack test 42001 \
		|| fail "blocked-ACK termination rejected one exact backend"
	rg --fixed-strings --quiet -- "waiter.pid = 42001" "${query_log}" \
		|| fail "blocked-ACK termination did not target the captured waiter PID"
	rg --fixed-strings --quiet -- "NOT barrier.granted" "${query_log}" \
		|| fail "blocked-ACK termination did not revalidate the ungranted lock"
	rg --fixed-strings --quiet -- "barrier.objsubid = 2" "${query_log}" \
		|| fail "blocked-ACK termination did not revalidate the exact advisory key kind"
	rg --fixed-strings --quiet -- "waiter.application_name = 'ifa_documentation_ack_waiter_ifa_doc_123_456_789'" "${query_log}" \
		|| fail "blocked-ACK termination did not revalidate the trigger-set waiter tag"

	query_output="0"
	ifa_documentation_wait_for_ack_backend_gone test 42001 1 \
		|| fail "waiter-gone proof rejected zero matching backend/locks"
	query_output="1"
	if ifa_documentation_wait_for_ack_backend_gone test 42001 1 >/dev/null 2>&1; then
		fail "waiter-gone proof accepted a surviving backend or advisory lock"
	fi

	query_output="work-1|claimed|1|reducer:pid|1723760000|1723759990"
	snapshot="$(ifa_documentation_claim_snapshot test)" \
		|| fail "claim snapshot rejected exact attempt-1 leased documentation row"
	[[ "${snapshot}" == "${query_output}" ]] \
		|| fail "claim snapshot changed exact attempt/lease fields: ${snapshot}"
	query_output="work-1|claimed|2|reducer:pid|1723760000|1723759990"
	if ifa_documentation_claim_snapshot test >/dev/null 2>&1; then
		fail "claim snapshot accepted attempt_count other than one before replacement"
	fi
)

# A DSN-free static run skips this case. The focused Postgres proof supplies a
# disposable Compose project so every production ACK-barrier statement is
# parsed and executed by PostgreSQL, not merely matched by a shell stub.
test_ifa_documentation_ack_barrier_sql_executes_on_postgres() (
	local probe_project="${IFA_ACK_POSTGRES_PROBE_PROJECT:-}"
	[[ -n "${probe_project}" ]] || return 0
	[[ "${probe_project}" =~ ^eshu-ifa-ack-probe-[0-9]+$ ]] \
		|| fail "documentation ACK Postgres probe project is not run-unique: ${probe_project@Q}"
	source "${det_lib}"
	source "${documentation_barrier_lib}"
	local probe_dir holder_pid holder_backend_pid blocked_pair waiter_pid blocked_holder_pid
	local waiter_client_pid snapshot holder_census waiter_census server_identity container_id image_identity
	local waiter_gone_count holder_gone_count residue tagged_sessions advisory_locks ddl_residue bg_owned
	probe_dir="$(mktemp -d -t ifa-documentation-postgres.XXXXXX)"
	use_compose=1
	FAULT_COMPOSE_PROJECT="${probe_project}"
	ESHU_POSTGRES_DSN="postgresql://unused"
	compose_file="${IFA_ACK_POSTGRES_PROBE_COMPOSE_FILE:-${repo_root}/docker-compose.yaml}"
	log_dir="${probe_dir}"
	bg_pids=()
	ifa_documentation_ack_run_id="ifa_doc_123_456_789"
	ifa_documentation_ack_barrier_active=0
	ifa_documentation_ack_holder_owned=0
	ifa_documentation_ack_ddl_owned=0
	ifa_documentation_ack_waiter_pid=""
	ifa_documentation_ack_holder_pid=""
	ifa_documentation_ack_holder_backend_pid=""
	ifa_documentation_ack_producers_safe=1
	ifa_documentation_probe_cleanup() {
		local exit_rc=$? cleanup_rc=0
		set +e
		# Production cleanup has bounded waiter/holder-gone polling and bounded
		# trigger/function DDL. Run it before removing logs so a failed probe
		# cannot silently leave its tagged sessions, locks, or DDL behind.
		ifa_documentation_cleanup_ack_barrier probe >/dev/null 2>&1 || cleanup_rc=$?
		rm -rf "${probe_dir}"
		if [[ "${cleanup_rc}" -ne 0 ]]; then
			printf 'documentation ACK Postgres probe fail-safe cleanup FAILED (exit %s)\n' "${cleanup_rc}" >&2
			exit 97
		fi
		exit "${exit_rc}"
	}
	trap ifa_documentation_probe_cleanup EXIT

	server_identity="$(ifa_det_pg "${FAULT_COMPOSE_PROJECT}" 1 "${ESHU_POSTGRES_DSN}" \
		"SELECT current_setting('server_version') || '|' || current_setting('server_version_num');" \
		"${compose_file}")" || fail "documentation ACK Postgres probe could not read server identity"
	[[ "${server_identity}" =~ ^18\.[0-9]+[^|]*\|18[0-9]{4}$ ]] \
		|| fail "documentation ACK Postgres probe used an unexpected server: ${server_identity@Q}"
	container_id="$(docker compose -p "${FAULT_COMPOSE_PROJECT}" -f "${compose_file}" ps -q postgres)"
	[[ "${container_id}" =~ ^[0-9a-f]{12,64}$ ]] \
		|| fail "documentation ACK Postgres probe could not resolve its exact container"
	image_identity="$(docker inspect --format '{{.Config.Image}}|{{.Image}}' "${container_id}")"
	[[ "${image_identity}" =~ ^postgres:18-alpine\|sha256:[0-9a-f]{64}$ ]] \
		|| fail "documentation ACK Postgres probe used an unexpected image: ${image_identity@Q}"

	ifa_det_pg "${FAULT_COMPOSE_PROJECT}" 1 "${ESHU_POSTGRES_DSN}" \
		"DROP TABLE IF EXISTS public.fact_work_items; CREATE TABLE public.fact_work_items (work_item_id text PRIMARY KEY, stage text NOT NULL, domain text NOT NULL, scope_id text NOT NULL, generation_id text NOT NULL, status text NOT NULL, attempt_count integer NOT NULL, lease_owner text, claim_until timestamptz NOT NULL, updated_at timestamptz NOT NULL); INSERT INTO public.fact_work_items VALUES ('doc-work-1', 'reducer', 'documentation_materialization', 'scope-ifa-documentation-family', 'gen-ifa-documentation-family-1', 'claimed', 1, 'reducer:probe', clock_timestamp() + interval '1 minute', clock_timestamp());" \
		"${compose_file}" >/dev/null \
		|| fail "documentation ACK Postgres probe could not create its isolated work row"
	ifa_documentation_start_ack_barrier probe holder_pid holder_backend_pid \
		|| fail "documentation ACK Postgres probe could not install/acquire its barrier"
	ifa_det_pg "${FAULT_COMPOSE_PROJECT}" 1 "${ESHU_POSTGRES_DSN}" \
		"UPDATE public.fact_work_items SET status = 'succeeded', lease_owner = NULL, updated_at = clock_timestamp() WHERE work_item_id = 'doc-work-1';" \
		"${compose_file}" >"${probe_dir}/waiter.log" 2>&1 &
	waiter_client_pid=$!
	bg_pids+=("${waiter_client_pid}")
	blocked_pair="$(ifa_documentation_wait_for_blocked_ack probe 20)" \
		|| fail "documentation ACK Postgres probe did not observe its blocked ACK"
	IFS='|' read -r waiter_pid blocked_holder_pid <<<"${blocked_pair}"
	[[ "${blocked_holder_pid}" == "${holder_backend_pid}" ]] \
		|| fail "documentation ACK Postgres probe observed the wrong holder backend"
	waiter_census="$(ifa_documentation_census_ack_waiter probe)"
	holder_census="$(ifa_documentation_census_ack_holder probe)"
	[[ "${waiter_census}" == "${waiter_pid}" && "${holder_census}" == "${holder_backend_pid}" ]] \
		|| fail "documentation ACK Postgres probe census disagreed with the blocked pair"
	snapshot="$(ifa_documentation_claim_snapshot probe)" \
		|| fail "documentation ACK Postgres probe could not execute the claim snapshot"
	[[ "${snapshot}" =~ ^doc-work-1\|claimed\|1\|reducer:probe\|[0-9]+\|[0-9]+$ ]] \
		|| fail "documentation ACK Postgres probe returned an unexpected claim snapshot: ${snapshot@Q}"
	ifa_documentation_terminate_blocked_ack probe "${waiter_pid}" \
		|| fail "documentation ACK Postgres probe could not terminate its waiter"
	ifa_documentation_wait_for_ack_backend_gone probe "${waiter_pid}" 10 \
		|| fail "documentation ACK Postgres probe waiter survived termination"
	waiter_gone_count="$(ifa_det_pg "${FAULT_COMPOSE_PROJECT}" 1 "${ESHU_POSTGRES_DSN}" \
		"SELECT ((SELECT count(*) FROM pg_catalog.pg_stat_activity WHERE pid = ${waiter_pid}) + (SELECT count(*) FROM pg_catalog.pg_locks WHERE pid = ${waiter_pid}))::text;" \
		"${compose_file}")" || fail "documentation ACK Postgres probe waiter-gone count failed"
	[[ "${waiter_gone_count}" == "0" ]] \
		|| fail "documentation ACK Postgres probe waiter backend/locks survived: ${waiter_gone_count}"
	ifa_det_stop_join_untrack_bg_pid "${waiter_client_pid}" TERM \
		|| fail "documentation ACK Postgres probe could not join its waiter client"
	ifa_documentation_release_ack_barrier probe "${holder_pid}" "${holder_backend_pid}" \
		|| fail "documentation ACK Postgres probe could not release its barrier"
	holder_gone_count="$(ifa_det_pg "${FAULT_COMPOSE_PROJECT}" 1 "${ESHU_POSTGRES_DSN}" \
		"SELECT ((SELECT count(*) FROM pg_catalog.pg_stat_activity WHERE pid = ${holder_backend_pid}) + (SELECT count(*) FROM pg_catalog.pg_locks WHERE pid = ${holder_backend_pid}))::text;" \
		"${compose_file}")" || fail "documentation ACK Postgres probe holder-gone count failed"
	[[ "${holder_gone_count}" == "0" ]] \
		|| fail "documentation ACK Postgres probe holder backend/locks survived: ${holder_gone_count}"
	residue="$(ifa_det_pg "${FAULT_COMPOSE_PROJECT}" 1 "${ESHU_POSTGRES_DSN}" \
		"SELECT (SELECT count(*) FROM pg_catalog.pg_stat_activity WHERE application_name IN ('ifa_documentation_ack_holder_ifa_doc_123_456_789', 'ifa_documentation_ack_waiter_ifa_doc_123_456_789'))::text || '|' || (SELECT count(*) FROM pg_catalog.pg_locks lock_row JOIN pg_catalog.pg_stat_activity activity ON activity.pid = lock_row.pid WHERE activity.application_name IN ('ifa_documentation_ack_holder_ifa_doc_123_456_789', 'ifa_documentation_ack_waiter_ifa_doc_123_456_789') AND lock_row.locktype = 'advisory' AND lock_row.classid = 5998 AND lock_row.objid = 5994 AND lock_row.objsubid = 2)::text || '|' || ((SELECT count(*) FROM pg_catalog.pg_trigger WHERE tgname = 'ifa_documentation_ack_barrier' AND NOT tgisinternal) + (SELECT count(*) FROM pg_catalog.pg_proc WHERE proname = 'ifa_documentation_block_ack'))::text;" \
		"${compose_file}")" \
		|| fail "documentation ACK Postgres probe residue query failed"
	IFS='|' read -r tagged_sessions advisory_locks ddl_residue <<<"${residue}"
	[[ "${tagged_sessions}" == "0" && "${advisory_locks}" == "0" && "${ddl_residue}" == "0" ]] \
		|| fail "documentation ACK Postgres probe left session/lock/DDL residue: ${residue@Q}"
	bg_owned="${#bg_pids[@]}"
	[[ "${bg_owned}" == "0" ]] || fail "documentation ACK Postgres probe retained ${bg_owned} owned background PIDs"
	printf 'ACK_POSTGRES_PROBE_PASS server=%s image=%s snapshot=exact waiter_gone=%s holder_gone=%s tagged_sessions=%s advisory_locks=%s ddl=%s bg_owned=%s\n' \
		"${server_identity}" "${image_identity}" "${waiter_gone_count}" "${holder_gone_count}" \
		"${tagged_sessions}" "${advisory_locks}" "${ddl_residue}" "${bg_owned}"
)
