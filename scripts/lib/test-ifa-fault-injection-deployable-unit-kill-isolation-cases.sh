#!/usr/bin/env bash
# shellcheck disable=SC2154  # Sourced test helper reads parent-owned paths.

# run_ifa_fault_injection_deployable_unit_kill_isolation_cases pins the
# process-wide kill boundary in the deployable-unit recovery cell. The killed
# reducer also owns shared-projection runners; killing as soon as the target
# fact is claimed can abandon an unrelated five-minute repo_dependency
# partition lease behind a four-minute gate drain.
_ifa_deployable_unit_kill_readiness_uses_pre_kill_log() {
	local cells_file="$1"
	local pre_kill_call post_kill_call
	pre_kill_call='ifa_deployable_unit_live_assert_readiness_opened "${log_dir}" "reducer-killworkerdeployableunit-before" "killworkerdeployableunit"'
	post_kill_call='ifa_deployable_unit_live_assert_readiness_opened "${log_dir}" "reducer-killworkerdeployableunit-after" "killworkerdeployableunit"'
	[[ "$(_ifa_count_code_matches "${pre_kill_call}" "${cells_file}")" -eq 1 \
		&& "$(_ifa_count_code_matches "${post_kill_call}" "${cells_file}")" -eq 0 ]]
}

run_ifa_fault_injection_deployable_unit_kill_isolation_cases() {
	require_deployable_unit_lock_lib "pre-kill isolation helper definition" "ifa_deployable_unit_wait_for_kill_isolation() {"
	require_deployable_unit_lock_lib "pre-kill isolation uses the gate's terminal fact predicate" "status NOT IN ('succeeded', 'superseded')"
	require_deployable_unit_lock_lib "pre-kill isolation excludes only the deliberately blocked target domain" "NOT (stage = 'reducer' AND domain = 'deployable_unit_correlation')"
	require_deployable_unit_lock_lib "pre-kill isolation drains every shared intent, including repo_dependency" "FROM shared_projection_intents WHERE completed_at IS NULL"
	require_deployable_unit_lock_lib "pre-kill isolation drains completion-event producers" "FROM cross_scope_completion_events"
	require_deployable_unit_lock_lib "pre-kill isolation is bounded by its caller's existing wait budget" "deadline timestamptz := clock_timestamp() + make_interval(secs => wait_seconds)"
	require_deployable_unit_cells "kill cell waits for cross-family isolation" 'ifa_deployable_unit_wait_for_kill_isolation "killworkerdeployableunit"'

	local claimed_line isolation_line kill_line
	claimed_line="$(rg -n --fixed-strings -- 'claimed_before="$(ifa_fault_wait_for_claimed' "${deployable_unit_cells_lib}" | cut -d: -f1 || true)"
	isolation_line="$(rg -n --fixed-strings -- 'ifa_deployable_unit_wait_for_kill_isolation "killworkerdeployableunit"' "${deployable_unit_cells_lib}" | cut -d: -f1 || true)"
	kill_line="$(rg -n --fixed-strings -- 'kill -9 "${reducer_pid_before}"' "${deployable_unit_cells_lib}" | cut -d: -f1 || true)"
	[[ "${claimed_line}" =~ ^[0-9]+$ && "${isolation_line}" =~ ^[0-9]+$ && "${kill_line}" =~ ^[0-9]+$ \
		&& "${claimed_line}" -lt "${isolation_line}" && "${isolation_line}" -lt "${kill_line}" ]] \
		|| fail "deployable-unit kill isolation must run after the target claim and before kill -9 (claim=${claimed_line}, isolation=${isolation_line}, kill=${kill_line})"

	_ifa_deployable_unit_kill_readiness_uses_pre_kill_log "${deployable_unit_cells_lib}" \
		|| fail "kill-worker readiness must use the original reducer log; pre-kill isolation drains the repo-dependency witness before the replacement reducer starts"
	local mutated_cells
	mutated_cells="$(mktemp -t ifa-deployable-unit-readiness-label.XXXXXX)" \
		|| fail "could not create readiness-label mutation file"
	sed '/ifa_deployable_unit_live_assert_readiness_opened/s/reducer-killworkerdeployableunit-before/reducer-killworkerdeployableunit-after/' \
		"${deployable_unit_cells_lib}" >"${mutated_cells}"
	if _ifa_deployable_unit_kill_readiness_uses_pre_kill_log "${mutated_cells}"; then
		fail "readiness-label regression test accepted a mutation from the original reducer log to the replacement reducer log"
	fi
	rm -f -- "${mutated_cells}"

	local output failure_rc
	output="$(
		# shellcheck source=scripts/lib/ifa_fault_injection_deployable_unit_lock.sh
		source "${deployable_unit_lock_lib}"
		ifa_det_pg() { printf '0|0|0\n'; }
		ifa_deployable_unit_wait_for_kill_isolation test_cell test_project 0 test_dsn test_compose 1
	)" || fail "pre-kill isolation helper rejected an exact terminal 0|0|0 state"
	[[ "${output}" == *"other fact work=0, shared intents=0, completion events=0"* ]] \
		|| fail "pre-kill isolation helper did not report its exact terminal state"

	if (
		# shellcheck source=scripts/lib/ifa_fault_injection_deployable_unit_lock.sh
		source "${deployable_unit_lock_lib}"
		ifa_det_pg() { printf '0|1|0\n'; }
		ifa_deployable_unit_wait_for_kill_isolation test_cell test_project 0 test_dsn test_compose 1 >/dev/null 2>&1
	); then
		fail "pre-kill isolation helper accepted one nonterminal shared intent"
	fi

	if (
		# shellcheck source=scripts/lib/ifa_fault_injection_deployable_unit_lock.sh
		source "${deployable_unit_lock_lib}"
		ifa_det_pg() { return 17; }
		ifa_deployable_unit_wait_for_kill_isolation test_cell test_project 0 test_dsn test_compose 1 >/dev/null 2>&1
	); then
		fail "pre-kill isolation helper accepted a failed durable-state query"
	else
		failure_rc=$?
	fi
	[[ "${failure_rc}" -eq 17 ]] \
		|| fail "pre-kill isolation helper returned ${failure_rc}, want durable query exit 17"

	if (
		# shellcheck source=scripts/lib/ifa_fault_injection_deployable_unit_lock.sh
		source "${deployable_unit_lock_lib}"
		ifa_det_pg() { printf 'unknown\n'; }
		ifa_deployable_unit_wait_for_kill_isolation test_cell test_project 0 test_dsn test_compose 1 >/dev/null 2>&1
	); then
		fail "pre-kill isolation helper accepted malformed durable state"
	fi
	if (
		# shellcheck source=scripts/lib/ifa_fault_injection_deployable_unit_lock.sh
		source "${deployable_unit_lock_lib}"
		ifa_det_pg() { printf '0|0|0\n'; }
		ifa_deployable_unit_wait_for_kill_isolation test_cell test_project 0 test_dsn test_compose '1; SELECT 1' >/dev/null 2>&1
	); then
		fail "pre-kill isolation helper accepted a non-numeric SQL budget"
	fi
}
