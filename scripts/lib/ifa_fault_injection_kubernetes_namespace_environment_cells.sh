#!/usr/bin/env bash
# shellcheck disable=SC2034,SC2154
# kubernetes_namespace_environment-targeted live fault cells (#6309),
# mirroring scripts/lib/ifa_fault_injection_codeowners_cells.sh's shape exactly.
# WIRED: scripts/verify-ifa-fault-injection.sh sources this file (through
# scripts/lib/ifa_fault_injection_sources.sh) and runs all three cells
# (baseline_kubernetes_namespace_environment,
# killworker_kubernetes_namespace_environment,
# failgraphwrite_kubernetes_namespace_environment) against a live Postgres +
# NornicDB stack. The cells rely on the driver's strict mode and globals
# (bin_dir, tagged_bin_dir, log_dir, work_dir, use_compose, compose_file,
# FAULT_COMPOSE_PROJECT, ESHU_POSTGRES_DSN, CLAIMED_ROW_WAIT_TIMEOUT,
# drive_workers, wall_times, bg_pids, log, die, plus the fresh_stack /
# drive_all_cassettes / run_drain_gate / assert_no_dead_letters /
# capture_digest / assert_matches_baseline / teardown_cell helpers from
# ifa_fault_injection_driver.sh). Drive and assert callbacks come from
# scripts/lib/ifa_direct_family_live.sh
# (ifa_kubernetes_namespace_environment_drive /
# ifa_kubernetes_namespace_environment_assert); the cassette and expected-edge
# variables come from scripts/lib/ifa_family_fixtures.sh.
#
# scripts/lib/test-ifa-fault-injection-kubernetes-namespace-environment-cases.sh
# sources this file and drives
# ifa_kubernetes_namespace_environment_start_fact_records_lock and
# ifa_kubernetes_namespace_environment_release_fact_records_lock under stubs,
# and scripts/test-verify-ifa-fault-injection.sh runs that module -- so editing
# either function's fail-closed behavior or its bg_pids bookkeeping will fail
# `make pre-pr`.
#
# kubernetes_namespace_environment is SINGLE-STAGE and DIRECT. The handler
# (KubernetesNamespaceMaterializationHandler) claims a fact_work_items row
# under domain "kubernetes_namespace_materialization", loads its facts via
# FactStore.ListFactsByKind -> FROM fact_records, and writes the
# TARGETS_ENVIRONMENT edges DIRECTLY to the graph through its cypher writer
# (go/internal/storage/cypher/kubernetes_namespace_node_writer.go:90), then
# acks. There is no shared_projection_intents row and no second-stage runner
# lease -- the two blocker kinds the registry schema offers that this family
# cannot use (see rows/12_kubernetes_namespace_environment.sh).
#
# The lock target is therefore the handler's first synchronous READ, not a
# write: ACCESS EXCLUSIVE on fact_records blocks readers, and the claim/ack
# path never touches that table, so the row stays claimed while the handler
# is held. The handler struct embeds FactLoader
# (go/internal/reducer/kubernetes_namespace_materialization.go:129) and Handle
# refuses to run without it (:147) before passing it to the extraction path
# (:157) -- the pre-write window the lock needs, measured for codeowners at
# 14s of visibly-waiting backends and re-proven by this family's own kill cell
# below rather than assumed from the sibling.
#
# ifa_kubernetes_namespace_environment_start_fact_records_lock holds an ACCESS
# EXCLUSIVE lock on fact_records so the kubernetes_namespace_materialization
# handler blocks on its FIRST synchronous read -- the fact load -- while its
# fact_work_items claim is already held. The claim, heartbeat, and ack path
# never reads fact_records, so the row stays claimed and alive for the whole
# hold. fact_records, not fact_work_items: the cell's own wait predicate polls
# fact_work_items, and locking that table would block the poll too.
ifa_kubernetes_namespace_environment_start_fact_records_lock() {
	local cell="$1" pid_var="$2"
	local app_name="ifa_kubernetes_namespace_environment_lock_${cell}"
	local lock_sql="SET application_name = '${app_name}'; BEGIN; LOCK TABLE fact_records IN ACCESS EXCLUSIVE MODE; SELECT pg_sleep(180); ROLLBACK;"
	if [[ "${use_compose}" -eq 1 ]]; then
		docker compose -p "${FAULT_COMPOSE_PROJECT}" -f "${compose_file}" exec -T postgres \
			psql -v ON_ERROR_STOP=1 -U eshu -d eshu -c "${lock_sql}" \
			>"${log_dir}/kubernetes-namespace-environment-lock-${cell}.log" 2>&1 &
	else
		psql "${ESHU_POSTGRES_DSN}" -v ON_ERROR_STOP=1 -c "${lock_sql}" \
			>"${log_dir}/kubernetes-namespace-environment-lock-${cell}.log" 2>&1 &
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
	# blocker instead of a bare timeout. The live
	# killworkerkubernetesnamespaceenvironment run died here with the
	# projector mid-sweep and named nothing.
	printf '%s: fact_records lock blockers at acquisition failure:\n' "${cell}"
	ifa_det_pg "${FAULT_COMPOSE_PROJECT}" "${use_compose}" "${ESHU_POSTGRES_DSN}" \
		"SELECT 'blocker_snapshot' AS snapshot_kind, a.application_name, l.locktype, l.mode, l.granted, a.state, left(a.query, 120) AS query FROM pg_locks l JOIN pg_stat_activity a ON a.pid = l.pid WHERE l.relation = 'fact_records'::regclass ORDER BY 2, 3;" \
		"${compose_file}" 2>/dev/null | sed 's/^/  /' || true
	return 1
}

