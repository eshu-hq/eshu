#!/usr/bin/env bash
# shellcheck disable=SC2034,SC2154
# ec2_uses_profile-targeted live fault cells (#6228 fault half),
# mirroring scripts/lib/ifa_fault_injection_iam_instance_profile_role_cells.sh's
# shape including the kill cell: this domain's claim is readiness-gated (see
# the readiness helper below), so the kill cell banks readiness through
# prereq-scoped reducers before the lock goes on.
# WIRED: scripts/verify-ifa-fault-injection.sh sources this file (through
# scripts/lib/ifa_fault_injection_sources.sh) and runs all three cells
# (baseline_ec2_uses_profile,
# killworker_ec2_uses_profile,
# failgraphwrite_ec2_uses_profile) against a live Postgres +
# NornicDB stack. The cells rely on the driver's strict mode and globals
# (bin_dir, tagged_bin_dir, log_dir, work_dir, use_compose, compose_file,
# FAULT_COMPOSE_PROJECT, ESHU_POSTGRES_DSN, CLAIMED_ROW_WAIT_TIMEOUT,
# drive_workers, wall_times, bg_pids, log, die, plus the fresh_stack /
# drive_all_cassettes / run_drain_gate / assert_no_dead_letters /
# capture_digest / assert_matches_baseline / teardown_cell helpers from
# ifa_fault_injection_driver.sh). Drive and assert callbacks come from
# scripts/lib/ifa_direct_family_live.sh
# (ifa_ec2_uses_profile_drive /
# ifa_ec2_uses_profile_assert); the cassette and expected-edge
# variables come from scripts/lib/ifa_family_fixtures.sh.
#
# scripts/lib/test-ifa-fault-injection-ec2-uses-profile-cases.sh
# sources this file and drives
# ifa_ec2_uses_profile_start_fact_records_lock and
# ifa_ec2_uses_profile_release_fact_records_lock under stubs,
# and scripts/test-verify-ifa-fault-injection.sh runs that module -- so editing
# either function's fail-closed behavior or its bg_pids bookkeeping will fail
# `make pre-pr`.
#
# ec2_uses_profile is SINGLE-STAGE and DIRECT. The handler
# (EC2UsesProfileMaterializationHandler) claims a fact_work_items row
# under domain "ec2_uses_profile_materialization", loads its facts via
# factload.LoadFactsForKinds -> FROM fact_records, and writes the
# USES_PROFILE edges DIRECTLY to the graph through its cypher writer
# (go/internal/storage/cypher/cloud/aws/ec2/uses_profile_edge_writer.go:47), then
# acks. There is no shared_projection_intents row and no second-stage runner
# lease -- the two blocker kinds the registry schema offers that this family
# cannot use (see rows/18_ec2_uses_profile.sh).
#
# The lock target is therefore the handler's first synchronous READ, not a
# write: ACCESS EXCLUSIVE on fact_records blocks readers, and the claim/ack
# path never touches that table, so the row stays claimed while the handler
# is held. The handler struct embeds FactLoader
# (go/internal/reducer/ec2usesprofile/ec2_uses_profile_materialization.go:115) and Handle
# refuses to run without it (:149) before passing it to the extraction path
# (:182) -- the pre-write window the lock needs, measured for codeowners at
# 14s of visibly-waiting backends and re-proven by this family's own kill cell
# below rather than assumed from the sibling.
#
# ifa_ec2_uses_profile_start_fact_records_lock holds an ACCESS
# EXCLUSIVE lock on fact_records so the ec2_uses_profile_materialization
# handler blocks on its FIRST synchronous read -- the fact load -- while its
# fact_work_items claim is already held. The claim, heartbeat, and ack path
# never reads fact_records, so the row stays claimed and alive for the whole
# hold. fact_records, not fact_work_items: the cell's own wait predicate polls
# fact_work_items, and locking that table would block the poll too.
ifa_ec2_uses_profile_start_fact_records_lock() {
	local cell="$1" pid_var="$2"
	# Short prefix deliberately: the repo holds application_name to 63 bytes
	# (ifa_fault_generic_runner_wait.sh, ifa_fault_injection_documentation_ack_setup.sh),
	# and a full family-name prefix plus a long kill cell name would be
	# silently truncated, so the grant poll below (which matches
	# the full name) would miss forever while the holder sat on its granted
	# lock. Proven live by the sibling kubernetes family's identical failure.
	# ifa_ec2_uses_profile_lock_ keeps the longest name this family uses at 49 bytes.
	local app_name="ifa_ec2_uses_profile_lock_${cell}"
	if ((${#app_name} > 63)); then
		printf '%s: lock application_name is %s bytes; past the 63-byte repo cap the name truncates and the grant poll would never match\n' "${cell}" "${#app_name}" >&2
		return 1
	fi
	local lock_sql="SET application_name = '${app_name}'; BEGIN; LOCK TABLE fact_records IN ACCESS EXCLUSIVE MODE; SELECT pg_sleep(180); ROLLBACK;"
	if [[ "${use_compose}" -eq 1 ]]; then
		bash scripts/ci/compose.sh -p "${FAULT_COMPOSE_PROJECT}" -f "${compose_file}" -- exec -T postgres \
			psql -v ON_ERROR_STOP=1 -U eshu -d eshu -c "${lock_sql}" \
			>"${log_dir}/ec2-uses-profile-lock-${cell}.log" 2>&1 &
	else
		psql "${ESHU_POSTGRES_DSN}" -v ON_ERROR_STOP=1 -c "${lock_sql}" \
			>"${log_dir}/ec2-uses-profile-lock-${cell}.log" 2>&1 &
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

# ifa_ec2_uses_profile_release_fact_records_lock terminates
# the named lock-holder backend, then joins its local psql/docker process
# before the replacement reducer starts.
ifa_ec2_uses_profile_release_fact_records_lock() {
	local cell="$1" holder_pid="$2"
	# Same name as the start function: it must terminate the backend
	# the start function actually created (see its 63-byte-cap comment).
	local app_name="ifa_ec2_uses_profile_lock_${cell}"
	ifa_det_pg "${FAULT_COMPOSE_PROJECT}" "${use_compose}" "${ESHU_POSTGRES_DSN}" \
		"SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE application_name = '${app_name}';" \
		"${compose_file}" >/dev/null
	wait "${holder_pid}" 2>/dev/null || true
	ifa_det_untrack_bg_pid "${holder_pid}"
}

# ifa_ec2_uses_profile_wait_for_readiness banks this family's claim
# readiness BEFORE the fact_records lock is taken. Unlike the sibling
# kubernetes direct family, this domain's claim is gated
# (go/internal/storage/postgres/reducer_queue_readiness_sql.go:
# ec2_uses_profile_materialization requires TWO
# canonical_nodes_committed/cloud_resource_uid phases, one per endpoint node
# family), and those phases are published by the ec2_instance_node and
# aws_resource reducers during Handle -- both of which read fact_records.
# Locking first therefore deadlocks the cell by design: the scoped reducer
# idles against a still-pending row forever (proven live on the sibling
# iam_instance_profile_role domain: 120s, zero claims, row still pending at
# death). So the cell runs TWO prereq-scoped reducers first (one per endpoint
# node domain, so the ec2 row can never be claimed early) and this helper
# waits until BOTH acceptance units the gate will ask for are banked for the
# ec2 row's own (scope, generation) identity. The gate derives each unit as
# scope_prefix || scope_id (reducerClaimReadinessAcceptanceUnitSQL), so the
# helper polls for exactly 'ec2_instance_node_materialization:<scope>' and
# 'aws_resource_materialization:<scope>'. One banked phase is still
# unclaimable, so it keeps polling. Fail-closed throughout: no identity row,
# a failed query, a malformed value, or budget exhaustion all return
# non-zero, and a non-numeric phase count is a failure, never a zero.
ifa_ec2_uses_profile_wait_for_readiness() {
	local cell="$1" budget="$2"
	if [[ ! "${budget}" =~ ^[1-9][0-9]*$ ]]; then
		printf '%s: readiness wait: budget must be a positive integer, got %s\n' "${cell}" "${budget}" >&2
		return 1
	fi
	local identity scope generation
	if ! identity="$(ifa_det_pg "${FAULT_COMPOSE_PROJECT}" "${use_compose}" "${ESHU_POSTGRES_DSN}" \
		"SELECT scope_id || '|' || generation_id FROM fact_work_items WHERE stage = 'reducer' AND domain = 'ec2_uses_profile_materialization' ORDER BY created_at ASC, work_item_id ASC LIMIT 1;" \
		"${compose_file}")"; then
		printf '%s: readiness wait: ec2 identity query failed\n' "${cell}" >&2
		return 1
	fi
	identity="$(printf '%s' "${identity}" | tr -d '[:space:]')"
	IFS='|' read -r scope generation <<<"${identity}"
	if [[ -z "${scope}" || -z "${generation}" ]]; then
		printf '%s: readiness wait: no ec2 row to resolve an identity from (got %q)\n' "${cell}" "${identity}" >&2
		return 1
	fi
	# Values come from our own queue rows, but they ride inside string
	# literals one level down: double any quote rather than trusting shape.
	scope="${scope//\'/\'\'}"
	generation="${generation//\'/\'\'}"
	local i phase_count
	for i in $(seq 1 "${budget}"); do
		if ! phase_count="$(ifa_det_pg "${FAULT_COMPOSE_PROJECT}" "${use_compose}" "${ESHU_POSTGRES_DSN}" \
			"SELECT count(*) FROM graph_projection_phase_state WHERE scope_id = '${scope}' AND acceptance_unit_id IN ('ec2_instance_node_materialization:${scope}', 'aws_resource_materialization:${scope}') AND source_run_id = '${generation}' AND generation_id = '${generation}' AND keyspace = 'cloud_resource_uid' AND phase = 'canonical_nodes_committed';" \
			"${compose_file}")"; then
			printf '%s: readiness wait: phase poll query failed\n' "${cell}" >&2
			return 1
		fi
		phase_count="$(printf '%s' "${phase_count}" | tr -d '[:space:]')"
		if [[ "${phase_count}" == "2" ]]; then
			printf '%s: readiness banked: both canonical_nodes_committed/cloud_resource_uid endpoint phases for scope %s\n' "${cell}" "${scope}"
			return 0
		fi
		if [[ "${phase_count}" != "0" && "${phase_count}" != "1" ]]; then
			printf '%s: readiness wait: phase poll returned non-numeric output %q\n' "${cell}" "${phase_count}" >&2
			return 1
		fi
		sleep 1
	done
	printf '%s: readiness wait: both canonical_nodes_committed/cloud_resource_uid endpoint phases for scope %s never banked within %ss\n' "${cell}" "${scope}" "${budget}" >&2
	return 1
}

# cell_baseline_ec2_uses_profile is this family's fault-free
# reference run. It exists because the ec2 cassette is driven ONLY by
# this family's own cells: ifa_fault_injection_driver.sh's header rules that a
# new family cassette is never added to drive_all_cassettes, since the extra
# succeeded reducer row enlarges ifa_fault_redeliver_succeeded's row set inside
# cell_duplicatedelivery. So the shared `baseline` digest contains no
# USES_PROFILE edges and every cell of this family would mismatch it.
# These cells therefore compare against
# digests[baseline_ec2_uses_profile] via assert_matches_baseline's
# baseline_key parameter.
#
# It also establishes baseline_ec2_uses_profile_retried: the
# fault-free excess-ATTEMPT total for domain
# ec2_uses_profile_materialization, which the kill cell must exceed to
# prove the replacement reducer re-executed THIS domain rather than merely
# draining another queued row. Attempts, not rows: this cassette creates one
# work item, so the row-count form saturates at 1 (see
# ifa_fault_count_retry_attempts).
cell_baseline_ec2_uses_profile() {
	local cell_start
	cell_start=$(date +%s)
	log "cell baseline-ec2-uses-profile: fresh stack"
	fresh_stack baseline_ec2_uses_profile
	drive_all_cassettes baseline_ec2_uses_profile
	ifa_ec2_uses_profile_drive "baseline_ec2_uses_profile" "${bin_dir}" "${ec2_uses_profile_cassette}" "${drive_workers}" "${log_dir}" \
		|| die "baseline-ec2-uses-profile: eshu-ifa drive (ec2 family) failed"
	local projector_pid reducer_pid
	ifa_det_start_bg "${log_dir}" "projector-baseline_ec2_uses_profile" projector_pid "${bin_dir}/eshu-projector"
	ifa_det_start_bg "${log_dir}" "reducer-baseline_ec2_uses_profile" reducer_pid "${bin_dir}/eshu-reducer"
	run_drain_gate baseline_ec2_uses_profile
	assert_no_dead_letters baseline_ec2_uses_profile
	ifa_ec2_uses_profile_assert "baseline_ec2_uses_profile" "${bin_dir}" "${ec2_uses_profile_expected_edges}" \
		|| die "baseline-ec2-uses-profile: fault-free graph does not match the three-edge exact set"
	baseline_ec2_uses_profile_retried="$(ifa_fault_count_retry_attempts "${FAULT_COMPOSE_PROJECT}" "${use_compose}" "${ESHU_POSTGRES_DSN}" "${compose_file}" "ec2_uses_profile_materialization")" \
		|| die "baseline-ec2-uses-profile: could not count the fault-free ec2_uses_profile_materialization retry-attempt baseline"
	printf 'baseline-ec2-uses-profile: fault-free ec2_uses_profile_materialization retry-attempt baseline: %s\n' "${baseline_ec2_uses_profile_retried}"
	capture_digest baseline_ec2_uses_profile
	teardown_cell baseline_ec2_uses_profile
	wall_times[baseline_ec2_uses_profile]=$(( $(date +%s) - cell_start ))
	printf 'baseline-ec2-uses-profile: cell wall time: %ss\n' "${wall_times[baseline_ec2_uses_profile]}"
}

cell_killworker_ec2_uses_profile() {
	local cell_start
	cell_start=$(date +%s)
	log "cell kill-worker-after-claim-ec2-uses-profile: fresh stack"
	fresh_stack killworkerec2usesprofile
	drive_all_cassettes killworkerec2usesprofile
	ifa_ec2_uses_profile_drive "killworkerec2usesprofile" "${bin_dir}" "${ec2_uses_profile_cassette}" "${drive_workers}" "${log_dir}" \
		|| die "kill-worker-after-claim-ec2-uses-profile: eshu-ifa drive (ec2 family) failed"
	local projector_pid reducer_pid_prereq_nodes reducer_pid_prereq_profiles reducer_pid_before reducer_pid_after lock_holder_pid claimed_before
	ifa_det_start_bg "${log_dir}" "projector-killworkerec2usesprofile" projector_pid "${bin_dir}/eshu-projector"
	# ec2_uses_profile_materialization's reducer intent is created by the
	# PROJECTOR, not ingestion. Wait for the row to exist (any status) before
	# going further: without it there is nothing to make ready and nothing
	# to kill.
	ifa_fault_wait_for_claimed "${FAULT_COMPOSE_PROJECT}" "${use_compose}" "${ESHU_POSTGRES_DSN}" "${compose_file}" \
		"${CLAIMED_ROW_WAIT_TIMEOUT}" "ec2_uses_profile_materialization" 1 >/dev/null \
		|| die "kill-worker-after-claim-ec2-uses-profile: ec2_uses_profile_materialization was never enqueued by the projector"
	# Readiness BEFORE the lock -- the shape that distinguishes this family
	# from the kubernetes kill cell. This domain's claim is gated on TWO
	# canonical_nodes_committed/cloud_resource_uid phases (one per endpoint
	# node family), which the ec2_instance_node and aws_resource reducers
	# publish during Handle -- and both Handles read fact_records. TWO
	# prereq-scoped reducers (one per endpoint domain, so the ec2 row can
	# never be claimed early) run unblocked until BOTH acceptance units the
	# gate will ask for are banked. Only then does the lock go on. Locking
	# first deadlocks by design: the claim gate never opens while the lock is
	# held, so the ec2-scoped reducer would idle against a still-pending row
	# until the wait budget dies.
	ifa_det_start_bg "${log_dir}" "reducer-killworkerec2usesprofile-prereq-nodes" reducer_pid_prereq_nodes \
		env "ESHU_REDUCER_CLAIM_DOMAIN=ec2_instance_node_materialization" "${bin_dir}/eshu-reducer"
	ifa_det_start_bg "${log_dir}" "reducer-killworkerec2usesprofile-prereq-profiles" reducer_pid_prereq_profiles \
		env "ESHU_REDUCER_CLAIM_DOMAIN=aws_resource_materialization" "${bin_dir}/eshu-reducer"
	ifa_ec2_uses_profile_wait_for_readiness "killworkerec2usesprofile" 300 \
		|| die "kill-worker-after-claim-ec2-uses-profile: claim readiness never banked"
	ifa_ec2_uses_profile_start_fact_records_lock "killworkerec2usesprofile" lock_holder_pid \
		|| die "kill-worker-after-claim-ec2-uses-profile: could not acquire the deterministic fact_records read blocker"
	# Scoped to ec2_uses_profile_materialization only: with the lock already
	# held, an unscoped reducer's workers can all grab OTHER pending domains
	# that also read fact_records and get stuck there for the lock's full
	# duration, starving this domain of a worker even though its own row
	# already exists. ESHU_REDUCER_CLAIM_DOMAIN filters the claim SQL itself
	# (domain = ANY(...)), so this instance never attempts another domain's row.
	# The claim fires because readiness was banked above: the gate's phase
	# rows persist, so the lock starves nothing the claim still needs.
	ifa_det_start_bg "${log_dir}" "reducer-killworkerec2usesprofile-before" reducer_pid_before \
		env "ESHU_REDUCER_CLAIM_DOMAIN=ec2_uses_profile_materialization" "${bin_dir}/eshu-reducer"
	claimed_before="$(ifa_fault_wait_for_claimed "${FAULT_COMPOSE_PROJECT}" "${use_compose}" "${ESHU_POSTGRES_DSN}" "${compose_file}" "${CLAIMED_ROW_WAIT_TIMEOUT}" "ec2_uses_profile_materialization")" \
		|| die "kill-worker-after-claim-ec2-uses-profile: no ec2_uses_profile_materialization row was claimed while its fact load was blocked"
	printf 'kill-worker-after-claim-ec2-uses-profile: non-vacuous: %s blocked claimed/running row(s) observed\n' "${claimed_before}"
	kill -9 "${reducer_pid_before}" >/dev/null 2>&1 || true
	ifa_ec2_uses_profile_release_fact_records_lock "killworkerec2usesprofile" "${lock_holder_pid}"
	ifa_det_start_bg "${log_dir}" "reducer-killworkerec2usesprofile-after" reducer_pid_after "${bin_dir}/eshu-reducer"
	run_drain_gate killworkerec2usesprofile
	assert_no_dead_letters killworkerec2usesprofile
	ifa_ec2_uses_profile_assert "killworkerec2usesprofile" "${bin_dir}" "${ec2_uses_profile_expected_edges}" \
		|| die "kill-worker-after-claim-ec2-uses-profile: recovered graph does not match the three-edge exact set"
	ifa_fault_assert_retry_attempts_above "${FAULT_COMPOSE_PROJECT}" "${use_compose}" "${ESHU_POSTGRES_DSN}" "${compose_file}" \
		"${baseline_ec2_uses_profile_retried}" 15 "ec2_uses_profile_materialization" \
		|| die "kill-worker-after-claim-ec2-uses-profile: ec2_uses_profile_materialization did not re-execute above its fault-free retry-attempt baseline"
	capture_digest killworkerec2usesprofile
	assert_matches_baseline killworkerec2usesprofile baseline_ec2_uses_profile
	teardown_cell killworkerec2usesprofile
	wall_times[killworkerec2usesprofile]=$(( $(date +%s) - cell_start ))
	printf 'kill-worker-after-claim-ec2-uses-profile: cell wall time: %ss\n' "${wall_times[killworkerec2usesprofile]}"
}

# cell_failgraphwrite_ec2_uses_profile fails the live
# USES_PROFILE MERGE exactly once, proves the fault decorator's durable
# marker names that write, and requires the full three-edge set to converge
# without dead letters. Anchor read off EXECUTED statement text, not source
# text: the writer's template reads MERGE (source)-[rel:%s]->(target)
# (go/internal/storage/cypher/cloud/aws/ec2/uses_profile_edge_writer.go:47),
# filled per row from a closed single-member vocabulary screened by
# validateEC2UsesProfileRelationshipType, so the anchor MUST carry the
# INTERPOLATED type MERGE (source)-[rel:USES_PROFILE]->(target).
# A literal %s matches no executed statement and the fault would never fire,
# giving a green cell that tested nothing. It is NOT EC2_USES_PROFILE,
# which is statement metadata beside the query and never reaches the graph.
cell_failgraphwrite_ec2_uses_profile() {
	local cell_start
	cell_start=$(date +%s)
	log "cell fail-graph-write-once-then-succeed-ec2-uses-profile: fresh stack"
	fresh_stack failgraphwriteec2usesprofile
	drive_all_cassettes failgraphwriteec2usesprofile
	ifa_ec2_uses_profile_drive "failgraphwriteec2usesprofile" "${bin_dir}" "${ec2_uses_profile_cassette}" "${drive_workers}" "${log_dir}" \
		|| die "fail-graph-write-once-then-succeed-ec2-uses-profile: eshu-ifa drive (ec2 family) failed"
	local fault_once_script projector_pid reducer_pid marker_rc
	fault_once_script="${work_dir}/fault-once-then-succeed-ec2-uses-profile.json"
	ifa_fault_write_once_script "${fault_once_script}" "${ec2_uses_profile_edge_operation_match}" "queue-retry"
	ifa_det_start_bg "${log_dir}" "projector-failgraphwriteec2usesprofile" projector_pid "${bin_dir}/eshu-projector"
	ifa_det_start_bg "${log_dir}" "reducer-failgraphwriteec2usesprofile" reducer_pid \
		env "ESHU_IFA_FAULT_SCRIPT=${fault_once_script}" "${tagged_bin_dir}/eshu-reducer"
	run_drain_gate failgraphwriteec2usesprofile
	assert_no_dead_letters failgraphwriteec2usesprofile
	ifa_ec2_uses_profile_assert "failgraphwriteec2usesprofile" "${bin_dir}" "${ec2_uses_profile_expected_edges}" \
		|| die "fail-graph-write-once-then-succeed-ec2-uses-profile: recovered graph does not match the three-edge exact set"
	ifa_det_pg "${FAULT_COMPOSE_PROJECT}" "${use_compose}" "${ESHU_POSTGRES_DSN}" \
		"SELECT count(*) AS total, count(*) FILTER (WHERE status = 'succeeded') AS succeeded FROM fact_work_items WHERE stage = 'reducer' AND domain = 'ec2_uses_profile_materialization';" \
		"${compose_file}" | sed 's/^/  ec2_uses_profile_materialization work-item window: /'
	marker_rc=0
	ifa_fault_assert_once_fault_marker "${fault_once_script}" "${ec2_uses_profile_edge_operation_match}" || marker_rc=$?
	if [[ "${marker_rc}" -eq 2 ]]; then
		# Distinguished from "no marker" deliberately, per #5974: rc=2 means the
		# injection WORKS and fired, but on a different write than the anchor
		# names. Collapsing that into a generic failure sends the next reader
		# looking at the decorator instead of at the anchor.
		die "fail-graph-write-once-then-succeed-ec2-uses-profile: the fault FIRED but on a different write than ${ec2_uses_profile_edge_operation_match} (marker contents above). The injection works; the anchor is pointed at the wrong statement."
	fi
	[[ "${marker_rc}" -eq 0 ]] \
		|| die "fail-graph-write-once-then-succeed-ec2-uses-profile: no once-fired marker at all -- the fault never fired, so this cell proved nothing about USES_PROFILE recovery (marker status ${marker_rc})"
	# Announced, not merely checked. Every sibling cell prints its own
	# non-vacuity line and this one did not, so a passing run said nothing about
	# whether the fault had actually fired -- the check worked while being
	# invisible, which is the shape this family's whole epic is about.
	printf 'fail-graph-write-once-then-succeed-ec2-uses-profile: non-vacuous: once-fired marker names the targeted USES_PROFILE MERGE (written by the fault decorator at injection time, not read from the reducer log)\n'
	capture_digest failgraphwriteec2usesprofile
	assert_matches_baseline failgraphwriteec2usesprofile baseline_ec2_uses_profile
	teardown_cell failgraphwriteec2usesprofile
	wall_times[failgraphwriteec2usesprofile]=$(( $(date +%s) - cell_start ))
	printf 'fail-graph-write-once-then-succeed-ec2-uses-profile: cell wall time: %ss\n' "${wall_times[failgraphwriteec2usesprofile]}"
}
