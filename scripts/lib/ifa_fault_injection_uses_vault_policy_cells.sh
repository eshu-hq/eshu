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
# scripts/lib/ifa_fault_injection_sources.sh) and runs all three cells
# (baseline_uses_vault_policy, failgraphwrite_uses_vault_policy,
# kill_uses_vault_policy) against a live Postgres +
# Neo4j stack. The kill cell is the per-statement hold seam (#8539,
# hold-statement-before-delegation): the shared
# secrets_iam_graph_projection handler wait stays owned first-landed by
# authenticates_vault_role (rows/21, arbiter verdict Option B on #6228),
# so the kill rendezvouses on wait_stage=statement (same key, rendezvous
# only) plus the hold marker. Unlike the authenticates kill cell, the
# kill cell runs an endpoint prereq stage (kubernetes_workload_materialization
# only): the uses-vault-policy rows reference workload endpoints, so the
# #1380 readiness gate defers until those nodes commit, and no other
# reducer in the cell may claim that domain. The cells rely on the driver's
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
# sources this file and the split trust-chain wait lib, and drives
# ifa_uses_vault_policy_wait_for_read_models and the succeeded wait
# under stubs, and scripts/test-verify-ifa-fault-injection.sh runs that
# module -- so editing any fail-closed behavior will fail `make pre-pr`.
#
# uses_vault_policy is SINGLE-STAGE-DIRECT at the graph: the projection
# handler (SecretsIAMGraphProjectionHandler) claims a fact_work_items row
# under domain "secrets_iam_graph_projection", loads its read-model facts via
# factload.LoadFactsForKinds -> FROM fact_records, and writes the
# SECRETS_IAM_USES_VAULT_POLICY edges DIRECTLY to the graph through
# its cypher writer
# (go/internal/storage/cypher/cloud/aws/iam/secrets_graph_writer.go:108),
# then acks. There is no shared_projection_intents row and no second-stage
# runner lease -- the blocker kinds the registry schema offers that this
# family cannot use (see rows/22_uses_vault_policy.sh). The kill cell takes
# no table lock (blocker_kind=statement_hold, not table_lock): the hold
# decorator engages the USES_VAULT_POLICY MERGE itself, post-retract and
# post-prior-edge-write, while the handler is genuinely mid-write. The
# read-model ordering below is a stage sequence, not a hold -- the
# trust-chain reducer is stopped before the projection reducer starts, so
# no two writers ever contend.
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
# Fail-closed throughout: no trust-chain row within budget, a failed query,
# or budget exhaustion all return non-zero, and a non-numeric count is a
# failure, never a zero.
ifa_uses_vault_policy_wait_for_read_models() {
	local cell="$1" budget="$2"
	if [[ ! "${budget}" =~ ^[1-9][0-9]*$ ]]; then
		printf '%s: read-model wait: budget must be a positive integer, got %s\n' "${cell}" "${budget}" >&2
		return 1
	fi
	# The projector enqueues the trust-chain row ~0.1-0.3s after the drive
	# returns (it drains a cassette backlog first), so a single scope read
	# races it and fails closed while the pipeline would have completed on
	# its own. Poll the scope resolution within the same budget instead;
	# the fact poll below gets whatever budget remains, so the whole wait
	# still costs at most budget seconds.
	local scope="" i model_count scope_rounds=0
	for i in $(seq 1 "${budget}"); do
		if ! scope="$(ifa_det_pg "${FAULT_COMPOSE_PROJECT}" "${use_compose}" "${ESHU_POSTGRES_DSN}" \
			"SELECT scope_id FROM fact_work_items WHERE stage = 'reducer' AND domain = 'secrets_iam_trust_chain' ORDER BY created_at ASC, work_item_id ASC LIMIT 1;" \
			"${compose_file}")"; then
			printf '%s: read-model wait: trust-chain identity query failed\n' "${cell}" >&2
			return 1
		fi
		scope="$(printf '%s' "${scope}" | tr -d '[:space:]')"
		if [[ -n "${scope}" ]]; then
			scope_rounds="${i}"
			break
		fi
		sleep 1
	done
	if [[ -z "${scope}" ]]; then
		printf '%s: read-model wait: no trust-chain row to resolve a scope from\n' "${cell}" >&2
		return 1
	fi
	# Values come from our own queue rows, but they ride inside string
	# literals one level down: double any quote rather than trusting shape.
	scope="${scope//\'/\'\'}"
	local fact_budget=$((budget - scope_rounds + 1))
	for i in $(seq 1 "${fact_budget}"); do
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

# ifa_uses_vault_policy_wait_for_endpoint_domains banks the graph
# endpoints the projection readiness gate
# (secrets_iam_graph_projection_readiness.go, issue #1380) needs before the
# hold is armed. The uses-vault-policy rows reference TWO endpoint
# keyspaces: kubernetes workload UIDs (kubernetes_workload_materialization)
# and cloud_resource_uid values (aws_resource_materialization). The cloud
# set is NOT empty though the cassette carries no iam_role_cloud_resource_uid
# field: the trust chain derives those UIDs from aws_iam_principal facts
# and the AWS handler commits the same UIDs from aws_resource facts -- so
# both domains must drain before the hold is armed. Shard 4/4 on #8737
# proved the single-domain shape fatal: the workload wait passed while the
# gate still deferred on 2 cloud endpoints. Classifies in shell so the
# queue-lifecycle mapping is pinned by the stub cases, not hidden in SQL:
# succeeded is done; pending/claimed/running/retrying is open (retrying is
# transient -- the reclaim path re-drives stale rows through it, see the
# claim SQL pinned by claim_reclaim_test.go); superseded is ignored
# (benign-terminal: residual is status NOT IN ('succeeded','superseded'));
# failed/dead_letter, any unknown status, or any row for a domain outside
# the waited set fails fast. Every listed domain needs ok>=1 with none
# open; a domain with zero rows never passes, so a forgotten domain fails
# loudly instead of arming a hold on a half-committed graph.
ifa_uses_vault_policy_wait_for_endpoint_domains() {
	local cell="$1" budget="$2"
	shift 2
	[[ "${budget}" =~ ^[1-9][0-9]*$ ]] \
		|| { printf '%s: endpoint wait: budget must be a positive integer, got %s\n' "${cell}" "${budget}" >&2; return 1; }
	[[ "$#" -gt 0 ]] \
		|| { printf '%s: endpoint wait: no endpoint domains listed -- refusing to arm the hold on an unspecified graph\n' "${cell}" >&2; return 1; }
	local domain in_list=""
	for domain in "$@"; do
		[[ "${domain}" =~ ^[a-z_]+$ ]] \
			|| { printf '%s: endpoint wait: invalid endpoint domain %q\n' "${cell}" "${domain}" >&2; return 1; }
		in_list="${in_list}'${domain}',"
	done
	in_list="${in_list%,}"
	local i rows row rest d status count all_ready ready_report
	local -A rok ropen rbad
	for i in $(seq 1 "${budget}"); do
		rows="$(ifa_det_pg "${FAULT_COMPOSE_PROJECT}" "${use_compose}" "${ESHU_POSTGRES_DSN}" \
			"SELECT domain, status, count(*) FROM fact_work_items WHERE stage = 'reducer' AND domain IN (${in_list}) GROUP BY domain, status;" \
			"${compose_file}")" \
			|| { printf '%s: endpoint wait: endpoint-domain poll query failed\n' "${cell}" >&2; return 1; }
		rok=()
		ropen=()
		rbad=()
		# Domains and statuses carry no spaces, so unquoted word-splitting
		# walks one domain|status|count row at a time; an empty result walks
		# zero times and no domain below can pass.
		for row in ${rows}; do
			d="${row%%|*}"
			rest="${row#*|}"
			status="${rest%%|*}"
			count="${rest##*|}"
			case " $* " in
			*" ${d} "*) ;;
			*) printf '%s: endpoint wait: endpoint-domain poll returned a row for unexpected domain %q -- refusing to arm the hold on an unclassified queue state\n' "${cell}" "${d}" >&2; return 1 ;;
			esac
			[[ "${count}" =~ ^[0-9]+$ ]] \
				|| { printf '%s: endpoint wait: endpoint-domain poll returned non-numeric output %q\n' "${cell}" "${rows}" >&2; return 1; }
			case "${status}" in
			succeeded) rok["${d}"]=$((${rok["${d}"]:-0} + count)) ;;
			pending | claimed | running | retrying) ropen["${d}"]=$((${ropen["${d}"]:-0} + count)) ;;
			superseded) ;; # Benign-terminal: replaced by a newer row.
			failed | dead_letter) rbad["${d}"]=$((${rbad["${d}"]:-0} + count)) ;;
			*) printf '%s: endpoint wait: endpoint-domain poll returned unknown status %q -- refusing to arm the hold on an unclassified queue state\n' "${cell}" "${status}" >&2; return 1 ;;
			esac
		done
		all_ready=1
		ready_report=""
		for domain in "$@"; do
			[[ "${rbad["${domain}"]:-0}" -eq 0 ]] \
				|| { printf '%s: endpoint wait: %s %s row(s) terminally bad -- refusing to arm the hold on a half-committed graph\n' "${cell}" "${rbad["${domain}"]}" "${domain}" >&2; return 1; }
			if [[ "${ropen["${domain}"]:-0}" -gt 0 || "${rok["${domain}"]:-0}" -eq 0 ]]; then all_ready=0; else ready_report="${ready_report}${domain}=${rok["${domain}"]} "; fi
		done
		[[ "${all_ready}" -eq 0 ]] || { printf '%s: endpoint wait: endpoint domains committed (%snone open)\n' "${cell}" "${ready_report}"; return 0; }
		sleep 1
	done
	printf '%s: endpoint wait: endpoint domains %s never all committed within %ss -- the hold would never engage\n' "${cell}" "$*" "${budget}" >&2
	return 1
}

