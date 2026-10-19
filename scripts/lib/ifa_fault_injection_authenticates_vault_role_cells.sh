#!/usr/bin/env bash
# shellcheck disable=SC2034,SC2154
# authenticates_vault_role-targeted live fault cells (#6228 slice A full
# build), mirroring scripts/lib/ifa_fault_injection_iam_escalation_cells.sh's
# shape with one structural difference: TWO-STAGE chain. The family's edges
# are written by the DomainSecretsIAMGraphProjection handler, which reads
# reducer-owned read-model facts -- so every cell runs the trust-chain stage
# first (trust-chain-scoped reducer, flag on) and only then the projection
# stage (projection-scoped reducer, flag on). The projection claim is NOT
# readiness-gated (no readiness row names this domain), so the kill cell
# needs no prereq-scoped reducer -- but it MUST wait for the trust-chain
# stage's read-model facts to commit before the lock goes on, or the
# projection claims an empty generation, succeeds vacuously, and never
# re-runs.
# WIRED: scripts/verify-ifa-fault-injection.sh sources this file (through
# scripts/lib/ifa_fault_injection_sources.sh) and runs all three cells
# (baseline_authenticates_vault_role,
# killworker_authenticates_vault_role,
# failgraphwrite_authenticates_vault_role) against a live Postgres +
# Neo4j stack. The cells rely on the driver's strict mode and globals
# (bin_dir, tagged_bin_dir, log_dir, work_dir, use_compose, compose_file,
# FAULT_COMPOSE_PROJECT, ESHU_POSTGRES_DSN, CLAIMED_ROW_WAIT_TIMEOUT,
# drive_workers, wall_times, bg_pids, log, die, plus the fresh_stack /
# drive_all_cassettes / run_drain_gate / assert_no_dead_letters /
# capture_digest / assert_matches_baseline / teardown_cell helpers from
# ifa_fault_injection_driver.sh). Drive and assert callbacks come from
# scripts/lib/ifa_direct_family_live.sh
# (ifa_authenticates_vault_role_drive /
# ifa_authenticates_vault_role_assert); the cassette and expected-edge
# variables come from scripts/lib/ifa_family_fixtures.sh.
#
# scripts/lib/test-ifa-fault-injection-authenticates-vault-role-cases.sh
# sources this file and drives
# ifa_authenticates_vault_role_start_fact_records_lock,
# ifa_authenticates_vault_role_release_fact_records_lock, and
# ifa_authenticates_vault_role_wait_for_read_models under stubs,
# and scripts/test-verify-ifa-fault-injection.sh runs that module -- so editing
# any fail-closed behavior or bg_pids bookkeeping will fail `make pre-pr`.
#
# authenticates_vault_role is SINGLE-STAGE-DIRECT at the graph: the projection
# handler (SecretsIAMGraphProjectionHandler) claims a fact_work_items row
# under domain "secrets_iam_graph_projection", loads its read-model facts via
# factload.LoadFactsForKinds -> FROM fact_records, and writes the
# SECRETS_IAM_AUTHENTICATES_TO_VAULT_ROLE edges DIRECTLY to the graph through
# its cypher writer
# (go/internal/storage/cypher/cloud/aws/iam/secrets_graph_writer.go:97),
# then acks. There is no shared_projection_intents row and no second-stage
# runner lease -- the two blocker kinds the registry schema offers that this
# family cannot use (see rows/21_authenticates_vault_role.sh).
#
# The lock target is therefore the handler's first synchronous READ, not a
# write: ACCESS EXCLUSIVE on fact_records blocks readers, and the claim/ack
# path never touches that table, so the row stays claimed while the handler
# is held. The handler struct embeds FactLoader
# (go/internal/reducer/secretsiam/secrets_iam_graph_projection.go:81) and
# Handle refuses to run without it (:101) before passing it to
# factload.LoadFactsForKinds (:121) -- the pre-write window the lock needs,
# measured for codeowners at 14s of visibly-waiting backends and re-proven by
# this family's own kill cell below rather than assumed from the sibling.
#
# ifa_authenticates_vault_role_start_fact_records_lock holds an ACCESS
# EXCLUSIVE lock on fact_records so the projection handler blocks on its
# FIRST synchronous read -- the read-model fact load -- while its
# fact_work_items claim is already held. The claim, heartbeat, and ack path
# never reads fact_records, so the row stays claimed and alive for the whole
# hold. fact_records, not fact_work_items: the cell's own wait predicate polls
# fact_work_items, and locking that table would block the poll too.
ifa_authenticates_vault_role_start_fact_records_lock() {
	local cell="$1" pid_var="$2"
	# Short prefix deliberately: the repo holds application_name to 63 bytes
	# (ifa_fault_generic_runner_wait.sh, ifa_fault_injection_documentation_ack_setup.sh),
	# and a full family-name prefix plus a long kill cell name would be
	# silently truncated, so the grant poll below (which matches
	# the full name) would miss forever while the holder sat on its granted
	# lock. Proven live by the sibling kubernetes family's identical failure.
	# ifa_auth_vault_lock_ keeps the longest lock name this family uses at 54 bytes.
	local app_name="ifa_auth_vault_lock_${cell}"
	if ((${#app_name} > 63)); then
		printf '%s: lock application_name is %s bytes; past the 63-byte repo cap the name truncates and the grant poll would never match\n' "${cell}" "${#app_name}" >&2
		return 1
	fi
	local lock_sql="SET application_name = '${app_name}'; BEGIN; LOCK TABLE fact_records IN ACCESS EXCLUSIVE MODE; SELECT pg_sleep(180); ROLLBACK;"
	if [[ "${use_compose}" -eq 1 ]]; then
		bash scripts/ci/compose.sh -p "${FAULT_COMPOSE_PROJECT}" -f "${compose_file}" -- exec -T postgres \
			psql -v ON_ERROR_STOP=1 -U eshu -d eshu -c "${lock_sql}" \
			>"${log_dir}/authenticates-vault-role-lock-${cell}.log" 2>&1 &
	else
		psql "${ESHU_POSTGRES_DSN}" -v ON_ERROR_STOP=1 -c "${lock_sql}" \
			>"${log_dir}/authenticates-vault-role-lock-${cell}.log" 2>&1 &
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

# ifa_authenticates_vault_role_release_fact_records_lock terminates
# the named lock-holder backend, then joins its local psql/docker process
# before the replacement reducer starts.
ifa_authenticates_vault_role_release_fact_records_lock() {
	local cell="$1" holder_pid="$2"
	# Same short name as the start function: it must terminate the backend
	# the start function actually created (see its 63-byte-cap comment).
	local app_name="ifa_auth_vault_lock_${cell}"
	ifa_det_pg "${FAULT_COMPOSE_PROJECT}" "${use_compose}" "${ESHU_POSTGRES_DSN}" \
		"SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE application_name = '${app_name}';" \
		"${compose_file}" >/dev/null
	wait "${holder_pid}" 2>/dev/null || true
	ifa_det_untrack_bg_pid "${holder_pid}"
}

# ifa_authenticates_vault_role_wait_for_read_models banks the projection
# stage's INPUT before the fact_records lock is taken. The projection handler
# loads reducer-owned read-model facts; if the lock goes on before the
# trust-chain stage commits them, the projection claims an empty generation,
# succeeds vacuously, and never re-runs -- a green cell that proved nothing.
# This helper resolves the scope off the trust-chain row's own identity and
# polls fact_records for the two read-model kinds until at least one exists.
# Fail-closed throughout: no trust-chain row, a failed query, or budget
# exhaustion all return non-zero, and a non-numeric count is a failure, never
# a zero.
ifa_authenticates_vault_role_wait_for_read_models() {
	local cell="$1" budget="$2"
	if [[ ! "${budget}" =~ ^[1-9][0-9]*$ ]]; then
		printf '%s: read-model wait: budget must be a positive integer, got %s\n' "${cell}" "${budget}" >&2
		return 1
	fi
	local scope
	if ! scope="$(ifa_det_pg "${FAULT_COMPOSE_PROJECT}" "${use_compose}" "${ESHU_POSTGRES_DSN}" \
		"SELECT scope_id FROM fact_work_items WHERE stage = 'reducer' AND domain = 'secrets_iam_trust_chain' ORDER BY created_at ASC, work_item_id ASC LIMIT 1;" \
		"${compose_file}")"; then
		printf '%s: read-model wait: trust-chain identity query failed\n' "${cell}" >&2
		return 1
	fi
	scope="$(printf '%s' "${scope}" | tr -d '[:space:]')"
	if [[ -z "${scope}" ]]; then
		printf '%s: read-model wait: no trust-chain row to resolve a scope from\n' "${cell}" >&2
		return 1
	fi
	# Values come from our own queue rows, but they ride inside string
	# literals one level down: double any quote rather than trusting shape.
	scope="${scope//\'/\'\'}"
	local i model_count
	for i in $(seq 1 "${budget}"); do
		if ! model_count="$(ifa_det_pg "${FAULT_COMPOSE_PROJECT}" "${use_compose}" "${ESHU_POSTGRES_DSN}" \
			"SELECT count(*) FROM fact_records WHERE scope_id = '${scope}' AND fact_kind IN ('reducer_secrets_iam_identity_trust_chain', 'reducer_secrets_iam_secret_access_path');" \
			"${compose_file}")"; then
			printf '%s: read-model wait: fact poll query failed\n' "${cell}" >&2
			return 1
		fi
		model_count="$(printf '%s' "${model_count}" | tr -d '[:space:]')"
		if [[ "${model_count}" =~ ^[1-9][0-9]*$ ]]; then
			printf '%s: read models banked: %s reducer secrets/IAM read-model fact(s) in scope %s\n' "${cell}" "${model_count}" "${scope}"
			return 0
		fi
		if [[ "${model_count}" != "0" ]]; then
			printf '%s: read-model wait: fact poll returned non-numeric output %q\n' "${cell}" "${model_count}" >&2
			return 1
		fi
		sleep 1
	done
	printf '%s: read-model wait: no reducer secrets/IAM read-model facts in scope %s within %ss\n' "${cell}" "${scope}" "${budget}" >&2
	return 1
}

# ifa_authenticates_vault_role_run_trust_chain_stage runs the trust-chain
# stage to committed read models and then STOPS its reducer: the stage is
# scoped to secrets_iam_trust_chain so it can never claim the projection
# row early, and stopping it before the lock frees its workers. The flag is
# on for this reducer so the follow-up writer is wired and the trust-chain
# handler enqueues the projection intent as its read models commit -- but
# this reducer never claims that row.
ifa_authenticates_vault_role_run_trust_chain_stage() {
	local cell="$1"
	local trust_reducer_pid
	ifa_det_start_bg "${log_dir}" "reducer-${cell}-trustchain" trust_reducer_pid \
		env "ESHU_REDUCER_SECRETS_IAM_GRAPH_PROJECTION_ENABLED=true" "ESHU_REDUCER_CLAIM_DOMAIN=secrets_iam_trust_chain" "${bin_dir}/eshu-reducer"
	ifa_authenticates_vault_role_wait_for_read_models "${cell}" 300 \
		|| die "${cell}: trust-chain stage never committed read models"
	kill "${trust_reducer_pid}" >/dev/null 2>&1 || true
	wait "${trust_reducer_pid}" 2>/dev/null || true
	ifa_det_untrack_bg_pid "${trust_reducer_pid}"
}

# cell_baseline_authenticates_vault_role is this family's fault-free
# reference run. It exists because the authenticates-vault-role cassette is
# driven ONLY by this family's own cells: ifa_fault_injection_driver.sh's
# header rules that a new family cassette is never added to
# drive_all_cassettes, since the extra succeeded reducer row enlarges
# ifa_fault_redeliver_succeeded's row set inside cell_duplicatedelivery. So
# the shared `baseline` digest contains no SECRETS_IAM_AUTHENTICATES_TO_VAULT_ROLE
# edges and every cell of this family would mismatch it. These cells therefore
# compare against digests[baseline_authenticates_vault_role] via
# assert_matches_baseline's baseline_key parameter.
#
# It also establishes baseline_authenticates_vault_role_retried: the
# fault-free excess-ATTEMPT total for domain secrets_iam_graph_projection,
# which the kill cell must exceed to prove the replacement reducer
# re-executed THIS domain rather than merely draining another queued row.
# Attempts, not rows: this cassette creates one projection work item, so the
# row-count form saturates at 1 (see ifa_fault_count_retry_attempts).
cell_baseline_authenticates_vault_role() {
	local cell_start
	cell_start=$(date +%s)
	log "cell baseline-authenticates-vault-role: fresh stack"
	fresh_stack baseline_authenticates_vault_role
	drive_all_cassettes baseline_authenticates_vault_role
	ifa_authenticates_vault_role_drive "baseline_authenticates_vault_role" "${bin_dir}" "${authenticates_vault_role_cassette}" "${drive_workers}" "${log_dir}" \
		|| die "baseline-authenticates-vault-role: eshu-ifa drive (authenticates-vault-role family) failed"
	local projector_pid reducer_pid
	ifa_det_start_bg "${log_dir}" "projector-baseline_authenticates_vault_role" projector_pid "${bin_dir}/eshu-projector"
	ifa_authenticates_vault_role_run_trust_chain_stage "baseline_authenticates_vault_role"
	# Drain reducer is flag-on but UNSCOPED like the sibling kill/failgraphwrite
	# cells: run_drain_gate requires global residual=0, and a CLAIM_DOMAIN
	# here would leave every other domain's rows unclaimable by construction.
	ifa_det_start_bg "${log_dir}" "reducer-baseline_authenticates_vault_role" reducer_pid \
		env "ESHU_REDUCER_SECRETS_IAM_GRAPH_PROJECTION_ENABLED=true" "${bin_dir}/eshu-reducer"
	run_drain_gate baseline_authenticates_vault_role
	assert_no_dead_letters baseline_authenticates_vault_role
	ifa_authenticates_vault_role_assert "baseline_authenticates_vault_role" "${bin_dir}" "${authenticates_vault_role_expected_edges}" \
		|| die "baseline-authenticates-vault-role: fault-free graph does not match the two-edge exact set"
	baseline_authenticates_vault_role_retried="$(ifa_fault_count_retry_attempts "${FAULT_COMPOSE_PROJECT}" "${use_compose}" "${ESHU_POSTGRES_DSN}" "${compose_file}" "secrets_iam_graph_projection")" \
		|| die "baseline-authenticates-vault-role: could not count the fault-free secrets_iam_graph_projection retry-attempt baseline"
	printf 'baseline-authenticates-vault-role: fault-free secrets_iam_graph_projection retry-attempt baseline: %s\n' "${baseline_authenticates_vault_role_retried}"
	capture_digest baseline_authenticates_vault_role
	teardown_cell baseline_authenticates_vault_role
	wall_times[baseline_authenticates_vault_role]=$(( $(date +%s) - cell_start ))
	printf 'baseline-authenticates-vault-role: cell wall time: %ss\n' "${wall_times[baseline_authenticates_vault_role]}"
}

cell_killworker_authenticates_vault_role() {
	local cell_start
	cell_start=$(date +%s)
	log "cell kill-worker-after-claim-authenticates-vault-role: fresh stack"
	fresh_stack killworkerauthenticatesvaultrole
	drive_all_cassettes killworkerauthenticatesvaultrole
	ifa_authenticates_vault_role_drive "killworkerauthenticatesvaultrole" "${bin_dir}" "${authenticates_vault_role_cassette}" "${drive_workers}" "${log_dir}" \
		|| die "kill-worker-after-claim-authenticates-vault-role: eshu-ifa drive (authenticates-vault-role family) failed"
	local projector_pid reducer_pid_before reducer_pid_after lock_holder_pid claimed_before
	ifa_det_start_bg "${log_dir}" "projector-killworkerauthenticatesvaultrole" projector_pid "${bin_dir}/eshu-projector"
	# Read models BEFORE the lock -- the shape that distinguishes this
	# family from every single-stage direct family. The projection handler
	# loads reducer-owned read-model facts; locking first would let it claim
	# an empty generation, succeed vacuously, and never re-run. The
	# trust-chain-scoped reducer (flag on, so the follow-up writer is wired)
	# can never claim the projection row early; it is stopped once the read
	# models commit.
	ifa_authenticates_vault_role_run_trust_chain_stage "killworkerauthenticatesvaultrole"
	# The projection intent is created by the trust-chain handler when its
	# read-model writes commit -- not by ingestion and not by the projector,
	# which fans out only the trust-chain intent. Wait for the row to exist
	# (any status) AFTER the trust-chain stage: waiting before it would time
	# out on a row that cannot exist yet. Without the row there is nothing
	# to hold in flight and nothing to kill.
	ifa_fault_wait_for_claimed "${FAULT_COMPOSE_PROJECT}" "${use_compose}" "${ESHU_POSTGRES_DSN}" "${compose_file}" \
		"${CLAIMED_ROW_WAIT_TIMEOUT}" "secrets_iam_graph_projection" 1 >/dev/null \
		|| die "kill-worker-after-claim-authenticates-vault-role: secrets_iam_graph_projection was never enqueued by the trust-chain stage"
	ifa_authenticates_vault_role_start_fact_records_lock "killworkerauthenticatesvaultrole" lock_holder_pid \
		|| die "kill-worker-after-claim-authenticates-vault-role: could not acquire the deterministic fact_records read blocker"
	# Scoped to secrets_iam_graph_projection only: with the lock already
	# held, an unscoped reducer's workers can all grab OTHER pending domains
	# that also read fact_records and get stuck there for the lock's full
	# duration, starving this domain of a worker even though its own row
	# already exists. ESHU_REDUCER_CLAIM_DOMAIN filters the claim SQL itself
	# (domain = ANY(...)), so this instance never attempts another domain's row.
	# The claim fires because the read models were banked above: the lock
	# starves nothing the claim still needs.
	ifa_det_start_bg "${log_dir}" "reducer-killworkerauthenticatesvaultrole-before" reducer_pid_before \
		env "ESHU_REDUCER_SECRETS_IAM_GRAPH_PROJECTION_ENABLED=true" "ESHU_REDUCER_CLAIM_DOMAIN=secrets_iam_graph_projection" "${bin_dir}/eshu-reducer"
	claimed_before="$(ifa_fault_wait_for_claimed "${FAULT_COMPOSE_PROJECT}" "${use_compose}" "${ESHU_POSTGRES_DSN}" "${compose_file}" "${CLAIMED_ROW_WAIT_TIMEOUT}" "secrets_iam_graph_projection")" \
		|| die "kill-worker-after-claim-authenticates-vault-role: no secrets_iam_graph_projection row was claimed while its fact load was blocked"
	printf 'kill-worker-after-claim-authenticates-vault-role: non-vacuous: %s blocked claimed/running row(s) observed\n' "${claimed_before}"
	kill -9 "${reducer_pid_before}" >/dev/null 2>&1 || true
	ifa_authenticates_vault_role_release_fact_records_lock "killworkerauthenticatesvaultrole" "${lock_holder_pid}"
	ifa_det_start_bg "${log_dir}" "reducer-killworkerauthenticatesvaultrole-after" reducer_pid_after \
		env "ESHU_REDUCER_SECRETS_IAM_GRAPH_PROJECTION_ENABLED=true" "${bin_dir}/eshu-reducer"
	run_drain_gate killworkerauthenticatesvaultrole
	assert_no_dead_letters killworkerauthenticatesvaultrole
	ifa_authenticates_vault_role_assert "killworkerauthenticatesvaultrole" "${bin_dir}" "${authenticates_vault_role_expected_edges}" \
		|| die "kill-worker-after-claim-authenticates-vault-role: recovered graph does not match the two-edge exact set"
	ifa_fault_assert_retry_attempts_above "${FAULT_COMPOSE_PROJECT}" "${use_compose}" "${ESHU_POSTGRES_DSN}" "${compose_file}" \
		"${baseline_authenticates_vault_role_retried}" 15 "secrets_iam_graph_projection" \
		|| die "kill-worker-after-claim-authenticates-vault-role: secrets_iam_graph_projection did not re-execute above its fault-free retry-attempt baseline"
	capture_digest killworkerauthenticatesvaultrole
	assert_matches_baseline killworkerauthenticatesvaultrole baseline_authenticates_vault_role
	teardown_cell killworkerauthenticatesvaultrole
	wall_times[killworkerauthenticatesvaultrole]=$(( $(date +%s) - cell_start ))
	printf 'kill-worker-after-claim-authenticates-vault-role: cell wall time: %ss\n' "${wall_times[killworkerauthenticatesvaultrole]}"
}

# cell_failgraphwrite_authenticates_vault_role fails the live
# SECRETS_IAM_AUTHENTICATES_TO_VAULT_ROLE MERGE exactly once, proves the
# fault decorator's durable marker names that write, and requires the full
# two-edge set to converge without dead letters. Anchor read off EXECUTED
# statement text, not source text: the writer's template bakes the static
# token directly into SecretsIAMAuthenticatesVaultRoleEdgeUpsertCypher
# (go/internal/storage/cypher/cloud/aws/iam/secrets_graph_writer.go:97), so
# the anchor carries the literal MERGE (s)-[rel:SECRETS_IAM_AUTHENTICATES_TO_VAULT_ROLE]->(v).
# A `%s` form would match no executed statement and the fault would never
# fire, giving a green cell that tested nothing. It is NOT
# AUTHENTICATES_VAULT_ROLE, which appears nowhere in code.
cell_failgraphwrite_authenticates_vault_role() {
	local cell_start
	cell_start=$(date +%s)
	log "cell fail-graph-write-once-then-succeed-authenticates-vault-role: fresh stack"
	fresh_stack failgraphwriteauthenticatesvaultrole
	drive_all_cassettes failgraphwriteauthenticatesvaultrole
	ifa_authenticates_vault_role_drive "failgraphwriteauthenticatesvaultrole" "${bin_dir}" "${authenticates_vault_role_cassette}" "${drive_workers}" "${log_dir}" \
		|| die "fail-graph-write-once-then-succeed-authenticates-vault-role: eshu-ifa drive (authenticates-vault-role family) failed"
	local fault_once_script projector_pid reducer_pid marker_rc
	fault_once_script="${work_dir}/fault-once-then-succeed-authenticates-vault-role.json"
	ifa_fault_write_once_script "${fault_once_script}" "${authenticates_vault_role_edge_operation_match}" "queue-retry"
	ifa_det_start_bg "${log_dir}" "projector-failgraphwriteauthenticatesvaultrole" projector_pid "${bin_dir}/eshu-projector"
	ifa_authenticates_vault_role_run_trust_chain_stage "failgraphwriteauthenticatesvaultrole"
	ifa_det_start_bg "${log_dir}" "reducer-failgraphwriteauthenticatesvaultrole" reducer_pid \
		env "ESHU_REDUCER_SECRETS_IAM_GRAPH_PROJECTION_ENABLED=true" "ESHU_IFA_FAULT_SCRIPT=${fault_once_script}" "${tagged_bin_dir}/eshu-reducer"
	run_drain_gate failgraphwriteauthenticatesvaultrole
	assert_no_dead_letters failgraphwriteauthenticatesvaultrole
	ifa_authenticates_vault_role_assert "failgraphwriteauthenticatesvaultrole" "${bin_dir}" "${authenticates_vault_role_expected_edges}" \
		|| die "fail-graph-write-once-then-succeed-authenticates-vault-role: recovered graph does not match the two-edge exact set"
	ifa_det_pg "${FAULT_COMPOSE_PROJECT}" "${use_compose}" "${ESHU_POSTGRES_DSN}" \
		"SELECT count(*) AS total, count(*) FILTER (WHERE status = 'succeeded') AS succeeded FROM fact_work_items WHERE stage = 'reducer' AND domain = 'secrets_iam_graph_projection';" \
		"${compose_file}" | sed 's/^/  secrets_iam_graph_projection work-item window: /'
	marker_rc=0
	ifa_fault_assert_once_fault_marker "${fault_once_script}" "${authenticates_vault_role_edge_operation_match}" || marker_rc=$?
	if [[ "${marker_rc}" -eq 2 ]]; then
		# Distinguished from "no marker" deliberately, per #5974: rc=2 means the
		# injection WORKS and fired, but on a different write than the anchor
		# names. Collapsing that into a generic failure sends the next reader
		# looking at the decorator instead of at the anchor.
		die "fail-graph-write-once-then-succeed-authenticates-vault-role: the fault FIRED but on a different write than ${authenticates_vault_role_edge_operation_match} (marker contents above). The injection works; the anchor is pointed at the wrong statement."
	fi
	[[ "${marker_rc}" -eq 0 ]] \
		|| die "fail-graph-write-once-then-succeed-authenticates-vault-role: no once-fired marker at all -- the fault never fired, so this cell proved nothing about SECRETS_IAM_AUTHENTICATES_TO_VAULT_ROLE recovery (marker status ${marker_rc})"
	# Announced, not merely checked. Every sibling cell prints its own
	# non-vacuity line and this one did not, so a passing run said nothing about
	# whether the fault had actually fired -- the check worked while being
	# invisible, which is the shape this family's whole epic is about.
	printf 'fail-graph-write-once-then-succeed-authenticates-vault-role: non-vacuous: once-fired marker names the targeted SECRETS_IAM_AUTHENTICATES_TO_VAULT_ROLE MERGE (written by the fault decorator at injection time, not read from the reducer log)\n'
	capture_digest failgraphwriteauthenticatesvaultrole
	assert_matches_baseline failgraphwriteauthenticatesvaultrole baseline_authenticates_vault_role
	teardown_cell failgraphwriteauthenticatesvaultrole
	wall_times[failgraphwriteauthenticatesvaultrole]=$(( $(date +%s) - cell_start ))
	printf 'fail-graph-write-once-then-succeed-authenticates-vault-role: cell wall time: %ss\n' "${wall_times[failgraphwriteauthenticatesvaultrole]}"
}
