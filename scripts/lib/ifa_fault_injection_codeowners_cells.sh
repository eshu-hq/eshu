#!/usr/bin/env bash
# shellcheck disable=SC2034,SC2154
# codeowners_ownership_edges-targeted live fault cells (#5992), mirroring
# scripts/lib/ifa_fault_injection_code_call_cells.sh's shape exactly.
# WIRED: scripts/verify-ifa-fault-injection.sh sources this file and runs
# all three cells (baseline_codeowners, kill-worker-after-claim-codeowners,
# fail-graph-write-once-then-succeed-codeowners) against a live Postgres +
# NornicDB stack. The cells rely on the driver's strict mode and globals
# (bin_dir, tagged_bin_dir, log_dir, work_dir, use_compose, compose_file,
# FAULT_COMPOSE_PROJECT, ESHU_POSTGRES_DSN, CLAIMED_ROW_WAIT_TIMEOUT,
# wall_times, bg_pids, log, die, plus the fresh_stack / drive_all_cassettes /
# run_drain_gate / assert_no_dead_letters / capture_digest /
# assert_matches_baseline / teardown_cell helpers from
# ifa_fault_injection_driver.sh).
#
# LIVE-PROVEN: all three cells pass in the 21-cell fault matrix, matching
# digest across baseline/killworker/failgraphwrite. codeowners_expected_edges
# and codeowners_edge_operation_match are declared in
# verify-ifa-fault-injection.sh; codeowners_expected_edges is also declared in
# verify-ifa-determinism.sh for the determinism-side digest comparison.
#
# scripts/lib/test-ifa-fault-injection-codeowners-cases.sh sources this file
# and drives ifa_codeowners_start_fact_records_lock and
# ifa_codeowners_release_fact_records_lock under stubs, and
# scripts/test-verify-ifa-fault-injection.sh runs that module -- so editing
# either function's fail-closed behavior or its bg_pids bookkeeping will fail
# `make pre-pr`.
#
# codeowners_ownership_edges is SINGLE-STAGE. An earlier version of this header
# described a two-stage pipeline and every cell below was built on it; the
# reading was wrong in every clause, and its own UNVERIFIED marker was right to
# doubt it (#5992). What actually happens:
#
#   CodeownersOwnershipEdgeMaterializationHandler claims a fact_work_items row
#   under domain "codeowners_ownership" (reducer.DomainCodeownersOwnership,
#   go/internal/reducer/intent.go:75), loads its facts via
#   FactStore.ListFactsByKind -> FROM fact_records
#   (go/internal/storage/postgres/facts_filtered.go:47,140), and writes the
#   DECLARES_CODEOWNER edges DIRECTLY to the graph through
#   EdgeWriter.WriteEdges (codeowners_ownership_materialization.go:86), then
#   acks. DomainCodeownersOwnershipEdges is a ProjectionDomain LABEL on those
#   rows, not a second queue stage.
#
# The handler struct carries no IntentWriter -- only FactLoader, EdgeWriter,
# PriorGenerationCheck, Instruments -- so it never writes
# shared_projection_intents at all. Locking that table (what this file used to
# do, copied from the code_calls family) blocks nothing here: the
# code_call sibling's FIRST-stage handler genuinely does write it via
# CodeCallIntentWriter (go/cmd/reducer/main.go:241), which is why the technique
# is true there and false here. That is the whole defect -- a mechanism claim
# inherited verbatim from the sibling it was copied from.
#
# The lock target is therefore the handler's first synchronous READ, not a
# write: ACCESS EXCLUSIVE on fact_records blocks readers, and the claim/ack
# path never touches that table (reducer_queue_claim_query.go,
# reducer_queue_batch.go, reducer_queue.go contain zero fact_records
# references), so the row stays claimed while the handler is held.
#
# PROVEN, not derived (#5992, live stack, 2026-08-17): with the lock held, the
# codeowners_ownership row sat claimed for 14 consecutive 1s samples while
# 3 reducer backends showed in pg_locks with granted=false on fact_records --
# the causal link, not just a slow handler. Releasing the lock let the row reach
# succeeded within 1s, and `ifa assert-edges` matched the five-edge set exactly.
# This is the deterministic pre-write window the documentation family's
# expire-lease cell never had.

