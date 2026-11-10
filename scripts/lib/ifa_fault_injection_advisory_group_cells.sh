#!/usr/bin/env bash
# shellcheck disable=SC2034,SC2154
# advisory_group-targeted live fault cells (#10466), mirroring
# scripts/lib/ifa_fault_injection_runtime_declaration_cells.sh's trio
# shape with one structural difference: this domain's intent is not
# created by the projector. Production enqueues it from a committed
# supply_chain_impact pass through AdvisoryGroupProjectionEnqueuer, so
# every cell drains TWICE -- once so the projector lands the
# Repository nodes the writer MATCHes, once after
# ifa_advisory_group_live_enqueue runs the production winners rebuild
# and enqueuer between the drains (scripts/lib/ifa_advisory_group_live.sh).
# No marker fact exists for this domain and none is invented here.
#
# WIRED: scripts/verify-ifa-fault-injection.sh sources this file
# (through scripts/lib/ifa_fault_injection_sources.sh) and runs all
# three cells (baseline_advisory_group, killworker_advisory_group,
# failgraphwrite_advisory_group) against a live Postgres + Neo4j
# stack. The cells rely on the driver's strict mode and globals
# (bin_dir, tagged_bin_dir, log_dir, work_dir, use_compose,
# compose_file, FAULT_COMPOSE_PROJECT, ESHU_POSTGRES_DSN,
# CLAIMED_ROW_WAIT_TIMEOUT, drive_workers, wall_times, bg_pids, log,
# die, plus the fresh_stack / drive_all_cassettes / run_drain_gate /
# assert_no_dead_letters / capture_digest / assert_matches_baseline /
# teardown_cell helpers from ifa_fault_injection_driver.sh).
#
# scripts/lib/test-ifa-fault-injection-advisory-group-cases.sh sources
# this file and drives ifa_advisory_group_start_fact_records_lock and
# ifa_advisory_group_release_fact_records_lock under stubs, and
# scripts/test-verify-ifa-fault-injection.sh runs that module.
#
# The handler (advisorygroup.Handler) claims a fact_work_items row
# under domain advisory_group_projection, waits for the winners
# watermark, then reads qualifying winners through a query that LEFT
# JOINs fact_records for purl and provenance members. The lock target
# is therefore that READ (fact_records), not fact_work_items, which
# the cell's own wait predicate polls.
ifa_advisory_group_start_fact_records_lock() {
	local cell="$1" pid_var="$2"
	# Short prefix deliberately: the repo holds application_name to 63
	# bytes, and a full family-name prefix plus a long kill cell name
	# would truncate silently so the grant poll never matches.
	# ifa_ag_lock_ keeps the longest name this family uses at 32 bytes.
	local app_name="ifa_ag_lock_${cell}"
	if ((${#app_name} > 63)); then
		printf '%s: lock application_name is %s bytes; past the 63-byte repo cap the name truncates and the grant poll would never match\n' "${cell}" "${#app_name}" >&2
		return 1
	fi
	local lock_sql="SET application_name = '${app_name}'; BEGIN; LOCK TABLE fact_records IN ACCESS EXCLUSIVE MODE; SELECT pg_sleep(180); ROLLBACK;"
	if [[ "${use_compose}" -eq 1 ]]; then
		bash scripts/ci/compose.sh -p "${FAULT_COMPOSE_PROJECT}" -f "${compose_file}" -- exec -T postgres \
			psql -v ON_ERROR_STOP=1 -U eshu -d eshu -c "${lock_sql}" \
			>"${log_dir}/advisory-group-lock-${cell}.log" 2>&1 &
	else
		psql "${ESHU_POSTGRES_DSN}" -v ON_ERROR_STOP=1 -c "${lock_sql}" \
			>"${log_dir}/advisory-group-lock-${cell}.log" 2>&1 &
	fi
	local holder_pid=$!
	bg_pids+=("${holder_pid}")
	printf -v "${pid_var}" '%s' "${holder_pid}"

	local i lock_count
	for i in $(seq 1 60); do
		lock_count="$(ifa_det_pg "${FAULT_COMPOSE_PROJECT}" "${use_compose}" "${ESHU_POSTGRES_DSN}" \
			"SELECT count(*) FROM pg_locks l JOIN pg_stat_activity a ON a.pid = l.pid WHERE a.application_name = '${app_name}' AND l.relation = 'fact_records'::regclass AND l.mode = 'AccessExclusiveLock' AND l.granted;" \
			"${compose_file}" | tr -d '[:space:]')"
		if [[ "${lock_count}" == "1" ]]; then
			return 0
		fi
		sleep 0.25
	done
	printf '%s: fact_records lock blockers at acquisition failure:\n' "${cell}"
	ifa_det_pg "${FAULT_COMPOSE_PROJECT}" "${use_compose}" "${ESHU_POSTGRES_DSN}" \
		"SELECT 'blocker_snapshot' AS snapshot_kind, a.application_name, l.locktype, l.mode, l.granted, a.state, left(a.query, 120) AS query FROM pg_locks l JOIN pg_stat_activity a ON a.pid = l.pid WHERE l.relation = 'fact_records'::regclass ORDER BY 2, 3;" \
		"${compose_file}" 2>/dev/null | sed 's/^/  /' || true
	return 1
}

# ifa_advisory_group_release_fact_records_lock terminates the named
# lock-holder backend, then joins its local psql/docker process before
# the replacement reducer starts.
ifa_advisory_group_release_fact_records_lock() {
	local cell="$1" holder_pid="$2"
	local app_name="ifa_ag_lock_${cell}"
	ifa_det_pg "${FAULT_COMPOSE_PROJECT}" "${use_compose}" "${ESHU_POSTGRES_DSN}" \
		"SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE application_name = '${app_name}';" \
		"${compose_file}" >/dev/null
	wait "${holder_pid}" 2>/dev/null || true
	ifa_det_untrack_bg_pid "${holder_pid}"
}

# cell_baseline_advisory_group is this family's fault-free reference
# run, family-scoped for the same reason as runtime_declaration's:
# the advisory_group cassette is driven ONLY by this family's own
# cells, so these cells compare against
# digests[baseline_advisory_group]. It also establishes
# baseline_advisory_group_retried, the fault-free excess-ATTEMPT total
# for domain advisory_group_projection, which the kill cell must
# exceed. Attempts, not rows: the enqueue admits exactly one row.
cell_baseline_advisory_group() {
	local cell_start
	cell_start=$(date +%s)
	log "cell baseline-advisory-group: fresh stack"
	fresh_stack baselineadvisory
	drive_all_cassettes baselineadvisory
	ifa_advisory_group_live_drive "${bin_dir}" "${advisory_group_cassette}" "${drive_workers}" \
		|| die "baseline-advisory-group: eshu-ifa drive (advisory_group family) failed"
	local projector_pid reducer_pid
	ifa_det_start_bg "${log_dir}" "projector-baselineadvisory" projector_pid "${bin_dir}/eshu-projector"
	ifa_det_start_bg "${log_dir}" "reducer-baselineadvisory" reducer_pid "${bin_dir}/eshu-reducer"
	run_drain_gate baselineadvisorypre
	ifa_advisory_group_live_assert_absent "${bin_dir}" baselineadvisory-pre-enqueue \
		|| die "baseline-advisory-group: AFFECTED_BY edges exist before the advisory-group enqueue; the drive must not mint them"
	ifa_advisory_group_live_enqueue "${bin_dir}" "${advisory_group_cassette}" \
		|| die "baseline-advisory-group: winners rebuild + advisory-group enqueue failed"
	run_drain_gate baselineadvisory
	assert_no_dead_letters baselineadvisory
	ifa_advisory_group_live_assert "${bin_dir}" "${advisory_group_expected_edges}" \
		|| die "baseline-advisory-group: fault-free graph does not match the two-edge exact set"
	ifa_advisory_group_live_assert_node_union "${bin_dir}" baselineadvisory \
		|| die "baseline-advisory-group: AdvisoryGroup node does not carry the full member union"
	baseline_advisory_group_retried="$(ifa_fault_count_retry_attempts "${FAULT_COMPOSE_PROJECT}" "${use_compose}" "${ESHU_POSTGRES_DSN}" "${compose_file}" "advisory_group_projection")" \
		|| die "baseline-advisory-group: could not count the fault-free advisory_group_projection retry-attempt baseline"
	printf 'baseline-advisory-group: fault-free advisory_group_projection retry-attempt baseline: %s\n' "${baseline_advisory_group_retried}"
	capture_digest baseline_advisory_group
	teardown_cell baselineadvisory
	wall_times[baselineadvisory]=$(( $(date +%s) - cell_start ))
	printf 'baseline-advisory-group: cell wall time: %ss\n' "${wall_times[baselineadvisory]}"
}

cell_killworker_advisory_group() {
	local cell_start
	cell_start=$(date +%s)
	log "cell kill-worker-after-claim-advisory-group: fresh stack"
	fresh_stack killworkeradvisory
	drive_all_cassettes killworkeradvisory
	ifa_advisory_group_live_drive "${bin_dir}" "${advisory_group_cassette}" "${drive_workers}" \
		|| die "kill-worker-after-claim-advisory-group: eshu-ifa drive (advisory_group family) failed"
	local projector_pid reducer_pid reducer_pid_before reducer_pid_after lock_holder_pid claimed_before
	ifa_det_start_bg "${log_dir}" "projector-killworkeradvisory" projector_pid "${bin_dir}/eshu-projector"
	ifa_det_start_bg "${log_dir}" "reducer-killworkeradvisory-pre" reducer_pid "${bin_dir}/eshu-reducer"
	# First drain: Repository nodes land; no advisory intent exists yet.
	run_drain_gate killworkeradvisorypre
	# Stop the pre-drain reducer before enqueueing: an unscoped reducer
	# left running would claim the intent the moment it lands and
	# finish it before the lock is even placed -- the kill would then
	# prove nothing about mid-handler recovery.
	ifa_det_stop_join_untrack_bg_pid "${reducer_pid}" TERM \
		|| die "kill-worker-after-claim-advisory-group: could not stop the pre-drain reducer"
	ifa_advisory_group_live_enqueue "${bin_dir}" "${advisory_group_cassette}" \
		|| die "kill-worker-after-claim-advisory-group: winners rebuild + advisory-group enqueue failed"
	ifa_advisory_group_start_fact_records_lock "killworkeradvisory" lock_holder_pid \
		|| die "kill-worker-after-claim-advisory-group: could not acquire the deterministic fact_records read blocker"
	# Scoped to advisory_group_projection only, for the same reason the
	# runtime_declaration cell scopes its kill-window reducer: with the
	# lock held, an unscoped reducer's workers can all grab OTHER
	# pending domains that also read fact_records and starve this
	# domain of a worker.
	ifa_det_start_bg "${log_dir}" "reducer-killworkeradvisory-before" reducer_pid_before \
		env "ESHU_REDUCER_CLAIM_DOMAIN=advisory_group_projection" "${bin_dir}/eshu-reducer"
	claimed_before="$(ifa_fault_wait_for_claimed "${FAULT_COMPOSE_PROJECT}" "${use_compose}" "${ESHU_POSTGRES_DSN}" "${compose_file}" "${CLAIMED_ROW_WAIT_TIMEOUT}" "advisory_group_projection")" \
		|| die "kill-worker-after-claim-advisory-group: no advisory_group_projection row was claimed while its winners read was blocked"
	printf 'kill-worker-after-claim-advisory-group: non-vacuous: %s blocked claimed/running row(s) observed\n' "${claimed_before}"
	kill -9 "${reducer_pid_before}" >/dev/null 2>&1 || true
	ifa_advisory_group_release_fact_records_lock "killworkeradvisory" "${lock_holder_pid}"
	ifa_det_start_bg "${log_dir}" "reducer-killworkeradvisory-after" reducer_pid_after "${bin_dir}/eshu-reducer"
	run_drain_gate killworkeradvisory
	assert_no_dead_letters killworkeradvisory
	ifa_advisory_group_live_assert "${bin_dir}" "${advisory_group_expected_edges}" \
		|| die "kill-worker-after-claim-advisory-group: recovered graph does not match the two-edge exact set"
	ifa_advisory_group_live_assert_node_union "${bin_dir}" killworkeradvisory \
		|| die "kill-worker-after-claim-advisory-group: AdvisoryGroup node does not carry the full member union after recovery"
	ifa_fault_assert_retry_attempts_above "${FAULT_COMPOSE_PROJECT}" "${use_compose}" "${ESHU_POSTGRES_DSN}" "${compose_file}" \
		"${baseline_advisory_group_retried}" 15 "advisory_group_projection" \
		|| die "kill-worker-after-claim-advisory-group: advisory_group_projection did not re-execute above its fault-free retry-attempt baseline"
	capture_digest killworkeradvisory
	assert_matches_baseline killworkeradvisory baseline_advisory_group
	teardown_cell killworkeradvisory
	wall_times[killworkeradvisory]=$(( $(date +%s) - cell_start ))
	printf 'kill-worker-after-claim-advisory-group: cell wall time: %ss\n' "${wall_times[killworkeradvisory]}"
}

# cell_failgraphwrite_advisory_group fails the live AFFECTED_BY MERGE
# exactly once, proves the fault decorator's durable marker names that
# write, and requires the full two-edge set plus node union to
# converge without dead letters. Anchor read off EXECUTED statement
# text: AdvisoryGroupUpsertCypher reads
# MERGE (r)-[e:AFFECTED_BY {package_id: row.package_id}]->(g)
# (go/internal/storage/cypher/edge/writer/advisory_group.go), and the
# anchor carries the FULL form -- a prefix stopping at the type would
# also match the stale-edge retract's MATCH in the same file.
cell_failgraphwrite_advisory_group() {
	local cell_start
	cell_start=$(date +%s)
	log "cell fail-graph-write-once-then-succeed-advisory-group: fresh stack"
	fresh_stack failgraphwriteadvisory
	drive_all_cassettes failgraphwriteadvisory
	ifa_advisory_group_live_drive "${bin_dir}" "${advisory_group_cassette}" "${drive_workers}" \
		|| die "fail-graph-write-once-then-succeed-advisory-group: eshu-ifa drive (advisory_group family) failed"
	local fault_once_script projector_pid reducer_pid marker_rc
	fault_once_script="${work_dir}/fault-once-then-succeed-advisory-group.json"
	ifa_fault_write_once_script "${fault_once_script}" "${advisory_group_edge_operation_match}" "queue-retry"
	ifa_det_start_bg "${log_dir}" "projector-failgraphwriteadvisory" projector_pid "${bin_dir}/eshu-projector"
	ifa_det_start_bg "${log_dir}" "reducer-failgraphwriteadvisory" reducer_pid \
		env "ESHU_IFA_FAULT_SCRIPT=${fault_once_script}" "${tagged_bin_dir}/eshu-reducer"
	run_drain_gate failgraphwriteadvisorypre
	ifa_advisory_group_live_enqueue "${bin_dir}" "${advisory_group_cassette}" \
		|| die "fail-graph-write-once-then-succeed-advisory-group: winners rebuild + advisory-group enqueue failed"
	run_drain_gate failgraphwriteadvisory
	assert_no_dead_letters failgraphwriteadvisory
	ifa_advisory_group_live_assert "${bin_dir}" "${advisory_group_expected_edges}" \
		|| die "fail-graph-write-once-then-succeed-advisory-group: recovered graph does not match the two-edge exact set"
	ifa_advisory_group_live_assert_node_union "${bin_dir}" failgraphwriteadvisory \
		|| die "fail-graph-write-once-then-succeed-advisory-group: AdvisoryGroup node does not carry the full member union after recovery"
	ifa_det_pg "${FAULT_COMPOSE_PROJECT}" "${use_compose}" "${ESHU_POSTGRES_DSN}" \
		"SELECT count(*) AS total, count(*) FILTER (WHERE status = 'succeeded') AS succeeded FROM fact_work_items WHERE stage = 'reducer' AND domain = 'advisory_group_projection';" \
		"${compose_file}" | sed 's/^/  advisory_group_projection work-item window: /'
	marker_rc=0
	ifa_fault_assert_once_fault_marker "${fault_once_script}" "${advisory_group_edge_operation_match}" || marker_rc=$?
	if [[ "${marker_rc}" -eq 2 ]]; then
		die "fail-graph-write-once-then-succeed-advisory-group: the fault FIRED but on a different write than ${advisory_group_edge_operation_match} (marker contents above). The injection works; the anchor is pointed at the wrong statement."
	fi
	[[ "${marker_rc}" -eq 0 ]] \
		|| die "fail-graph-write-once-then-succeed-advisory-group: no once-fired marker at all -- the fault never fired, so this cell proved nothing about AFFECTED_BY recovery (marker status ${marker_rc})"
	printf 'fail-graph-write-once-then-succeed-advisory-group: non-vacuous: once-fired marker names the targeted AFFECTED_BY MERGE (written by the fault decorator at injection time, not read from the reducer log)\n'
	capture_digest failgraphwriteadvisory
	assert_matches_baseline failgraphwriteadvisory baseline_advisory_group
	teardown_cell failgraphwriteadvisory
	wall_times[failgraphwriteadvisory]=$(( $(date +%s) - cell_start ))
	printf 'fail-graph-write-once-then-succeed-advisory-group: cell wall time: %ss\n' "${wall_times[failgraphwriteadvisory]}"
}