# ifa_kubernetes_namespace_environment_release_fact_records_lock terminates
# the named lock-holder backend, then joins its local psql/docker process
# before the replacement reducer starts.
ifa_kubernetes_namespace_environment_release_fact_records_lock() {
	local cell="$1" holder_pid="$2"
	local app_name="ifa_kubernetes_namespace_environment_lock_${cell}"
	ifa_det_pg "${FAULT_COMPOSE_PROJECT}" "${use_compose}" "${ESHU_POSTGRES_DSN}" \
		"SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE application_name = '${app_name}';" \
		"${compose_file}" >/dev/null
	wait "${holder_pid}" 2>/dev/null || true
	ifa_det_untrack_bg_pid "${holder_pid}"
}

# cell_baseline_kubernetes_namespace_environment is this family's fault-free
# reference run. It exists because the kubernetes cassette is driven ONLY by
# this family's own cells: ifa_fault_injection_driver.sh's header rules that a
# new family cassette is never added to drive_all_cassettes, since the extra
# succeeded reducer row enlarges ifa_fault_redeliver_succeeded's row set inside
# cell_duplicatedelivery. So the shared `baseline` digest contains no
# TARGETS_ENVIRONMENT edges and every cell of this family would mismatch it.
# These cells therefore compare against
# digests[baseline_kubernetes_namespace_environment] via assert_matches_baseline's
# baseline_key parameter.
#
# It also establishes baseline_kubernetes_namespace_environment_retried: the
# fault-free attempt_count for domain kubernetes_namespace_materialization,
# which the kill cell must exceed to prove the replacement reducer re-executed
# THIS domain rather than merely draining another queued row.
cell_baseline_kubernetes_namespace_environment() {
	local cell_start
	cell_start=$(date +%s)
	log "cell baseline-kubernetes-namespace-environment: fresh stack"
	fresh_stack baseline_kubernetes_namespace_environment
	drive_all_cassettes baseline_kubernetes_namespace_environment
	ifa_kubernetes_namespace_environment_drive "baseline_kubernetes_namespace_environment" "${bin_dir}" "${kubernetes_namespace_environment_cassette}" "${drive_workers}" "${log_dir}" \
		|| die "baseline-kubernetes-namespace-environment: eshu-ifa drive (kubernetes family) failed"
	local projector_pid reducer_pid
	ifa_det_start_bg "${log_dir}" "projector-baseline_kubernetes_namespace_environment" projector_pid "${bin_dir}/eshu-projector"
	ifa_det_start_bg "${log_dir}" "reducer-baseline_kubernetes_namespace_environment" reducer_pid "${bin_dir}/eshu-reducer"
	run_drain_gate baseline_kubernetes_namespace_environment
	assert_no_dead_letters baseline_kubernetes_namespace_environment
	ifa_kubernetes_namespace_environment_assert "baseline_kubernetes_namespace_environment" "${bin_dir}" "${kubernetes_namespace_environment_expected_edges}" \
		|| die "baseline-kubernetes-namespace-environment: fault-free graph does not match the two-edge exact set"
	baseline_kubernetes_namespace_environment_retried="$(ifa_fault_count_retried "${FAULT_COMPOSE_PROJECT}" "${use_compose}" "${ESHU_POSTGRES_DSN}" "${compose_file}" "kubernetes_namespace_materialization")" \
		|| die "baseline-kubernetes-namespace-environment: could not count the fault-free kubernetes_namespace_materialization retry baseline"
	printf 'baseline-kubernetes-namespace-environment: fault-free kubernetes_namespace_materialization retry baseline: %s\n' "${baseline_kubernetes_namespace_environment_retried}"
	capture_digest baseline_kubernetes_namespace_environment
	teardown_cell baseline_kubernetes_namespace_environment
	wall_times[baseline_kubernetes_namespace_environment]=$(( $(date +%s) - cell_start ))
	printf 'baseline-kubernetes-namespace-environment: cell wall time: %ss\n' "${wall_times[baseline_kubernetes_namespace_environment]}"
}