# ifa_codeowners_start_fact_records_lock holds an ACCESS EXCLUSIVE lock on
# fact_records so the codeowners_ownership handler blocks on its FIRST
# synchronous read -- the fact load -- while its fact_work_items claim is
# already held. It does NOT mirror the code_calls family's table
# choice: that sibling locks shared_projection_intents because its first-stage
# handler writes it, and this handler never does (see the header). The claim,
# heartbeat, and ack path never reads fact_records, so the row stays claimed
# and alive for the whole hold -- measured at 14s with the reducer backends
# visibly waiting on this relation.
ifa_codeowners_start_fact_records_lock() {
	local cell="$1" pid_var="$2"
	local app_name="ifa_codeowners_lock_${cell}"
	local lock_sql="SET application_name = '${app_name}'; BEGIN; LOCK TABLE fact_records IN ACCESS EXCLUSIVE MODE; SELECT pg_sleep(180); ROLLBACK;"
	if [[ "${use_compose}" -eq 1 ]]; then
		docker compose -p "${FAULT_COMPOSE_PROJECT}" -f "${compose_file}" exec -T postgres \
			psql -v ON_ERROR_STOP=1 -U eshu -d eshu -c "${lock_sql}" \
			>"${log_dir}/codeowners-lock-${cell}.log" 2>&1 &
	else
		psql "${ESHU_POSTGRES_DSN}" -v ON_ERROR_STOP=1 -c "${lock_sql}" \
			>"${log_dir}/codeowners-lock-${cell}.log" 2>&1 &
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
	return 1
}

# ifa_codeowners_release_fact_records_lock terminates the named lock-holder
# backend, then joins its local psql/docker process before the replacement
# reducer starts.
ifa_codeowners_release_fact_records_lock() {
	local cell="$1" holder_pid="$2"
	local app_name="ifa_codeowners_lock_${cell}"
	ifa_det_pg "${FAULT_COMPOSE_PROJECT}" "${use_compose}" "${ESHU_POSTGRES_DSN}" \
		"SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE application_name = '${app_name}';" \
		"${compose_file}" >/dev/null
	wait "${holder_pid}" 2>/dev/null || true
	ifa_det_untrack_bg_pid "${holder_pid}"
}

# cell_killworker_codeowners proves a genuinely in-flight codeowners-ownership
# handler is reclaimed after process death. The fact_records lock holds the
# handler on its fact load -- PRE-write, claim already held -- so the kill lands
# on real in-flight work rather than racing a handler that has already acked;
# attempt_count > the clean baseline proves the replacement reducer re-executed
# that domain, not merely another queued row.
#
# The drive is family-scoped on purpose. drive_all_cassettes does NOT drive the
# codeowners cassette (ifa_fault_injection_driver.sh:82-99 drives demo-org,
# synth-multiscope, SQL, code_call, and documentation), so without the explicit
# ifa_codeowners_drive below no codeowners.ownership fact is ever ingested and
# nothing this cell asserts can fire.
# cell_baseline_codeowners is this family's fault-free reference run. It exists
# because the codeowners cassette is driven ONLY by this family's own cells:
# ifa_fault_injection_driver.sh's header rules that a new family cassette is
# never added to drive_all_cassettes, since the extra succeeded reducer row
# enlarges ifa_fault_redeliver_succeeded's row set inside cell_duplicatedelivery
# -- the exact live failure #5993 hit. So the shared `baseline` digest contains
# no DECLARES_CODEOWNER edges and every codeowners cell would mismatch it.
# These cells therefore compare against digests[baseline_codeowners] via
# assert_matches_baseline's baseline_key parameter.
#
# It also establishes baseline_codeowners_retried: the fault-free attempt_count
# for domain codeowners_ownership, which cell_killworker_codeowners must exceed
# to prove the replacement reducer re-executed THIS domain rather than merely
# draining another queued row.
cell_baseline_codeowners() {
	local cell_start
	cell_start=$(date +%s)
	log "cell baseline-codeowners: fresh stack"
	fresh_stack baseline_codeowners
	drive_all_cassettes baseline_codeowners
	ifa_codeowners_drive "baseline_codeowners" "${bin_dir}" "${codeowners_cassette}" "${drive_workers}" "${log_dir}" \
		|| die "baseline-codeowners: eshu-ifa drive (codeowners family) failed"
	local projector_pid reducer_pid
	ifa_det_start_bg "${log_dir}" "projector-baseline_codeowners" projector_pid "${bin_dir}/eshu-projector"
	ifa_det_start_bg "${log_dir}" "reducer-baseline_codeowners" reducer_pid "${bin_dir}/eshu-reducer"
	run_drain_gate baseline_codeowners
	assert_no_dead_letters baseline_codeowners
	ifa_codeowners_assert "baseline_codeowners" "${bin_dir}" "${codeowners_expected_edges}" \
		|| die "baseline-codeowners: fault-free graph does not match the five-edge exact set"
	baseline_codeowners_retried="$(ifa_fault_count_retried "${FAULT_COMPOSE_PROJECT}" "${use_compose}" "${ESHU_POSTGRES_DSN}" "${compose_file}" "codeowners_ownership")" \
		|| die "baseline-codeowners: could not count the fault-free codeowners_ownership retry baseline"
	printf 'baseline-codeowners: fault-free codeowners_ownership retry baseline: %s\n' "${baseline_codeowners_retried}"
	capture_digest baseline_codeowners
	teardown_cell baseline_codeowners
	wall_times[baseline_codeowners]=$(( $(date +%s) - cell_start ))
	printf 'baseline-codeowners: cell wall time: %ss\n' "${wall_times[baseline_codeowners]}"
}

