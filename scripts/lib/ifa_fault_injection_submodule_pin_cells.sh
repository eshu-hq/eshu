#!/usr/bin/env bash
# shellcheck disable=SC2034,SC2154
# submodule_pin_edges-targeted live fault cells (#6002), mirroring
# scripts/lib/ifa_fault_injection_codeowners_cells.sh's shape exactly:
# SubmodulePinEdgeMaterializationHandler is the same EdgeWriter-only,
# no-IntentWriter shape as CodeownersOwnershipEdgeMaterializationHandler
# (both: FactLoader, EdgeWriter, PriorGenerationCheck, Instruments; `rg -c
# IntentWriter go/internal/reducer/submodule_pin_materialization.go` returns
# nothing, exit 1), so the same lock target -- fact_records, blocking the
# handler's first synchronous read -- applies for the same reason.
#
# CUSTOM, NOT GENERIC -- and for a DIFFERENT reason than codeowners' row
# states for itself. codeowners_ownership_edges stays custom because #6160
# hand-wired its three cells before scripts/lib/ifa_fault_generic_cells.sh
# existed and never migrated them (a dispatch-history reason). This family
# is custom because the generic path is PROVEN BROKEN for fact_records:
# scripts/lib/ifa_fault_generic_table_lock.sh's
# _ifa_generic_require_table_domain_written runs `SELECT count(*) FROM
# <table> WHERE domain = '<wait_key>'`, which assumes the locked table has a
# domain column. fact_records does not: go/internal/storage/postgres/
# migrations/003_fact_records.sql's CREATE TABLE plus its own four ALTER
# TABLE ADD COLUMN statements list fact_id, scope_id, generation_id,
# fact_kind, stable_fact_key, schema_version, collector_kind, fencing_token,
# source_confidence, source_system, source_fact_key, source_uri,
# source_record_id, observed_at, ingested_at, is_tombstone, payload -- no
# domain column anywhere, and no later migration adds one (`rg "ALTER TABLE
# fact_records" go/internal/storage/postgres/migrations/*.sql` returns only
# 003's own four). ifa_fault_generic_table_lock.sh's own header already
# names codeowners_ownership_edges (also table_lock:fact_records) as the
# worked example of this exact mismatch; this family shares the same table
# and therefore the same mismatch. cell_kind=custom records that fact, not a
# dispatch-history accident.
#
# WIRED: scripts/verify-ifa-fault-injection.sh sources this file and runs
# all three cells (baseline_submodule_pin, kill-worker-after-claim-
# submodule-pin, fail-graph-write-once-then-succeed-submodule-pin) against a
# live Postgres + NornicDB stack. The cells rely on the driver's strict mode
# and globals (bin_dir, tagged_bin_dir, log_dir, work_dir, use_compose,
# compose_file, FAULT_COMPOSE_PROJECT, ESHU_POSTGRES_DSN,
# CLAIMED_ROW_WAIT_TIMEOUT, wall_times, bg_pids, log, die, plus the
# fresh_stack / drive_all_cassettes / run_drain_gate / assert_no_dead_letters
# / capture_digest / assert_matches_baseline / teardown_cell helpers from
# ifa_fault_injection_driver.sh).
#
# submodule_pin_edges is SINGLE-STAGE, the same shape codeowners_ownership_edges'
# header documents at length: SubmodulePinEdgeMaterializationHandler claims a
# fact_work_items row under domain "submodule_pin"
# (reducer.DomainSubmodulePin, go/internal/reducer/intent.go:82), loads its
# facts via FactStore.ListFactsByKind -> FROM fact_records
# (go/internal/storage/postgres/facts_filtered.go:140,
# go/internal/reducer/submodule_pin_delta_scope.go:38-55), and writes the
# PINS_SUBMODULE edges DIRECTLY to the graph through EdgeWriter.WriteEdges
# (submodule_pin_materialization.go:92), then acks. DomainSubmodulePinEdges
# is a ProjectionDomain LABEL on those rows (submodule_pin_materialization.go:83,94),
# not a second queue stage.
#
# The lock target is therefore the handler's first synchronous READ, not a
# write: ACCESS EXCLUSIVE on fact_records blocks readers, and the claim/ack
# path never touches that table (reducer_queue_claim_query.go,
# reducer_queue_batch.go, reducer_queue.go contain zero fact_records
# references), so the row stays claimed while the handler is held -- the
# identical mechanism codeowners_ownership_edges proved live (#5992,
# 2026-08-17: 14 consecutive 1s samples with reducer backends visibly
# waiting on fact_records in pg_locks).

