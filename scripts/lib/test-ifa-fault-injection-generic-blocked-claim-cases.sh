#!/usr/bin/env bash
# shellcheck disable=SC1090,SC2034,SC2154
# Hermetic RED/GREEN proof for scripts/lib/ifa_fault_generic_blocked_claim.sh
# -- the shared_intent_lock kill proof that replaced the final-state
# attempt_count-vs-baseline poll (#10955).
#
# The poll it replaced failed open in exactly one direction: a kill window
# that never engaged (no backend ever parked behind the lock) reported a
# recovery failure, and no hermetic test could distinguish the two verdicts
# because the verdicts shared one code path. These cases pin the new
# verdicts apart with a faked ifa_det_pg, the same seam the deployable-unit
# kill-isolation cases use: each lib function makes exactly one pg call per
# invocation, so one canned stdout line per test fully determines the branch.
#
# Sourced by test-verify-ifa-fault-injection.sh; the parent owns strict mode,
# fail(), repo_root, and the ${generic_blocked_claim_lib} and
# ${generic_cells_lib} path variables.
run_ifa_fault_injection_generic_blocked_claim_cases() {
	test_ifa_generic_blocked_claim_no_waiter_is_not_engaged
	test_ifa_generic_blocked_claim_no_claim_is_not_engaged
	test_ifa_generic_blocked_claim_malformed_rows_are_unknown
	test_ifa_generic_blocked_claim_acked_before_kill_is_not_engaged
	test_ifa_generic_blocked_claim_unmoved_attempt_is_not_reexecuted
	test_ifa_generic_blocked_claim_green_passes
	test_ifa_generic_blocked_claim_snapshot_writer_is_fail_closed
	test_ifa_generic_blocked_claim_body_orders_census_before_kill_before_proof
}

# Two killed-claim entries with distinct digests, attempts, and epochs. The
# lease owners differ to prove the monotonic check does not compare them.
_ifa_test_blocked_claim_rows() {
	printf '%s' 'aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa 3 1750000000000000 holder-1,bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb 5 1750000001000000 holder-2'
}

# RED-1a: holder present, no waiter -- the lock never engaged because no
# reducer backend ever parked on it. Must die as not-engaged, must NOT emit
# the recovery-blame verdict, and must leave the output variables unset so a
# caller cannot proceed with a stale killed set.
test_ifa_generic_blocked_claim_no_waiter_is_not_engaged() (
	# shellcheck source=scripts/lib/ifa_fault_generic_blocked_claim.sh
	source "${generic_blocked_claim_lib}"
	local rc=0 output killed_rows="" census_state="unset"
	local FAULT_COMPOSE_PROJECT="test-project" use_compose=0 ESHU_POSTGRES_DSN="test-dsn" compose_file="compose.yaml"
	ifa_det_pg() { printf '1|0|0|'; }

	output="$(ifa_fault_wait_for_blocked_claim testcell testns test_materialization 30 killed_rows census_state 2>&1)" || rc=$?
	[[ "${rc}" -eq 1 ]] \
		|| fail "blocked-claim census passed with zero waiters (rc=${rc})"
	[[ "${output}" == *"blocker did not engage"* ]] \
		|| fail "a waiter-less census did not report 'blocker did not engage': ${output}"
	[[ "${output}" != *"re-executed"* ]] \
		|| fail "a waiter-less census reached for the recovery verdict"
	[[ -z "${killed_rows}" ]] \
		|| fail "a waiter-less census left a killed set behind for the caller to use"
	[[ "${census_state}" == "unset" ]] \
		|| fail "a waiter-less census left census state behind for the snapshot to use"
)

# RED-1b: waiters present but no claimed/running row for the domain -- the
# parked backends are other domains' (the normal shared-table case), so this
# family's window still never engaged. Same verdict, same emptiness.
test_ifa_generic_blocked_claim_no_claim_is_not_engaged() (
	# shellcheck source=scripts/lib/ifa_fault_generic_blocked_claim.sh
	source "${generic_blocked_claim_lib}"
	local rc=0 output killed_rows="" census_state="unset"
	local FAULT_COMPOSE_PROJECT="test-project" use_compose=0 ESHU_POSTGRES_DSN="test-dsn" compose_file="compose.yaml"
	ifa_det_pg() { printf '1|2|0|'; }

	output="$(ifa_fault_wait_for_blocked_claim testcell testns test_materialization 30 killed_rows census_state 2>&1)" || rc=$?
	[[ "${rc}" -eq 1 ]] \
		|| fail "blocked-claim census passed with zero claimed rows (rc=${rc})"
	[[ "${output}" == *"blocker did not engage"* ]] \
		|| fail "a claim-less census did not report 'blocker did not engage': ${output}"
	[[ -z "${killed_rows}" && "${census_state}" == "unset" ]] \
		|| fail "a claim-less census left output behind for the caller to use"
)