# ifa_uses_vault_policy_run_trust_chain_stage runs the trust-chain
# stage to a committed queue row and then STOPS its reducer: the stage is
# scoped to secrets_iam_trust_chain so it can never claim the projection
# row early, and stopping it before the lock frees its workers. The flag is
# on for this reducer so the follow-up writer is wired and the trust-chain
# handler enqueues the projection intent as its read models commit -- but
# this reducer never claims that row. The SIGTERM lands only after the
# trust-chain row reaches `succeeded` (see the split wait lib): read
# models banked does not mean the row acked.
ifa_uses_vault_policy_run_trust_chain_stage() {
	local cell="$1"
	local trust_reducer_pid
	ifa_det_start_bg "${log_dir}" "reducer-${cell}-trustchain" trust_reducer_pid \
		env "ESHU_REDUCER_SECRETS_IAM_GRAPH_PROJECTION_ENABLED=true" "ESHU_REDUCER_CLAIM_DOMAIN=secrets_iam_trust_chain" "${bin_dir}/eshu-reducer"
	ifa_uses_vault_policy_wait_for_read_models "${cell}" 300 \
		|| die "${cell}: trust-chain stage never committed read models"
	# Succeeded BEFORE SIGTERM: banked read models do not mean the row
	# acked -- killing the worker mid-ack strands it. Timeout fails the
	# cell loudly and never proceeds to kill.
	ifa_uses_vault_policy_wait_for_trust_chain_succeeded "${cell}" 300 \
		|| die "${cell}: trust-chain row never reached succeeded -- refusing to SIGTERM a live worker"
	kill "${trust_reducer_pid}" >/dev/null 2>&1 || true
	wait "${trust_reducer_pid}" 2>/dev/null || true
	ifa_det_untrack_bg_pid "${trust_reducer_pid}"
}