# ifa_submodule_pin_start_fact_records_lock holds an ACCESS EXCLUSIVE lock on
# fact_records so the submodule_pin handler blocks on its FIRST synchronous
# read -- the fact load -- while its fact_work_items claim is already held.
ifa_submodule_pin_start_fact_records_lock() {
	local cell="$1" pid_var="$2"
	local app_name="ifa_submodule_pin_lock_${cell}"
	local lock_sql="SET application_name = '${app_name}'; BEGIN; LOCK TABLE fact_records IN ACCESS EXCLUSIVE MODE; SELECT pg_sleep(180); ROLLBACK;"
	if [[ "${use_compose}" -eq 1 ]]; then
		docker compose -p "${FAULT_COMPOSE_PROJECT}" -f "${compose_file}" exec -T postgres \
			psql -v ON_ERROR_STOP=1 -U eshu -d eshu -c "${lock_sql}" \
			>"${log_dir}/submodule-pin-lock-${cell}.log" 2>&1 &
	else
		psql "${ESHU_POSTGRES_DSN}" -v ON_ERROR_STOP=1 -c "${lock_sql}" \
			>"${log_dir}/submodule-pin-lock-${cell}.log" 2>&1 &
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

# ifa_submodule_pin_release_fact_records_lock terminates the named lock-holder
# backend, then joins its local psql/docker process before the replacement
# reducer starts.
ifa_submodule_pin_release_fact_records_lock() {
	local cell="$1" holder_pid="$2"
	local app_name="ifa_submodule_pin_lock_${cell}"
	ifa_det_pg "${FAULT_COMPOSE_PROJECT}" "${use_compose}" "${ESHU_POSTGRES_DSN}" \
		"SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE application_name = '${app_name}';" \
		"${compose_file}" >/dev/null
	wait "${holder_pid}" 2>/dev/null || true
	ifa_det_untrack_bg_pid "${holder_pid}"
}

# cell_baseline_submodule_pin is this family's fault-free reference run. It
# exists because the submodule_pin cassette is driven ONLY by this family's
# own cells: ifa_fault_injection_driver.sh's header rules that a new family
# cassette is never added to drive_all_cassettes, since the extra succeeded
# reducer row enlarges ifa_fault_redeliver_succeeded's row set inside
# cell_duplicatedelivery -- the exact live failure #5993 hit. So the shared
# `baseline` digest contains no PINS_SUBMODULE edges and every submodule_pin
# cell would mismatch it. These cells therefore compare against
# digests[baseline_submodule_pin] via assert_matches_baseline's baseline_key
# parameter.
#
# It also establishes baseline_submodule_pin_retried: the fault-free
# attempt_count for domain submodule_pin, which cell_killworker_submodule_pin
# must exceed to prove the replacement reducer re-executed THIS domain
# rather than merely draining another queued row.
cell_baseline_submodule_pin() {
	local cell_start
	cell_start=$(date +%s)
	log "cell baseline-submodule-pin: fresh stack"
	fresh_stack baseline_submodule_pin
	drive_all_cassettes baseline_submodule_pin
	ifa_submodule_pin_drive "baseline_submodule_pin" "${bin_dir}" "${submodule_pin_cassette}" "${drive_workers}" "${log_dir}" \
		|| die "baseline-submodule-pin: eshu-ifa drive (submodule-pin family) failed"
	local projector_pid reducer_pid
	ifa_det_start_bg "${log_dir}" "projector-baseline_submodule_pin" projector_pid "${bin_dir}/eshu-projector"
	ifa_det_start_bg "${log_dir}" "reducer-baseline_submodule_pin" reducer_pid "${bin_dir}/eshu-reducer"
	run_drain_gate baseline_submodule_pin
	assert_no_dead_letters baseline_submodule_pin
	ifa_submodule_pin_assert "baseline_submodule_pin" "${bin_dir}" "${submodule_pin_expected_edges}" \
		|| die "baseline-submodule-pin: fault-free graph does not match the three-edge exact set"
	baseline_submodule_pin_retried="$(ifa_fault_count_retried "${FAULT_COMPOSE_PROJECT}" "${use_compose}" "${ESHU_POSTGRES_DSN}" "${compose_file}" "submodule_pin")" \
		|| die "baseline-submodule-pin: could not count the fault-free submodule_pin retry baseline"
	printf 'baseline-submodule-pin: fault-free submodule_pin retry baseline: %s\n' "${baseline_submodule_pin_retried}"
	capture_digest baseline_submodule_pin
	teardown_cell baseline_submodule_pin
	wall_times[baseline_submodule_pin]=$(( $(date +%s) - cell_start ))
	printf 'baseline-submodule-pin: cell wall time: %ss\n' "${wall_times[baseline_submodule_pin]}"
}

# cell_killworker_submodule_pin proves a genuinely in-flight submodule_pin
# handler is reclaimed after process death. The fact_records lock holds the
# handler on its fact load -- PRE-write, claim already held -- so the kill
# lands on real in-flight work rather than racing a handler that has already
# acked; attempt_count > the clean baseline proves the replacement reducer
# re-executed that domain, not merely another queued row.
#
# The drive is family-scoped on purpose, mirroring codeowners: drive_all_cassettes
# does NOT drive the submodule_pin cassette, so without the explicit
# ifa_submodule_pin_drive below no submodule.pin fact is ever ingested and
# nothing this cell asserts can fire.
cell_killworker_submodule_pin() {
	local cell_start
	cell_start=$(date +%s)
	log "cell kill-worker-after-claim-submodule-pin: fresh stack"
	fresh_stack killworkersubmodulepin
	drive_all_cassettes killworkersubmodulepin
	ifa_submodule_pin_drive "killworkersubmodulepin" "${bin_dir}" "${submodule_pin_cassette}" "${drive_workers}" "${log_dir}" \
		|| die "kill-worker-after-claim-submodule-pin: eshu-ifa drive (submodule-pin family) failed"
	local projector_pid reducer_pid_before reducer_pid_after lock_holder_pid claimed_before
	ifa_det_start_bg "${log_dir}" "projector-killworkersubmodulepin" projector_pid "${bin_dir}/eshu-projector"
	# submodule_pin's reducer intent is created by the PROJECTOR, not
	# ingestion, mirroring codeowners_ownership: this cell's demo-org/synth-
	# multiscope/family fixture load gives the projector up to ~15 scopes to
	# work through with 4 workers before it reaches this one. Locking
	# fact_records before that intent exists risks starving the projector's
	# OWN fact reads for this scope too, so the row could never be created for
	# as long as the lock is held -- not merely delayed. Waiting for the row
	# to exist (any status) first removes that dependency before the lock --
	# and the tight claimed/running wait below -- begin.
	ifa_fault_wait_for_claimed "${FAULT_COMPOSE_PROJECT}" "${use_compose}" "${ESHU_POSTGRES_DSN}" "${compose_file}" \
		"${CLAIMED_ROW_WAIT_TIMEOUT}" "submodule_pin" 1 >/dev/null \
		|| die "kill-worker-after-claim-submodule-pin: submodule_pin was never enqueued by the projector"
	ifa_submodule_pin_start_fact_records_lock "killworkersubmodulepin" lock_holder_pid \
		|| die "kill-worker-after-claim-submodule-pin: could not acquire the deterministic fact_records read blocker"
	# Scoped to submodule_pin only, mirroring codeowners: with the lock
	# already held, an unscoped reducer's 4 workers can all grab OTHER
	# pending domains that also read fact_records and get stuck there for the
	# lock's full duration, starving this domain of a worker even though its
	# own row already exists. ESHU_REDUCER_CLAIM_DOMAIN filters the claim SQL
	# itself (domain = ANY(...)), so this instance never attempts another
	# domain's row.
	ifa_det_start_bg "${log_dir}" "reducer-killworkersubmodulepin-before" reducer_pid_before \
		env "ESHU_REDUCER_CLAIM_DOMAIN=submodule_pin" "${bin_dir}/eshu-reducer"
	claimed_before="$(ifa_fault_wait_for_claimed "${FAULT_COMPOSE_PROJECT}" "${use_compose}" "${ESHU_POSTGRES_DSN}" "${compose_file}" "${CLAIMED_ROW_WAIT_TIMEOUT}" "submodule_pin")" \
		|| die "kill-worker-after-claim-submodule-pin: no submodule_pin row was claimed while its fact load was blocked"
	printf 'kill-worker-after-claim-submodule-pin: non-vacuous: %s blocked claimed/running row(s) observed\n' "${claimed_before}"
	kill -9 "${reducer_pid_before}" >/dev/null 2>&1 || true
	ifa_submodule_pin_release_fact_records_lock "killworkersubmodulepin" "${lock_holder_pid}"
	ifa_det_start_bg "${log_dir}" "reducer-killworkersubmodulepin-after" reducer_pid_after "${bin_dir}/eshu-reducer"
	run_drain_gate killworkersubmodulepin
	assert_no_dead_letters killworkersubmodulepin
	ifa_submodule_pin_assert "killworkersubmodulepin" "${bin_dir}" "${submodule_pin_expected_edges}" \
		|| die "kill-worker-after-claim-submodule-pin: recovered graph does not match the three-edge exact set"
	ifa_fault_assert_retried_above "${FAULT_COMPOSE_PROJECT}" "${use_compose}" "${ESHU_POSTGRES_DSN}" "${compose_file}" \
		"${baseline_submodule_pin_retried}" 15 "submodule_pin" \
		|| die "kill-worker-after-claim-submodule-pin: submodule_pin did not re-execute above its fault-free retry baseline"
	capture_digest killworkersubmodulepin
	assert_matches_baseline killworkersubmodulepin baseline_submodule_pin
	teardown_cell killworkersubmodulepin
	wall_times[killworkersubmodulepin]=$(( $(date +%s) - cell_start ))
	printf 'kill-worker-after-claim-submodule-pin: cell wall time: %ss\n' "${wall_times[killworkersubmodulepin]}"
}

# cell_failgraphwrite_submodule_pin fails the live PINS_SUBMODULE MERGE
# exactly once, proves the fault decorator's durable marker names that write,
# and requires the full submodule_pin_edges intent/edge set to converge
# without dead letters.
cell_failgraphwrite_submodule_pin() {
	local cell_start
	cell_start=$(date +%s)
	log "cell fail-graph-write-once-then-succeed-submodule-pin: fresh stack"
	fresh_stack failgraphwritesubmodulepin
	drive_all_cassettes failgraphwritesubmodulepin
	ifa_submodule_pin_drive "failgraphwritesubmodulepin" "${bin_dir}" "${submodule_pin_cassette}" "${drive_workers}" "${log_dir}" \
		|| die "fail-graph-write-once-then-succeed-submodule-pin: eshu-ifa drive (submodule-pin family) failed"
	local fault_once_script projector_pid reducer_pid marker_rc
	fault_once_script="${work_dir}/fault-once-then-succeed-submodule-pin.json"
	ifa_fault_write_once_script "${fault_once_script}" "${submodule_pin_edge_operation_match}" "queue-retry"
	ifa_det_start_bg "${log_dir}" "projector-failgraphwritesubmodulepin" projector_pid "${bin_dir}/eshu-projector"
	ifa_det_start_bg "${log_dir}" "reducer-failgraphwritesubmodulepin" reducer_pid \
		env "ESHU_IFA_FAULT_SCRIPT=${fault_once_script}" "${tagged_bin_dir}/eshu-reducer"
	run_drain_gate failgraphwritesubmodulepin
	assert_no_dead_letters failgraphwritesubmodulepin
	ifa_submodule_pin_assert "failgraphwritesubmodulepin" "${bin_dir}" "${submodule_pin_expected_edges}" \
		|| die "fail-graph-write-once-then-succeed-submodule-pin: recovered graph does not match the three-edge exact set"
	ifa_det_pg "${FAULT_COMPOSE_PROJECT}" "${use_compose}" "${ESHU_POSTGRES_DSN}" \
		"SELECT count(*) AS total, count(*) FILTER (WHERE status = 'succeeded') AS succeeded FROM fact_work_items WHERE stage = 'reducer' AND domain = 'submodule_pin';" \
		"${compose_file}" | sed 's/^/  submodule_pin work-item window: /'
	marker_rc=0
	ifa_fault_assert_once_fault_marker "${fault_once_script}" "${submodule_pin_edge_operation_match}" || marker_rc=$?
	if [[ "${marker_rc}" -eq 2 ]]; then
		# Distinguished from "no marker" deliberately, per #5974: rc=2 means the
		# injection WORKS and fired, but on a different write than the anchor
		# names.
		die "fail-graph-write-once-then-succeed-submodule-pin: the fault FIRED but on a different write than ${submodule_pin_edge_operation_match} (marker contents above). The injection works; the anchor is pointed at the wrong statement."
	fi
	[[ "${marker_rc}" -eq 0 ]] \
		|| die "fail-graph-write-once-then-succeed-submodule-pin: no once-fired marker at all -- the fault never fired, so this cell proved nothing about PINS_SUBMODULE recovery (marker status ${marker_rc})"
	printf 'fail-graph-write-once-then-succeed-submodule-pin: non-vacuous: once-fired marker names the targeted PINS_SUBMODULE MERGE (written by the fault decorator at injection time, not read from the reducer log)\n'
	capture_digest failgraphwritesubmodulepin
	assert_matches_baseline failgraphwritesubmodulepin baseline_submodule_pin
	teardown_cell failgraphwritesubmodulepin
	wall_times[failgraphwritesubmodulepin]=$(( $(date +%s) - cell_start ))
	printf 'fail-graph-write-once-then-succeed-submodule-pin: cell wall time: %ss\n' "${wall_times[failgraphwritesubmodulepin]}"
}
