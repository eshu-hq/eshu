#!/usr/bin/env bash
# shellcheck disable=SC2034,SC2154
# iam_escalation-targeted live fault cells (#6228 fault half),
# mirroring scripts/lib/ifa_fault_injection_iam_can_assume_cells.sh's
# shape including the kill cell: this domain's claim is readiness-gated (see
# the readiness helper below), so the kill cell banks readiness through a
# prereq-scoped reducer before the lock goes on.
# WIRED: scripts/verify-ifa-fault-injection.sh sources this file (through
# scripts/lib/ifa_fault_injection_sources.sh) and runs all three cells
# (baseline_iam_escalation,
# killworker_iam_escalation,
# failgraphwrite_iam_escalation) against a live Postgres +
# Neo4j stack. The cells rely on the driver's strict mode and globals
# (bin_dir, tagged_bin_dir, log_dir, work_dir, use_compose, compose_file,
# FAULT_COMPOSE_PROJECT, ESHU_POSTGRES_DSN, CLAIMED_ROW_WAIT_TIMEOUT,
# drive_workers, wall_times, bg_pids, log, die, plus the fresh_stack /
# drive_all_cassettes / run_drain_gate / assert_no_dead_letters /
# capture_digest / assert_matches_baseline / teardown_cell helpers from
# ifa_fault_injection_driver.sh). Drive and assert callbacks come from
# scripts/lib/ifa_direct_family_live.sh
# (ifa_iam_escalation_drive /
# ifa_iam_escalation_assert); the cassette and expected-edge
# variables come from scripts/lib/ifa_family_fixtures.sh.
#
# scripts/lib/test-ifa-fault-injection-iam-escalation-cases.sh
# sources this file and drives
# ifa_iam_escalation_start_fact_records_lock and
# ifa_iam_escalation_release_fact_records_lock under stubs,
# and scripts/test-verify-ifa-fault-injection.sh runs that module -- so editing
# either function's fail-closed behavior or its bg_pids bookkeeping will fail
# `make pre-pr`.
#
# iam_escalation is SINGLE-STAGE and DIRECT. The handler
# (IAMEscalationMaterializationHandler) claims a fact_work_items row
# under domain "iam_escalation_materialization", loads its facts via
# factload.LoadFactsForKinds -> FROM fact_records, and writes the
# CAN_ESCALATE_TO edges DIRECTLY to the graph through its cypher writer
# (go/internal/storage/cypher/cloud/aws/iam/escalation_edge_writer.go:42),
# then acks. There is no shared_projection_intents row and no second-stage
# runner lease -- the two blocker kinds the registry schema offers that this
# family cannot use (see rows/17_iam_escalation.sh).
#
# The lock target is therefore the handler's first synchronous READ, not a
# write: ACCESS EXCLUSIVE on fact_records blocks readers, and the claim/ack
# path never touches that table, so the row stays claimed while the handler
# is held. The handler struct embeds FactLoader
# (go/internal/reducer/iamescalation/iam_escalation_materialization.go:81) and
# Handle refuses to run without it (:107) before passing it to
# factload.LoadFactsForKinds (:139) -- the pre-write window the lock needs,
# measured for codeowners at 14s of visibly-waiting backends and re-proven by
# this family's own kill cell below rather than assumed from the sibling.
#
# ifa_iam_escalation_start_fact_records_lock holds an ACCESS
# EXCLUSIVE lock on fact_records so the iam_escalation_materialization
# handler blocks on its FIRST synchronous read -- the fact load -- while its
# fact_work_items claim is already held. The claim, heartbeat, and ack path
# never reads fact_records, so the row stays claimed and alive for the whole
# hold. fact_records, not fact_work_items: the cell's own wait predicate polls
# fact_work_items, and locking that table would block the poll too.
ifa_iam_escalation_start_fact_records_lock() {
	local cell="$1" pid_var="$2"
	# Short prefix deliberately: the repo holds application_name to 63 bytes
	# (ifa_fault_generic_runner_wait.sh, ifa_fault_injection_documentation_ack_setup.sh),
	# and a full family-name prefix plus a long kill cell name would be
	# silently truncated, so the grant poll below (which matches
	# the full name) would miss forever while the holder sat on its granted
	# lock. Proven live by the sibling kubernetes family's identical failure.
	# ifa_iam_escal_lock_ keeps the longest lock name this family uses at 47 bytes.
	local app_name="ifa_iam_escal_lock_${cell}"
	if ((${#app_name} > 63)); then
		printf '%s: lock application_name is %s bytes; past the 63-byte repo cap the name truncates and the grant poll would never match\n' "${cell}" "${#app_name}" >&2
		return 1
	fi
	local lock_sql="SET application_name = '${app_name}'; BEGIN; LOCK TABLE fact_records IN ACCESS EXCLUSIVE MODE; SELECT pg_sleep(180); ROLLBACK;"
	if [[ "${use_compose}" -eq 1 ]]; then
		bash scripts/ci/compose.sh -p "${FAULT_COMPOSE_PROJECT}" -f "${compose_file}" -- exec -T postgres \
			psql -v ON_ERROR_STOP=1 -U eshu -d eshu -c "${lock_sql}" \
			>"${log_dir}/iam-escalation-lock-${cell}.log" 2>&1 &
	else
		psql "${ESHU_POSTGRES_DSN}" -v ON_ERROR_STOP=1 -c "${lock_sql}" \
			>"${log_dir}/iam-escalation-lock-${cell}.log" 2>&1 &
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
	# blocker instead of a bare timeout. Added after the live
	# killworkerkubernetesnamespaceenvironment run died on the sibling
	# family's identical window with the projector mid-sweep and named
	# nothing; this family's cell takes its lock at the same point.
	printf '%s: fact_records lock blockers at acquisition failure:\n' "${cell}"
	ifa_det_pg "${FAULT_COMPOSE_PROJECT}" "${use_compose}" "${ESHU_POSTGRES_DSN}" \
		"SELECT 'blocker_snapshot' AS snapshot_kind, a.application_name, l.locktype, l.mode, l.granted, a.state, left(a.query, 120) AS query FROM pg_locks l JOIN pg_stat_activity a ON a.pid = l.pid WHERE l.relation = 'fact_records'::regclass ORDER BY 2, 3;" \
		"${compose_file}" 2>/dev/null | sed 's/^/  /' || true
	return 1
}

# ifa_iam_escalation_release_fact_records_lock terminates
# the named lock-holder backend, then joins its local psql/docker process
# before the replacement reducer starts.
ifa_iam_escalation_release_fact_records_lock() {
	local cell="$1" holder_pid="$2"
	# Same short name as the start function: it must terminate the backend
	# the start function actually created (see its 63-byte-cap comment).
	local app_name="ifa_iam_escal_lock_${cell}"
	ifa_det_pg "${FAULT_COMPOSE_PROJECT}" "${use_compose}" "${ESHU_POSTGRES_DSN}" \
		"SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE application_name = '${app_name}';" \
		"${compose_file}" >/dev/null
	wait "${holder_pid}" 2>/dev/null || true
	ifa_det_untrack_bg_pid "${holder_pid}"
}

# ifa_iam_escalation_wait_for_readiness banks this family's claim
# readiness BEFORE the fact_records lock is taken. Like the sibling
# iam_can_assume domain, this domain's claim is gated
# (go/internal/storage/postgres/reducer_queue_readiness_sql.go:
# iam_escalation_materialization requires the
# canonical_nodes_committed/cloud_resource_uid phase), and that phase is
# published by the aws_resource_materialization reducer during Handle --
# which reads fact_records. Locking first therefore deadlocks the cell by
# design: the scoped reducer idles against a still-pending row forever
# (proven live on the sibling iam_instance_profile_role domain: 120s, zero
# claims, row still pending at death). So the cell runs a prereq-scoped
# reducer first and this helper waits for exactly the acceptance unit the
# claim gate will ask for -- resolved off the escalation row's own (scope,
# generation, entity_key-or-scope) identity, mirroring the gate's
# COALESCE(NULLIF(payload->>'entity_key', ''), scope_id) -- before the lock
# goes on. Fail-closed throughout: no identity row, a failed query, a
# malformed value, or budget exhaustion all return non-zero, and a
# non-numeric phase count is a failure, never a zero.
ifa_iam_escalation_wait_for_readiness() {
	local cell="$1" budget="$2"
	if [[ ! "${budget}" =~ ^[1-9][0-9]*$ ]]; then
		printf '%s: readiness wait: budget must be a positive integer, got %s\n' "${cell}" "${budget}" >&2
		return 1
	fi
	local identity scope generation unit
	if ! identity="$(ifa_det_pg "${FAULT_COMPOSE_PROJECT}" "${use_compose}" "${ESHU_POSTGRES_DSN}" \
		"SELECT scope_id || '|' || generation_id || '|' || COALESCE(NULLIF(payload->>'entity_key', ''), scope_id) FROM fact_work_items WHERE stage = 'reducer' AND domain = 'iam_escalation_materialization' ORDER BY created_at ASC, work_item_id ASC LIMIT 1;" \
		"${compose_file}")"; then
		printf '%s: readiness wait: escalation identity query failed\n' "${cell}" >&2
		return 1
	fi
	identity="$(printf '%s' "${identity}" | tr -d '[:space:]')"
	IFS='|' read -r scope generation unit <<<"${identity}"
	if [[ -z "${scope}" || -z "${generation}" || -z "${unit}" ]]; then
		printf '%s: readiness wait: no escalation row to resolve an identity from (got %q)\n' "${cell}" "${identity}" >&2
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

# cell_baseline_iam_escalation is this family's fault-free
# reference run. It exists because the escalation cassette is driven ONLY by
# this family's own cells: ifa_fault_injection_driver.sh's header rules that a
# new family cassette is never added to drive_all_cassettes, since the extra
# succeeded reducer row enlarges ifa_fault_redeliver_succeeded's row set inside
# cell_duplicatedelivery. So the shared `baseline` digest contains no
# CAN_ESCALATE_TO edges and every cell of this family would mismatch it.
# These cells therefore compare against
# digests[baseline_iam_escalation] via assert_matches_baseline's
# baseline_key parameter.
#
# It also establishes baseline_iam_escalation_retried: the
# fault-free excess-ATTEMPT total for domain
# iam_escalation_materialization, which the kill cell must exceed to
# prove the replacement reducer re-executed THIS domain rather than merely
# draining another queued row. Attempts, not rows: this cassette creates one
# work item, so the row-count form saturates at 1 (see
# ifa_fault_count_retry_attempts).
cell_baseline_iam_escalation() {
	local cell_start
	cell_start=$(date +%s)
	log "cell baseline-iam-escalation: fresh stack"
	fresh_stack baseline_iam_escalation
	drive_all_cassettes baseline_iam_escalation
	ifa_iam_escalation_drive "baseline_iam_escalation" "${bin_dir}" "${iam_escalation_cassette}" "${drive_workers}" "${log_dir}" \
		|| die "baseline-iam-escalation: eshu-ifa drive (escalation family) failed"
	local projector_pid reducer_pid
	ifa_det_start_bg "${log_dir}" "projector-baseline_iam_escalation" projector_pid "${bin_dir}/eshu-projector"
	ifa_det_start_bg "${log_dir}" "reducer-baseline_iam_escalation" reducer_pid "${bin_dir}/eshu-reducer"
	run_drain_gate baseline_iam_escalation
	assert_no_dead_letters baseline_iam_escalation
	ifa_iam_escalation_assert "baseline_iam_escalation" "${bin_dir}" "${iam_escalation_expected_edges}" \
		|| die "baseline-iam-escalation: fault-free graph does not match the five-edge exact set"
	baseline_iam_escalation_retried="$(ifa_fault_count_retry_attempts "${FAULT_COMPOSE_PROJECT}" "${use_compose}" "${ESHU_POSTGRES_DSN}" "${compose_file}" "iam_escalation_materialization")" \
		|| die "baseline-iam-escalation: could not count the fault-free iam_escalation_materialization retry-attempt baseline"
	printf 'baseline-iam-escalation: fault-free iam_escalation_materialization retry-attempt baseline: %s\n' "${baseline_iam_escalation_retried}"
	capture_digest baseline_iam_escalation
	teardown_cell baseline_iam_escalation
	wall_times[baseline_iam_escalation]=$(( $(date +%s) - cell_start ))
	printf 'baseline-iam-escalation: cell wall time: %ss\n' "${wall_times[baseline_iam_escalation]}"
}

cell_killworker_iam_escalation() {
	local cell_start
	cell_start=$(date +%s)
	log "cell kill-worker-after-claim-iam-escalation: fresh stack"
	fresh_stack killworkeriamescalation
	drive_all_cassettes killworkeriamescalation
	ifa_iam_escalation_drive "killworkeriamescalation" "${bin_dir}" "${iam_escalation_cassette}" "${drive_workers}" "${log_dir}" \
		|| die "kill-worker-after-claim-iam-escalation: eshu-ifa drive (escalation family) failed"
	local projector_pid reducer_pid_prereq reducer_pid_before reducer_pid_after lock_holder_pid claimed_before
	ifa_det_start_bg "${log_dir}" "projector-killworkeriamescalation" projector_pid "${bin_dir}/eshu-projector"
	# iam_escalation_materialization's reducer intent is created by the
	# PROJECTOR, not ingestion. Wait for the row to exist (any status) before
	# going further: without it there is nothing to make ready and nothing
	# to kill.
	ifa_fault_wait_for_claimed "${FAULT_COMPOSE_PROJECT}" "${use_compose}" "${ESHU_POSTGRES_DSN}" "${compose_file}" \
		"${CLAIMED_ROW_WAIT_TIMEOUT}" "iam_escalation_materialization" 1 >/dev/null \
		|| die "kill-worker-after-claim-iam-escalation: iam_escalation_materialization was never enqueued by the projector"
	# Readiness BEFORE the lock -- the shape that distinguishes this family
	# from the kubernetes kill cell. This domain's claim is gated on the
	# canonical_nodes_committed/cloud_resource_uid phase, which the
	# aws_resource_materialization reducer publishes during Handle -- and
	# Handle reads fact_records. A prereq-scoped reducer (aws_resource
	# only, so the escalation row can never be claimed early) runs unblocked
	# until the exact acceptance unit the gate will ask for is banked. Only
	# then does the lock go on. Locking first deadlocks by design: the claim
	# gate never opens while the lock is held, so the escalation-scoped
	# reducer would idle against a still-pending row until the wait budget dies.
	ifa_det_start_bg "${log_dir}" "reducer-killworkeriamescalation-prereq" reducer_pid_prereq \
		env "ESHU_REDUCER_CLAIM_DOMAIN=aws_resource_materialization" "${bin_dir}/eshu-reducer"
	ifa_iam_escalation_wait_for_readiness "killworkeriamescalation" 300 \
		|| die "kill-worker-after-claim-iam-escalation: claim readiness never banked"
	ifa_iam_escalation_start_fact_records_lock "killworkeriamescalation" lock_holder_pid \
		|| die "kill-worker-after-claim-iam-escalation: could not acquire the deterministic fact_records read blocker"
	# Scoped to iam_escalation_materialization only: with the lock already
	# held, an unscoped reducer's workers can all grab OTHER pending domains
	# that also read fact_records and get stuck there for the lock's full
	# duration, starving this domain of a worker even though its own row
	# already exists. ESHU_REDUCER_CLAIM_DOMAIN filters the claim SQL itself
	# (domain = ANY(...)), so this instance never attempts another domain's row.
	# The claim fires because readiness was banked above: the gate's phase
	# row persists, so the lock starves nothing the claim still needs.
	ifa_det_start_bg "${log_dir}" "reducer-killworkeriamescalation-before" reducer_pid_before \
		env "ESHU_REDUCER_CLAIM_DOMAIN=iam_escalation_materialization" "${bin_dir}/eshu-reducer"
	claimed_before="$(ifa_fault_wait_for_claimed "${FAULT_COMPOSE_PROJECT}" "${use_compose}" "${ESHU_POSTGRES_DSN}" "${compose_file}" "${CLAIMED_ROW_WAIT_TIMEOUT}" "iam_escalation_materialization")" \
		|| die "kill-worker-after-claim-iam-escalation: no iam_escalation_materialization row was claimed while its fact load was blocked"
	printf 'kill-worker-after-claim-iam-escalation: non-vacuous: %s blocked claimed/running row(s) observed\n' "${claimed_before}"
	kill -9 "${reducer_pid_before}" >/dev/null 2>&1 || true
	ifa_iam_escalation_release_fact_records_lock "killworkeriamescalation" "${lock_holder_pid}"
	ifa_det_start_bg "${log_dir}" "reducer-killworkeriamescalation-after" reducer_pid_after "${bin_dir}/eshu-reducer"
	run_drain_gate killworkeriamescalation
	assert_no_dead_letters killworkeriamescalation
	ifa_iam_escalation_assert "killworkeriamescalation" "${bin_dir}" "${iam_escalation_expected_edges}" \
		|| die "kill-worker-after-claim-iam-escalation: recovered graph does not match the five-edge exact set"
	ifa_fault_assert_retry_attempts_above "${FAULT_COMPOSE_PROJECT}" "${use_compose}" "${ESHU_POSTGRES_DSN}" "${compose_file}" \
		"${baseline_iam_escalation_retried}" 15 "iam_escalation_materialization" \
		|| die "kill-worker-after-claim-iam-escalation: iam_escalation_materialization did not re-execute above its fault-free retry-attempt baseline"
	capture_digest killworkeriamescalation
	assert_matches_baseline killworkeriamescalation baseline_iam_escalation
	teardown_cell killworkeriamescalation
	wall_times[killworkeriamescalation]=$(( $(date +%s) - cell_start ))
	printf 'kill-worker-after-claim-iam-escalation: cell wall time: %ss\n' "${wall_times[killworkeriamescalation]}"
}

# cell_failgraphwrite_iam_escalation fails the live
# CAN_ESCALATE_TO MERGE exactly once, proves the fault decorator's durable
# marker names that write, and requires the full five-edge set to converge
# without dead letters. Anchor read off EXECUTED statement text, not source
# text: the writer's template bakes the static CAN_ESCALATE_TO token directly
# into CanonicalIAMEscalationEdgeUpsertCypher
# (go/internal/storage/cypher/cloud/aws/iam/escalation_edge_writer.go:42),
# so the anchor carries the literal MERGE (p)-[rel:CAN_ESCALATE_TO]->(t).
# A `%s` form would match no executed statement and the fault would never
# fire, giving a green cell that tested nothing. It is NOT IAM_ESCALATION,
# which appears nowhere in code.
cell_failgraphwrite_iam_escalation() {
	local cell_start
	cell_start=$(date +%s)
	log "cell fail-graph-write-once-then-succeed-iam-escalation: fresh stack"
	fresh_stack failgraphwriteiamescalation
	drive_all_cassettes failgraphwriteiamescalation
	ifa_iam_escalation_drive "failgraphwriteiamescalation" "${bin_dir}" "${iam_escalation_cassette}" "${drive_workers}" "${log_dir}" \
		|| die "fail-graph-write-once-then-succeed-iam-escalation: eshu-ifa drive (escalation family) failed"
	local fault_once_script projector_pid reducer_pid marker_rc
	fault_once_script="${work_dir}/fault-once-then-succeed-iam-escalation.json"
	ifa_fault_write_once_script "${fault_once_script}" "${iam_escalation_edge_operation_match}" "queue-retry"
	ifa_det_start_bg "${log_dir}" "projector-failgraphwriteiamescalation" projector_pid "${bin_dir}/eshu-projector"
	ifa_det_start_bg "${log_dir}" "reducer-failgraphwriteiamescalation" reducer_pid \
		env "ESHU_IFA_FAULT_SCRIPT=${fault_once_script}" "${tagged_bin_dir}/eshu-reducer"
	run_drain_gate failgraphwriteiamescalation
	assert_no_dead_letters failgraphwriteiamescalation
	ifa_iam_escalation_assert "failgraphwriteiamescalation" "${bin_dir}" "${iam_escalation_expected_edges}" \
		|| die "fail-graph-write-once-then-succeed-iam-escalation: recovered graph does not match the five-edge exact set"
	ifa_det_pg "${FAULT_COMPOSE_PROJECT}" "${use_compose}" "${ESHU_POSTGRES_DSN}" \
		"SELECT count(*) AS total, count(*) FILTER (WHERE status = 'succeeded') AS succeeded FROM fact_work_items WHERE stage = 'reducer' AND domain = 'iam_escalation_materialization';" \
		"${compose_file}" | sed 's/^/  iam_escalation_materialization work-item window: /'
	marker_rc=0
	ifa_fault_assert_once_fault_marker "${fault_once_script}" "${iam_escalation_edge_operation_match}" || marker_rc=$?
	if [[ "${marker_rc}" -eq 2 ]]; then
		# Distinguished from "no marker" deliberately, per #5974: rc=2 means the
		# injection WORKS and fired, but on a different write than the anchor
		# names. Collapsing that into a generic failure sends the next reader
		# looking at the decorator instead of at the anchor.
		die "fail-graph-write-once-then-succeed-iam-escalation: the fault FIRED but on a different write than ${iam_escalation_edge_operation_match} (marker contents above). The injection works; the anchor is pointed at the wrong statement."
	fi
	[[ "${marker_rc}" -eq 0 ]] \
		|| die "fail-graph-write-once-then-succeed-iam-escalation: no once-fired marker at all -- the fault never fired, so this cell proved nothing about CAN_ESCALATE_TO recovery (marker status ${marker_rc})"
	# Announced, not merely checked. Every sibling cell prints its own
	# non-vacuity line and this one did not, so a passing run said nothing about
	# whether the fault had actually fired -- the check worked while being
	# invisible, which is the shape this family's whole epic is about.
	printf 'fail-graph-write-once-then-succeed-iam-escalation: non-vacuous: once-fired marker names the targeted CAN_ESCALATE_TO MERGE (written by the fault decorator at injection time, not read from the reducer log)\n'
	capture_digest failgraphwriteiamescalation
	assert_matches_baseline failgraphwriteiamescalation baseline_iam_escalation
	teardown_cell failgraphwriteiamescalation
	wall_times[failgraphwriteiamescalation]=$(( $(date +%s) - cell_start ))
	printf 'fail-graph-write-once-then-succeed-iam-escalation: cell wall time: %ss\n' "${wall_times[failgraphwriteiamescalation]}"
}