# ifa_uses_vault_policy_wait_for_hold_marker polls for the
# hold-statement-before-delegation marker file the tagged reducer's hold
# decorator writes the moment it engages the USES_VAULT_POLICY MERGE, before
# it blocks. Waiting for it (rather than sleeping a fixed delay after the
# claim) is what makes the kill non-vacuous: kill -9 lands only once the
# worker is provably parked inside the held statement. Fail-closed
# throughout: a bad budget, or budget exhaustion with no marker, all return
# non-zero.
ifa_uses_vault_policy_wait_for_hold_marker() {
	local cell="$1" script_path="$2" budget="$3"
	if [[ ! "${budget}" =~ ^[1-9][0-9]*$ ]]; then
		printf '%s: hold-marker wait: budget must be a positive integer, got %s\n' "${cell}" "${budget}" >&2
		return 1
	fi
	local marker="${script_path}.restart-sentinel.statement-held"
	local waited=0
	while [[ ! -f "${marker}" ]]; do
		if [[ "${waited}" -ge "${budget}" ]]; then
			printf '%s: hold-marker wait: no hold marker at %s within %ss -- the hold never engaged\n' "${cell}" "${marker}" "${budget}" >&2
			return 1
		fi
		sleep 1
		waited=$((waited + 1))
	done
	printf '%s: hold engaged: marker present at %s\n' "${cell}" "${marker}"
	return 0
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
#
# It also establishes baseline_uses_vault_policy_retried: the
# fault-free excess-ATTEMPT total for domain secrets_iam_graph_projection,
# which the kill cell must exceed to prove the replacement reducer
# re-executed THIS domain rather than merely draining another queued row.
# Attempts, not rows: this cassette creates one projection work item, so the
# row-count form saturates at 1 (see ifa_fault_count_retry_attempts).
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
	# Drain reducer is flag-on but UNSCOPED like the sibling kill/failgraphwrite
	# cells: run_drain_gate requires global residual=0, and a CLAIM_DOMAIN
	# here would leave every other domain's rows unclaimable by construction.
	ifa_det_start_bg "${log_dir}" "reducer-baseline_uses_vault_policy" reducer_pid \
		env "ESHU_REDUCER_SECRETS_IAM_GRAPH_PROJECTION_ENABLED=true" "${bin_dir}/eshu-reducer"
	run_drain_gate baseline_uses_vault_policy
	assert_no_dead_letters baseline_uses_vault_policy
	ifa_uses_vault_policy_assert "baseline_uses_vault_policy" "${bin_dir}" "${uses_vault_policy_expected_edges}" \
		|| die "baseline-uses-vault-policy: fault-free graph does not match the three-edge exact set"
	baseline_uses_vault_policy_retried="$(ifa_fault_count_retry_attempts "${FAULT_COMPOSE_PROJECT}" "${use_compose}" "${ESHU_POSTGRES_DSN}" "${compose_file}" "secrets_iam_graph_projection")" \
		|| die "baseline-uses-vault-policy: could not count the fault-free secrets_iam_graph_projection retry-attempt baseline"
	printf 'baseline-uses-vault-policy: fault-free secrets_iam_graph_projection retry-attempt baseline: %s\n' "${baseline_uses_vault_policy_retried}"
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

# cell_kill_uses_vault_policy is the per-statement hold kill/reclaim cell
# (#8539 seam (2)). It parks the projection worker inside the USES_VAULT_POLICY
# MERGE -- post-retract, post-prior-edge-write, genuinely mid-handler -- via
# the hold-statement-before-delegation fault, kills the worker with kill -9
# while held (lease-expiry reclaim path, the same recovery the authenticates
# table_lock kill exercises from the pre-retract fact-load window), then
# restarts unfaulted and requires the full three-edge set to converge with
# zero dead letters against the family baseline digest.
#
# The hold engages at the ExecuteGroup seam: the secrets writer's dispatch
# (go/internal/storage/cypher/cloud/aws/iam/secrets_graph_writer.go,
# writeBatched/dispatch) takes the GroupExecutor path whenever inner supports
# it, and go/cmd/reducer's reducerNeo4jExecutor always does
# (go/cmd/reducer/executor_adapters.go) -- so the live reducer wiring takes
# ExecuteGroup for this family's edge writes, not single-statement Execute.
# The Go unit proof covers both seams regardless.
cell_kill_uses_vault_policy() {
	local cell_start
	cell_start=$(date +%s)
	log "cell kill-statement-hold-uses-vault-policy: fresh stack"
	fresh_stack killusesvaultpolicy
	drive_all_cassettes killusesvaultpolicy
	ifa_uses_vault_policy_drive "killusesvaultpolicy" "${bin_dir}" "${uses_vault_policy_cassette}" "${drive_workers}" "${log_dir}" \
		|| die "kill-statement-hold-uses-vault-policy: eshu-ifa drive (uses-vault-policy family) failed"
	local projector_pid reducer_pid_before reducer_pid_after claimed_before hold_script marker_rc
	ifa_det_start_bg "${log_dir}" "projector-killusesvaultpolicy" projector_pid "${bin_dir}/eshu-projector"
	# Read models BEFORE the hold -- the shape that distinguishes the vault
	# pair from every single-stage direct family (same comment as the
	# baseline/failgraphwrite cells above and the authenticates kill cell).
	ifa_uses_vault_policy_run_trust_chain_stage "killusesvaultpolicy"
	# The projection intent is created by the trust-chain handler when its
	# read-model writes commit -- not by ingestion and not by the projector.
	# Wait for the row to exist (any status) BEFORE the sentinel goes down:
	# without the row there is nothing to hold in flight and nothing to
	# kill.
	ifa_fault_wait_for_claimed "${FAULT_COMPOSE_PROJECT}" "${use_compose}" "${ESHU_POSTGRES_DSN}" "${compose_file}" \
		"${CLAIMED_ROW_WAIT_TIMEOUT}" "secrets_iam_graph_projection" 1 >/dev/null \
		|| die "kill-statement-hold-uses-vault-policy: secrets_iam_graph_projection was never enqueued by the trust-chain stage"
	# Endpoint readiness BEFORE the hold -- the shape the authenticates
	# kill cell never needed: its rows reference no workload endpoints, so
	# its gate is trivially ready. The uses-vault-policy rows reference TWO
	# endpoint keyspaces: kubernetes workload UIDs (committed by the
	# kubernetes_workload_materialization domain) and cloud_resource_uid
	# values (committed by the aws_resource_materialization domain -- the
	# trust chain derives them from aws_iam_principal facts and the AWS
	# resource handler commits the same UIDs from aws_resource facts).
	# Both domains' rows sit unclaimed while every other reducer in this
	# cell is scoped elsewhere. A prereq-scoped reducer (endpoint domains
	# only, so the projection row can never be claimed early) runs until
	# BOTH endpoint domains drain. Only then is the hold armed: without
	# this stage the handler defers on secrets_iam_endpoint_not_ready
	# forever and the marker wait dies at budget with the hold never
	# engaged (CI shard 4/4 on #8737: workload-only drain passed while the
	# gate still deferred on 2 cloud endpoints).
	local endpoint_reducer_pid
	ifa_det_start_bg "${log_dir}" "reducer-killusesvaultpolicy-endpoints" endpoint_reducer_pid \
		env "ESHU_REDUCER_SECRETS_IAM_GRAPH_PROJECTION_ENABLED=true" "ESHU_REDUCER_CLAIM_DOMAINS=kubernetes_workload_materialization,aws_resource_materialization" "${bin_dir}/eshu-reducer"
	ifa_uses_vault_policy_wait_for_endpoint_domains "killusesvaultpolicy" 300 kubernetes_workload_materialization aws_resource_materialization \
		|| die "kill-statement-hold-uses-vault-policy: endpoint domains never committed"
	kill "${endpoint_reducer_pid}" >/dev/null 2>&1 || true
	wait "${endpoint_reducer_pid}" 2>/dev/null || true
	ifa_det_untrack_bg_pid "${endpoint_reducer_pid}"
	# The hold script plus the sentinel it blocks on. Order matters: the
	# sentinel must exist before the tagged reducer starts, or the first
	# matching write would engage-and-immediately-release before any
	# observer could rendezvous on it.
	hold_script="${work_dir}/fault-hold-uses-vault-policy.json"
	ifa_fault_write_hold_script "${hold_script}" "${uses_vault_policy_edge_operation_match}" \
		|| die "kill-statement-hold-uses-vault-policy: could not write the hold fault script"
	: > "${hold_script}.restart-sentinel" \
		|| die "kill-statement-hold-uses-vault-policy: could not create the hold sentinel"
	# Scoped to secrets_iam_graph_projection only: the claim fires because
	# the read models were banked above, and no other pending domain can
	# starve this domain of a worker (same reasoning as the authenticates
	# kill cell).
	ifa_det_start_bg "${log_dir}" "reducer-killusesvaultpolicy-before" reducer_pid_before \
		env "ESHU_REDUCER_SECRETS_IAM_GRAPH_PROJECTION_ENABLED=true" "ESHU_REDUCER_CLAIM_DOMAIN=secrets_iam_graph_projection" "ESHU_IFA_FAULT_SCRIPT=${hold_script}" "${tagged_bin_dir}/eshu-reducer"
	claimed_before="$(ifa_fault_wait_for_claimed "${FAULT_COMPOSE_PROJECT}" "${use_compose}" "${ESHU_POSTGRES_DSN}" "${compose_file}" "${CLAIMED_ROW_WAIT_TIMEOUT}" "secrets_iam_graph_projection")" \
		|| die "kill-statement-hold-uses-vault-policy: no secrets_iam_graph_projection row was claimed while its write was held"
	printf 'kill-statement-hold-uses-vault-policy: non-vacuous: %s blocked claimed/running row(s) observed\n' "${claimed_before}"
	# The hold rendezvous: the marker names the exact USES_VAULT_POLICY
	# MERGE the held worker engaged, written synchronously before it
	# blocked. kill -9 lands only after this wait succeeds -- killing on the
	# claim alone could land before the handler reaches the held write.
	ifa_uses_vault_policy_wait_for_hold_marker "killusesvaultpolicy" "${hold_script}" "${CLAIMED_ROW_WAIT_TIMEOUT}" \
		|| die "kill-statement-hold-uses-vault-policy: the hold never engaged, so this cell proved nothing about SECRETS_IAM_USES_VAULT_POLICY kill/reclaim"
	marker_rc=0
	ifa_fault_assert_hold_marker "${hold_script}" "${uses_vault_policy_edge_operation_match}" || marker_rc=$?
	if [[ "${marker_rc}" -eq 2 ]]; then
		# Distinguished from "no marker" deliberately, per #5974: rc=2 means
		# the hold ENGAGED but on a different write than the anchor names.
		# Collapsing that into a generic failure sends the next reader
		# looking at the decorator instead of at the anchor.
		die "kill-statement-hold-uses-vault-policy: the hold ENGAGED but on a different write than ${uses_vault_policy_edge_operation_match} (marker contents above). The injection works; the anchor is pointed at the wrong statement."
	fi
	[[ "${marker_rc}" -eq 0 ]] \
		|| die "kill-statement-hold-uses-vault-policy: hold marker assertion failed (marker status ${marker_rc})"
	# Announced, not merely checked, like the failgraphwrite cell's marker
	# line above: a passing run must say what fired.
	printf 'kill-statement-hold-uses-vault-policy: non-vacuous: hold marker names the targeted SECRETS_IAM_USES_VAULT_POLICY MERGE (written by the fault decorator at engage time, not read from the reducer log)\n'
	kill -9 "${reducer_pid_before}" >/dev/null 2>&1 || true
	rm -f "${hold_script}.restart-sentinel"
	# Restart UNFAULTED: the replacement reducer runs the untagged binary
	# with no fault script, so the reclaim re-executes retract plus all five
	# edge sets with no hold in the way -- recovery, not a second fault.
	ifa_det_start_bg "${log_dir}" "reducer-killusesvaultpolicy-after" reducer_pid_after \
		env "ESHU_REDUCER_SECRETS_IAM_GRAPH_PROJECTION_ENABLED=true" "${bin_dir}/eshu-reducer"
	run_drain_gate killusesvaultpolicy
	assert_no_dead_letters killusesvaultpolicy
	ifa_uses_vault_policy_assert "killusesvaultpolicy" "${bin_dir}" "${uses_vault_policy_expected_edges}" \
		|| die "kill-statement-hold-uses-vault-policy: recovered graph does not match the three-edge exact set"
	ifa_fault_assert_retry_attempts_above "${FAULT_COMPOSE_PROJECT}" "${use_compose}" "${ESHU_POSTGRES_DSN}" "${compose_file}" \
		"${baseline_uses_vault_policy_retried}" 15 "secrets_iam_graph_projection" \
		|| die "kill-statement-hold-uses-vault-policy: secrets_iam_graph_projection did not re-execute above its fault-free retry-attempt baseline"
	capture_digest killusesvaultpolicy
	assert_matches_baseline killusesvaultpolicy baseline_uses_vault_policy
	teardown_cell killusesvaultpolicy
	wall_times[killusesvaultpolicy]=$(( $(date +%s) - cell_start ))
	printf 'kill-statement-hold-uses-vault-policy: cell wall time: %ss\n' "${wall_times[killusesvaultpolicy]}"
}
