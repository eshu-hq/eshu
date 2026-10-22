#!/usr/bin/env bash
# shellcheck disable=SC2034,SC2154
# code_interproc_evidence-targeted live fault cells (#6228
# code_interproc_evidence live leaf), mirroring
# scripts/lib/ifa_fault_injection_crossplane_satisfied_by_cells.sh's shape
# including the kill cell: this domain's claim is NOT readiness-gated
# (code_interproc_evidence carries no row in the reducer-claim readiness
# requirements), AND its in-handler path carries no readiness gate before
# the fact load either (Handle goes from the nil guards straight to
# LoadCodeInterprocEvidenceFacts; the projector commits the endpoint nodes
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
# (baseline_code_interproc_evidence,
# killworker_code_interproc_evidence,
# failgraphwrite_code_interproc_evidence) against a live Postgres +
# NornicDB stack. The cells rely on the driver's strict mode and globals
# (bin_dir, tagged_bin_dir, log_dir, work_dir, use_compose, compose_file,
# FAULT_COMPOSE_PROJECT, ESHU_POSTGRES_DSN, CLAIMED_ROW_WAIT_TIMEOUT,
# drive_workers, wall_times, bg_pids, log, die, plus the fresh_stack /
# drive_all_cassettes / run_drain_gate / assert_no_dead_letters /
# capture_digest / assert_matches_baseline / teardown_cell helpers from
# ifa_fault_injection_driver.sh). Drive and assert callbacks come from
# scripts/lib/ifa_direct_family_live_code_interproc_evidence.sh
# (ifa_code_interproc_evidence_drive /
# ifa_code_interproc_evidence_assert); the cassette and expected-edge
# variables come from scripts/lib/ifa_family_fixtures.sh.
#
# scripts/lib/test-ifa-fault-injection-code-interproc-evidence-cases.sh
# sources this file and drives
# ifa_code_interproc_evidence_start_fact_records_lock and
# ifa_code_interproc_evidence_release_fact_records_lock under stubs, and
# scripts/test-verify-ifa-fault-injection.sh runs that module -- so editing
# either function's fail-closed behavior or its bg_pids bookkeeping will
# fail `make pre-pr`.
#
# code_interproc_evidence spans ONE scope (the fixture repository), with one
# work item under domain "code_interproc_evidence". The handler
# (InterprocEvidenceHandler) claims a fact_work_items row under that domain,
# loads its facts via factload (LoadCodeInterprocEvidenceFacts -> FROM
# fact_records, own scope only) and the entities phase commits the endpoint
# Function nodes from the same generation's content_entity facts, then
# writes the TAINT_FLOWS_TO edges DIRECTLY to the graph through its
# interproc evidence writer, then acks. There is no
# shared_projection_intents row and no second-stage runner lease -- the two
# blocker kinds the registry schema offers that this family cannot use.
#
# The lock target is therefore the handler's first synchronous READ, not a
# write: ACCESS EXCLUSIVE on fact_records blocks readers, and the claim/ack
# path never touches that table, so the row stays claimed while the handler
# is held. fact_records, not fact_work_items: the cell's own wait predicate
# polls fact_work_items, and locking that table would block the poll too.
ifa_code_interproc_evidence_start_fact_records_lock() {
	local cell="$1" pid_var="$2"
	# Short prefix deliberately: the repo holds application_name to 63 bytes
	# (ifa_fault_generic_runner_wait.sh, ifa_fault_injection_documentation_ack_setup.sh),
	# and a full family-name prefix plus a long kill cell name would be
	# silently truncated, so the grant poll below (which matches
	# the full name) would miss forever while the holder sat on its granted
	# lock. Proven live by the sibling kubernetes family's identical failure.
	# ifa_ciproc_lock_ keeps the longest name this family uses at 32 bytes.
	local app_name="ifa_ciproc_lock_${cell}"
	if ((${#app_name} > 63)); then
		printf '%s: lock application_name is %s bytes; past the 63-byte repo cap the name truncates and the grant poll would never match\n' "${cell}" "${#app_name}" >&2
		return 1
	fi
	local lock_sql="SET application_name = '${app_name}'; BEGIN; LOCK TABLE fact_records IN ACCESS EXCLUSIVE MODE; SELECT pg_sleep(180); ROLLBACK;"
	if [[ "${use_compose}" -eq 1 ]]; then
		bash scripts/ci/compose.sh -p "${FAULT_COMPOSE_PROJECT}" -f "${compose_file}" -- exec -T postgres \
			psql -v ON_ERROR_STOP=1 -U eshu -d eshu -c "${lock_sql}" \
			>"${log_dir}/code-interproc-evidence-lock-${cell}.log" 2>&1 &
	else
		psql "${ESHU_POSTGRES_DSN}" -v ON_ERROR_STOP=1 -c "${lock_sql}" \
			>"${log_dir}/code-interproc-evidence-lock-${cell}.log" 2>&1 &
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

# ifa_code_interproc_evidence_release_fact_records_lock terminates the
# named lock-holder backend, then joins its local psql/docker process
# before the replacement reducer starts.
ifa_code_interproc_evidence_release_fact_records_lock() {
	local cell="$1" holder_pid="$2"
	# Same short name as the start function: it must terminate the backend
	# the start function actually created (see its 63-byte-cap comment).
	local app_name="ifa_ciproc_lock_${cell}"
	ifa_det_pg "${FAULT_COMPOSE_PROJECT}" "${use_compose}" "${ESHU_POSTGRES_DSN}" \
		"SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE application_name = '${app_name}';" \
		"${compose_file}" >/dev/null
	wait "${holder_pid}" 2>/dev/null || true
	ifa_det_untrack_bg_pid "${holder_pid}"
}

# cell_baseline_code_interproc_evidence is this family's fault-free
# reference run. It exists because the code_interproc_evidence cassette is
# driven ONLY by this family's own cells:
# ifa_fault_injection_driver.sh's header rules that a new family cassette is
# never added to drive_all_cassettes, since the extra succeeded reducer row
# enlarges ifa_fault_redeliver_succeeded's row set inside
# cell_duplicatedelivery. So the shared `baseline` digest contains no
# TAINT_FLOWS_TO edges and every cell of this family would mismatch it. These
# cells therefore compare against
# digests[baseline_code_interproc_evidence] via assert_matches_baseline's
# baseline_key parameter.
#
# It also establishes baseline_code_interproc_evidence_retried: the
# fault-free excess-ATTEMPT total for domain
# code_interproc_evidence, which the kill cell must exceed to
# prove the replacement reducer re-executed THIS domain rather than merely
# draining another queued row. Attempts, not rows: this cassette creates one
# work item (single scope, one row), and the row-count form saturates at 1
# per row for a single natural retry (see ifa_fault_count_retry_attempts).
cell_baseline_code_interproc_evidence() {
	local cell_start
	cell_start=$(date +%s)
	log "cell baseline-code-interproc-evidence: fresh stack"
	fresh_stack baselineciproc
	drive_all_cassettes baselineciproc
	ifa_code_interproc_evidence_drive "baselineciproc" "${bin_dir}" "${code_interproc_evidence_cassette}" "${drive_workers}" "${log_dir}" \
		|| die "baseline-code-interproc-evidence: eshu-ifa drive (code_interproc_evidence family) failed"
	local projector_pid reducer_pid
	ifa_det_start_bg "${log_dir}" "projector-baselineciproc" projector_pid "${bin_dir}/eshu-projector"
	ifa_det_start_bg "${log_dir}" "reducer-baselineciproc" reducer_pid "${bin_dir}/eshu-reducer"
	run_drain_gate baselineciproc
	assert_no_dead_letters baselineciproc
	ifa_code_interproc_evidence_assert "baselineciproc" "${bin_dir}" "${code_interproc_evidence_expected_edges}" \
		|| die "baseline-code-interproc-evidence: fault-free graph does not match the two-edge exact set"
	baseline_code_interproc_evidence_retried="$(ifa_fault_count_retry_attempts "${FAULT_COMPOSE_PROJECT}" "${use_compose}" "${ESHU_POSTGRES_DSN}" "${compose_file}" "code_interproc_evidence")" \
		|| die "baseline-code-interproc-evidence: could not count the fault-free code_interproc_evidence retry-attempt baseline"
	printf 'baseline-code-interproc-evidence: fault-free code_interproc_evidence retry-attempt baseline: %s\n' "${baseline_code_interproc_evidence_retried}"
	# Captured under the BASELINE KEY, not the cell label: the kill and
	# fail-graph-write cells assert against
	# digests[baseline_code_interproc_evidence]. Capturing under the label
	# leaves that key unset and the match dies on an unbound variable
	# under the gate's `set -u` (found by the crc-image lane's targeted
	# live proof, which got past the attempts assert the old lock-first
	# shape never reached).
	capture_digest baseline_code_interproc_evidence
	teardown_cell baselineciproc
	wall_times[baselineciproc]=$(( $(date +%s) - cell_start ))
	printf 'baseline-code-interproc-evidence: cell wall time: %ss\n' "${wall_times[baselineciproc]}"
}

cell_killworker_code_interproc_evidence() {
	local cell_start
	cell_start=$(date +%s)
	log "cell kill-worker-after-claim-code-interproc-evidence: fresh stack"
	fresh_stack killworkerciproc
	drive_all_cassettes killworkerciproc
	ifa_code_interproc_evidence_drive "killworkerciproc" "${bin_dir}" "${code_interproc_evidence_cassette}" "${drive_workers}" "${log_dir}" \
		|| die "kill-worker-after-claim-code-interproc-evidence: eshu-ifa drive (code_interproc_evidence family) failed"
	local projector_pid reducer_pid_before reducer_pid_after lock_holder_pid claimed_before
	ifa_det_start_bg "${log_dir}" "projector-killworkerciproc" projector_pid "${bin_dir}/eshu-projector"
	# code_interproc_evidence's reducer intent is created by the
	# PROJECTOR, not ingestion. Wait for the row to exist (any status)
	# before going further: without it there is nothing to kill, and locking
	# fact_records before that intent exists risks starving the projector's
	# OWN fact reads for this scope too, so the row could never be created
	# for as long as the lock is held -- not merely delayed.
	ifa_fault_wait_for_claimed "${FAULT_COMPOSE_PROJECT}" "${use_compose}" "${ESHU_POSTGRES_DSN}" "${compose_file}" \
		"${CLAIMED_ROW_WAIT_TIMEOUT}" "code_interproc_evidence" 1 >/dev/null \
		|| die "kill-worker-after-claim-code-interproc-evidence: code_interproc_evidence was never enqueued by the projector"
	# No claim-time readiness gate on this domain and no in-handler gate
	# before the fact load, so the claim fires as soon as the row is pending
	# and the handler blocks deterministically in its load: the lock goes on
	# now. A prereq-scoped reducer would add a wait nothing here needs (the
	# crc-image shape does not transfer: ITS handler gates on readiness
	# before its load).
	ifa_code_interproc_evidence_start_fact_records_lock "killworkerciproc" lock_holder_pid \
		|| die "kill-worker-after-claim-code-interproc-evidence: could not acquire the deterministic fact_records read blocker"
	# Scoped to code_interproc_evidence only: with the lock
	# already held, an unscoped reducer's workers can all grab OTHER pending
	# domains that also read fact_records and get stuck there for the lock's
	# full duration, starving this domain of a worker even though its own row
	# already exists. ESHU_REDUCER_CLAIM_DOMAIN filters the claim SQL itself
	# (domain = ANY(...)), so this instance never attempts another domain's
	# row. The claim fires without waiting on any phase: this domain carries
	# no readiness requirement, so the lock starves nothing the claim still
	# needs.
	ifa_det_start_bg "${log_dir}" "reducer-killworkerciproc-before" reducer_pid_before \
		env "ESHU_REDUCER_CLAIM_DOMAIN=code_interproc_evidence" "${bin_dir}/eshu-reducer"
	claimed_before="$(ifa_fault_wait_for_claimed "${FAULT_COMPOSE_PROJECT}" "${use_compose}" "${ESHU_POSTGRES_DSN}" "${compose_file}" "${CLAIMED_ROW_WAIT_TIMEOUT}" "code_interproc_evidence")" \
		|| die "kill-worker-after-claim-code-interproc-evidence: no code_interproc_evidence row was claimed while its fact load was blocked"
	printf 'kill-worker-after-claim-code-interproc-evidence: non-vacuous: %s blocked claimed/running row(s) observed\n' "${claimed_before}"
	kill -9 "${reducer_pid_before}" >/dev/null 2>&1 || true
	ifa_code_interproc_evidence_release_fact_records_lock "killworkerciproc" "${lock_holder_pid}"
	ifa_det_start_bg "${log_dir}" "reducer-killworkerciproc-after" reducer_pid_after "${bin_dir}/eshu-reducer"
	run_drain_gate killworkerciproc
	assert_no_dead_letters killworkerciproc
	ifa_code_interproc_evidence_assert "killworkerciproc" "${bin_dir}" "${code_interproc_evidence_expected_edges}" \
		|| die "kill-worker-after-claim-code-interproc-evidence: recovered graph does not match the two-edge exact set"
	ifa_fault_assert_retry_attempts_above "${FAULT_COMPOSE_PROJECT}" "${use_compose}" "${ESHU_POSTGRES_DSN}" "${compose_file}" \
		"${baseline_code_interproc_evidence_retried}" 15 "code_interproc_evidence" \
		|| die "kill-worker-after-claim-code-interproc-evidence: code_interproc_evidence did not re-execute above its fault-free retry-attempt baseline"
	capture_digest killworkerciproc
	assert_matches_baseline killworkerciproc baseline_code_interproc_evidence
	teardown_cell killworkerciproc
	wall_times[killworkerciproc]=$(( $(date +%s) - cell_start ))
	printf 'kill-worker-after-claim-code-interproc-evidence: cell wall time: %ss\n' "${wall_times[killworkerciproc]}"
}

# cell_failgraphwrite_code_interproc_evidence fails the live TAINT_FLOWS_TO
# MERGE exactly once, proves the fault decorator's durable marker names that
# write, and requires the full two-edge set to converge without dead
# letters. Anchor read off EXECUTED statement text, not source text: the
# writer's template reads
# MERGE (s)-[rel:TAINT_FLOWS_TO {evidence_uid: row.uid}]->(t)
# (go/internal/storage/cypher/edge/writer/code_interproc_evidence.go:29)
# with no vocabulary substitution, so the anchor carries the PREFIX form
# that matches executed text. A bare `]->(t)` spelling would match no
# executed statement (the template's MERGE carries the property map) and a
# parameterized form would match the retract DELETEs instead -- either way
# the fault would never fire on the upsert, giving a green cell that tested
# nothing.
cell_failgraphwrite_code_interproc_evidence() {
	local cell_start
	cell_start=$(date +%s)
	log "cell fail-graph-write-once-then-succeed-code-interproc-evidence: fresh stack"
	fresh_stack failgraphwriteciproc
	drive_all_cassettes failgraphwriteciproc
	ifa_code_interproc_evidence_drive "failgraphwriteciproc" "${bin_dir}" "${code_interproc_evidence_cassette}" "${drive_workers}" "${log_dir}" \
		|| die "fail-graph-write-once-then-succeed-code-interproc-evidence: eshu-ifa drive (code_interproc_evidence family) failed"
	local fault_once_script projector_pid reducer_pid marker_rc
	fault_once_script="${work_dir}/fault-once-then-succeed-code-interproc-evidence.json"
	ifa_fault_write_once_script "${fault_once_script}" "${code_interproc_evidence_edge_operation_match}" "queue-retry"
	ifa_det_start_bg "${log_dir}" "projector-failgraphwriteciproc" projector_pid "${bin_dir}/eshu-projector"
	ifa_det_start_bg "${log_dir}" "reducer-failgraphwriteciproc" reducer_pid \
		env "ESHU_IFA_FAULT_SCRIPT=${fault_once_script}" "${tagged_bin_dir}/eshu-reducer"
	run_drain_gate failgraphwriteciproc
	assert_no_dead_letters failgraphwriteciproc
	ifa_code_interproc_evidence_assert "failgraphwriteciproc" "${bin_dir}" "${code_interproc_evidence_expected_edges}" \
		|| die "fail-graph-write-once-then-succeed-code-interproc-evidence: recovered graph does not match the two-edge exact set"
	ifa_det_pg "${FAULT_COMPOSE_PROJECT}" "${use_compose}" "${ESHU_POSTGRES_DSN}" \
		"SELECT count(*) AS total, count(*) FILTER (WHERE status = 'succeeded') AS succeeded FROM fact_work_items WHERE stage = 'reducer' AND domain = 'code_interproc_evidence';" \
		"${compose_file}" | sed 's/^/  code_interproc_evidence work-item window: /'
	marker_rc=0
	ifa_fault_assert_once_fault_marker "${fault_once_script}" "${code_interproc_evidence_edge_operation_match}" || marker_rc=$?
	if [[ "${marker_rc}" -eq 2 ]]; then
		# Distinguished from "no marker" deliberately, per #5974: rc=2 means the
		# injection WORKS and fired, but on a different write than the anchor
		# names. Collapsing that into a generic failure sends the next reader
		# looking at the decorator instead of at the anchor.
		die "fail-graph-write-once-then-succeed-code-interproc-evidence: the fault FIRED but on a different write than ${code_interproc_evidence_edge_operation_match} (marker contents above). The injection works; the anchor is pointed at the wrong statement."
	fi
	[[ "${marker_rc}" -eq 0 ]] \
		|| die "fail-graph-write-once-then-succeed-code-interproc-evidence: no once-fired marker at all -- the fault never fired, so this cell proved nothing about TAINT_FLOWS_TO recovery (marker status ${marker_rc})"
	# Announced, not merely checked. Every sibling cell prints its own
	# non-vacuity line and this one did not, so a passing run would say
	# nothing about whether the fault had actually fired -- the check worked
	# while being invisible, which is the shape this family's whole epic is
	# about.
	printf 'fail-graph-write-once-then-succeed-code-interproc-evidence: non-vacuous: once-fired marker names the targeted TAINT_FLOWS_TO MERGE (written by the fault decorator at injection time, not read from the reducer log)\n'
	capture_digest failgraphwriteciproc
	assert_matches_baseline failgraphwriteciproc baseline_code_interproc_evidence
	teardown_cell failgraphwriteciproc
	wall_times[failgraphwriteciproc]=$(( $(date +%s) - cell_start ))
	printf 'fail-graph-write-once-then-succeed-code-interproc-evidence: cell wall time: %ss\n' "${wall_times[failgraphwriteciproc]}"
}
