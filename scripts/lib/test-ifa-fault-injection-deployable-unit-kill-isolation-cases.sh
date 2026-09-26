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
	_run_ifa_deployable_unit_blocked_claim_cases
	require_deployable_unit_lock_lib "pre-kill isolation helper definition" "ifa_deployable_unit_wait_for_kill_isolation() {"
	require_deployable_unit_lock_lib "pre-kill isolation uses the gate's terminal fact predicate" "status NOT IN ('succeeded', 'superseded')"
	require_deployable_unit_lock_lib "pre-kill isolation excludes only the deliberately blocked target domain" "NOT (stage = 'reducer' AND domain = 'deployable_unit_correlation')"
	require_deployable_unit_lock_lib "pre-kill isolation drains every shared intent, including repo_dependency" "FROM shared_projection_intents WHERE completed_at IS NULL"
	require_deployable_unit_lock_lib "pre-kill isolation drains completion-event producers" "FROM cross_scope_completion_events"
	require_deployable_unit_lock_lib "pre-kill isolation is bounded by its caller's existing wait budget" "deadline timestamptz := clock_timestamp() + make_interval(secs => wait_seconds)"
	require_deployable_unit_cells "kill cell waits for cross-family isolation" 'ifa_deployable_unit_wait_for_kill_isolation "killworkerdeployableunit"'

	local claimed_line isolation_line kill_line
	claimed_line="$(rg -n --fixed-strings -- 'ifa_deployable_unit_wait_for_blocked_claim "killworkerdeployableunit"' "${deployable_unit_cells_lib}" | head -n 1 | cut -d: -f1 || true)"
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