cell_killworker_codeowners() {
	local cell_start
	cell_start=$(date +%s)
	log "cell kill-worker-after-claim-codeowners: fresh stack"
	fresh_stack killworkercodeowners
	drive_all_cassettes killworkercodeowners
	ifa_codeowners_drive "killworkercodeowners" "${bin_dir}" "${codeowners_cassette}" "${drive_workers}" "${log_dir}" \
		|| die "kill-worker-after-claim-codeowners: eshu-ifa drive (codeowners family) failed"
	local projector_pid reducer_pid_before reducer_pid_after lock_holder_pid claimed_before
	ifa_det_start_bg "${log_dir}" "projector-killworkercodeowners" projector_pid "${bin_dir}/eshu-projector"
	# codeowners_ownership's reducer intent is created by the PROJECTOR, not
	# ingestion, and this cell's demo-org/synth-multiscope/family fixture load
	# gives the projector up to ~15 scopes to work through with 4 workers before
	# it reaches this one. Locking fact_records before that intent exists risks
	# starving the projector's OWN fact reads for this scope too (it is started
	# moments earlier, not guaranteed to have reached it yet), so the row could
	# never be created for as long as the lock is held -- not merely delayed.
	# Waiting for the row to exist (any status) first removes that dependency
	# before the lock -- and the tight claimed/running wait below -- begin.
	ifa_fault_wait_for_claimed "${FAULT_COMPOSE_PROJECT}" "${use_compose}" "${ESHU_POSTGRES_DSN}" "${compose_file}" \
		"${CLAIMED_ROW_WAIT_TIMEOUT}" "codeowners_ownership" 1 >/dev/null \
		|| die "kill-worker-after-claim-codeowners: codeowners_ownership was never enqueued by the projector"
	ifa_codeowners_start_fact_records_lock "killworkercodeowners" lock_holder_pid \
		|| die "kill-worker-after-claim-codeowners: could not acquire the deterministic fact_records read blocker"
	# Scoped to codeowners_ownership only: with the lock already held, an
	# unscoped reducer's 4 workers can all grab OTHER pending domains that also
	# read fact_records (cloud_inventory_admission, code_call_materialization,
	# sql_relationship_materialization, documentation_materialization all do)
	# and get stuck there for the lock's full duration, starving this domain of
	# a worker even though its own row already exists -- proven live on CI
	# after the enqueue-wait fix alone still left it unclaimed for 120s.
	# ESHU_REDUCER_CLAIM_DOMAIN filters the claim SQL itself (domain = ANY(...)),
	# so this instance never attempts another domain's row.
	ifa_det_start_bg "${log_dir}" "reducer-killworkercodeowners-before" reducer_pid_before \
		env "ESHU_REDUCER_CLAIM_DOMAIN=codeowners_ownership" "${bin_dir}/eshu-reducer"
	claimed_before="$(ifa_fault_wait_for_claimed "${FAULT_COMPOSE_PROJECT}" "${use_compose}" "${ESHU_POSTGRES_DSN}" "${compose_file}" "${CLAIMED_ROW_WAIT_TIMEOUT}" "codeowners_ownership")" \
		|| die "kill-worker-after-claim-codeowners: no codeowners_ownership row was claimed while its fact load was blocked"
	printf 'kill-worker-after-claim-codeowners: non-vacuous: %s blocked claimed/running row(s) observed\n' "${claimed_before}"
	kill -9 "${reducer_pid_before}" >/dev/null 2>&1 || true
	ifa_codeowners_release_fact_records_lock "killworkercodeowners" "${lock_holder_pid}"
	ifa_det_start_bg "${log_dir}" "reducer-killworkercodeowners-after" reducer_pid_after "${bin_dir}/eshu-reducer"
	run_drain_gate killworkercodeowners
	assert_no_dead_letters killworkercodeowners
	ifa_codeowners_assert "killworkercodeowners" "${bin_dir}" "${codeowners_expected_edges}" \
		|| die "kill-worker-after-claim-codeowners: recovered graph does not match the five-edge exact set"
	ifa_fault_assert_retried_above "${FAULT_COMPOSE_PROJECT}" "${use_compose}" "${ESHU_POSTGRES_DSN}" "${compose_file}" \
		"${baseline_codeowners_retried}" 15 "codeowners_ownership" \
		|| die "kill-worker-after-claim-codeowners: codeowners_ownership did not re-execute above its fault-free retry baseline"
	capture_digest killworkercodeowners
	assert_matches_baseline killworkercodeowners baseline_codeowners
	teardown_cell killworkercodeowners
	wall_times[killworkercodeowners]=$(( $(date +%s) - cell_start ))
	printf 'kill-worker-after-claim-codeowners: cell wall time: %ss\n' "${wall_times[killworkercodeowners]}"
}

