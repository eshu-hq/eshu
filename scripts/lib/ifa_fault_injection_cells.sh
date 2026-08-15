#!/usr/bin/env bash
# shellcheck disable=SC2034  # The reducer_/projector_pid locals are
# filled indirectly by ifa_det_start_bg via printf -v, so shellcheck
# sees the declaration but not the write.
# shellcheck disable=SC2154  # This file is sourced by
# scripts/verify-ifa-fault-injection.sh and reads globals it owns
# (bin_dir, log_dir, work_dir, use_compose, compose_file, wall_times,
# baseline_retried, and the *_operation_match anchors). Without this,
# linting the library standalone buries a genuinely new SC2154 in ~30
# expected ones.
# The five original scripts/verify-ifa-fault-injection.sh cells (issue #4580
# P6 slice S5), each as a standalone function so the driver script can call
# them in sequence and stay under the repo's 500-line cap
# (.agents/skills/generator-script-discipline). See that script's own header
# comment for what each cell proves; this extraction changes nothing about
# cell behavior, only where the code lives. The two new SQL-targeted cells
# issue #5555 adds live in scripts/lib/ifa_fault_injection_sql_cells.sh.
#
# This file is a plain function library, not a script (no `set -euo
# pipefail`; see ifa_fault_injection_driver.sh's identical note). Every
# function here reads driver-owned globals (bin_dir, tagged_bin_dir,
# log_dir, work_dir, cassette paths, wall_times, bg_pids, log, die, plus the
# fresh_stack / drive_all_cassettes / run_drain_gate / assert_no_dead_letters
# / capture_digest / assert_matches_baseline / teardown_cell helpers from
# ifa_fault_injection_driver.sh) rather than taking them as arguments.
# cell_baseline sets the package-level baseline_retried global that
# cell_failgraphwrite reads (cell_failgraphwrite_sql in
# ifa_fault_injection_sql_cells.sh proves its own fault fired a different
# way -- see that file's header comment).

# cell_baseline (cell 1): fault-free; establishes the digest every other cell
# is compared against, the SQL relationship family's absolute-set
# non-vacuity assertion, and the fault-free retry-count snapshots the
# queue-retry cells need.
cell_baseline() {
	local cell_start
	cell_start=$(date +%s)
	log "cell baseline: fresh stack"
	fresh_stack baseline
	drive_all_cassettes baseline
	local projector_pid reducer_pid
	ifa_det_start_bg "${log_dir}" "projector-baseline" projector_pid "${bin_dir}/eshu-projector"
	ifa_det_start_bg "${log_dir}" "reducer-baseline" reducer_pid "${bin_dir}/eshu-reducer"
	run_drain_gate baseline
	assert_no_dead_letters baseline
	capture_digest baseline
	# Non-vacuity assertion for the SQL relationship family (#5351): the fault-free
	# baseline graph must carry EXACTLY the nine expected SQL edges. This is what
	# gives the per-cell "identical to baseline" digest comparison teeth for this
	# family — if the SQL family materialized zero edges, the baseline digest and
	# every recovery-cell digest would still match (empty == empty) and pass
	# vacuously; asserting the absolute set here proves the baseline is non-empty,
	# so a fault that drops a SQL edge on recovery then diverges from a KNOWN-good
	# baseline. Backs the materialized_edges:sql_relationships manifest row's
	# proof_gate: ifa-fault-injection claim.
	log "baseline: assert SQL relationship family materialized edges (absolute set, non-vacuity)"
	"${bin_dir}/eshu-ifa" assert-edges \
		-domain sql_relationships \
		-expected "${sql_expected_edges}" \
		|| die "baseline: SQL relationship family materialized edge set did not match the expected set (fault-free baseline must materialize all nine SQL edges before the recovery cells compare against it)"
	ifa_code_call_assert "baseline" "${bin_dir}" "${code_call_expected_edges}" \
		|| die "baseline: code-call family materialized edge set did not match the five-edge exact set"
	# Snapshot the fault-free retry count so cell_failgraphwrite can prove its
	# injected fault ADDED a retry this identical drive did not produce on
	# its own (guards the non-vacuity check against a natural counting-class
	# retry greening it while the decorator sits inert). Captured before
	# teardown while this cell's stack is up. cell_failgraphwrite_sql (in
	# ifa_fault_injection_sql_cells.sh) proves its own fault fired a
	# different way -- the fact_work_items attempt_count signal this captures
	# does not exist for the SQL family's shared-projection graph writes.
	baseline_retried="$(ifa_fault_count_retried "${FAULT_COMPOSE_PROJECT}" "${use_compose}" "${ESHU_POSTGRES_DSN}" "${compose_file}")"
	baseline_retried="${baseline_retried:-0}"
	printf 'baseline: fault-free gcp_resource_materialization retried rows (attempt_count>1): %s\n' "${baseline_retried}"
	baseline_code_call_retried="$(ifa_fault_count_retried "${FAULT_COMPOSE_PROJECT}" "${use_compose}" "${ESHU_POSTGRES_DSN}" "${compose_file}" "code_call_materialization")"
	baseline_code_call_retried="${baseline_code_call_retried:-0}"
	printf 'baseline: fault-free code_call_materialization retried rows (attempt_count>1): %s\n' "${baseline_code_call_retried}"
	teardown_cell baseline
	wall_times[baseline]=$(( $(date +%s) - cell_start ))
	printf 'baseline: cell wall time: %ss\n' "${wall_times[baseline]}"
}