# A census row that is not "md5 attempt epoch lease" is unknown state, not
# an empty set and not a passing one. In particular a NULL last_attempt_at
# (surfaced as the literal 'null' by the snapshot query) must fail here:
# reading it as epoch zero would let the monotonic check trivially pass
# later on a row that was never attempt-stamped.
test_ifa_generic_blocked_claim_malformed_rows_are_unknown() (
	# shellcheck source=scripts/lib/ifa_fault_generic_blocked_claim.sh
	source "${generic_blocked_claim_lib}"
	local rc=0 output killed_rows="" census_state="unset"
	local FAULT_COMPOSE_PROJECT="test-project" use_compose=0 ESHU_POSTGRES_DSN="test-dsn" compose_file="compose.yaml"
	ifa_det_pg() { printf '1|1|1|not-a-killed-row'; }

	output="$(ifa_fault_wait_for_blocked_claim testcell testns test_materialization 30 killed_rows census_state 2>&1)" || rc=$?
	[[ "${rc}" -eq 1 ]] \
		|| fail "blocked-claim census passed a malformed killed row (rc=${rc})"
	[[ "${output}" == *"state is unknown"* ]] \
		|| fail "a malformed killed row was not reported as unknown state: ${output}"
)

# RED-2: the census captured two rows but one acked before SIGKILL landed.
# The kill proved nothing about it, so the verdict names the non-engaged
# blocker on this domain -- never the downstream recovery.
test_ifa_generic_blocked_claim_acked_before_kill_is_not_engaged() (
	# shellcheck source=scripts/lib/ifa_fault_generic_blocked_claim.sh
	source "${generic_blocked_claim_lib}"
	local rc=0 output rows
	local FAULT_COMPOSE_PROJECT="test-project" use_compose=0 ESHU_POSTGRES_DSN="test-dsn" compose_file="compose.yaml"
	rows="$(_ifa_test_blocked_claim_rows)"
	# The fake also pins the recheck query shape: a recheck that stopped
	# filtering to still-held rows would pass on canned output.
	ifa_det_pg() {
		[[ "$4" == *"FILTER (WHERE w.status IN ('claimed', 'running'))"* ]] || return 1
		printf '2|1'
	}

	output="$(ifa_fault_assert_blocked_claims_still_held testcell test-project 0 test-dsn compose.yaml test_materialization "${rows}" 2>&1)" || rc=$?
	[[ "${rc}" -eq 1 ]] \
		|| fail "post-kill recheck passed a killed set with an acked row (rc=${rc})"
	[[ "${output}" == *"acked before kill"* ]] \
		|| fail "an acked-before-kill row did not name the non-engaged blocker: ${output}"
)

# RED-3: both rows still held after the kill, but the replacement never
# gave them a new attempt (attempt_count and last_attempt_at unmoved). The
# monotonic proof must fail even though a final-state count-vs-baseline poll
# on a 0-baseline would have passed -- this is the exact gap #10955 closed.
test_ifa_generic_blocked_claim_unmoved_attempt_is_not_reexecuted() (
	# shellcheck source=scripts/lib/ifa_fault_generic_blocked_claim.sh
	source "${generic_blocked_claim_lib}"
	local rc=0 output rows
	local FAULT_COMPOSE_PROJECT="test-project" use_compose=0 ESHU_POSTGRES_DSN="test-dsn" compose_file="compose.yaml"
	rows="$(_ifa_test_blocked_claim_rows)"
	# The fake also pins the monotonic query shape: a proof that compared
	# with >= (or dropped a predicate) would pass on canned output.
	ifa_det_pg() {
		[[ "$4" == *"w.attempt_count > killed.attempt_count"* && "$4" == *"(extract(epoch FROM w.last_attempt_at) * 1000000)::bigint > killed.last_attempt_us"* ]] || return 1
		printf '2|1'
	}

	output="$(ifa_fault_assert_killed_claims_reexecuted testcell test-project 0 test-dsn compose.yaml test_materialization "${rows}" 2>&1)" || rc=$?
	[[ "${rc}" -eq 1 ]] \
		|| fail "monotonic proof passed an unmoved killed set (rc=${rc})"
	[[ "${output}" == *"not re-executed"* ]] \
		|| fail "an unmoved killed set did not report non-re-execution: ${output}"
)

