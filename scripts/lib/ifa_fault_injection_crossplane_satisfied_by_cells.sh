#!/usr/bin/env bash
# shellcheck disable=SC2034,SC2154
# crossplane_satisfied_by-targeted live fault cells (#6228 crossplane live
# leaf), mirroring
# scripts/lib/ifa_fault_injection_built_from_cells.sh's shape including the
# kill cell: this domain's claim is NOT readiness-gated
# (crossplane_satisfied_by_materialization carries no row in the
# reducer-claim readiness requirements), AND its in-handler path carries no
# readiness gate before the fact load either (reviewed deviation, issue
# #5347 -- the projector commits both endpoint nodes before the intent
# enqueues, and cross-scope XRDs gate on generation activation). So the kill
# cell takes the lock as soon as the projector enqueues the row, with no
# prereq-scoped reducer and no readiness wait. That is the k8s shape, not
# the crc-image shape: the image handler's sourceNodesReady gate runs
# BEFORE its fact load, so ITS kill cell banks readiness through an
# aws_resource-scoped prereq reducer before the lock; doing that here would
# add a wait this domain's claim never needs, and the wait would idle
# against a phase this handler never publishes.
#
# WIRED: scripts/verify-ifa-fault-injection.sh sources this file (through
# scripts/lib/ifa_fault_injection_sources.sh) and runs all three cells
# (baseline_crossplane_satisfied_by,
# killworker_crossplane_satisfied_by,
# failgraphwrite_crossplane_satisfied_by) against a live Postgres +
# NornicDB stack. The cells rely on the driver's strict mode and globals
# (bin_dir, tagged_bin_dir, log_dir, work_dir, use_compose, compose_file,
# FAULT_COMPOSE_PROJECT, ESHU_POSTGRES_DSN, CLAIMED_ROW_WAIT_TIMEOUT,
# drive_workers, wall_times, bg_pids, log, die, plus the fresh_stack /
# drive_all_cassettes / run_drain_gate / assert_no_dead_letters /
# capture_digest / assert_matches_baseline / teardown_cell helpers from
# ifa_fault_injection_driver.sh). Drive and assert callbacks come from
# scripts/lib/ifa_direct_family_live_crossplane_satisfied_by.sh
# (ifa_crossplane_satisfied_by_drive /
# ifa_crossplane_satisfied_by_assert); the cassette and expected-edge
# variables come from scripts/lib/ifa_family_fixtures.sh.
#
# scripts/lib/test-ifa-fault-injection-crossplane-satisfied-by-cases.sh
# sources this file and drives
# ifa_crossplane_satisfied_by_start_fact_records_lock and
# ifa_crossplane_satisfied_by_release_fact_records_lock under stubs, and
# scripts/test-verify-ifa-fault-injection.sh runs that module -- so editing
# either function's fail-closed behavior or its bg_pids bookkeeping will
# fail `make pre-pr`.
#
# crossplane_satisfied_by spans TWO scopes (claims + platform) and ONE
# shared generation. The handler
# (CrossplaneSatisfiedByMaterializationHandler) claims a
# fact_work_items row under domain "crossplane_satisfied_by_materialization",
# loads its facts via factload through loadEdgeFacts -> FROM fact_records
# (own scope) plus the cross-scope active-XRD load, and writes the
# SATISFIED_BY edges DIRECTLY to the graph through its crossplane edge
# writer, then acks. There is no shared_projection_intents row and no
# second-stage runner lease -- the two blocker kinds the registry schema
# offers that this family cannot use.
#
# The lock target is therefore the handler's first synchronous READ, not a
# write: ACCESS EXCLUSIVE on fact_records blocks readers, and the claim/ack
# path never touches that table, so the row stays claimed while the handler
# is held. The handler struct embeds FactLoader
# (go/internal/reducer/crossplane/satisfied_by_materialization.go:144-148)
# and Handle refuses to run without it (:177-179) before passing it to its
# fact loads (:205) -- the pre-write window the lock needs.
#
# ifa_crossplane_satisfied_by_start_fact_records_lock holds an ACCESS
# EXCLUSIVE lock on fact_records so the crossplane_satisfied_by_materialization
# handler blocks on its FIRST synchronous read -- the fact load -- while its
# fact_work_items claim is already held. The claim, heartbeat, and ack path
# never reads fact_records, so the row stays claimed and alive for the whole
# hold. fact_records, not fact_work_items: the cell's own wait predicate polls
# fact_work_items, and locking that table would block the poll too.
ifa_crossplane_satisfied_by_start_fact_records_lock() {
	local cell="$1" pid_var="$2"
	# Short prefix deliberately: the repo holds application_name to 63 bytes
	# (ifa_fault_generic_runner_wait.sh, ifa_fault_injection_documentation_ack_setup.sh),
	# and a full family-name prefix plus a long kill cell name would be
	# silently truncated, so the grant poll below (which matches
	# the full name) would miss forever while the holder sat on its granted
	# lock. Proven live by the sibling kubernetes family's identical failure.
	# ifa_xplane_lock_ keeps the longest name this family uses at 41 bytes.
	local app_name="ifa_xplane_lock_${cell}"
	if ((${#app_name} > 63)); then
		printf '%s: lock application_name is %s bytes; past the 63-byte repo cap the name truncates and the grant poll would never match\n' "${cell}" "${#app_name}" >&2
		return 1
	fi
	local lock_sql="SET application_name = '${app_name}'; BEGIN; LOCK TABLE fact_records IN ACCESS EXCLUSIVE MODE; SELECT pg_sleep(180); ROLLBACK;"
	if [[ "${use_compose}" -eq 1 ]]; then
		bash scripts/ci/compose.sh -p "${FAULT_COMPOSE_PROJECT}" -f "${compose_file}" -- exec -T postgres \
			psql -v ON_ERROR_STOP=1 -U eshu -d eshu -c "${lock_sql}" \
			>"${log_dir}/crossplane-satisfied-by-lock-${cell}.log" 2>&1 &
	else
		psql "${ESHU_POSTGRES_DSN}" -v ON_ERROR_STOP=1 -c "${lock_sql}" \
			>"${log_dir}/crossplane-satisfied-by-lock-${cell}.log" 2>&1 &
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

# ifa_crossplane_satisfied_by_release_fact_records_lock terminates the
# named lock-holder backend, then joins its local psql/docker process
# before the replacement reducer starts.
ifa_crossplane_satisfied_by_release_fact_records_lock() {
	local cell="$1" holder_pid="$2"
	# Same short name as the start function: it must terminate the backend
	# the start function actually created (see its 63-byte-cap comment).
	local app_name="ifa_xplane_lock_${cell}"
	ifa_det_pg "${FAULT_COMPOSE_PROJECT}" "${use_compose}" "${ESHU_POSTGRES_DSN}" \
		"SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE application_name = '${app_name}';" \
		"${compose_file}" >/dev/null
	wait "${holder_pid}" 2>/dev/null || true
	ifa_det_untrack_bg_pid "${holder_pid}"
}

# cell_baseline_crossplane_satisfied_by is this family's fault-free
# reference run. It exists because the crossplane_satisfied_by cassette is
# driven ONLY by this family's own cells:
# ifa_fault_injection_driver.sh's header rules that a new family cassette is
# never added to drive_all_cassettes, since the extra succeeded reducer row
# enlarges ifa_fault_redeliver_succeeded's row set inside
# cell_duplicatedelivery. So the shared `baseline` digest contains no
# SATISFIED_BY edges and every cell of this family would mismatch it. These
# cells therefore compare against
# digests[baseline_crossplane_satisfied_by] via assert_matches_baseline's
# baseline_key parameter.
#
# It also establishes baseline_crossplane_satisfied_by_retried: the
# fault-free excess-ATTEMPT total for domain
# crossplane_satisfied_by_materialization, which the kill cell must exceed to
# prove the replacement reducer re-executed THIS domain rather than merely
# draining another queued row. Attempts, not rows: this cassette creates one
# work item per scope (two rows), and the row-count form saturates at 1 per
# row for a single natural retry (see ifa_fault_count_retry_attempts).
cell_baseline_crossplane_satisfied_by() {
	local cell_start
	cell_start=$(date +%s)
	log "cell baseline-crossplane-satisfied-by: fresh stack"
	fresh_stack baselinexplane
	drive_all_cassettes baselinexplane
	ifa_crossplane_satisfied_by_drive "baselinexplane" "${bin_dir}" "${crossplane_satisfied_by_cassette}" "${drive_workers}" "${log_dir}" \
		|| die "baseline-crossplane-satisfied-by: eshu-ifa drive (crossplane_satisfied_by family) failed"
	local projector_pid reducer_pid
	ifa_det_start_bg "${log_dir}" "projector-baselinexplane" projector_pid "${bin_dir}/eshu-projector"
	ifa_det_start_bg "${log_dir}" "reducer-baselinexplane" reducer_pid "${bin_dir}/eshu-reducer"
	run_drain_gate baselinexplane
	assert_no_dead_letters baselinexplane
	ifa_crossplane_satisfied_by_assert "baselinexplane" "${bin_dir}" "${crossplane_satisfied_by_expected_edges}" \
		|| die "baseline-crossplane-satisfied-by: fault-free graph does not match the two-edge exact set"
	baseline_crossplane_satisfied_by_retried="$(ifa_fault_count_retry_attempts "${FAULT_COMPOSE_PROJECT}" "${use_compose}" "${ESHU_POSTGRES_DSN}" "${compose_file}" "crossplane_satisfied_by_materialization")" \
		|| die "baseline-crossplane-satisfied-by: could not count the fault-free crossplane_satisfied_by_materialization retry-attempt baseline"
	printf 'baseline-crossplane-satisfied-by: fault-free crossplane_satisfied_by_materialization retry-attempt baseline: %s\n' "${baseline_crossplane_satisfied_by_retried}"
	# Captured under the BASELINE KEY, not the cell label: the kill and
	# fail-graph-write cells assert against
	# digests[baseline_crossplane_satisfied_by]. Capturing under the label
	# leaves that key unset and the match dies on an unbound variable
	# under the gate's `set -u` (found by the crc-image lane's targeted
	# live proof, which got past the attempts assert the old lock-first
	# shape never reached).
	capture_digest baseline_crossplane_satisfied_by
	teardown_cell baselinexplane
	wall_times[baselinexplane]=$(( $(date +%s) - cell_start ))
	printf 'baseline-crossplane-satisfied-by: cell wall time: %ss\n' "${wall_times[baselinexplane]}"
}

cell_killworker_crossplane_satisfied_by() {
	local cell_start
	cell_start=$(date +%s)
	log "cell kill-worker-after-claim-crossplane-satisfied-by: fresh stack"
	fresh_stack killworkerxplane
	drive_all_cassettes killworkerxplane
	ifa_crossplane_satisfied_by_drive "killworkerxplane" "${bin_dir}" "${crossplane_satisfied_by_cassette}" "${drive_workers}" "${log_dir}" \
		|| die "kill-worker-after-claim-crossplane-satisfied-by: eshu-ifa drive (crossplane_satisfied_by family) failed"
	local projector_pid reducer_pid_before reducer_pid_after lock_holder_pid claimed_before
	ifa_det_start_bg "${log_dir}" "projector-killworkerxplane" projector_pid "${bin_dir}/eshu-projector"
	# crossplane_satisfied_by_materialization's reducer intent is created by
	# the PROJECTOR, not ingestion. Wait for the row to exist (any status)
	# before going further: without it there is nothing to kill, and locking
	# fact_records before that intent exists risks starving the projector's
	# OWN fact reads for this scope too, so the row could never be created
	# for as long as the lock is held -- not merely delayed.
	ifa_fault_wait_for_claimed "${FAULT_COMPOSE_PROJECT}" "${use_compose}" "${ESHU_POSTGRES_DSN}" "${compose_file}" \
		"${CLAIMED_ROW_WAIT_TIMEOUT}" "crossplane_satisfied_by_materialization" 1 >/dev/null \
		|| die "kill-worker-after-claim-crossplane-satisfied-by: crossplane_satisfied_by_materialization was never enqueued by the projector"
	# No claim-time readiness gate on this domain and no in-handler gate
	# before the fact load (reviewed deviation, issue #5347), so the claim
	# fires as soon as the row is pending and the handler blocks
	# deterministically in its load: the lock goes on now. A prereq-scoped
	# reducer would add a wait nothing here needs (the crc-image shape does
	# not transfer: ITS handler gates on readiness before its load).
	ifa_crossplane_satisfied_by_start_fact_records_lock "killworkerxplane" lock_holder_pid \
		|| die "kill-worker-after-claim-crossplane-satisfied-by: could not acquire the deterministic fact_records read blocker"
	# Scoped to crossplane_satisfied_by_materialization only: with the lock
	# already held, an unscoped reducer's workers can all grab OTHER pending
	# domains that also read fact_records and get stuck there for the lock's
	# full duration, starving this domain of a worker even though its own row
	# already exists. ESHU_REDUCER_CLAIM_DOMAIN filters the claim SQL itself
	# (domain = ANY(...)), so this instance never attempts another domain's
	# row. The claim fires without waiting on any phase: this domain carries
	# no readiness requirement, so the lock starves nothing the claim still
	# needs.
	ifa_det_start_bg "${log_dir}" "reducer-killworkerxplane-before" reducer_pid_before \
		env "ESHU_REDUCER_CLAIM_DOMAIN=crossplane_satisfied_by_materialization" "${bin_dir}/eshu-reducer"
	claimed_before="$(ifa_fault_wait_for_claimed "${FAULT_COMPOSE_PROJECT}" "${use_compose}" "${ESHU_POSTGRES_DSN}" "${compose_file}" "${CLAIMED_ROW_WAIT_TIMEOUT}" "crossplane_satisfied_by_materialization")" \
		|| die "kill-worker-after-claim-crossplane-satisfied-by: no crossplane_satisfied_by_materialization row was claimed while its fact load was blocked"
	printf 'kill-worker-after-claim-crossplane-satisfied-by: non-vacuous: %s blocked claimed/running row(s) observed\n' "${claimed_before}"
	kill -9 "${reducer_pid_before}" >/dev/null 2>&1 || true
	ifa_crossplane_satisfied_by_release_fact_records_lock "killworkerxplane" "${lock_holder_pid}"
	ifa_det_start_bg "${log_dir}" "reducer-killworkerxplane-after" reducer_pid_after "${bin_dir}/eshu-reducer"
	run_drain_gate killworkerxplane
	assert_no_dead_letters killworkerxplane
	ifa_crossplane_satisfied_by_assert "killworkerxplane" "${bin_dir}" "${crossplane_satisfied_by_expected_edges}" \
		|| die "kill-worker-after-claim-crossplane-satisfied-by: recovered graph does not match the two-edge exact set"
	ifa_fault_assert_retry_attempts_above "${FAULT_COMPOSE_PROJECT}" "${use_compose}" "${ESHU_POSTGRES_DSN}" "${compose_file}" \
		"${baseline_crossplane_satisfied_by_retried}" 15 "crossplane_satisfied_by_materialization" \
		|| die "kill-worker-after-claim-crossplane-satisfied-by: crossplane_satisfied_by_materialization did not re-execute above its fault-free retry-attempt baseline"
	capture_digest killworkerxplane
	assert_matches_baseline killworkerxplane baseline_crossplane_satisfied_by
	teardown_cell killworkerxplane
	wall_times[killworkerxplane]=$(( $(date +%s) - cell_start ))
	printf 'kill-worker-after-claim-crossplane-satisfied-by: cell wall time: %ss\n' "${wall_times[killworkerxplane]}"
}

# cell_failgraphwrite_crossplane_satisfied_by fails the live SATISFIED_BY
# MERGE exactly once, proves the fault decorator's durable marker names that
# write, and requires the full two-edge set to converge without dead
# letters. Anchor read off EXECUTED statement text, not source text: the
# writer's template reads MERGE (claim)-[rel:SATISFIED_BY]->(xrd)
# (go/internal/storage/cypher/canonical/crossplane/edges.go:33-36) with no
# vocabulary substitution, so the anchor carries that fixed spelling. A
# parameterized form would match no executed statement and the fault would
# never fire, giving a green cell that tested nothing.
cell_failgraphwrite_crossplane_satisfied_by() {
	local cell_start
	cell_start=$(date +%s)
	log "cell fail-graph-write-once-then-succeed-crossplane-satisfied-by: fresh stack"
	fresh_stack failgraphwritexplane
	drive_all_cassettes failgraphwritexplane
	ifa_crossplane_satisfied_by_drive "failgraphwritexplane" "${bin_dir}" "${crossplane_satisfied_by_cassette}" "${drive_workers}" "${log_dir}" \
		|| die "fail-graph-write-once-then-succeed-crossplane-satisfied-by: eshu-ifa drive (crossplane_satisfied_by family) failed"
	local fault_once_script projector_pid reducer_pid marker_rc
	fault_once_script="${work_dir}/fault-once-then-succeed-crossplane-satisfied-by.json"
	ifa_fault_write_once_script "${fault_once_script}" "${crossplane_satisfied_by_edge_operation_match}" "queue-retry"
	ifa_det_start_bg "${log_dir}" "projector-failgraphwritexplane" projector_pid "${bin_dir}/eshu-projector"
	ifa_det_start_bg "${log_dir}" "reducer-failgraphwritexplane" reducer_pid \
		env "ESHU_IFA_FAULT_SCRIPT=${fault_once_script}" "${tagged_bin_dir}/eshu-reducer"
	run_drain_gate failgraphwritexplane
	assert_no_dead_letters failgraphwritexplane
	ifa_crossplane_satisfied_by_assert "failgraphwritexplane" "${bin_dir}" "${crossplane_satisfied_by_expected_edges}" \
		|| die "fail-graph-write-once-then-succeed-crossplane-satisfied-by: recovered graph does not match the two-edge exact set"
	ifa_det_pg "${FAULT_COMPOSE_PROJECT}" "${use_compose}" "${ESHU_POSTGRES_DSN}" \
		"SELECT count(*) AS total, count(*) FILTER (WHERE status = 'succeeded') AS succeeded FROM fact_work_items WHERE stage = 'reducer' AND domain = 'crossplane_satisfied_by_materialization';" \
		"${compose_file}" | sed 's/^/  crossplane_satisfied_by_materialization work-item window: /'
	marker_rc=0
	ifa_fault_assert_once_fault_marker "${fault_once_script}" "${crossplane_satisfied_by_edge_operation_match}" || marker_rc=$?
	if [[ "${marker_rc}" -eq 2 ]]; then
		# Distinguished from "no marker" deliberately, per #5974: rc=2 means the
		# injection WORKS and fired, but on a different write than the anchor
		# names. Collapsing that into a generic failure sends the next reader
		# looking at the decorator instead of at the anchor.
		die "fail-graph-write-once-then-succeed-crossplane-satisfied-by: the fault FIRED but on a different write than ${crossplane_satisfied_by_edge_operation_match} (marker contents above). The injection works; the anchor is pointed at the wrong statement."
	fi
	[[ "${marker_rc}" -eq 0 ]] \
		|| die "fail-graph-write-once-then-succeed-crossplane-satisfied-by: no once-fired marker at all -- the fault never fired, so this cell proved nothing about SATISFIED_BY recovery (marker status ${marker_rc})"
	# Announced, not merely checked. Every sibling cell prints its own
	# non-vacuity line and this one did not, so a passing run would say
	# nothing about whether the fault had actually fired -- the check worked
	# while being invisible, which is the shape this family's whole epic is
	# about.
	printf 'fail-graph-write-once-then-succeed-crossplane-satisfied-by: non-vacuous: once-fired marker names the targeted SATISFIED_BY MERGE (written by the fault decorator at injection time, not read from the reducer log)\n'
	capture_digest failgraphwritexplane
	assert_matches_baseline failgraphwritexplane baseline_crossplane_satisfied_by
	teardown_cell failgraphwritexplane
	wall_times[failgraphwritexplane]=$(( $(date +%s) - cell_start ))
	printf 'fail-graph-write-once-then-succeed-crossplane-satisfied-by: cell wall time: %ss\n' "${wall_times[failgraphwritexplane]}"
}
