#!/usr/bin/env bash
# shellcheck disable=SC2034,SC2154
# incident_routing_evidence-targeted live fault cells (#6228
# incident_routing_evidence live leaf), mirroring
# scripts/lib/ifa_fault_injection_crossplane_satisfied_by_cells.sh's shape
# for the kill cell: this domain carries no claim-time readiness requirement
# and the handler holds no in-handler readiness gate before its load, so the
# first claim blocks deterministically in the load and no prereq-scoped
# reducer is needed (the observability_coverage dual-domain prereq shape
# does not transfer: ITS alarm edge resolves against a separately-committed
# EC2 node).
# WIRED: scripts/verify-ifa-fault-injection.sh sources this file (through
# scripts/lib/ifa_fault_injection_sources.sh) and runs all three cells
# (baseline_incident_routing_evidence,
# killworker_incident_routing_evidence,
# failgraphwrite_incident_routing_evidence) against a live Postgres +
# NornicDB stack. The cells rely on the driver's strict mode and globals
# (bin_dir, tagged_bin_dir, log_dir, work_dir, use_compose, compose_file,
# FAULT_COMPOSE_PROJECT, ESHU_POSTGRES_DSN, CLAIMED_ROW_WAIT_TIMEOUT,
# drive_workers, wall_times, bg_pids, plus the fresh_stack /
# drive_all_cassettes / run_drain_gate / assert_no_dead_letters /
# capture_digest / assert_matches_baseline / teardown_cell helpers from
# ifa_fault_injection_driver.sh). Drive and assert callbacks come from
# scripts/lib/ifa_direct_family_live_incident_routing_evidence.sh
# (ifa_incident_routing_evidence_drive /
# ifa_incident_routing_evidence_assert); the cassette and expected-edge
# variables come from scripts/lib/ifa_family_fixtures.sh.
#
# scripts/lib/test-ifa-fault-injection-incident-routing-evidence-cases.sh
# sources this file and drives
# ifa_incident_routing_evidence_start_fact_records_lock and
# ifa_incident_routing_evidence_release_fact_records_lock under stubs, and
# scripts/test-verify-ifa-fault-injection.sh runs that module -- so editing
# either of those functions' fail-closed behavior or bg_pids bookkeeping
# will fail `make pre-pr`.
#
# incident_routing_evidence is SINGLE-STAGE and DIRECT. The handler
# (IncidentRoutingMaterializationHandler) claims a fact_work_items row
# under domain "incident_routing_materialization", loads its facts via
# LoadIncidentRoutingRawEvidence -> ListFactsByKind FROM fact_records plus
# its service-name-scoped content_entities declared read, and writes the
# HAS_*_ROUTING edges DIRECTLY to the graph through its edge writer
# (go/internal/storage/cypher/edge/writer/incident_routing_evidence.go:73),
# then acks. There is no shared_projection_intents row and no second-stage
# runner lease -- the two blocker kinds the registry schema offers that
# this family cannot use (see rows/30_incident_routing_evidence.sh).
#
# The lock target is therefore the handler's first synchronous READ, not a
# write: ACCESS EXCLUSIVE on fact_records blocks readers, and the claim/ack
# path never touches that table, so the row stays claimed while the handler
# is held. The handler refuses to run without its Loader
# (go/internal/reducer/incident/routing_materialization.go:64) before
# loading through it at :72 into ListFactsByKind -- the pre-write window
# the lock needs, measured for codeowners at 14s of visibly-waiting
# backends and re-proven by this family's own kill cell below rather than
# assumed from the sibling.
#
# ifa_incident_routing_evidence_start_fact_records_lock holds an ACCESS
# EXCLUSIVE lock on fact_records so the incident_routing_materialization
# handler blocks on its FIRST synchronous read -- the fact load -- while its
# fact_work_items claim is already held. The claim, heartbeat, and ack path
# never reads fact_records, so the row stays claimed and alive for the whole
# hold. fact_records, not fact_work_items: the cell's own wait predicate polls
# fact_work_items, and locking that table would block the poll too.
ifa_incident_routing_evidence_start_fact_records_lock() {
	local cell="$1" pid_var="$2"
	# Short prefix deliberately: the repo holds application_name to 63 bytes
	# (ifa_fault_generic_runner_wait.sh, ifa_fault_injection_documentation_ack_setup.sh),
	# and a full family-name prefix plus a long kill cell name would be
	# silently truncated, so the grant poll below (which matches
	# the full name) would miss forever while the holder sat on its granted
	# lock. Proven live by the sibling kubernetes family's identical failure.
	# ifa_incroute_lock_ keeps the longest name this family uses at 50 bytes.
	local app_name="ifa_incroute_lock_${cell}"
	if ((${#app_name} > 63)); then
		printf '%s: lock application_name is %s bytes; past the 63-byte repo cap the name truncates and the grant poll would never match\n' "${cell}" "${#app_name}" >&2
		return 1
	fi
	local lock_sql="SET application_name = '${app_name}'; BEGIN; LOCK TABLE fact_records IN ACCESS EXCLUSIVE MODE; SELECT pg_sleep(180); ROLLBACK;"
	if [[ "${use_compose}" -eq 1 ]]; then
		bash scripts/ci/compose.sh -p "${FAULT_COMPOSE_PROJECT}" -f "${compose_file}" -- exec -T postgres \
			psql -v ON_ERROR_STOP=1 -U eshu -d eshu -c "${lock_sql}" \
			>"${log_dir}/incident-routing-evidence-lock-${cell}.log" 2>&1 &
	else
		psql "${ESHU_POSTGRES_DSN}" -v ON_ERROR_STOP=1 -c "${lock_sql}" \
			>"${log_dir}/incident-routing-evidence-lock-${cell}.log" 2>&1 &
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

# ifa_incident_routing_evidence_release_fact_records_lock terminates
# the named lock-holder backend, then joins its local psql/docker process
# before the replacement reducer starts.
ifa_incident_routing_evidence_release_fact_records_lock() {
	local cell="$1" holder_pid="$2"
	# Same short name as the start function: it must terminate the backend
	# the start function actually created (see its 63-byte-cap comment).
	local app_name="ifa_incroute_lock_${cell}"
	ifa_det_pg "${FAULT_COMPOSE_PROJECT}" "${use_compose}" "${ESHU_POSTGRES_DSN}" \
		"SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE application_name = '${app_name}';" \
		"${compose_file}" >/dev/null
	wait "${holder_pid}" 2>/dev/null || true
	ifa_det_untrack_bg_pid "${holder_pid}"
}

# cell_baseline_incident_routing_evidence is this family's fault-free
# reference run. It exists because the incident_routing_evidence cassette is driven
# ONLY by this family's own cells:
# ifa_fault_injection_driver.sh's header rules that a new family cassette is
# never added to drive_all_cassettes, since the extra succeeded reducer row
# enlarges ifa_fault_redeliver_succeeded's row set inside
# cell_duplicatedelivery. So the shared `baseline` digest contains no
# HAS_*_ROUTING edges and every cell of this family would mismatch it.
# These cells therefore compare against
# digests[baseline_incident_routing_evidence] via assert_matches_baseline's
# baseline_key parameter.
#
# It also establishes baseline_incident_routing_evidence_retried: the
# fault-free excess-ATTEMPT total for domain
# incident_routing_materialization, which the kill cell must exceed to
# prove the replacement reducer re-executed THIS domain rather than merely
# draining another queued row. Attempts, not rows: this cassette creates one
# work item, so the row-count form saturates at 1 (see
# ifa_fault_count_retry_attempts).
cell_baseline_incident_routing_evidence() {
	local cell_start
	cell_start=$(date +%s)
	log "cell baseline-incident-routing-evidence: fresh stack"
	fresh_stack baseline_incident_routing_evidence
	drive_all_cassettes baseline_incident_routing_evidence
	ifa_incident_routing_evidence_drive "baseline_incident_routing_evidence" "${bin_dir}" "${incident_routing_evidence_cassette}" "${drive_workers}" "${log_dir}" \
		|| die "baseline-incident-routing-evidence: eshu-ifa drive (incident_routing_evidence family) failed"
	local projector_pid reducer_pid
	ifa_det_start_bg "${log_dir}" "projector-baseline_incident_routing_evidence" projector_pid "${bin_dir}/eshu-projector"
	ifa_det_start_bg "${log_dir}" "reducer-baseline_incident_routing_evidence" reducer_pid "${bin_dir}/eshu-reducer"
	run_drain_gate baseline_incident_routing_evidence
	assert_no_dead_letters baseline_incident_routing_evidence
	ifa_incident_routing_evidence_assert "baseline_incident_routing_evidence" "${bin_dir}" "${incident_routing_evidence_expected_edges}" \
		|| die "baseline-incident-routing-evidence: fault-free graph does not match the three-edge exact set"
	baseline_incident_routing_evidence_retried="$(ifa_fault_count_retry_attempts "${FAULT_COMPOSE_PROJECT}" "${use_compose}" "${ESHU_POSTGRES_DSN}" "${compose_file}" "incident_routing_materialization")" \
		|| die "baseline-incident-routing-evidence: could not count the fault-free incident_routing_materialization retry-attempt baseline"
	printf 'baseline-incident-routing-evidence: fault-free incident_routing_materialization retry-attempt baseline: %s\n' "${baseline_incident_routing_evidence_retried}"
	capture_digest baseline_incident_routing_evidence
	teardown_cell baseline_incident_routing_evidence
	wall_times[baseline_incident_routing_evidence]=$(( $(date +%s) - cell_start ))
	printf 'baseline-incident-routing-evidence: cell wall time: %ss\n' "${wall_times[baseline_incident_routing_evidence]}"
}

cell_killworker_incident_routing_evidence() {
	local cell_start
	cell_start=$(date +%s)
	log "cell kill-worker-after-claim-incident-routing-evidence: fresh stack"
	fresh_stack killworkerincidentroute
	drive_all_cassettes killworkerincidentroute
	ifa_incident_routing_evidence_drive "killworkerincidentroute" "${bin_dir}" "${incident_routing_evidence_cassette}" "${drive_workers}" "${log_dir}" \
		|| die "kill-worker-after-claim-incident-routing-evidence: eshu-ifa drive (incident_routing_evidence family) failed"
	local projector_pid reducer_pid_before reducer_pid_after lock_holder_pid claimed_before
	ifa_det_start_bg "${log_dir}" "projector-killworkerincidentroute" projector_pid "${bin_dir}/eshu-projector"
	# incident_routing_materialization's reducer intent is created by the
	# PROJECTOR, not ingestion. Wait for the row to exist (any status)
	# before going further: without it there is nothing to kill, and locking
	# fact_records before that intent exists risks starving the projector's
	# OWN fact reads for this scope too, so the row could never be created
	# for as long as the lock is held -- not merely delayed.
	ifa_fault_wait_for_claimed "${FAULT_COMPOSE_PROJECT}" "${use_compose}" "${ESHU_POSTGRES_DSN}" "${compose_file}" \
		"${CLAIMED_ROW_WAIT_TIMEOUT}" "incident_routing_materialization" 1 >/dev/null \
		|| die "kill-worker-after-claim-incident-routing-evidence: incident_routing_materialization was never enqueued by the projector"
	# No claim-time readiness gate on this domain and no in-handler gate
	# before the fact load, so the claim fires as soon as the row is pending
	# and the handler blocks deterministically in its load: the lock goes on
	# now. A prereq-scoped reducer would add a wait nothing here needs (the
	# observability_coverage dual-domain shape does not transfer: ITS alarm
	# edge resolves against a separately-committed EC2 node, while this
	# family's content_entities half is committed before its own generation's
	# intent can exist).
	ifa_incident_routing_evidence_start_fact_records_lock "killworkerincidentroute" lock_holder_pid \
		|| die "kill-worker-after-claim-incident-routing-evidence: could not acquire the deterministic fact_records read blocker"
	# Scoped to incident_routing_materialization only: with the lock
	# already held, an unscoped reducer's workers can all grab OTHER pending
	# domains that also read fact_records and get stuck there for the lock's
	# full duration, starving this domain of a worker even though its own
	# row already exists. ESHU_REDUCER_CLAIM_DOMAIN filters the claim SQL
	# itself (domain = ANY(...)), so this instance never attempts another
	# domain's row. The claim fires without waiting on any phase: this
	# domain carries no readiness requirement, so the lock starves nothing
	# the claim still needs.
	ifa_det_start_bg "${log_dir}" "reducer-killworkerincidentroute-before" reducer_pid_before \
		env "ESHU_REDUCER_CLAIM_DOMAIN=incident_routing_materialization" "${bin_dir}/eshu-reducer"
	claimed_before="$(ifa_fault_wait_for_claimed "${FAULT_COMPOSE_PROJECT}" "${use_compose}" "${ESHU_POSTGRES_DSN}" "${compose_file}" "${CLAIMED_ROW_WAIT_TIMEOUT}" "incident_routing_materialization")" \
		|| die "kill-worker-after-claim-incident-routing-evidence: no incident_routing_materialization row was claimed while its fact load was blocked"
	printf 'kill-worker-after-claim-incident-routing-evidence: non-vacuous: %s blocked claimed/running row(s) observed\n' "${claimed_before}"
	kill -9 "${reducer_pid_before}" >/dev/null 2>&1 || true
	ifa_incident_routing_evidence_release_fact_records_lock "killworkerincidentroute" "${lock_holder_pid}"
	ifa_det_start_bg "${log_dir}" "reducer-killworkerincidentroute-after" reducer_pid_after "${bin_dir}/eshu-reducer"
	run_drain_gate killworkerincidentroute
	assert_no_dead_letters killworkerincidentroute
	ifa_incident_routing_evidence_assert "killworkerincidentroute" "${bin_dir}" "${incident_routing_evidence_expected_edges}" \
		|| die "kill-worker-after-claim-incident-routing-evidence: recovered graph does not match the three-edge exact set"
	ifa_fault_assert_retry_attempts_above "${FAULT_COMPOSE_PROJECT}" "${use_compose}" "${ESHU_POSTGRES_DSN}" "${compose_file}" \
		"${baseline_incident_routing_evidence_retried}" 15 "incident_routing_materialization" \
		|| die "kill-worker-after-claim-incident-routing-evidence: incident_routing_materialization did not re-execute above its fault-free retry-attempt baseline"
	capture_digest killworkerincidentroute
	assert_matches_baseline killworkerincidentroute baseline_incident_routing_evidence
	teardown_cell killworkerincidentroute
	wall_times[killworkerincidentroute]=$(( $(date +%s) - cell_start ))
	printf 'kill-worker-after-claim-incident-routing-evidence: cell wall time: %ss\n' "${wall_times[killworkerincidentroute]}"
}

# cell_failgraphwrite_incident_routing_evidence fails the live
# HAS_*_ROUTING MERGE exactly once, proves the fault decorator's durable
# marker names that write, and requires the full three-edge set to converge
# without dead letters. Anchor read off EXECUTED statement text, not source
# text: the writer's template reads MERGE (incident)-[rel:%s]->(routing)
# (go/internal/storage/cypher/edge/writer/incident_routing_evidence.go:73),
# filled per row from the closed 3-member routing-slot vocabulary, so the
# anchor MUST carry the PREFIX form MERGE (incident)-[rel:HAS_ (one
# statement per slot, sorted by type). Pinning a full `]->(routing)`
# spelling matches no single executed statement, and pinning one filled
# type would leave the other two slots unfaulted -- either way the fault
# would never fire on the whole set, giving a green cell that tested
# nothing. It is NOT a bare relationship name: those strings are edge-label
# metadata carried beside the query that never reaches the graph.
cell_failgraphwrite_incident_routing_evidence() {
	local cell_start
	cell_start=$(date +%s)
	log "cell fail-graph-write-once-then-succeed-incident-routing-evidence: fresh stack"
	fresh_stack failgraphwriteincidentroute
	drive_all_cassettes failgraphwriteincidentroute
	ifa_incident_routing_evidence_drive "failgraphwriteincidentroute" "${bin_dir}" "${incident_routing_evidence_cassette}" "${drive_workers}" "${log_dir}" \
		|| die "fail-graph-write-once-then-succeed-incident-routing-evidence: eshu-ifa drive (incident_routing_evidence family) failed"
	local fault_once_script projector_pid reducer_pid marker_rc
	fault_once_script="${work_dir}/fault-once-then-succeed-incident-routing-evidence.json"
	ifa_fault_write_once_script "${fault_once_script}" "${incident_routing_evidence_edge_operation_match}" "queue-retry"
	ifa_det_start_bg "${log_dir}" "projector-failgraphwriteincidentroute" projector_pid "${bin_dir}/eshu-projector"
	ifa_det_start_bg "${log_dir}" "reducer-failgraphwriteincidentroute" reducer_pid \
		env "ESHU_IFA_FAULT_SCRIPT=${fault_once_script}" "${tagged_bin_dir}/eshu-reducer"
	run_drain_gate failgraphwriteincidentroute
	assert_no_dead_letters failgraphwriteincidentroute
	ifa_incident_routing_evidence_assert "failgraphwriteincidentroute" "${bin_dir}" "${incident_routing_evidence_expected_edges}" \
		|| die "fail-graph-write-once-then-succeed-incident-routing-evidence: recovered graph does not match the three-edge exact set"
	ifa_det_pg "${FAULT_COMPOSE_PROJECT}" "${use_compose}" "${ESHU_POSTGRES_DSN}" \
		"SELECT count(*) AS total, count(*) FILTER (WHERE status = 'succeeded') AS succeeded FROM fact_work_items WHERE stage = 'reducer' AND domain = 'incident_routing_materialization';" \
		"${compose_file}" | sed 's/^/  incident_routing_materialization work-item window: /'
	marker_rc=0
	ifa_fault_assert_once_fault_marker "${fault_once_script}" "${incident_routing_evidence_edge_operation_match}" || marker_rc=$?
	if [[ "${marker_rc}" -eq 2 ]]; then
		# Distinguished from "no marker" deliberately, per #5974: rc=2 means the
		# injection WORKS and fired, but on a different write than the anchor
		# names. Collapsing that into a generic failure sends the next reader
		# looking at the decorator instead of at the anchor.
		die "fail-graph-write-once-then-succeed-incident-routing-evidence: the fault FIRED but on a different write than ${incident_routing_evidence_edge_operation_match} (marker contents above). The injection works; the anchor is pointed at the wrong statement."
	fi
	[[ "${marker_rc}" -eq 0 ]] \
		|| die "fail-graph-write-once-then-succeed-incident-routing-evidence: no once-fired marker at all -- the fault never fired, so this cell proved nothing about HAS_*_ROUTING recovery (marker status ${marker_rc})"
	# Announced, not merely checked. Every sibling cell prints its own
	# non-vacuity line and this one did not, so a passing run said nothing about
	# whether the fault had actually fired -- the check worked while being
	# invisible, which is the shape this family's whole epic is about.
	printf 'fail-graph-write-once-then-succeed-incident-routing-evidence: non-vacuous: once-fired marker names the targeted HAS_*_ROUTING MERGE (written by the fault decorator at injection time, not read from the reducer log)\n'
	capture_digest failgraphwriteincidentroute
	assert_matches_baseline failgraphwriteincidentroute baseline_incident_routing_evidence
	teardown_cell failgraphwriteincidentroute
	wall_times[failgraphwriteincidentroute]=$(( $(date +%s) - cell_start ))
	printf 'fail-graph-write-once-then-succeed-incident-routing-evidence: cell wall time: %ss\n' "${wall_times[failgraphwriteincidentroute]}"
}