# GREEN: the full path passes -- census captures two rows with state, the
# post-kill recheck holds both, and the monotonic check sees both bumped.
# A guard that always fails would satisfy every RED above while making the
# mechanism unusable; this is the case that pins the difference.
test_ifa_generic_blocked_claim_green_passes() (
	# shellcheck source=scripts/lib/ifa_fault_generic_blocked_claim.sh
	source "${generic_blocked_claim_lib}"
	local rc=0 got_rows got_state canned_rows
	local FAULT_COMPOSE_PROJECT="test-project" use_compose=0 ESHU_POSTGRES_DSN="test-dsn" compose_file="compose.yaml"
	# NOTE: the fake and the output variables must not use the lib's local
	# names (rows/state/raw): the fake runs in the lib's dynamic scope, so
	# under `set -u` it would see the lib's still-unset local instead of
	# this test's value -- and printf -v would land in the lib's local
	# instead of here. Names the lib never declares stay bound to this
	# scope (the kill body likewise passes killed_rows/census_state).
	canned_rows="$(_ifa_test_blocked_claim_rows)"
	ifa_det_pg() { printf '1|2|2|%s' "${canned_rows}"; }

	ifa_fault_wait_for_blocked_claim testcell testns test_materialization 30 got_rows got_state >/dev/null \
		|| fail "blocked-claim census rejected a parked claim set"
	[[ "${got_rows}" == "$(_ifa_test_blocked_claim_rows)" ]] \
		|| fail "census did not return the captured killed set unchanged"
	[[ "${got_state}" == "1|2|2" ]] \
		|| fail "census did not return the holders|waiters|claimed state, got ${got_state}"

	# Each call gets its own shape-pinned fake: a recheck SQL sent to the
	# proof (or vice versa) must not pass on canned output.
	ifa_det_pg() {
		[[ "$4" == *"FILTER (WHERE w.status IN ('claimed', 'running'))"* ]] || return 1
		printf '2|2'
	}
	ifa_fault_assert_blocked_claims_still_held testcell test-project 0 test-dsn compose.yaml test_materialization "${got_rows}" >/dev/null \
		|| fail "post-kill recheck rejected a fully held killed set"
	ifa_det_pg() {
		[[ "$4" == *"w.attempt_count > killed.attempt_count"* && "$4" == *"(extract(epoch FROM w.last_attempt_at) * 1000000)::bigint > killed.last_attempt_us"* ]] || return 1
		printf '2|2'
	}
	ifa_fault_assert_killed_claims_reexecuted testcell test-project 0 test-dsn compose.yaml test_materialization "${got_rows}" >/dev/null \
		|| fail "monotonic proof rejected a fully re-executed killed set"
)

# The snapshot writer is evidence, not a best-effort log: a pg failure while
# freezing the kill window must fail the cell rather than let the recovery
# assert run without its evidence. The GREEN half proves the file lands with
# the census and killed set inside.
test_ifa_generic_blocked_claim_snapshot_writer_is_fail_closed() (
	# shellcheck source=scripts/lib/ifa_fault_generic_blocked_claim.sh
	source "${generic_blocked_claim_lib}"
	local rc=0 tmp_dir rows snapshot
	local FAULT_COMPOSE_PROJECT="test-project" use_compose=0 ESHU_POSTGRES_DSN="test-dsn" compose_file="compose.yaml"
	tmp_dir="$(mktemp -d)"
	# shellcheck disable=SC2064
	trap "rm -rf '${tmp_dir}'" EXIT
	local log_dir="${tmp_dir}"
	rows="$(_ifa_test_blocked_claim_rows)"
	ifa_det_pg() { printf 'ifa_test_lock|AccessExclusiveLock|granted|12345'; }
	snapshot="${tmp_dir}/testcell-kill-snapshot.txt"

	_ifa_generic_write_kill_snapshot testcell shared_intent_lock testns test_materialization "${rows}" "1|2|2" "" >/dev/null \
		|| fail "snapshot writer rejected a healthy kill window"
	[[ -s "${snapshot}" ]] \
		|| fail "snapshot writer did not leave ${snapshot} behind"
	rg --fixed-strings --quiet -- "killed_rows: ${rows}" "${snapshot}" \
		|| fail "snapshot does not carry the killed set"
	rg --fixed-strings --quiet -- "census: holders|waiters|claimed=1|2|2" "${snapshot}" \
		|| fail "snapshot does not carry the census that authorized SIGKILL"

	ifa_det_pg() { return 1; }
	_ifa_generic_write_kill_snapshot testcell shared_intent_lock testns test_materialization "${rows}" "1|2|2" "" >/dev/null 2>&1 || rc=$?
	[[ "${rc}" -eq 1 ]] \
		|| fail "snapshot writer passed a failed pg_locks freeze (rc=${rc})"
)