# cell_failgraphwrite_codeowners fails the live DECLARES_CODEOWNER MERGE
# exactly once, proves the fault decorator's durable marker names that write,
# and requires the full codeowners_ownership_edges intent/edge set to
# converge without dead letters.
cell_failgraphwrite_codeowners() {
	local cell_start
	cell_start=$(date +%s)
	log "cell fail-graph-write-once-then-succeed-codeowners: fresh stack"
	fresh_stack failgraphwritecodeowners
	drive_all_cassettes failgraphwritecodeowners
	ifa_codeowners_drive "failgraphwritecodeowners" "${bin_dir}" "${codeowners_cassette}" "${drive_workers}" "${log_dir}" \
		|| die "fail-graph-write-once-then-succeed-codeowners: eshu-ifa drive (codeowners family) failed"
	local fault_once_script projector_pid reducer_pid marker_rc
	fault_once_script="${work_dir}/fault-once-then-succeed-codeowners.json"
	ifa_fault_write_once_script "${fault_once_script}" "${codeowners_edge_operation_match}" "queue-retry"
	ifa_det_start_bg "${log_dir}" "projector-failgraphwritecodeowners" projector_pid "${bin_dir}/eshu-projector"
	ifa_det_start_bg "${log_dir}" "reducer-failgraphwritecodeowners" reducer_pid \
		env "ESHU_IFA_FAULT_SCRIPT=${fault_once_script}" "${tagged_bin_dir}/eshu-reducer"
	run_drain_gate failgraphwritecodeowners
	assert_no_dead_letters failgraphwritecodeowners
	ifa_codeowners_assert "failgraphwritecodeowners" "${bin_dir}" "${codeowners_expected_edges}" \
		|| die "fail-graph-write-once-then-succeed-codeowners: recovered graph does not match the five-edge exact set"
	ifa_det_pg "${FAULT_COMPOSE_PROJECT}" "${use_compose}" "${ESHU_POSTGRES_DSN}" \
		"SELECT count(*) AS total, count(*) FILTER (WHERE status = 'succeeded') AS succeeded FROM fact_work_items WHERE stage = 'reducer' AND domain = 'codeowners_ownership';" \
		"${compose_file}" | sed 's/^/  codeowners_ownership work-item window: /'
	marker_rc=0
	ifa_fault_assert_once_fault_marker "${fault_once_script}" "${codeowners_edge_operation_match}" || marker_rc=$?
	if [[ "${marker_rc}" -eq 2 ]]; then
		# Distinguished from "no marker" deliberately, per #5974: rc=2 means the
		# injection WORKS and fired, but on a different write than the anchor
		# names. Collapsing that into a generic failure sends the next reader
		# looking at the decorator instead of at the anchor.
		die "fail-graph-write-once-then-succeed-codeowners: the fault FIRED but on a different write than ${codeowners_edge_operation_match} (marker contents above). The injection works; the anchor is pointed at the wrong statement."
	fi
	[[ "${marker_rc}" -eq 0 ]] \
		|| die "fail-graph-write-once-then-succeed-codeowners: no once-fired marker at all -- the fault never fired, so this cell proved nothing about DECLARES_CODEOWNER recovery (marker status ${marker_rc})"
	# Announced, not merely checked. Every sibling cell prints its own
	# non-vacuity line and this one did not, so a passing run said nothing about
	# whether the fault had actually fired -- the check worked while being
	# invisible, which is the shape this family's whole epic is about.
	printf 'fail-graph-write-once-then-succeed-codeowners: non-vacuous: once-fired marker names the targeted DECLARES_CODEOWNER MERGE (written by the fault decorator at injection time, not read from the reducer log)\n'
	capture_digest failgraphwritecodeowners
	assert_matches_baseline failgraphwritecodeowners baseline_codeowners
	teardown_cell failgraphwritecodeowners
	wall_times[failgraphwritecodeowners]=$(( $(date +%s) - cell_start ))
	printf 'fail-graph-write-once-then-succeed-codeowners: cell wall time: %ss\n' "${wall_times[failgraphwritecodeowners]}"
}