# #7123 shape B: the kill cell used to count any claimed/running row as a
# "blocked" claim. In CI (run 36125876678) it caught a transient claim 5 ms
# before that row failed the readiness gate with a NON-counting class, so the
# kill hit no in-flight handler, the reclaim kept attempt_count, and the
# row-count comparison against a 0-or-1 baseline failed. These cases pin the
# three replacements: a DB-level readiness fence, a pg_locks census of claims
# actually parked behind the admission_decisions holder, and a per-work-item
# re-execution check.
_run_ifa_deployable_unit_blocked_claim_cases() {
	local killed_rows='0123456789abcdef0123456789abcdef 1 1790000000250000'
	require_deployable_unit_cells "kill cell waits for the readiness fence before its census" 'ifa_deployable_unit_wait_for_readiness_fence "killworkerdeployableunit"'
	require_deployable_unit_cells_count "kill cell takes the census before isolation and again at kill time" 'ifa_deployable_unit_wait_for_blocked_claim "killworkerdeployableunit"' 2
	require_deployable_unit_cells "kill cell asserts re-execution per killed work item" 'ifa_deployable_unit_assert_killed_claims_reexecuted "killworkerdeployableunit"'
	[[ "$(_ifa_count_code_matches 'ifa_fault_wait_for_claimed' "${deployable_unit_cells_lib}")" -eq 0 ]] \
		|| fail "deployable-unit kill cell still accepts any claimed/running row as a blocked claim"
	[[ "$(_ifa_count_code_matches '"${killed_retried}" -gt "${baseline_deployable_unit_retried}"' "${deployable_unit_cells_lib}")" -eq 0 ]] \
		|| fail "deployable-unit kill cell still compares a domain row count against a nondeterministic baseline"

	# The B1 shape: one claimed row carrying the non-counting readiness class
	# and no backend parked behind the holder. The old predicate accepts it.
	local old_count
	old_count="$(
		# shellcheck source=scripts/lib/ifa_fault_injection_common.sh
		source "${fault_lib}"
		ifa_det_pg() { printf '1\n'; }
		ifa_fault_wait_for_claimed test_project 0 test_dsn test_compose 1 deployable_unit_correlation
	)" || old_count=0
	[[ "${old_count}" == "1" ]] \
		|| fail "control: the retired claimed/running predicate no longer accepts the B1 transient claim (got ${old_count})"
	if (
		# shellcheck source=scripts/lib/ifa_fault_injection_deployable_unit_lock.sh
		source "${deployable_unit_lock_lib}"
		ifa_det_pg() { printf '1|0|1|0123456789abcdef0123456789abcdef 1 1790000000250000\n'; }
		ifa_deployable_unit_wait_for_blocked_claim killworkerdeployableunit test_project 0 test_dsn test_compose 1 census_rows >/dev/null 2>&1
	); then
		fail "blocked-claim census accepted a claimed row with no waiter behind the admission_decisions holder (non-counting readiness claim)"
	fi
	local census
	census="$(
		# shellcheck source=scripts/lib/ifa_fault_injection_deployable_unit_lock.sh
		source "${deployable_unit_lock_lib}"
		ifa_det_pg() {
			[[ "$4" == *"'admission_decisions'::regclass"* && "$4" == *"NOT lock_row.granted"* &&
				"$4" == *"held.granted"* && "$4" == *"ifa_deployable_unit_lock_killworkerdeployableunit"* &&
				"$4" == *"claimed = waiters"* && "$4" == *"md5(work_item_id)"* ]] || return 1
			printf '1|1|1|%s\n' "${killed_rows}"
		}
		ifa_deployable_unit_wait_for_blocked_claim killworkerdeployableunit test_project 0 test_dsn test_compose 1 census_rows >/dev/null
		printf '%s' "${census_rows}"
	)" || fail "blocked-claim census rejected one claim parked behind the admission_decisions holder"
	[[ "${census}" == "${killed_rows}" ]] \
		|| fail "blocked-claim census did not hand back the parked claim's identity (got ${census})"
	# Each state is well formed, so only the count guards can reject it: more
	# claimed rows than parked waiters (one claim is a readiness deferral), and
	# a parked waiter with no labeled holder.
	local census_state
	for census_state in "1|1|2|${killed_rows},${killed_rows/0123/4567}" "0|1|1|${killed_rows}"; do
		if (
			# shellcheck source=scripts/lib/ifa_fault_injection_deployable_unit_lock.sh
			source "${deployable_unit_lock_lib}"
			ifa_det_pg() { printf '%s\n' "${census_state}"; }
			ifa_deployable_unit_wait_for_blocked_claim killworkerdeployableunit test_project 0 test_dsn test_compose 1 census_rows >/dev/null 2>&1
		); then
			fail "blocked-claim census accepted holders|waiters|claimed state ${census_state%|*}"
		fi
	done

	if ! (
		# shellcheck source=scripts/lib/ifa_fault_injection_deployable_unit_lock.sh
		source "${deployable_unit_lock_lib}"
		ifa_det_pg() {
			[[ "$4" == *"relationship_generations"* && "$4" == *"deployment_mapping"* ]] || return 1
			printf '1|0\n'
		}
		ifa_deployable_unit_wait_for_readiness_fence killworkerdeployableunit test_project 0 test_dsn test_compose 1 >/dev/null
	); then
		fail "readiness fence rejected an open fence with one ready deployable_unit_correlation row"
	fi
	local fence_state
	for fence_state in '0|0' '1|1' 'unknown'; do
		if (
			# shellcheck source=scripts/lib/ifa_fault_injection_deployable_unit_lock.sh
			source "${deployable_unit_lock_lib}"
			ifa_det_pg() { printf '%s\n' "${fence_state}"; }
			ifa_deployable_unit_wait_for_readiness_fence killworkerdeployableunit test_project 0 test_dsn test_compose 1 >/dev/null 2>&1
		); then
			fail "readiness fence accepted state ${fence_state}"
		fi
	done

	if ! (
		# shellcheck source=scripts/lib/ifa_fault_injection_deployable_unit_lock.sh
		source "${deployable_unit_lock_lib}"
		ifa_det_pg() {
			[[ "$4" == *"w.attempt_count > killed.attempt_count"* && "$4" == *"(extract(epoch FROM w.last_attempt_at) * 1000000)::bigint > killed.last_attempt_us"* ]] || return 1
			printf '1|1\n'
		}
		ifa_deployable_unit_assert_killed_claims_reexecuted killworkerdeployableunit test_project 0 test_dsn test_compose "${killed_rows}" >/dev/null
	); then
		fail "re-execution check rejected a killed claim the replacement reclaimed"
	fi
	local reexec_rc=0
	(
		# shellcheck source=scripts/lib/ifa_fault_injection_deployable_unit_lock.sh
		source "${deployable_unit_lock_lib}"
		ifa_det_pg() { printf '1|0\n'; }
		ifa_deployable_unit_assert_killed_claims_reexecuted killworkerdeployableunit test_project 0 test_dsn test_compose "${killed_rows}" >/dev/null 2>&1
	) || reexec_rc=$?
	[[ "${reexec_rc}" -ne 0 ]] || fail "re-execution check accepted a killed claim whose attempt was never reclaimed"
	reexec_rc=0
	(
		# shellcheck source=scripts/lib/ifa_fault_injection_deployable_unit_lock.sh
		source "${deployable_unit_lock_lib}"
		ifa_det_pg() { return 17; }
		ifa_deployable_unit_assert_killed_claims_reexecuted killworkerdeployableunit test_project 0 test_dsn test_compose "${killed_rows}" >/dev/null 2>&1
	) || reexec_rc=$?
	[[ "${reexec_rc}" -eq 17 ]] || fail "re-execution check returned ${reexec_rc} on a failed query, want 17"
	if (
		# shellcheck source=scripts/lib/ifa_fault_injection_deployable_unit_lock.sh
		source "${deployable_unit_lock_lib}"
		ifa_det_pg() { printf '1|1\n'; }
		ifa_deployable_unit_assert_killed_claims_reexecuted killworkerdeployableunit test_project 0 test_dsn test_compose "x'); DROP TABLE t; -- 1 1" >/dev/null 2>&1
	); then
		fail "re-execution check accepted a killed-claim identity that is not an md5 digest"
	fi
}
