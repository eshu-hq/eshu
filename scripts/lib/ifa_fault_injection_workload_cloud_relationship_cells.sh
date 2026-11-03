#!/usr/bin/env bash
# shellcheck disable=SC2034,SC2154
# workload_cloud_relationship-targeted live fault cells (#6228 fault trio),
# mirroring scripts/lib/ifa_fault_injection_iam_can_assume_cells.sh's
# shape including the kill cell: this domain's claim is readiness-gated (see
# the readiness helper below), so the kill cell banks readiness through a
# prereq-scoped reducer before the lock goes on.
# WIRED: scripts/verify-ifa-fault-injection.sh sources this file (through
# scripts/lib/ifa_fault_injection_sources.sh) and runs all three cells
# (baseline_workload_cloud_relationship,
# killworker_workload_cloud_relationship,
# failgraphwrite_workload_cloud_relationship) against a live Postgres +
# NornicDB stack. The cells rely on the driver's strict mode and globals
# (bin_dir, tagged_bin_dir, log_dir, work_dir, use_compose, compose_file,
# FAULT_COMPOSE_PROJECT, ESHU_POSTGRES_DSN, CLAIMED_ROW_WAIT_TIMEOUT,
# drive_workers, wall_times, bg_pids, log, die, plus the fresh_stack /
# drive_all_cassettes / run_drain_gate / assert_no_dead_letters /
# capture_digest / assert_matches_baseline / teardown_cell helpers from
# ifa_fault_injection_driver.sh). Drive and assert callbacks come from
# scripts/lib/ifa_direct_family_live.sh
# (ifa_workload_cloud_relationship_drive /
# ifa_workload_cloud_relationship_assert, the drive seeding the positive
# anchor's endpoints); the cassette and expected-edge
# variables come from scripts/lib/ifa_family_fixtures.sh.
#
# scripts/lib/test-ifa-fault-injection-workload-cloud-relationship-cases.sh
# sources this file and drives
# ifa_workload_cloud_relationship_start_fact_records_lock and
# ifa_workload_cloud_relationship_release_fact_records_lock under stubs,
# and scripts/test-verify-ifa-fault-injection.sh runs that module -- so editing
# either function's fail-closed behavior or its bg_pids bookkeeping will fail
# `make pre-pr`.
#
# workload_cloud_relationship is SINGLE-STAGE and DIRECT. The handler
# (WorkloadCloudRelationshipMaterializationHandler) claims a fact_work_items
# row under domain "workload_cloud_relationship_materialization", loads its
# facts via factload (loadFactsForKinds -> FROM
# fact_records, own scope) and projects the USES edges DIRECTLY to the
# graph through its cypher writer
# (go/internal/storage/cypher/cloud/aws/relationship/workload_cloud_writer.go:25),
# then acks. There is no shared_projection_intents row and no second-stage
# runner lease -- the two blocker kinds the registry schema offers that this
# family cannot use (see rows/16_workload_cloud_relationship.sh).
#
# The lock target is therefore the handler's first synchronous READ, not a
# write: ACCESS EXCLUSIVE on fact_records blocks readers, and the claim/ack
# path never touches that table, so the row stays claimed while the handler
# is held. The handler struct embeds FactLoader
# (go/internal/reducer/workload_cloud_relationship_materialization.go:58)
# and Handle refuses to run without it (:103) before passing it to the
# extraction path (:138) -- the pre-write window the lock needs, measured for
# codeowners at 14s of visibly-waiting backends and re-proven by this
# family's own kill cell below rather than assumed from the sibling.
#
# ifa_workload_cloud_relationship_start_fact_records_lock holds an ACCESS
# EXCLUSIVE lock on fact_records so the
# workload_cloud_relationship_materialization handler blocks on its FIRST
# synchronous read -- the fact load -- while its fact_work_items claim is
# already held. The claim, heartbeat, and ack path never reads fact_records,
# so the row stays claimed and alive for the whole hold. fact_records, not
# fact_work_items: the cell's own wait predicate polls fact_work_items, and
# locking that table would block the poll too.
ifa_workload_cloud_relationship_start_fact_records_lock() {
	local cell="$1" pid_var="$2"
	# Short prefix deliberately: the repo holds application_name to 63 bytes
	# (ifa_fault_generic_runner_wait.sh, ifa_fault_injection_documentation_ack_setup.sh),
	# and a full family-name prefix plus a long kill cell name would be
	# silently truncated, so the grant poll below (which matches
	# the full name) would miss forever while the holder sat on its granted
	# lock. Proven live by the sibling kubernetes-namespace family's identical
	# failure. ifa_wkl_rel_lock_ keeps the longest lock name this family
	# uses at 55 bytes.
	local app_name="ifa_wkl_rel_lock_${cell}"
	if ((${#app_name} > 63)); then
		printf '%s: lock application_name is %s bytes; past the 63-byte repo cap the name truncates and the grant poll would never match\n' "${cell}" "${#app_name}" >&2
		return 1
	fi
	local lock_sql="SET application_name = '${app_name}'; BEGIN; LOCK TABLE fact_records IN ACCESS EXCLUSIVE MODE; SELECT pg_sleep(180); ROLLBACK;"
	if [[ "${use_compose}" -eq 1 ]]; then
		bash scripts/ci/compose.sh -p "${FAULT_COMPOSE_PROJECT}" -f "${compose_file}" -- exec -T postgres \
			psql -v ON_ERROR_STOP=1 -U eshu -d eshu -c "${lock_sql}" \
			>"${log_dir}/workload-cloud-relationship-lock-${cell}.log" 2>&1 &
	else
		psql "${ESHU_POSTGRES_DSN}" -v ON_ERROR_STOP=1 -c "${lock_sql}" \
			>"${log_dir}/workload-cloud-relationship-lock-${cell}.log" 2>&1 &
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
	# Exhausted without a grant: snapshot who holds or waits on
	# fact_records before reporting failure, so the next reader sees the
	# blocker instead of a bare timeout.
	printf '%s: fact_records lock blockers at acquisition failure:\n' "${cell}"
	ifa_det_pg "${FAULT_COMPOSE_PROJECT}" "${use_compose}" "${ESHU_POSTGRES_DSN}" \
		"SELECT 'blocker_snapshot' AS snapshot_kind, a.application_name, l.locktype, l.mode, l.granted, a.state, left(a.query, 120) AS query FROM pg_locks l JOIN pg_stat_activity a ON a.pid = l.pid WHERE l.relation = 'fact_records'::regclass ORDER BY 2, 3;" \
		"${compose_file}" 2>/dev/null | sed 's/^/  /' || true
	return 1
}

# ifa_workload_cloud_relationship_release_fact_records_lock terminates
# the named lock-holder backend, then joins its local psql/docker process
# before the replacement reducer starts.
ifa_workload_cloud_relationship_release_fact_records_lock() {
	local cell="$1" holder_pid="$2"
	# Same short name as the start function: it must terminate the backend
	# the start function actually created (see its 63-byte-cap comment).
	local app_name="ifa_wkl_rel_lock_${cell}"
	ifa_det_pg "${FAULT_COMPOSE_PROJECT}" "${use_compose}" "${ESHU_POSTGRES_DSN}" \
		"SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE application_name = '${app_name}';" \
		"${compose_file}" >/dev/null
	wait "${holder_pid}" 2>/dev/null || true
	ifa_det_untrack_bg_pid "${holder_pid}"
}

# ifa_workload_cloud_relationship_wait_for_readiness banks this family's claim
# readiness BEFORE the fact_records lock is taken. This domain's claim is
# gated (go/internal/storage/postgres/reducer_queue_readiness_sql.go:
# workload_cloud_relationship_materialization requires the
# canonical_nodes_committed/cloud_resource_uid phase), and that phase is
# published by the aws_resource_materialization reducer during Handle --
# which reads fact_records. Locking first therefore deadlocks the cell by
# design: the scoped reducer idles against a still-pending row forever
# (proven live on the sibling iam_instance_profile_role domain: 120s, zero
# claims, row still pending at death). So the cell runs a prereq-scoped
# reducer first and this helper waits for exactly the acceptance unit the
# claim gate will ask for -- resolved off the relationship row's own (scope,
# generation, entity_key-or-scope) identity, mirroring the gate's
# COALESCE(NULLIF(payload->>'entity_key', ''), scope_id) -- before the lock
# goes on. Fail-closed throughout: no identity row, a failed query, a
# malformed value, or budget exhaustion all return non-zero, and a
# non-numeric phase count is a failure, never a zero.
ifa_workload_cloud_relationship_wait_for_readiness() {
	local cell="$1" budget="$2"
	if [[ ! "${budget}" =~ ^[1-9][0-9]*$ ]]; then
		printf '%s: readiness wait: budget must be a positive integer, got %s\n' "${cell}" "${budget}" >&2
		return 1
	fi
	local identity scope generation unit
	if ! identity="$(ifa_det_pg "${FAULT_COMPOSE_PROJECT}" "${use_compose}" "${ESHU_POSTGRES_DSN}" \
		"SELECT scope_id || '|' || generation_id || '|' || COALESCE(NULLIF(payload->>'entity_key', ''), scope_id) FROM fact_work_items WHERE stage = 'reducer' AND domain = 'workload_cloud_relationship_materialization' ORDER BY created_at ASC, work_item_id ASC LIMIT 1;" \
		"${compose_file}")"; then
		printf '%s: readiness wait: relationship identity query failed\n' "${cell}" >&2
		return 1
	fi
	identity="$(printf '%s' "${identity}" | tr -d '[:space:]')"
	IFS='|' read -r scope generation unit <<<"${identity}"
	if [[ -z "${scope}" || -z "${generation}" || -z "${unit}" ]]; then
		printf '%s: readiness wait: no relationship row to resolve an identity from (got %q)\n' "${cell}" "${identity}" >&2
		return 1
	fi
	# Values come from our own queue rows, but they ride inside string
	# literals one level down: double any quote rather than trusting shape.
	scope="${scope//\'/\'\'}"
	generation="${generation//\'/\'\'}"
	unit="${unit//\'/\'\'}"
	local i phase_count
	for i in $(seq 1 "${budget}"); do
		if ! phase_count="$(ifa_det_pg "${FAULT_COMPOSE_PROJECT}" "${use_compose}" "${ESHU_POSTGRES_DSN}" \
			"SELECT count(*) FROM graph_projection_phase_state WHERE scope_id = '${scope}' AND acceptance_unit_id = '${unit}' AND source_run_id = '${generation}' AND generation_id = '${generation}' AND keyspace = 'cloud_resource_uid' AND phase = 'canonical_nodes_committed';" \
			"${compose_file}")"; then
			printf '%s: readiness wait: phase poll query failed\n' "${cell}" >&2
			return 1
		fi
		phase_count="$(printf '%s' "${phase_count}" | tr -d '[:space:]')"
		if [[ "${phase_count}" =~ ^[1-9][0-9]*$ ]]; then
			printf '%s: readiness banked: canonical_nodes_committed/cloud_resource_uid for acceptance unit %s\n' "${cell}" "${unit}"
			return 0
		fi
		if [[ "${phase_count}" != "0" ]]; then
			printf '%s: readiness wait: phase poll returned non-numeric output %q\n' "${cell}" "${phase_count}" >&2
			return 1
		fi
		sleep 1
	done
	printf '%s: readiness wait: no canonical_nodes_committed/cloud_resource_uid phase for acceptance unit %s within %ss\n' "${cell}" "${unit}" "${budget}" >&2
	return 1
}

# cell_baseline_workload_cloud_relationship is this family's fault-free
# reference run. It exists because the workload-cloud-relationship cassette
# is driven ONLY by this family's own cells:
# ifa_fault_injection_driver.sh's header rules that a
# new family cassette is never added to drive_all_cassettes, since the extra
# succeeded reducer row enlarges ifa_fault_redeliver_succeeded's row set inside
# cell_duplicatedelivery. So the shared `baseline` digest contains no
# USES edges and every cell of this family would mismatch it.
# These cells therefore compare against
# digests[baseline_workload_cloud_relationship] via assert_matches_baseline's
# baseline_key parameter.
#
# It also establishes baseline_workload_cloud_relationship_retried: the
# fault-free excess-ATTEMPT total for domain
# workload_cloud_relationship_materialization, which the kill cell must exceed
# to prove the replacement reducer re-executed THIS domain rather than merely
# draining another queued row. Attempts, not rows: this cassette creates one
# work item, so the row-count form saturates at 1 (see
# ifa_fault_count_retry_attempts).
cell_baseline_workload_cloud_relationship() {
	local cell_start
	cell_start=$(date +%s)
	log "cell baseline-workload-cloud-relationship: fresh stack"
	fresh_stack baseline_workload_cloud_relationship
	drive_all_cassettes baseline_workload_cloud_relationship
	ifa_workload_cloud_relationship_drive "baseline_workload_cloud_relationship" "${bin_dir}" "${workload_cloud_relationship_cassette}" "${drive_workers}" "${log_dir}" \
		|| die "baseline-workload-cloud-relationship: eshu-ifa drive (workload family) failed"
	local projector_pid reducer_pid
	ifa_det_start_bg "${log_dir}" "projector-baseline_workload_cloud_relationship" projector_pid "${bin_dir}/eshu-projector"
	ifa_det_start_bg "${log_dir}" "reducer-baseline_workload_cloud_relationship" reducer_pid "${bin_dir}/eshu-reducer"
	run_drain_gate baseline_workload_cloud_relationship
	assert_no_dead_letters baseline_workload_cloud_relationship
	ifa_workload_cloud_relationship_assert "baseline_workload_cloud_relationship" "${bin_dir}" "${workload_cloud_relationship_expected_edges}" \
		|| die "baseline-workload-cloud-relationship: fault-free graph does not match the two-edge exact set"
	baseline_workload_cloud_relationship_retried="$(ifa_fault_count_retry_attempts "${FAULT_COMPOSE_PROJECT}" "${use_compose}" "${ESHU_POSTGRES_DSN}" "${compose_file}" "workload_cloud_relationship_materialization")" \
		|| die "baseline-workload-cloud-relationship: could not count the fault-free workload_cloud_relationship_materialization retry-attempt baseline"
	printf 'baseline-workload-cloud-relationship: fault-free workload_cloud_relationship_materialization retry-attempt baseline: %s\n' "${baseline_workload_cloud_relationship_retried}"
	capture_digest baseline_workload_cloud_relationship
	teardown_cell baseline_workload_cloud_relationship
	wall_times[baseline_workload_cloud_relationship]=$(( $(date +%s) - cell_start ))
	printf 'baseline-workload-cloud-relationship: cell wall time: %ss\n' "${wall_times[baseline_workload_cloud_relationship]}"
}

cell_killworker_workload_cloud_relationship() {
	local cell_start
	cell_start=$(date +%s)
	log "cell kill-worker-after-claim-workload-cloud-relationship: fresh stack"
	fresh_stack killworker_workload_cloud_relationship
	drive_all_cassettes killworker_workload_cloud_relationship
	ifa_workload_cloud_relationship_drive "killworker_workload_cloud_relationship" "${bin_dir}" "${workload_cloud_relationship_cassette}" "${drive_workers}" "${log_dir}" \
		|| die "kill-worker-after-claim-workload-cloud-relationship: eshu-ifa drive (workload family) failed"
	local projector_pid reducer_pid_prereq reducer_pid_before reducer_pid_after lock_holder_pid claimed_before
	ifa_det_start_bg "${log_dir}" "projector-killworker_workload_cloud_relationship" projector_pid "${bin_dir}/eshu-projector"
	# workload_cloud_relationship_materialization's reducer intent is created
	# by the PROJECTOR, not ingestion. Wait for the row to exist (any status)
	# before going further: without it there is nothing to make ready and
	# nothing to kill.
	ifa_fault_wait_for_claimed "${FAULT_COMPOSE_PROJECT}" "${use_compose}" "${ESHU_POSTGRES_DSN}" "${compose_file}" \
		"${CLAIMED_ROW_WAIT_TIMEOUT}" "workload_cloud_relationship_materialization" 1 >/dev/null \
		|| die "kill-worker-after-claim-workload-cloud-relationship: workload_cloud_relationship_materialization was never enqueued by the projector"
	# Readiness BEFORE the lock -- the same gated-claim shape as the sibling
	# iam_can_assume domain. This domain's claim is gated on the
	# canonical_nodes_committed/cloud_resource_uid phase, which the
	# aws_resource_materialization reducer publishes during Handle -- and
	# Handle reads fact_records. A prereq-scoped reducer (aws_resource
	# only, so the relationship row can never be claimed early) runs
	# unblocked until the exact acceptance unit the gate will ask for is
	# banked. Only then does the lock go on. Locking first deadlocks by
	# design: the claim gate never opens while the lock is held, so the
	# relationship-scoped reducer would idle against a still-pending row
	# until the wait budget dies.
	ifa_det_start_bg "${log_dir}" "reducer-killworker_workload_cloud_relationship-prereq" reducer_pid_prereq \
		env "ESHU_REDUCER_CLAIM_DOMAIN=aws_resource_materialization" "${bin_dir}/eshu-reducer"
	ifa_workload_cloud_relationship_wait_for_readiness "killworker_workload_cloud_relationship" 300 \
		|| die "kill-worker-after-claim-workload-cloud-relationship: claim readiness never banked"
	ifa_workload_cloud_relationship_start_fact_records_lock "killworker_workload_cloud_relationship" lock_holder_pid \
		|| die "kill-worker-after-claim-workload-cloud-relationship: could not acquire the deterministic fact_records read blocker"
	# Scoped to workload_cloud_relationship_materialization only: with the
	# lock already held, an unscoped reducer's workers can all grab OTHER
	# pending domains that also read fact_records and get stuck there for
	# the lock's full duration, starving this domain of a worker even though
	# its own row already exists. ESHU_REDUCER_CLAIM_DOMAIN filters the claim
	# SQL itself (domain = ANY(...)), so this instance never attempts another
	# domain's row. The claim fires because readiness was banked above: the
	# gate's phase row persists, so the lock starves nothing the claim still
	# needs.
	ifa_det_start_bg "${log_dir}" "reducer-killworker_workload_cloud_relationship-before" reducer_pid_before \
		env "ESHU_REDUCER_CLAIM_DOMAIN=workload_cloud_relationship_materialization" "${bin_dir}/eshu-reducer"
	claimed_before="$(ifa_fault_wait_for_claimed "${FAULT_COMPOSE_PROJECT}" "${use_compose}" "${ESHU_POSTGRES_DSN}" "${compose_file}" "${CLAIMED_ROW_WAIT_TIMEOUT}" "workload_cloud_relationship_materialization")" \
		|| die "kill-worker-after-claim-workload-cloud-relationship: no workload_cloud_relationship_materialization row was claimed while its fact load was blocked"
	printf 'kill-worker-after-claim-workload-cloud-relationship: non-vacuous: %s blocked claimed/running row(s) observed\n' "${claimed_before}"
	kill -9 "${reducer_pid_before}" >/dev/null 2>&1 || true
	ifa_workload_cloud_relationship_release_fact_records_lock "killworker_workload_cloud_relationship" "${lock_holder_pid}"
	ifa_det_start_bg "${log_dir}" "reducer-killworker_workload_cloud_relationship-after" reducer_pid_after "${bin_dir}/eshu-reducer"
	run_drain_gate killworker_workload_cloud_relationship
	assert_no_dead_letters killworker_workload_cloud_relationship
	ifa_workload_cloud_relationship_assert "killworker_workload_cloud_relationship" "${bin_dir}" "${workload_cloud_relationship_expected_edges}" \
		|| die "kill-worker-after-claim-workload-cloud-relationship: recovered graph does not match the two-edge exact set"
	ifa_fault_assert_retry_attempts_above "${FAULT_COMPOSE_PROJECT}" "${use_compose}" "${ESHU_POSTGRES_DSN}" "${compose_file}" \
		"${baseline_workload_cloud_relationship_retried}" 15 "workload_cloud_relationship_materialization" \
		|| die "kill-worker-after-claim-workload-cloud-relationship: workload_cloud_relationship_materialization did not re-execute above its fault-free retry-attempt baseline"
	capture_digest killworker_workload_cloud_relationship
	assert_matches_baseline killworker_workload_cloud_relationship baseline_workload_cloud_relationship
	teardown_cell killworker_workload_cloud_relationship
	wall_times[killworker_workload_cloud_relationship]=$(( $(date +%s) - cell_start ))
	printf 'kill-worker-after-claim-workload-cloud-relationship: cell wall time: %ss\n' "${wall_times[killworker_workload_cloud_relationship]}"
}

# cell_failgraphwrite_workload_cloud_relationship fails the live
# USES MERGE exactly once, proves the fault decorator's durable
# marker names that write, and requires the full two-edge set to converge
# without dead letters. Anchor read off EXECUTED statement text, not source
# text: the writer's template reads MERGE (instance)-[rel:%s]->(resource)
# (go/internal/storage/cypher/cloud/aws/relationship/workload_cloud_writer.go:25),
# filled from the closed single-member workloadCloudRelationshipVocabulary,
# so the anchor MUST carry the INTERPOLATED type MERGE
# (instance)-[rel:USES]->(resource). A literal %s would match no executed
# statement and the fault would never fire, giving a green cell that tested
# nothing. It is NOT WORKLOAD_USES_CLOUD_RESOURCE, which is statement
# metadata beside the query that never reaches the graph.
cell_failgraphwrite_workload_cloud_relationship() {
	local cell_start
	cell_start=$(date +%s)
	log "cell fail-graph-write-once-then-succeed-workload-cloud-relationship: fresh stack"
	fresh_stack failgraphwrite_workload_cloud_relationship
	drive_all_cassettes failgraphwrite_workload_cloud_relationship
	ifa_workload_cloud_relationship_drive "failgraphwrite_workload_cloud_relationship" "${bin_dir}" "${workload_cloud_relationship_cassette}" "${drive_workers}" "${log_dir}" \
		|| die "fail-graph-write-once-then-succeed-workload-cloud-relationship: eshu-ifa drive (workload family) failed"
	local fault_once_script projector_pid reducer_pid marker_rc
	fault_once_script="${work_dir}/fault-once-then-succeed-workload-cloud-relationship.json"
	ifa_fault_write_once_script "${fault_once_script}" "${workload_cloud_relationship_edge_operation_match}" "queue-retry"
	ifa_det_start_bg "${log_dir}" "projector-failgraphwrite_workload_cloud_relationship" projector_pid "${bin_dir}/eshu-projector"
	ifa_det_start_bg "${log_dir}" "reducer-failgraphwrite_workload_cloud_relationship" reducer_pid \
		env "ESHU_IFA_FAULT_SCRIPT=${fault_once_script}" "${tagged_bin_dir}/eshu-reducer"
	run_drain_gate failgraphwrite_workload_cloud_relationship
	assert_no_dead_letters failgraphwrite_workload_cloud_relationship
	ifa_workload_cloud_relationship_assert "failgraphwrite_workload_cloud_relationship" "${bin_dir}" "${workload_cloud_relationship_expected_edges}" \
		|| die "fail-graph-write-once-then-succeed-workload-cloud-relationship: recovered graph does not match the two-edge exact set"
	ifa_det_pg "${FAULT_COMPOSE_PROJECT}" "${use_compose}" "${ESHU_POSTGRES_DSN}" \
		"SELECT count(*) AS total, count(*) FILTER (WHERE status = 'succeeded') AS succeeded FROM fact_work_items WHERE stage = 'reducer' AND domain = 'workload_cloud_relationship_materialization';" \
		"${compose_file}" | sed 's/^/  workload_cloud_relationship_materialization work-item window: /'
	marker_rc=0
	ifa_fault_assert_once_fault_marker "${fault_once_script}" "${workload_cloud_relationship_edge_operation_match}" || marker_rc=$?
	if [[ "${marker_rc}" -eq 2 ]]; then
		# Distinguished from "no marker" deliberately, per #5974: rc=2 means the
		# injection WORKS and fired, but on a different write than the anchor
		# names. Collapsing that into a generic failure sends the next reader
		# looking at the decorator instead of at the anchor.
		die "fail-graph-write-once-then-succeed-workload-cloud-relationship: the fault FIRED but on a different write than ${workload_cloud_relationship_edge_operation_match} (marker contents above). The injection works; the anchor is pointed at the wrong statement."
	fi
	[[ "${marker_rc}" -eq 0 ]] \
		|| die "fail-graph-write-once-then-succeed-workload-cloud-relationship: no once-fired marker at all -- the fault never fired, so this cell proved nothing about USES recovery (marker status ${marker_rc})"
	# Announced, not merely checked. Every sibling cell prints its own
	# non-vacuity line and this one did not, so a passing run said nothing about
	# whether the fault had actually fired -- the check worked while being
	# invisible, which is the shape this family's whole epic is about.
	printf 'fail-graph-write-once-then-succeed-workload-cloud-relationship: non-vacuous: once-fired marker names the targeted USES MERGE (written by the fault decorator at injection time, not read from the reducer log)\n'
	capture_digest failgraphwrite_workload_cloud_relationship
	assert_matches_baseline failgraphwrite_workload_cloud_relationship baseline_workload_cloud_relationship
	teardown_cell failgraphwrite_workload_cloud_relationship
	wall_times[failgraphwrite_workload_cloud_relationship]=$(( $(date +%s) - cell_start ))
	printf 'fail-graph-write-once-then-succeed-workload-cloud-relationship: cell wall time: %ss\n' "${wall_times[failgraphwrite_workload_cloud_relationship]}"
}