cell_killworker_kubernetes_namespace_environment() {
	local cell_start
	cell_start=$(date +%s)
	log "cell kill-worker-after-claim-kubernetes-namespace-environment: fresh stack"
	fresh_stack killworkerkubernetesnamespaceenvironment
	drive_all_cassettes killworkerkubernetesnamespaceenvironment
	ifa_kubernetes_namespace_environment_drive "killworkerkubernetesnamespaceenvironment" "${bin_dir}" "${kubernetes_namespace_environment_cassette}" "${drive_workers}" "${log_dir}" \
		|| die "kill-worker-after-claim-kubernetes-namespace-environment: eshu-ifa drive (kubernetes family) failed"
	local projector_pid reducer_pid_before reducer_pid_after lock_holder_pid claimed_before
	ifa_det_start_bg "${log_dir}" "projector-killworkerkubernetesnamespaceenvironment" projector_pid "${bin_dir}/eshu-projector"
	# kubernetes_namespace_materialization's reducer intent is created by the
	# PROJECTOR, not ingestion. Wait for the row to exist (any status) before
	# taking the lock: locking fact_records before that intent exists risks
	# starving the projector's OWN fact reads for this scope too, so the row
	# could never be created for as long as the lock is held -- not merely
	# delayed.
	ifa_fault_wait_for_claimed "${FAULT_COMPOSE_PROJECT}" "${use_compose}" "${ESHU_POSTGRES_DSN}" "${compose_file}" \
		"${CLAIMED_ROW_WAIT_TIMEOUT}" "kubernetes_namespace_materialization" 1 >/dev/null \
		|| die "kill-worker-after-claim-kubernetes-namespace-environment: kubernetes_namespace_materialization was never enqueued by the projector"
	ifa_kubernetes_namespace_environment_start_fact_records_lock "killworkerkubernetesnamespaceenvironment" lock_holder_pid \
		|| die "kill-worker-after-claim-kubernetes-namespace-environment: could not acquire the deterministic fact_records read blocker"
	# Scoped to kubernetes_namespace_materialization only: with the lock already
	# held, an unscoped reducer's workers can all grab OTHER pending domains
	# that also read fact_records and get stuck there for the lock's full
	# duration, starving this domain of a worker even though its own row
	# already exists. ESHU_REDUCER_CLAIM_DOMAIN filters the claim SQL itself
	# (domain = ANY(...)), so this instance never attempts another domain's row.
	ifa_det_start_bg "${log_dir}" "reducer-killworkerkubernetesnamespaceenvironment-before" reducer_pid_before \
		env "ESHU_REDUCER_CLAIM_DOMAIN=kubernetes_namespace_materialization" "${bin_dir}/eshu-reducer"
	claimed_before="$(ifa_fault_wait_for_claimed "${FAULT_COMPOSE_PROJECT}" "${use_compose}" "${ESHU_POSTGRES_DSN}" "${compose_file}" "${CLAIMED_ROW_WAIT_TIMEOUT}" "kubernetes_namespace_materialization")" \
		|| die "kill-worker-after-claim-kubernetes-namespace-environment: no kubernetes_namespace_materialization row was claimed while its fact load was blocked"
	printf 'kill-worker-after-claim-kubernetes-namespace-environment: non-vacuous: %s blocked claimed/running row(s) observed\n' "${claimed_before}"
	kill -9 "${reducer_pid_before}" >/dev/null 2>&1 || true
	ifa_kubernetes_namespace_environment_release_fact_records_lock "killworkerkubernetesnamespaceenvironment" "${lock_holder_pid}"
	ifa_det_start_bg "${log_dir}" "reducer-killworkerkubernetesnamespaceenvironment-after" reducer_pid_after "${bin_dir}/eshu-reducer"
	run_drain_gate killworkerkubernetesnamespaceenvironment
	assert_no_dead_letters killworkerkubernetesnamespaceenvironment
	ifa_kubernetes_namespace_environment_assert "killworkerkubernetesnamespaceenvironment" "${bin_dir}" "${kubernetes_namespace_environment_expected_edges}" \
		|| die "kill-worker-after-claim-kubernetes-namespace-environment: recovered graph does not match the two-edge exact set"
	ifa_fault_assert_retried_above "${FAULT_COMPOSE_PROJECT}" "${use_compose}" "${ESHU_POSTGRES_DSN}" "${compose_file}" \
		"${baseline_kubernetes_namespace_environment_retried}" 15 "kubernetes_namespace_materialization" \
		|| die "kill-worker-after-claim-kubernetes-namespace-environment: kubernetes_namespace_materialization did not re-execute above its fault-free retry baseline"
	capture_digest killworkerkubernetesnamespaceenvironment
	assert_matches_baseline killworkerkubernetesnamespaceenvironment baseline_kubernetes_namespace_environment
	teardown_cell killworkerkubernetesnamespaceenvironment
	wall_times[killworkerkubernetesnamespaceenvironment]=$(( $(date +%s) - cell_start ))
	printf 'kill-worker-after-claim-kubernetes-namespace-environment: cell wall time: %ss\n' "${wall_times[killworkerkubernetesnamespaceenvironment]}"
}