# The branches above prove each helper BEHAVES correctly when called. They
# cannot prove the body calls them in an order that means anything: a
# census after the release, or a monotonic proof before the kill, would pass
# every one of them. This pins the sequence inside the shared body --
# drive-produced check, then census, then SIGKILL, then hold recheck, then
# lock release, then monotonic proof.
test_ifa_generic_blocked_claim_body_orders_census_before_kill_before_proof() {
	local body drive_line census_line kill_line recheck_line release_line proof_line
	body="$(sed -n '/^_ifa_generic_cell_killworker_body() {$/,/^}$/p' \
		"${generic_cells_lib}")"
	[[ -n "${body}" ]] \
		|| fail "could not find _ifa_generic_cell_killworker_body in ${generic_cells_lib##*/}"
	for needle in \
		'drive|_ifa_generic_wait_for_claimed "${family}" "${cell}" "${CLAIMED_ROW_WAIT_TIMEOUT}" 1' \
		'census|ifa_fault_wait_for_blocked_claim "${cell}" "${blocker_arg}" "${wait_key}" "${CLAIMED_ROW_WAIT_TIMEOUT}" killed_rows census_state' \
		'kill|ifa_det_stop_join_untrack_bg_pid "${reducer_pid_before}" KILL' \
		'snapshot|_ifa_generic_write_kill_snapshot "${cell}" "${blocker_kind}" "${blocker_arg}" "${wait_key}" "${killed_rows}" "${census_state}" "${claimed_before}"' \
		'recheck|ifa_fault_assert_blocked_claims_still_held' \
		'release|ifa_fault_release_shared_intent_lock "${cell}" "${blocker_arg}" "${lock_holder_pid}"' \
		'proof|ifa_fault_assert_killed_claims_reexecuted'; do
		# shellcheck disable=SC2162 # name|needle split is intentional.
		IFS='|' read name probe <<<"${needle}"
		line="$(printf '%s\n' "${body}" \
			| rg --line-number --fixed-strings --max-count 1 -- "${probe}" \
			| cut -d: -f1 || true)"
		[[ -n "${line}" ]] \
			|| fail "kill-worker body never invokes ${name} (${probe}); the proof step is not wired"
		printf -v "${name}_line" '%s' "${line}"
	done
	[[ "${drive_line}" -lt "${census_line}" ]] \
		|| fail "drive-produced check (line ${drive_line}) does not precede the census (line ${census_line})"
	[[ "${census_line}" -lt "${kill_line}" ]] \
		|| fail "blocked-claim census (line ${census_line}) does not precede SIGKILL (line ${kill_line})"
	[[ "${kill_line}" -lt "${snapshot_line}" ]] \
		|| fail "SIGKILL (line ${kill_line}) does not precede the kill-window snapshot freeze (line ${snapshot_line})"
	[[ "${snapshot_line}" -lt "${recheck_line}" ]] \
		|| fail "kill-window snapshot freeze (line ${snapshot_line}) does not precede the hold recheck (line ${recheck_line})"
	[[ "${recheck_line}" -lt "${release_line}" ]] \
		|| fail "hold recheck (line ${recheck_line}) does not precede lock release (line ${release_line})"
	[[ "${release_line}" -lt "${proof_line}" ]] \
		|| fail "lock release (line ${release_line}) does not precede the monotonic proof (line ${proof_line})"
}
