#!/usr/bin/env bash
# shellcheck disable=SC2034,SC2154
# uses_vault_policy-targeted live fault cells (#6228 slice A full
# build), mirroring scripts/lib/ifa_fault_injection_iam_escalation_cells.sh's
# shape with one structural difference: TWO-STAGE chain. The family's edges
# are written by the DomainSecretsIAMGraphProjection handler, which reads
# reducer-owned read-model facts -- so every cell runs the trust-chain stage
# first (trust-chain-scoped reducer, flag on) and only then the projection
# stage (projection-scoped reducer, flag on). The projection claim is NOT
# readiness-gated (no readiness row names this domain), so no cell needs a
# prereq-scoped reducer -- but every cell MUST wait for the trust-chain
# stage's read-model facts to commit before the projection stage starts, or
# the projection claims an empty generation, succeeds vacuously, and never
# re-runs.
# WIRED: scripts/verify-ifa-fault-injection.sh sources this file (through
# scripts/lib/ifa_fault_injection_sources.sh) and runs both cells
# (baseline_uses_vault_policy,
# failgraphwrite_uses_vault_policy) against a live Postgres +
# Neo4j stack. No kill-worker cell: the shared
# secrets_iam_graph_projection handler wait is owned first-landed by
# authenticates_vault_role (rows/21, arbiter verdict Option B on #6228),
# and mid-handler kill/reclaim for this family is a named, tracked gap
# (#8539), not claimed by this coverage. The cells rely on the driver's
# strict mode and globals
# (bin_dir, tagged_bin_dir, log_dir, work_dir, use_compose, compose_file,
# FAULT_COMPOSE_PROJECT, ESHU_POSTGRES_DSN, CLAIMED_ROW_WAIT_TIMEOUT,
# drive_workers, wall_times, bg_pids, log, die, plus the fresh_stack /
# drive_all_cassettes / run_drain_gate / assert_no_dead_letters /
# capture_digest / assert_matches_baseline / teardown_cell helpers from
# ifa_fault_injection_driver.sh). Drive and assert callbacks come from
# scripts/lib/ifa_vault_family_live.sh
# (ifa_uses_vault_policy_drive /
# ifa_uses_vault_policy_assert); the cassette and expected-edge
# variables come from scripts/lib/ifa_family_fixtures.sh.
#
# scripts/lib/test-ifa-fault-injection-uses-vault-policy-cases.sh
# sources this file and drives
# ifa_uses_vault_policy_wait_for_read_models under stubs,
# and scripts/test-verify-ifa-fault-injection.sh runs that module -- so editing
# any fail-closed behavior will fail `make pre-pr`.
#
# uses_vault_policy is SINGLE-STAGE-DIRECT at the graph: the projection
# handler (SecretsIAMGraphProjectionHandler) claims a fact_work_items row
# under domain "secrets_iam_graph_projection", loads its read-model facts via
# factload.LoadFactsForKinds -> FROM fact_records, and writes the
# SECRETS_IAM_USES_VAULT_POLICY edges DIRECTLY to the graph through
# its cypher writer
# (go/internal/storage/cypher/cloud/aws/iam/secrets_graph_writer.go:108),
# then acks. There is no shared_projection_intents row and no second-stage
# runner lease -- the two blocker kinds the registry schema offers that this
# family cannot use (see rows/22_uses_vault_policy.sh). No remaining cell
# takes a lock (blocker_kind=none): the read-model ordering below is a stage
# sequence, not a hold -- the trust-chain reducer is stopped before the
# projection reducer starts, so no two writers ever contend.
#
# The handler struct embeds FactLoader
# (go/internal/reducer/secretsiam/secrets_iam_graph_projection.go:81) and
# Handle refuses to run without it (:101) before passing it to
# factload.LoadFactsForKinds (:121): the projection stage reads fact_records
# AFTER claiming, so the read models must be banked first or the projection
# claims an empty generation, succeeds vacuously, and never re-runs.
#
# ifa_uses_vault_policy_wait_for_read_models banks the projection
# stage's INPUT before the fact_records lock is taken. The projection handler
# loads reducer-owned read-model facts; if the lock goes on before the
# trust-chain stage commits them, the projection claims an empty generation,
# succeeds vacuously, and never re-runs -- a green cell that proved nothing.
# This helper resolves the scope off the trust-chain row's own identity and
# polls fact_records for the two read-model kinds until at least one exists.
# Fail-closed throughout: no trust-chain row, a failed query, or budget
# exhaustion all return non-zero, and a non-numeric count is a failure, never
# a zero.
ifa_uses_vault_policy_wait_for_read_models() {
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

# ifa_uses_vault_policy_run_trust_chain_stage runs the trust-chain
# stage to committed read models and then STOPS its reducer: the stage is
# scoped to secrets_iam_trust_chain so it can never claim the projection
# row early, and stopping it before the lock frees its workers. The flag is
# on for this reducer so the follow-up writer is wired and the trust-chain
# handler enqueues the projection intent as its read models commit -- but
# this reducer never claims that row.
ifa_uses_vault_policy_run_trust_chain_stage() {
	local cell="$1"
	local trust_reducer_pid
	ifa_det_start_bg "${log_dir}" "reducer-${cell}-trustchain" trust_reducer_pid \
		env "ESHU_REDUCER_SECRETS_IAM_GRAPH_PROJECTION_ENABLED=true" "ESHU_REDUCER_CLAIM_DOMAIN=secrets_iam_trust_chain" "${bin_dir}/eshu-reducer"
	ifa_uses_vault_policy_wait_for_read_models "${cell}" 300 \
		|| die "${cell}: trust-chain stage never committed read models"
	kill "${trust_reducer_pid}" >/dev/null 2>&1 || true
	wait "${trust_reducer_pid}" 2>/dev/null || true
	ifa_det_untrack_bg_pid "${trust_reducer_pid}"
}

# cell_baseline_uses_vault_policy is this family's fault-free
# reference run. It exists because the uses-vault-policy cassette is
# driven ONLY by this family's own cells: ifa_fault_injection_driver.sh's
# header rules that a new family cassette is never added to
# drive_all_cassettes, since the extra succeeded reducer row enlarges
# ifa_fault_redeliver_succeeded's row set inside cell_duplicatedelivery. So
# the shared `baseline` digest contains no SECRETS_IAM_USES_VAULT_POLICY
# edges and every cell of this family would mismatch it. These cells therefore
# compare against digests[baseline_uses_vault_policy] via
# assert_matches_baseline's baseline_key parameter.
cell_baseline_uses_vault_policy() {
	local cell_start
	cell_start=$(date +%s)
	log "cell baseline-uses-vault-policy: fresh stack"
	fresh_stack baseline_uses_vault_policy
	drive_all_cassettes baseline_uses_vault_policy
	ifa_uses_vault_policy_drive "baseline_uses_vault_policy" "${bin_dir}" "${uses_vault_policy_cassette}" "${drive_workers}" "${log_dir}" \
		|| die "baseline-uses-vault-policy: eshu-ifa drive (uses-vault-policy family) failed"
	local projector_pid reducer_pid
	ifa_det_start_bg "${log_dir}" "projector-baseline_uses_vault_policy" projector_pid "${bin_dir}/eshu-projector"
	ifa_uses_vault_policy_run_trust_chain_stage "baseline_uses_vault_policy"
	ifa_det_start_bg "${log_dir}" "reducer-baseline_uses_vault_policy" reducer_pid \
		env "ESHU_REDUCER_SECRETS_IAM_GRAPH_PROJECTION_ENABLED=true" "ESHU_REDUCER_CLAIM_DOMAIN=secrets_iam_graph_projection" "${bin_dir}/eshu-reducer"
	run_drain_gate baseline_uses_vault_policy
	assert_no_dead_letters baseline_uses_vault_policy
	ifa_uses_vault_policy_assert "baseline_uses_vault_policy" "${bin_dir}" "${uses_vault_policy_expected_edges}" \
		|| die "baseline-uses-vault-policy: fault-free graph does not match the three-edge exact set"
	capture_digest baseline_uses_vault_policy
	teardown_cell baseline_uses_vault_policy
	wall_times[baseline_uses_vault_policy]=$(( $(date +%s) - cell_start ))
	printf 'baseline-uses-vault-policy: cell wall time: %ss\n' "${wall_times[baseline_uses_vault_policy]}"
}

# cell_failgraphwrite_uses_vault_policy fails the live
# SECRETS_IAM_USES_VAULT_POLICY MERGE exactly once, proves the
# fault decorator's durable marker names that write, and requires the full
# three-edge set to converge without dead letters. Anchor read off EXECUTED
# statement text, not source text: the writer's template bakes the static
# token directly into SecretsIAMUsesVaultPolicyEdgeUpsertCypher
# (go/internal/storage/cypher/cloud/aws/iam/secrets_graph_writer.go:108), so
# the anchor carries the literal MERGE (v)-[rel:SECRETS_IAM_USES_VAULT_POLICY]->(p).
# A `%s` form would match no executed statement and the fault would never
# fire, giving a green cell that tested nothing. It is NOT the bare
# USES_VAULT_POLICY fragment: the full MERGE literal pins the exact statement
# (Vault-role endpoint to Vault-policy endpoint), so the fault cannot fire on
# a different write that merely names the same type.
cell_failgraphwrite_uses_vault_policy() {
	local cell_start
	cell_start=$(date +%s)
	log "cell fail-graph-write-once-then-succeed-uses-vault-policy: fresh stack"
	fresh_stack failgraphwriteusesvaultpolicy
	drive_all_cassettes failgraphwriteusesvaultpolicy
	ifa_uses_vault_policy_drive "failgraphwriteusesvaultpolicy" "${bin_dir}" "${uses_vault_policy_cassette}" "${drive_workers}" "${log_dir}" \
		|| die "fail-graph-write-once-then-succeed-uses-vault-policy: eshu-ifa drive (uses-vault-policy family) failed"
	local fault_once_script projector_pid reducer_pid marker_rc
	fault_once_script="${work_dir}/fault-once-then-succeed-uses-vault-policy.json"
	ifa_fault_write_once_script "${fault_once_script}" "${uses_vault_policy_edge_operation_match}" "queue-retry"
	ifa_det_start_bg "${log_dir}" "projector-failgraphwriteusesvaultpolicy" projector_pid "${bin_dir}/eshu-projector"
	ifa_uses_vault_policy_run_trust_chain_stage "failgraphwriteusesvaultpolicy"
	ifa_det_start_bg "${log_dir}" "reducer-failgraphwriteusesvaultpolicy" reducer_pid \
		env "ESHU_REDUCER_SECRETS_IAM_GRAPH_PROJECTION_ENABLED=true" "ESHU_IFA_FAULT_SCRIPT=${fault_once_script}" "${tagged_bin_dir}/eshu-reducer"
	run_drain_gate failgraphwriteusesvaultpolicy
	assert_no_dead_letters failgraphwriteusesvaultpolicy
	ifa_uses_vault_policy_assert "failgraphwriteusesvaultpolicy" "${bin_dir}" "${uses_vault_policy_expected_edges}" \
		|| die "fail-graph-write-once-then-succeed-uses-vault-policy: recovered graph does not match the three-edge exact set"
	ifa_det_pg "${FAULT_COMPOSE_PROJECT}" "${use_compose}" "${ESHU_POSTGRES_DSN}" \
		"SELECT count(*) AS total, count(*) FILTER (WHERE status = 'succeeded') AS succeeded FROM fact_work_items WHERE stage = 'reducer' AND domain = 'secrets_iam_graph_projection';" \
		"${compose_file}" | sed 's/^/  secrets_iam_graph_projection work-item window: /'
	marker_rc=0
	ifa_fault_assert_once_fault_marker "${fault_once_script}" "${uses_vault_policy_edge_operation_match}" || marker_rc=$?
	if [[ "${marker_rc}" -eq 2 ]]; then
		# Distinguished from "no marker" deliberately, per #5974: rc=2 means the
		# injection WORKS and fired, but on a different write than the anchor
		# names. Collapsing that into a generic failure sends the next reader
		# looking at the decorator instead of at the anchor.
		die "fail-graph-write-once-then-succeed-uses-vault-policy: the fault FIRED but on a different write than ${uses_vault_policy_edge_operation_match} (marker contents above). The injection works; the anchor is pointed at the wrong statement."
	fi
	[[ "${marker_rc}" -eq 0 ]] \
		|| die "fail-graph-write-once-then-succeed-uses-vault-policy: no once-fired marker at all -- the fault never fired, so this cell proved nothing about SECRETS_IAM_USES_VAULT_POLICY recovery (marker status ${marker_rc})"
	# Announced, not merely checked. Every sibling cell prints its own
	# non-vacuity line and this one did not, so a passing run said nothing about
	# whether the fault had actually fired -- the check worked while being
	# invisible, which is the shape this family's whole epic is about.
	printf 'fail-graph-write-once-then-succeed-uses-vault-policy: non-vacuous: once-fired marker names the targeted SECRETS_IAM_USES_VAULT_POLICY MERGE (written by the fault decorator at injection time, not read from the reducer log)\n'
	capture_digest failgraphwriteusesvaultpolicy
	assert_matches_baseline failgraphwriteusesvaultpolicy baseline_uses_vault_policy
	teardown_cell failgraphwriteusesvaultpolicy
	wall_times[failgraphwriteusesvaultpolicy]=$(( $(date +%s) - cell_start ))
	printf 'fail-graph-write-once-then-succeed-uses-vault-policy: cell wall time: %ss\n' "${wall_times[failgraphwriteusesvaultpolicy]}"
}
