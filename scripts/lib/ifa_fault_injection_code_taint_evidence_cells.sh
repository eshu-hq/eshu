#!/usr/bin/env bash
# shellcheck disable=SC2034,SC2154
# code_taint_evidence-targeted live fault cells (#6228
# code_taint_evidence live leaf), mirroring
# scripts/lib/ifa_fault_injection_code_interproc_evidence_cells.sh's shape
# including the kill cell: this domain's claim is NOT readiness-gated
# (code_taint_evidence carries no row in the reducer-claim readiness
# requirements), AND its in-handler path carries no readiness gate before
# the fact load either (Handle goes from the nil guards straight to
# LoadCodeTaintEvidence; the projector commits the endpoint Function nodes
# before the intent enqueues). So the kill cell takes the lock as soon as
# the projector enqueues the row, with no prereq-scoped reducer and no
# readiness wait. That is the k8s shape, not the crc-image shape: the image
# handler's sourceNodesReady gate runs BEFORE its fact load, so ITS kill
# cell banks readiness through an aws_resource-scoped prereq reducer before
# the lock; doing that here would add a wait this domain's claim never
# needs, and the wait would idle against a phase this handler never
# publishes.
#
# WIRED: scripts/verify-ifa-fault-injection.sh sources this file (through
# scripts/lib/ifa_fault_injection_sources.sh) and runs all three cells
# (baseline_code_taint_evidence,
# killworker_code_taint_evidence,
# failgraphwrite_code_taint_evidence) against a live Postgres +
# NornicDB stack. The cells rely on the driver's strict mode and globals
# (bin_dir, tagged_bin_dir, log_dir, work_dir, use_compose, compose_file,
# FAULT_COMPOSE_PROJECT, ESHU_POSTGRES_DSN, CLAIMED_ROW_WAIT_TIMEOUT,
# drive_workers, wall_times, bg_pids, log, die, plus the fresh_stack /
# drive_all_cassettes / run_drain_gate / assert_no_dead_letters /
# capture_digest / assert_matches_baseline / teardown_cell helpers from
# ifa_fault_injection_driver.sh). Drive and assert callbacks come from
# scripts/lib/ifa_direct_family_live_code_taint_evidence.sh
# (ifa_code_taint_evidence_drive /
# ifa_code_taint_evidence_assert); the cassette and expected-edge
# variables come from scripts/lib/ifa_family_fixtures.sh.
#
# scripts/lib/test-ifa-fault-injection-code-taint-evidence-cases.sh
# sources this file and drives
# ifa_code_taint_evidence_start_fact_records_lock and
# ifa_code_taint_evidence_release_fact_records_lock under stubs, and
# scripts/test-verify-ifa-fault-injection.sh runs that module -- so editing
# either function's fail-closed behavior or its bg_pids bookkeeping will
# fail `make pre-pr`.
#
# code_taint_evidence spans ONE scope (the fixture repository), with one
# work item under domain "code_taint_evidence". The handler
# (EvidenceHandler) claims a fact_work_items row under that domain, loads
# its facts via factload (LoadCodeTaintEvidence -> FROM
# fact_records, own scope only) and the entities phase commits the endpoint
# Function nodes from the same generation's content_entity facts, then
# writes the HAS_TAINT_EVIDENCE edges DIRECTLY to the graph through its
# taint evidence writer, then acks. There is no
# shared_projection_intents row and no second-stage runner lease -- the two
# blocker kinds the registry schema offers that this family cannot use.
#
# The lock target is therefore the handler's first synchronous READ, not a
# write: ACCESS EXCLUSIVE on fact_records blocks readers, and the claim/ack
# path never touches that table, so the row stays claimed while the handler
# is held. fact_records, not fact_work_items: the cell's own wait predicate
# polls fact_work_items, and locking that table would block the poll too.
ifa_code_taint_evidence_start_fact_records_lock() {
	local cell="$1" pid_var="$2"
	# Short prefix deliberately: the repo holds application_name to 63 bytes
	# (ifa_fault_generic_runner_wait.sh, ifa_fault_injection_documentation_ack_setup.sh),
	# and a full family-name prefix plus a long kill cell name would be
	# silently truncated, so the grant poll below (which matches
	# the full name) would miss forever while the holder sat on its granted
	# lock. Proven live by the sibling kubernetes family's identical failure.
	# ifa_ctaint_lock_ keeps the longest name this family uses at 34 bytes.
	local app_name="ifa_ctaint_lock_${cell}"
	if ((${#app_name} > 63)); then
		printf '%s: lock application_name is %s bytes; past the 63-byte repo cap the name truncates and the grant poll would never match\n' "${cell}" "${#app_name}" >&2
		return 1
	fi
	local lock_sql="SET application_name = '${app_name}'; BEGIN; LOCK TABLE fact_records IN ACCESS EXCLUSIVE MODE; SELECT pg_sleep(180); ROLLBACK;"
	if [[ "${use_compose}" -eq 1 ]]; then
		bash scripts/ci/compose.sh -p "${FAULT_COMPOSE_PROJECT}" -f "${compose_file}" -- exec -T postgres \
			psql -v ON_ERROR_STOP=1 -U eshu -d eshu -c "${lock_sql}" \
			>"${log_dir}/code-taint-evidence-lock-${cell}.log" 2>&1 &
	else
		psql "${ESHU_POSTGRES_DSN}" -v ON_ERROR_STOP=1 -c "${lock_sql}" \
			>"${log_dir}/code-taint-evidence-lock-${cell}.log" 2>&1 &
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

# ifa_code_taint_evidence_release_fact_records_lock terminates the
# named lock-holder backend, then joins its local psql/docker process
# before the replacement reducer starts.
ifa_code_taint_evidence_release_fact_records_lock() {
	local cell="$1" holder_pid="$2"
	# Same short name as the start function: it must terminate the backend
	# the start function actually created (see its 63-byte-cap comment).
	local app_name="ifa_ctaint_lock_${cell}"
	ifa_det_pg "${FAULT_COMPOSE_PROJECT}" "${use_compose}" "${ESHU_POSTGRES_DSN}" \
		"SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE application_name = '${app_name}';" \
		"${compose_file}" >/dev/null
	wait "${holder_pid}" 2>/dev/null || true
	ifa_det_untrack_bg_pid "${holder_pid}"
}

# cell_baseline_code_taint_evidence is this family's fault-free
# reference run. It exists because the code_taint_evidence cassette is
# driven ONLY by this family's own cells:
# ifa_fault_injection_driver.sh's header rules that a new family cassette is
# never added to drive_all_cassettes, since the extra succeeded reducer row
# enlarges ifa_fault_redeliver_succeeded's row set inside
# cell_duplicatedelivery. So the shared `baseline` digest contains no
# HAS_TAINT_EVIDENCE edges and every cell of this family would mismatch it. These
# cells therefore compare against
# digests[baseline_code_taint_evidence] via assert_matches_baseline's
# baseline_key parameter.
#
# It also establishes baseline_code_taint_evidence_retried: the
# fault-free excess-ATTEMPT total for domain
# code_taint_evidence, which the kill cell must exceed to
# prove the replacement reducer re-executed THIS domain rather than merely
# draining another queued row. Attempts, not rows: this cassette creates one
# work item (single scope, one row), and the row-count form saturates at 1
# per row for a single natural retry (see ifa_fault_count_retry_attempts).
cell_baseline_code_taint_evidence() {
	local cell_start
	cell_start=$(date +%s)
	log "cell baseline-code-taint-evidence: fresh stack"
	fresh_stack baselinetaint
	drive_all_cassettes baselinetaint
	ifa_code_taint_evidence_drive "baselinetaint" "${bin_dir}" "${code_taint_evidence_cassette}" "${drive_workers}" "${log_dir}" \
		|| die "baseline-code-taint-evidence: eshu-ifa drive (code_taint_evidence family) failed"
	local projector_pid reducer_pid
	ifa_det_start_bg "${log_dir}" "projector-baselinetaint" projector_pid "${bin_dir}/eshu-projector"
	ifa_det_start_bg "${log_dir}" "reducer-baselinetaint" reducer_pid "${bin_dir}/eshu-reducer"
	run_drain_gate baselinetaint
	assert_no_dead_letters baselinetaint
	ifa_code_taint_evidence_assert "baselinetaint" "${bin_dir}" "${code_taint_evidence_expected_edges}" \
		|| die "baseline-code-taint-evidence: fault-free graph does not match the two-edge exact set"
	baseline_code_taint_evidence_retried="$(ifa_fault_count_retry_attempts "${FAULT_COMPOSE_PROJECT}" "${use_compose}" "${ESHU_POSTGRES_DSN}" "${compose_file}" "code_taint_evidence")" \
		|| die "baseline-code-taint-evidence: could not count the fault-free code_taint_evidence retry-attempt baseline"
	printf 'baseline-code-taint-evidence: fault-free code_taint_evidence retry-attempt baseline: %s\n' "${baseline_code_taint_evidence_retried}"
	# Captured under the BASELINE KEY, not the cell label: the kill and
	# fail-graph-write cells assert against
	# digests[baseline_code_taint_evidence]. Capturing under the label
	# leaves that key unset and the match dies on an unbound variable
	# under the gate's `set -u` (found by the crc-image lane's targeted
	# live proof, which got past the attempts assert the old lock-first
	# shape never reached).
	capture_digest baseline_code_taint_evidence
	teardown_cell baselinetaint
	wall_times[baselinetaint]=$(( $(date +%s) - cell_start ))
	printf 'baseline-code-taint-evidence: cell wall time: %ss\n' "${wall_times[baselinetaint]}"
}

cell_killworker_code_taint_evidence() {
	local cell_start
	cell_start=$(date +%s)
	log "cell kill-worker-after-claim-code-taint-evidence: fresh stack"
	fresh_stack killworkertaint
	drive_all_cassettes killworkertaint
	ifa_code_taint_evidence_drive "killworkertaint" "${bin_dir}" "${code_taint_evidence_cassette}" "${drive_workers}" "${log_dir}" \
		|| die "kill-worker-after-claim-code-taint-evidence: eshu-ifa drive (code_taint_evidence family) failed"
	local projector_pid reducer_pid_before reducer_pid_after lock_holder_pid claimed_before
	ifa_det_start_bg "${log_dir}" "projector-killworkertaint" projector_pid "${bin_dir}/eshu-projector"
	# code_taint_evidence's reducer intent is created by the
	# PROJECTOR, not ingestion. Wait for the row to exist (any status)
	# before going further: without it there is nothing to kill, and locking
	# fact_records before that intent exists risks starving the projector's
	# OWN fact reads for this scope too, so the row could never be created
	# for as long as the lock is held -- not merely delayed.
	ifa_fault_wait_for_claimed "${FAULT_COMPOSE_PROJECT}" "${use_compose}" "${ESHU_POSTGRES_DSN}" "${compose_file}" \
		"${CLAIMED_ROW_WAIT_TIMEOUT}" "code_taint_evidence" 1 >/dev/null \
		|| die "kill-worker-after-claim-code-taint-evidence: code_taint_evidence was never enqueued by the projector"
	# No claim-time readiness gate on this domain and no in-handler gate
	# before the fact load, so the claim fires as soon as the row is pending
	# and the handler blocks deterministically in its load: the lock goes on
	# now. A prereq-scoped reducer would add a wait nothing here needs (the
	# crc-image shape does not transfer: ITS handler gates on readiness
	# before its load).
	ifa_code_taint_evidence_start_fact_records_lock "killworkertaint" lock_holder_pid \
		|| die "kill-worker-after-claim-code-taint-evidence: could not acquire the deterministic fact_records read blocker"
	# Scoped to code_taint_evidence only: with the lock
	# already held, an unscoped reducer's workers can all grab OTHER pending
	# domains that also read fact_records and get stuck there for the lock's
	# full duration, starving this domain of a worker even though its own row
	# already exists. ESHU_REDUCER_CLAIM_DOMAIN filters the claim SQL itself
	# (domain = ANY(...)), so this instance never attempts another domain's
	# row. The claim fires without waiting on any phase: this domain carries
	# no readiness requirement, so the lock starves nothing the claim still
	# needs.
	ifa_det_start_bg "${log_dir}" "reducer-killworkertaint-before" reducer_pid_before \
		env "ESHU_REDUCER_CLAIM_DOMAIN=code_taint_evidence" "${bin_dir}/eshu-reducer"
	claimed_before="$(ifa_fault_wait_for_claimed "${FAULT_COMPOSE_PROJECT}" "${use_compose}" "${ESHU_POSTGRES_DSN}" "${compose_file}" "${CLAIMED_ROW_WAIT_TIMEOUT}" "code_taint_evidence")" \
		|| die "kill-worker-after-claim-code-taint-evidence: no code_taint_evidence row was claimed while its fact load was blocked"
	printf 'kill-worker-after-claim-code-taint-evidence: non-vacuous: %s blocked claimed/running row(s) observed\n' "${claimed_before}"
	kill -9 "${reducer_pid_before}" >/dev/null 2>&1 || true
	ifa_code_taint_evidence_release_fact_records_lock "killworkertaint" "${lock_holder_pid}"
	ifa_det_start_bg "${log_dir}" "reducer-killworkertaint-after" reducer_pid_after "${bin_dir}/eshu-reducer"
	run_drain_gate killworkertaint
	assert_no_dead_letters killworkertaint
	ifa_code_taint_evidence_assert "killworkertaint" "${bin_dir}" "${code_taint_evidence_expected_edges}" \
		|| die "kill-worker-after-claim-code-taint-evidence: recovered graph does not match the two-edge exact set"
	ifa_fault_assert_retry_attempts_above "${FAULT_COMPOSE_PROJECT}" "${use_compose}" "${ESHU_POSTGRES_DSN}" "${compose_file}" \
		"${baseline_code_taint_evidence_retried}" 15 "code_taint_evidence" \
		|| die "kill-worker-after-claim-code-taint-evidence: code_taint_evidence did not re-execute above its fault-free retry-attempt baseline"
	capture_digest killworkertaint
	assert_matches_baseline killworkertaint baseline_code_taint_evidence
	teardown_cell killworkertaint
	wall_times[killworkertaint]=$(( $(date +%s) - cell_start ))
	printf 'kill-worker-after-claim-code-taint-evidence: cell wall time: %ss\n' "${wall_times[killworkertaint]}"
}

# cell_failgraphwrite_code_taint_evidence fails the live HAS_TAINT_EVIDENCE
# MERGE exactly once, proves the fault decorator's durable marker names that
# write, and requires the full two-edge set to converge without dead
# letters. Anchor read off EXECUTED statement text, not source text: the
# writer's template reads
# MERGE (f)-[rel:HAS_TAINT_EVIDENCE]->(ev)
# (go/internal/storage/cypher/edge/writer/code_taint_evidence.go:47)
# with no vocabulary substitution and no property map on the MERGE (the
# SETs follow it), so the anchor carries the FULL form that matches
# executed text. A parameterized form would match the retract DELETEs
# instead, and a PREFIX form stopping before `->(ev)` would also match the
# node MERGE above it -- either way the fault would never fire on the
# upsert alone, giving a green cell that tested nothing.
cell_failgraphwrite_code_taint_evidence() {
	local cell_start
	cell_start=$(date +%s)
	log "cell fail-graph-write-once-then-succeed-code-taint-evidence: fresh stack"
	fresh_stack failgraphwritetaint
	drive_all_cassettes failgraphwritetaint
	ifa_code_taint_evidence_drive "failgraphwritetaint" "${bin_dir}" "${code_taint_evidence_cassette}" "${drive_workers}" "${log_dir}" \
		|| die "fail-graph-write-once-then-succeed-code-taint-evidence: eshu-ifa drive (code_taint_evidence family) failed"
	local fault_once_script projector_pid reducer_pid marker_rc
	fault_once_script="${work_dir}/fault-once-then-succeed-code-taint-evidence.json"
	ifa_fault_write_once_script "${fault_once_script}" "${code_taint_evidence_edge_operation_match}" "queue-retry"
	ifa_det_start_bg "${log_dir}" "projector-failgraphwritetaint" projector_pid "${bin_dir}/eshu-projector"
	ifa_det_start_bg "${log_dir}" "reducer-failgraphwritetaint" reducer_pid \
		env "ESHU_IFA_FAULT_SCRIPT=${fault_once_script}" "${tagged_bin_dir}/eshu-reducer"
	run_drain_gate failgraphwritetaint
	assert_no_dead_letters failgraphwritetaint
	ifa_code_taint_evidence_assert "failgraphwritetaint" "${bin_dir}" "${code_taint_evidence_expected_edges}" \
		|| die "fail-graph-write-once-then-succeed-code-taint-evidence: recovered graph does not match the two-edge exact set"
	ifa_det_pg "${FAULT_COMPOSE_PROJECT}" "${use_compose}" "${ESHU_POSTGRES_DSN}" \
		"SELECT count(*) AS total, count(*) FILTER (WHERE status = 'succeeded') AS succeeded FROM fact_work_items WHERE stage = 'reducer' AND domain = 'code_taint_evidence';" \
		"${compose_file}" | sed 's/^/  code_taint_evidence work-item window: /'
	marker_rc=0
	ifa_fault_assert_once_fault_marker "${fault_once_script}" "${code_taint_evidence_edge_operation_match}" || marker_rc=$?
	if [[ "${marker_rc}" -eq 2 ]]; then
		# Distinguished from "no marker" deliberately, per #5974: rc=2 means the
		# injection WORKS and fired, but on a different write than the anchor
		# names. Collapsing that into a generic failure sends the next reader
		# looking at the decorator instead of at the anchor.
		die "fail-graph-write-once-then-succeed-code-taint-evidence: the fault FIRED but on a different write than ${code_taint_evidence_edge_operation_match} (marker contents above). The injection works; the anchor is pointed at the wrong statement."
	fi
	[[ "${marker_rc}" -eq 0 ]] \
		|| die "fail-graph-write-once-then-succeed-code-taint-evidence: no once-fired marker at all -- the fault never fired, so this cell proved nothing about HAS_TAINT_EVIDENCE recovery (marker status ${marker_rc})"
	# Announced, not merely checked. Every sibling cell prints its own
	# non-vacuity line and this one did not, so a passing run would say
	# nothing about whether the fault had actually fired -- the check worked
	# while being invisible, which is the shape this family's whole epic is
	# about.
	printf 'fail-graph-write-once-then-succeed-code-taint-evidence: non-vacuous: once-fired marker names the targeted HAS_TAINT_EVIDENCE MERGE (written by the fault decorator at injection time, not read from the reducer log)\n'
	capture_digest failgraphwritetaint
	assert_matches_baseline failgraphwritetaint baseline_code_taint_evidence
	teardown_cell failgraphwritetaint
	wall_times[failgraphwritetaint]=$(( $(date +%s) - cell_start ))
	printf 'fail-graph-write-once-then-succeed-code-taint-evidence: cell wall time: %ss\n' "${wall_times[failgraphwritetaint]}"
}
