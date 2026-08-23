#!/usr/bin/env bash
# shellcheck disable=SC1090,SC2034,SC2154
# Hermetic proof for the durable runner-lease audit. The parent mirror owns
# strict mode, fail(), repo_root, and the library path variables.

run_ifa_fault_injection_generic_runner_lease_audit_cases() {
	test_ifa_runner_lease_hold_durable_reclaim_is_expiry_fenced
	test_ifa_runner_lease_audit_rejects_pre_expiry_transition
}

test_ifa_runner_lease_hold_durable_reclaim_is_expiry_fenced() (
	# shellcheck source=scripts/lib/ifa_fault_generic_runner_wait.sh
	source "${generic_runner_wait_lib}"
	local FAULT_COMPOSE_PROJECT=test-project use_compose=0 ESHU_POSTGRES_DSN=test-dsn
	local compose_file=test-compose.yml mode=capture captured=""
	local dead_pid=111 replacement_pid=222 nonce=0123456789abcdef0123456789abcdef
	local dead_owner="ifa-runner-lease-audit:test-host:${dead_pid}:${nonce}"
	local replacement_owner="ifa-runner-lease-audit:test-host:${replacement_pid}:${nonce}"

	ifa_det_pg() {
		local sql="$4"
		case "${mode}" in
		capture)
			[[ "${sql}" == *"runner_lease_hold capture durable leases"* &&
				"${sql}" == *"lease_expires_at > clock_timestamp() + INTERVAL '2 seconds'"* &&
				"${sql}" == *":${dead_pid}:[0-9a-f]{16,32}"* ]] || return 1
			printf '0|8|%s|200.000000|100.000000' "${dead_owner}"
			;;
		attempt)
			[[ "${sql}" == *"runner_lease_hold pre-expiry replacement attempt fence"* &&
				"${sql}" == *"(0,8,'${dead_owner}',to_timestamp(200.000000),to_timestamp(100.000000))"* &&
				"${sql}" == *"audit.event_kind = 'attempt'"* &&
				"${sql}" == *":${replacement_pid}:[0-9a-f]{16,32}"* ]] || return 1
			printf '1|1|1'
			;;
		install)
			[[ "${sql}" == *"CREATE TRIGGER ${_IFA_RUNNER_LEASE_AUDIT_ATTEMPT_TRIGGER} BEFORE INSERT"* && "${sql}" == *"CREATE TRIGGER ${_IFA_RUNNER_LEASE_AUDIT_TRANSITION_TRIGGER} AFTER INSERT OR UPDATE"* && "${sql}" == *"event_kind TEXT NOT NULL"* ]] || return 1
			;;
		expiry)
			[[ "${sql}" == *"runner_lease_hold wait captured expiry"* && "${sql}" == *"MAX(captured.dead_expiry)"* ]] || return 1
			printf '1'
			;;
		audit)
			[[ "${sql}" == *"runner_lease_hold replacement durable lease audit"* &&
				"${sql}" == *"JOIN ${_IFA_RUNNER_LEASE_AUDIT_TABLE} AS audit"* &&
				"${sql}" == *"audit.event_kind = 'transition'"* &&
				"${sql}" == *"audit.lease_expires_at - INTERVAL '8s' >= captured.dead_expiry"* &&
				"${sql}" == *"audit.lease_expires_at - INTERVAL '8s' < captured.dead_expiry"* &&
				"${sql}" == *":${replacement_pid}:[0-9a-f]{16,32}"* ]] || return 1
			printf '1|0'
			;;
		drop) [[ "${sql}" == *"DROP TRIGGER IF EXISTS ${_IFA_RUNNER_LEASE_AUDIT_ATTEMPT_TRIGGER}"* && "${sql}" == *"DROP TRIGGER IF EXISTS ${_IFA_RUNNER_LEASE_AUDIT_TRANSITION_TRIGGER}"* ]] || return 1 ;;
		reclaimed)
			[[ "${sql}" == *"runner_lease_hold post-reclaim durable lease release"* ]] || return 1
			printf '1|1|1'
			;;
		esac
	}

	ifa_fault_capture_runner_partition_leases proof handles_route "${dead_pid}" 2 captured || return 1
	[[ "${captured}" == "0|8|${dead_owner}|200.000000|100.000000" ]] || return 1
	mode=install; ifa_fault_install_runner_lease_audit proof handles_route "${captured}" || return 1
	[[ "${ifa_runner_lease_audit_owned}" -eq 1 ]] || fail "runner lease audit installation did not register cleanup ownership"
	mode=attempt; ifa_fault_wait_for_runner_lease_attempt_fenced proof handles_route "${replacement_pid}" "${captured}" 1 || return 1
	mode=expiry; ifa_fault_wait_for_runner_lease_expiry proof "${captured}" 1 || return 1
	mode=audit; ifa_fault_wait_for_replacement_runner_lease_audit proof handles_route "${replacement_pid}" "${captured}" 1 8s || return 1
	mode=reclaimed
	ifa_fault_require_runner_leases_reclaimed proof handles_route "${captured}" || return 1
	mode=drop; ifa_fault_cleanup_runner_lease_audit || return 1
	[[ "${ifa_runner_lease_audit_owned}" -eq 0 ]] || fail "runner lease audit cleanup retained ownership"
	local cell_source
	cell_source="$(<"${repo_root}/scripts/lib/ifa_fault_injection_symbol_runtime_cells.sh")"
	local owner_env='ESHU_SHARED_PROJECTION_LEASE_OWNER="${_IFA_SYMBOL_RUNTIME_RECLAIM_LEASE_OWNER}"' after_first
	after_first="${cell_source#*${owner_env}}"
	[[ "${after_first}" != "${cell_source}" && "${after_first#*${owner_env}}" != "${after_first}" ]] \
		|| fail "symbol-runtime runner cells do not pin the audit owner prefix for both reducer process boots"
	[[ "${cell_source}" == *'ESHU_SHARED_PROJECTION_LEASE_TTL="${_IFA_SYMBOL_RUNTIME_RECLAIM_LEASE_TTL}"'* &&
		"${cell_source}" == *'ifa_fault_capture_runner_partition_leases'* &&
		"${cell_source}" == *'ifa_fault_wait_for_runner_lease_attempt_fenced'* &&
		"${cell_source}" == *'ifa_fault_wait_for_replacement_runner_lease_audit'*'"${_IFA_SYMBOL_RUNTIME_RECLAIM_LEASE_TTL}"'* &&
		"${cell_source}" == *'ifa_fault_require_runner_leases_reclaimed'* ]] \
		|| fail "symbol-runtime runner cells do not prove durable dead-owner expiry and distinct-owner reclaim"
	[[ "${cell_source}" == *'ifa_fault_release_runner_lease_hold "${cell}" "${family}" "${holder_before}"'*'ifa_fault_capture_runner_partition_leases "${cell}" "${family}" "${reducer_before}"'* ]] \
		|| fail "symbol-runtime runner cell does not release the holder before capturing the killed waiter's committed durable lease"
	[[ "${cell_source}" != *'holder_after'* ]] || fail "replacement runner is hidden behind a second advisory holder instead of exercising the durable lease fence"
	[[ "$(<"${repo_root}/scripts/verify-ifa-fault-injection.sh")" == *'ifa_fault_cleanup_runner_lease_audit'* ]] \
		|| fail "top-level EXIT cleanup does not remove a caller-owned runner lease audit"
)

test_ifa_runner_lease_audit_rejects_pre_expiry_transition() (
	# shellcheck source=scripts/lib/ifa_fault_generic_runner_wait.sh
	source "${generic_runner_wait_lib}"
	local FAULT_COMPOSE_PROJECT=test-project use_compose=0 ESHU_POSTGRES_DSN=test-dsn
	local compose_file=test-compose.yml pid=222 nonce=0123456789abcdef0123456789abcdef rc=0 output
	local captured="0|8|ifa-runner-lease-audit:test-host:111:${nonce}|200.000000|100.000000"
	ifa_det_pg() {
		[[ "$4" == *"runner_lease_hold replacement durable lease audit"* ]] || return 1
		printf '1|1'
	}
	sleep() { :; }
	output="$(ifa_fault_wait_for_replacement_runner_lease_audit proof handles_route "${pid}" "${captured}" 1 8s 2>&1)" || rc=$?
	[[ "${rc}" -ne 0 && "${output}" == *"1|1, want 1|0"* ]] \
		|| fail "runner lease audit accepted a pre-expiry replacement transition (rc=${rc}, output=${output})"
)