# cell_killworker (cell 2): kill -9 the live host eshu-reducer process after a
# row is genuinely claimed, then start a fresh reducer process and let the
# fixed 1-minute lease expire and get reclaimed.
cell_killworker() {
	local cell_start
	cell_start=$(date +%s)
	log "cell kill-worker-after-claim: fresh stack"
	fresh_stack killworker
	drive_all_cassettes killworker
	local projector_pid reducer_pid_before reducer_pid_after claimed_before
	ifa_det_start_bg "${log_dir}" "projector-killworker" projector_pid "${bin_dir}/eshu-projector"
	ifa_det_start_bg "${log_dir}" "reducer-killworker-before" reducer_pid_before "${bin_dir}/eshu-reducer"
	claimed_before="$(ifa_fault_wait_for_claimed "${FAULT_COMPOSE_PROJECT}" "${use_compose}" "${ESHU_POSTGRES_DSN}" "${compose_file}" "${CLAIMED_ROW_WAIT_TIMEOUT}")" \
		|| die "kill-worker-after-claim: no row was ever claimed before the kill -- non-vacuous precondition failed"
	printf 'kill-worker-after-claim: non-vacuous: %s claimed/running row(s) observed before kill\n' "${claimed_before}"
	log "kill-worker-after-claim: kill -9 the live reducer (pid ${reducer_pid_before})"
	kill -9 "${reducer_pid_before}" >/dev/null 2>&1 || true
	log "kill-worker-after-claim: start a fresh reducer process (1-minute lease expiry reclaim)"
	ifa_det_start_bg "${log_dir}" "reducer-killworker-after" reducer_pid_after "${bin_dir}/eshu-reducer"
	run_drain_gate killworker
	assert_no_dead_letters killworker
	capture_digest killworker
	assert_matches_baseline killworker
	teardown_cell killworker
	wall_times[killworker]=$(( $(date +%s) - cell_start ))
	printf 'kill-worker-after-claim: cell wall time: %ss\n' "${wall_times[killworker]}"
}

# cell_expirelease (cell 3): force claim_until = now() directly via SQL on a
# genuinely claimed row (no kill), so the running reducer's OWN claim query
# reclaims it on the next poll while the original handler goroutine is still
# in flight.
cell_expirelease() {
	local cell_start
	cell_start=$(date +%s)
	log "cell expire-lease-mid-handler: fresh stack"
	fresh_stack expirelease
	drive_all_cassettes expirelease
	local projector_pid reducer_pid claimed_before
	ifa_det_start_bg "${log_dir}" "projector-expirelease" projector_pid "${bin_dir}/eshu-projector"
	ifa_det_start_bg "${log_dir}" "reducer-expirelease" reducer_pid "${bin_dir}/eshu-reducer"
	claimed_before="$(ifa_fault_wait_for_claimed "${FAULT_COMPOSE_PROJECT}" "${use_compose}" "${ESHU_POSTGRES_DSN}" "${compose_file}" "${CLAIMED_ROW_WAIT_TIMEOUT}")" \
		|| die "expire-lease-mid-handler: no row was ever claimed before the forced expiry -- non-vacuous precondition failed"
	printf 'expire-lease-mid-handler: non-vacuous: %s claimed/running row(s) observed before forced expiry\n' "${claimed_before}"
	log "expire-lease-mid-handler: force claim_until = now() on every claimed/running reducer row (SQL, no kill)"
	ifa_det_pg "${FAULT_COMPOSE_PROJECT}" "${use_compose}" "${ESHU_POSTGRES_DSN}" \
		"UPDATE fact_work_items SET claim_until = now() WHERE stage = 'reducer' AND status IN ('claimed', 'running');" \
		"${compose_file}" >/dev/null
	run_drain_gate expirelease
	assert_no_dead_letters expirelease
	capture_digest expirelease
	assert_matches_baseline expirelease
	teardown_cell expirelease
	wall_times[expirelease]=$(( $(date +%s) - cell_start ))
	printf 'expire-lease-mid-handler: cell wall time: %ss\n' "${wall_times[expirelease]}"
}