# cell_failgraphwrite_kubernetes_namespace_environment fails the live
# TARGETS_ENVIRONMENT MERGE exactly once, proves the fault decorator's durable
# marker names that write, and requires the full two-edge set to converge
# without dead letters. Anchor read off the writer's executed statement text
# (go/internal/storage/cypher/kubernetes_namespace_node_writer.go:90):
# MERGE (n)-[env_rel:TARGETS_ENVIRONMENT]->(env).
cell_failgraphwrite_kubernetes_namespace_environment() {
	local cell_start
	cell_start=$(date +%s)
	log "cell fail-graph-write-once-then-succeed-kubernetes-namespace-environment: fresh stack"
	fresh_stack failgraphwritekubernetesnamespaceenvironment
	drive_all_cassettes failgraphwritekubernetesnamespaceenvironment
	ifa_kubernetes_namespace_environment_drive "failgraphwritekubernetesnamespaceenvironment" "${bin_dir}" "${kubernetes_namespace_environment_cassette}" "${drive_workers}" "${log_dir}" \
		|| die "fail-graph-write-once-then-succeed-kubernetes-namespace-environment: eshu-ifa drive (kubernetes family) failed"
	local fault_once_script projector_pid reducer_pid marker_rc
	fault_once_script="${work_dir}/fault-once-then-succeed-kubernetes-namespace-environment.json"
	ifa_fault_write_once_script "${fault_once_script}" "${kubernetes_namespace_environment_edge_operation_match}" "queue-retry"
	ifa_det_start_bg "${log_dir}" "projector-failgraphwritekubernetesnamespaceenvironment" projector_pid "${bin_dir}/eshu-projector"
	ifa_det_start_bg "${log_dir}" "reducer-failgraphwritekubernetesnamespaceenvironment" reducer_pid \
		env "ESHU_IFA_FAULT_SCRIPT=${fault_once_script}" "${tagged_bin_dir}/eshu-reducer"
	run_drain_gate failgraphwritekubernetesnamespaceenvironment
	assert_no_dead_letters failgraphwritekubernetesnamespaceenvironment
	ifa_kubernetes_namespace_environment_assert "failgraphwritekubernetesnamespaceenvironment" "${bin_dir}" "${kubernetes_namespace_environment_expected_edges}" \
		|| die "fail-graph-write-once-then-succeed-kubernetes-namespace-environment: recovered graph does not match the two-edge exact set"
	ifa_det_pg "${FAULT_COMPOSE_PROJECT}" "${use_compose}" "${ESHU_POSTGRES_DSN}" \
		"SELECT count(*) AS total, count(*) FILTER (WHERE status = 'succeeded') AS succeeded FROM fact_work_items WHERE stage = 'reducer' AND domain = 'kubernetes_namespace_materialization';" \
		"${compose_file}" | sed 's/^/  kubernetes_namespace_materialization work-item window: /'
	marker_rc=0
	ifa_fault_assert_once_fault_marker "${fault_once_script}" "${kubernetes_namespace_environment_edge_operation_match}" || marker_rc=$?
	if [[ "${marker_rc}" -eq 2 ]]; then
		# Distinguished from "no marker" deliberately, per #5974: rc=2 means the
		# injection WORKS and fired, but on a different write than the anchor
		# names. Collapsing that into a generic failure sends the next reader
		# looking at the decorator instead of at the anchor.
		die "fail-graph-write-once-then-succeed-kubernetes-namespace-environment: the fault FIRED but on a different write than ${kubernetes_namespace_environment_edge_operation_match} (marker contents above). The injection works; the anchor is pointed at the wrong statement."
	fi
	[[ "${marker_rc}" -eq 0 ]] \
		|| die "fail-graph-write-once-then-succeed-kubernetes-namespace-environment: no once-fired marker at all -- the fault never fired, so this cell proved nothing about TARGETS_ENVIRONMENT recovery (marker status ${marker_rc})"
	# Announced, not merely checked. Every sibling cell prints its own
	# non-vacuity line and this one did not, so a passing run said nothing about
	# whether the fault had actually fired -- the check worked while being
	# invisible, which is the shape this family's whole epic is about.
	printf 'fail-graph-write-once-then-succeed-kubernetes-namespace-environment: non-vacuous: once-fired marker names the targeted TARGETS_ENVIRONMENT MERGE (written by the fault decorator at injection time, not read from the reducer log)\n'
	capture_digest failgraphwritekubernetesnamespaceenvironment
	assert_matches_baseline failgraphwritekubernetesnamespaceenvironment baseline_kubernetes_namespace_environment
	teardown_cell failgraphwritekubernetesnamespaceenvironment
	wall_times[failgraphwritekubernetesnamespaceenvironment]=$(( $(date +%s) - cell_start ))
	printf 'fail-graph-write-once-then-succeed-kubernetes-namespace-environment: cell wall time: %ss\n' "${wall_times[failgraphwritekubernetesnamespaceenvironment]}"
}
