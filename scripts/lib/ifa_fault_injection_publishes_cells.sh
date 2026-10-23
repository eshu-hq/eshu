#!/usr/bin/env bash
# shellcheck disable=SC2034,SC2154
# publishes-targeted live fault cells (#6228
# publishes live leaf), mirroring
# scripts/lib/ifa_fault_injection_code_taint_evidence_cells.sh's shape
# including the kill cell: this domain's claim is NOT readiness-gated
# (package_source_correlation carries no row in the reducer-claim readiness
# requirements), AND its in-handler path carries no readiness gate before
# the fact load either (Handle goes from the nil guards straight to
# LoadFactsForKinds; canonical projection commits the endpoint
# Repository/Package/PackageVersion nodes from the same generation before
# the intent enqueues). So the kill cell takes the lock as soon as
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
# (baseline_publishes,
# killworker_publishes,
# failgraphwrite_publishes) against a live Postgres +
# NornicDB stack. The cells rely on the driver's strict mode and globals
# (bin_dir, tagged_bin_dir, log_dir, work_dir, use_compose, compose_file,
# FAULT_COMPOSE_PROJECT, ESHU_POSTGRES_DSN, CLAIMED_ROW_WAIT_TIMEOUT,
# drive_workers, wall_times, bg_pids, log, die, plus the fresh_stack /
# drive_all_cassettes / run_drain_gate / assert_no_dead_letters /
# capture_digest / assert_matches_baseline / teardown_cell helpers from
# ifa_fault_injection_driver.sh). Drive and assert callbacks come from
# scripts/lib/ifa_direct_family_live_publishes.sh
# (ifa_publishes_drive /
# ifa_publishes_assert); the cassette and expected-edge
# variables come from scripts/lib/ifa_family_fixtures.sh.
#
# scripts/lib/test-ifa-fault-injection-publishes-cases.sh
# sources this file and drives
# ifa_publishes_start_fact_records_lock and
# ifa_publishes_release_fact_records_lock under stubs, and
# scripts/test-verify-ifa-fault-injection.sh runs that module -- so editing
# either function's fail-closed behavior or its bg_pids bookkeeping will
# fail `make pre-pr`.
#
# publishes spans ONE scope (the fixture package-registry scope), with one
# work item under domain "package_source_correlation". The handler
# (PackageSourceHandler) claims a fact_work_items row under that domain, loads
# its facts via factload (LoadFactsForKinds -> FROM
# fact_records, own scope, plus the cross-scope active-repository fallback
# over the same table) and projects the PUBLISHES edges through its
# provenance writer, then acks. The fixture carries no manifest dependency
# facts, so the consumption-to-owner join yields no repo-dependency
# intents: there is no shared_projection_intents row and no second-stage
# runner lease -- the two blocker kinds the registry schema offers that
# this family cannot use.
#
# The lock target is therefore the handler's first synchronous READ, not a
# write: ACCESS EXCLUSIVE on fact_records blocks readers, and the claim/ack
# path never touches that table, so the row stays claimed while the handler
# is held. fact_records, not fact_work_items: the cell's own wait predicate
# polls fact_work_items, and locking that table would block the poll too.
ifa_publishes_start_fact_records_lock() {
	local cell="$1" pid_var="$2"
	# Short prefix deliberately: the repo holds application_name to 63 bytes
	# (ifa_fault_generic_runner_wait.sh, ifa_fault_injection_documentation_ack_setup.sh),
	# and a full family-name prefix plus a long kill cell name would be
	# silently truncated, so the grant poll below (which matches
	# the full name) would miss forever while the holder sat on its granted
	# lock. Proven live by the sibling kubernetes family's identical failure.
	# ifa_pub_lock_ keeps the longest lock name this family uses at 32 bytes.
	local app_name="ifa_pub_lock_${cell}"
	if ((${#app_name} > 63)); then
		printf '%s: lock application_name is %s bytes; past the 63-byte repo cap the name truncates and the grant poll would never match\n' "${cell}" "${#app_name}" >&2
		return 1
	fi
	local lock_sql="SET application_name = '${app_name}'; BEGIN; LOCK TABLE fact_records IN ACCESS EXCLUSIVE MODE; SELECT pg_sleep(180); ROLLBACK;"
	if [[ "${use_compose}" -eq 1 ]]; then
		bash scripts/ci/compose.sh -p "${FAULT_COMPOSE_PROJECT}" -f "${compose_file}" -- exec -T postgres \
			psql -v ON_ERROR_STOP=1 -U eshu -d eshu -c "${lock_sql}" \
			>"${log_dir}/publishes-lock-${cell}.log" 2>&1 &
	else
		psql "${ESHU_POSTGRES_DSN}" -v ON_ERROR_STOP=1 -c "${lock_sql}" \
			>"${log_dir}/publishes-lock-${cell}.log" 2>&1 &
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

# ifa_publishes_release_fact_records_lock terminates the
# named lock-holder backend, then joins its local psql/docker process
# before the replacement reducer starts.
ifa_publishes_release_fact_records_lock() {
	local cell="$1" holder_pid="$2"
	# Same short name as the start function: it must terminate the backend
	# the start function actually created (see its 63-byte-cap comment).
	local app_name="ifa_pub_lock_${cell}"
	ifa_det_pg "${FAULT_COMPOSE_PROJECT}" "${use_compose}" "${ESHU_POSTGRES_DSN}" \
		"SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE application_name = '${app_name}';" \
		"${compose_file}" >/dev/null
	wait "${holder_pid}" 2>/dev/null || true
	ifa_det_untrack_bg_pid "${holder_pid}"
}

# cell_baseline_publishes is this family's fault-free
# reference run. It exists because the publishes cassette is
# driven ONLY by this family's own cells:
# ifa_fault_injection_driver.sh's header rules that a new family cassette is
# never added to drive_all_cassettes, since the extra succeeded reducer row
# enlarges ifa_fault_redeliver_succeeded's row set inside
# cell_duplicatedelivery. So the shared `baseline` digest contains no
# PUBLISHES edges and every cell of this family would mismatch it. These
# cells therefore compare against
# digests[baseline_publishes] via assert_matches_baseline's
# baseline_key parameter.
#
# It also establishes baseline_publishes_retried: the
# fault-free excess-ATTEMPT total for domain
# package_source_correlation, which the kill cell must exceed to
# prove the replacement reducer re-executed THIS domain rather than merely
# draining another queued row. Attempts, not rows: this cassette creates one
# work item (single scope, one row), and the row-count form saturates at 1
# per row for a single natural retry (see ifa_fault_count_retry_attempts).
cell_baseline_publishes() {
	local cell_start
	cell_start=$(date +%s)
	log "cell baseline-publishes: fresh stack"
	fresh_stack baselinepublishes
	drive_all_cassettes baselinepublishes
	ifa_publishes_drive "baselinepublishes" "${bin_dir}" "${publishes_cassette}" "${drive_workers}" "${log_dir}" \
		|| die "baseline-publishes: eshu-ifa drive (publishes family) failed"
	local projector_pid reducer_pid
	ifa_det_start_bg "${log_dir}" "projector-baselinepublishes" projector_pid "${bin_dir}/eshu-projector"
	ifa_det_start_bg "${log_dir}" "reducer-baselinepublishes" reducer_pid "${bin_dir}/eshu-reducer"
	run_drain_gate baselinepublishes
	assert_no_dead_letters baselinepublishes
	ifa_publishes_assert "baselinepublishes" "${bin_dir}" "${publishes_expected_edges}" \
		|| die "baseline-publishes: fault-free graph does not match the three-edge exact set"
	baseline_publishes_retried="$(ifa_fault_count_retry_attempts "${FAULT_COMPOSE_PROJECT}" "${use_compose}" "${ESHU_POSTGRES_DSN}" "${compose_file}" "package_source_correlation")" \
		|| die "baseline-publishes: could not count the fault-free package_source_correlation retry-attempt baseline"
	printf 'baseline-publishes: fault-free package_source_correlation retry-attempt baseline: %s\n' "${baseline_publishes_retried}"
	# Captured under the BASELINE KEY, not the cell label: the kill and
	# fail-graph-write cells assert against
	# digests[baseline_publishes]. Capturing under the label
	# leaves that key unset and the match dies on an unbound variable
	# under the gate's `set -u` (found by the crc-image lane's targeted
	# live proof, which got past the attempts assert the old lock-first
	# shape never reached).
	capture_digest baseline_publishes
	teardown_cell baselinepublishes
	wall_times[baselinepublishes]=$(( $(date +%s) - cell_start ))
	printf 'baseline-publishes: cell wall time: %ss\n' "${wall_times[baselinepublishes]}"
}

cell_killworker_publishes() {
	local cell_start
	cell_start=$(date +%s)
	log "cell kill-worker-after-claim-publishes: fresh stack"
	fresh_stack killworkerpublishes
	drive_all_cassettes killworkerpublishes
	ifa_publishes_drive "killworkerpublishes" "${bin_dir}" "${publishes_cassette}" "${drive_workers}" "${log_dir}" \
		|| die "kill-worker-after-claim-publishes: eshu-ifa drive (publishes family) failed"
	local projector_pid reducer_pid_before reducer_pid_after lock_holder_pid claimed_before
	ifa_det_start_bg "${log_dir}" "projector-killworkerpublishes" projector_pid "${bin_dir}/eshu-projector"
	# publishes' reducer intent is created by the
	# PROJECTOR, not ingestion. Wait for the row to exist (any status)
	# before going further: without it there is nothing to kill, and locking
	# fact_records before that intent exists risks starving the projector's
	# OWN fact reads for this scope too, so the row could never be created
	# for as long as the lock is held -- not merely delayed.
	ifa_fault_wait_for_claimed "${FAULT_COMPOSE_PROJECT}" "${use_compose}" "${ESHU_POSTGRES_DSN}" "${compose_file}" \
		"${CLAIMED_ROW_WAIT_TIMEOUT}" "package_source_correlation" 1 >/dev/null \
		|| die "kill-worker-after-claim-publishes: package_source_correlation was never enqueued by the projector"
	# No claim-time readiness gate on this domain and no in-handler gate
	# before the fact load, so the claim fires as soon as the row is pending
	# and the handler blocks deterministically in its load: the lock goes on
	# now. A prereq-scoped reducer would add a wait nothing here needs (the
	# crc-image shape does not transfer: ITS handler gates on readiness
	# before its load).
	ifa_publishes_start_fact_records_lock "killworkerpublishes" lock_holder_pid \
		|| die "kill-worker-after-claim-publishes: could not acquire the deterministic fact_records read blocker"
	# Scoped to package_source_correlation only: with the lock
	# already held, an unscoped reducer's workers can all grab OTHER pending
	# domains that also read fact_records and get stuck there for the lock's
	# full duration, starving this domain of a worker even though its own row
	# already exists. ESHU_REDUCER_CLAIM_DOMAIN filters the claim SQL itself
	# (domain = ANY(...)), so this instance never attempts another domain's
	# row. The claim fires without waiting on any phase: this domain carries
	# no readiness requirement, so the lock starves nothing the claim still
	# needs.
	ifa_det_start_bg "${log_dir}" "reducer-killworkerpublishes-before" reducer_pid_before \
		env "ESHU_REDUCER_CLAIM_DOMAIN=package_source_correlation" "${bin_dir}/eshu-reducer"
	claimed_before="$(ifa_fault_wait_for_claimed "${FAULT_COMPOSE_PROJECT}" "${use_compose}" "${ESHU_POSTGRES_DSN}" "${compose_file}" "${CLAIMED_ROW_WAIT_TIMEOUT}" "package_source_correlation")" \
		|| die "kill-worker-after-claim-publishes: no package_source_correlation row was claimed while its fact load was blocked"
	printf 'kill-worker-after-claim-publishes: non-vacuous: %s blocked claimed/running row(s) observed\n' "${claimed_before}"
	kill -9 "${reducer_pid_before}" >/dev/null 2>&1 || true
	ifa_publishes_release_fact_records_lock "killworkerpublishes" "${lock_holder_pid}"
	ifa_det_start_bg "${log_dir}" "reducer-killworkerpublishes-after" reducer_pid_after "${bin_dir}/eshu-reducer"
	run_drain_gate killworkerpublishes
	assert_no_dead_letters killworkerpublishes
	ifa_publishes_assert "killworkerpublishes" "${bin_dir}" "${publishes_expected_edges}" \
		|| die "kill-worker-after-claim-publishes: recovered graph does not match the three-edge exact set"
	ifa_fault_assert_retry_attempts_above "${FAULT_COMPOSE_PROJECT}" "${use_compose}" "${ESHU_POSTGRES_DSN}" "${compose_file}" \
		"${baseline_publishes_retried}" 15 "package_source_correlation" \
		|| die "kill-worker-after-claim-publishes: package_source_correlation did not re-execute above its fault-free retry-attempt baseline"
	capture_digest killworkerpublishes
	assert_matches_baseline killworkerpublishes baseline_publishes
	teardown_cell killworkerpublishes
	wall_times[killworkerpublishes]=$(( $(date +%s) - cell_start ))
	printf 'kill-worker-after-claim-publishes: cell wall time: %ss\n' "${wall_times[killworkerpublishes]}"
}

# cell_failgraphwrite_publishes fails the live PUBLISHES
# MERGE exactly once, proves the fault decorator's durable marker names that
# write, and requires the full three-edge set to converge without dead
# letters. Anchor read off EXECUTED statement text, not source text: the
# writer's templates read
# MERGE (repo)-[rel:PUBLISHES {
# (go/internal/storage/cypher/edge/writer/provenance.go:140,154, Package
# and PackageVersion twins) with the scope_id/evidence_source identity map
# on the MERGE itself, so the anchor carries the PREFIX form the pair
# shares. A full `]->(target)` spelling would match no executed statement
# (the opposite of the code_taint_evidence FULL rationale, whose template
# carries no property map), and a parameterized form would match the
# retract DELETEs instead -- either way the fault would never fire on the
# upsert alone, giving a green cell that tested nothing.
cell_failgraphwrite_publishes() {
	local cell_start
	cell_start=$(date +%s)
	log "cell fail-graph-write-once-then-succeed-publishes: fresh stack"
	fresh_stack failgraphwritepublishes
	drive_all_cassettes failgraphwritepublishes
	ifa_publishes_drive "failgraphwritepublishes" "${bin_dir}" "${publishes_cassette}" "${drive_workers}" "${log_dir}" \
		|| die "fail-graph-write-once-then-succeed-publishes: eshu-ifa drive (publishes family) failed"
	local fault_once_script projector_pid reducer_pid marker_rc
	fault_once_script="${work_dir}/fault-once-then-succeed-publishes.json"
	ifa_fault_write_once_script "${fault_once_script}" "${publishes_edge_operation_match}" "queue-retry"
	ifa_det_start_bg "${log_dir}" "projector-failgraphwritepublishes" projector_pid "${bin_dir}/eshu-projector"
	ifa_det_start_bg "${log_dir}" "reducer-failgraphwritepublishes" reducer_pid \
		env "ESHU_IFA_FAULT_SCRIPT=${fault_once_script}" "${tagged_bin_dir}/eshu-reducer"
	run_drain_gate failgraphwritepublishes
	assert_no_dead_letters failgraphwritepublishes
	ifa_publishes_assert "failgraphwritepublishes" "${bin_dir}" "${publishes_expected_edges}" \
		|| die "fail-graph-write-once-then-succeed-publishes: recovered graph does not match the three-edge exact set"
	ifa_det_pg "${FAULT_COMPOSE_PROJECT}" "${use_compose}" "${ESHU_POSTGRES_DSN}" \
		"SELECT count(*) AS total, count(*) FILTER (WHERE status = 'succeeded') AS succeeded FROM fact_work_items WHERE stage = 'reducer' AND domain = 'package_source_correlation';" \
		"${compose_file}" | sed 's/^/  package_source_correlation work-item window: /'
	marker_rc=0
	ifa_fault_assert_once_fault_marker "${fault_once_script}" "${publishes_edge_operation_match}" || marker_rc=$?
	if [[ "${marker_rc}" -eq 2 ]]; then
		# Distinguished from "no marker" deliberately, per #5974: rc=2 means the
		# injection WORKS and fired, but on a different write than the anchor
		# names. Collapsing that into a generic failure sends the next reader
		# looking at the decorator instead of at the anchor.
		die "fail-graph-write-once-then-succeed-publishes: the fault FIRED but on a different write than ${publishes_edge_operation_match} (marker contents above). The injection works; the anchor is pointed at the wrong statement."
	fi
	[[ "${marker_rc}" -eq 0 ]] \
		|| die "fail-graph-write-once-then-succeed-publishes: no once-fired marker at all -- the fault never fired, so this cell proved nothing about PUBLISHES recovery (marker status ${marker_rc})"
	# Announced, not merely checked. Every sibling cell prints its own
	# non-vacuity line and this one did not, so a passing run would say
	# nothing about whether the fault had actually fired -- the check worked
	# while being invisible, which is the shape this family's whole epic is
	# about.
	printf 'fail-graph-write-once-then-succeed-publishes: non-vacuous: once-fired marker names the targeted PUBLISHES MERGE (written by the fault decorator at injection time, not read from the reducer log)\n'
	capture_digest failgraphwritepublishes
	assert_matches_baseline failgraphwritepublishes baseline_publishes
	teardown_cell failgraphwritepublishes
	wall_times[failgraphwritepublishes]=$(( $(date +%s) - cell_start ))
	printf 'fail-graph-write-once-then-succeed-publishes: cell wall time: %ss\n' "${wall_times[failgraphwritepublishes]}"
}