# cell_failgraphwrite (cell 4): the tagged (-tags ifafaultinjection)
# eshu-reducer with ESHU_IFA_FAULT_SCRIPT pointed at a queue-retry fault
# script that fails the CloudResource MERGE exactly once.
cell_failgraphwrite() {
	local cell_start
	cell_start=$(date +%s)
	log "cell fail-graph-write-once-then-succeed: fresh stack"
	fresh_stack failgraphwrite
	drive_all_cassettes failgraphwrite
	local fault_once_script projector_pid reducer_pid
	fault_once_script="${work_dir}/fault-once-then-succeed.json"
	ifa_fault_write_once_script "${fault_once_script}" "${cloud_resource_operation_match}" "queue-retry"
	ifa_det_start_bg "${log_dir}" "projector-failgraphwrite" projector_pid "${bin_dir}/eshu-projector"
	ifa_det_start_bg "${log_dir}" "reducer-failgraphwrite" reducer_pid \
		env "ESHU_IFA_FAULT_SCRIPT=${fault_once_script}" "${tagged_bin_dir}/eshu-reducer"
	run_drain_gate failgraphwrite
	assert_no_dead_letters failgraphwrite
	capture_digest failgraphwrite
	assert_matches_baseline failgraphwrite
	ifa_fault_assert_retried_above "${FAULT_COMPOSE_PROJECT}" "${use_compose}" "${ESHU_POSTGRES_DSN}" "${compose_file}" "${baseline_retried}" \
		|| die "fail-graph-write-once-then-succeed: the scripted fault never fired -- the count of retried gcp_resource_materialization intents (succeeded, attempt_count > 1) did not exceed the fault-free baseline (${baseline_retried}). An inert script, not a pass. This is the live integration proof of the ifafaultinjection decorator; if the fault never fires, root-cause the wiring (ESHU_IFA_FAULT_SCRIPT read, NewFaultingExecutor construction, operation_match against the real CloudResource MERGE text) before treating this gate as usable."
	printf 'fail-graph-write-once-then-succeed: non-vacuous: retried gcp_resource_materialization intents exceed the fault-free baseline (%s)\n' "${baseline_retried}"
	teardown_cell failgraphwrite
	wall_times[failgraphwrite]=$(( $(date +%s) - cell_start ))
	printf 'fail-graph-write-once-then-succeed: cell wall time: %ss\n' "${wall_times[failgraphwrite]}"
}

# cell_restartbackend (cell 5): the same tagged reducer with a fault script
# that pauses after the first completed graph-write group; this gate
# restarts the nornicdb Compose service while the reducer is blocked on that
# pause, then releases it. SKIPPED under --no-compose, which cannot restart
# a backend this script does not own.
cell_restartbackend() {
	if [[ "${use_compose}" -eq 0 ]]; then
		log "cell restart-backend-between-phase-groups: SKIPPED (--no-compose cannot restart a backend it does not own)"
		return 0
	fi
	local cell_start
	cell_start=$(date +%s)
	log "cell restart-backend-between-phase-groups: fresh stack"
	fresh_stack restartbackend
	drive_all_cassettes restartbackend
	local fault_restart_script restart_sentinel restart_result
	local projector_pid reducer_pid watcher_pid restart_fired
	fault_restart_script="${work_dir}/fault-restart-backend.json"
	ifa_fault_write_restart_script "${fault_restart_script}" 1
	restart_sentinel="${fault_restart_script}.restart-sentinel"
	restart_result="${work_dir}/restart-watch-result"
	ifa_det_start_bg "${log_dir}" "projector-restartbackend" projector_pid "${bin_dir}/eshu-projector"
	ifa_det_start_bg "${log_dir}" "reducer-restartbackend" reducer_pid \
		env "ESHU_IFA_FAULT_SCRIPT=${fault_restart_script}" "${tagged_bin_dir}/eshu-reducer"
	ifa_fault_watch_restart_sentinel "${restart_sentinel}" "${FAULT_COMPOSE_PROJECT}" "${compose_file}" \
		"${restart_result}" "${RESTART_SENTINEL_WAIT_TIMEOUT}" &
	watcher_pid=$!
	bg_pids+=("${watcher_pid}")
	run_drain_gate restartbackend
	wait "${watcher_pid}" 2>/dev/null || true
	restart_fired="$(cat "${restart_result}" 2>/dev/null || echo missing)"
	[[ "${restart_fired}" == "fired" ]] \
		|| die "restart-backend-between-phase-groups: the scripted fault never fired (sentinel ${restart_sentinel} never appeared) -- inert script, not a pass. Root-cause the ifafaultinjection decorator's ExecuteGroup/ExecutePhaseGroup wiring before treating this gate as usable."
	printf 'restart-backend-between-phase-groups: non-vacuous: sentinel fired, nornicdb restarted mid-drain\n'
	assert_no_dead_letters restartbackend
	capture_digest restartbackend
	assert_matches_baseline restartbackend
	teardown_cell restartbackend
	wall_times[restartbackend]=$(( $(date +%s) - cell_start ))
	printf 'restart-backend-between-phase-groups: cell wall time: %ss\n' "${wall_times[restartbackend]}"
}
