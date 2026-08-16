#!/usr/bin/env bash
# deployable_unit_edges live-proof helpers (#5993). This file is sourced by
# verify-ifa-determinism.sh; the caller owns strict mode, logging, and cleanup.
#
# UNLIKE ifa_det_run_sql_delta_live (ifa_sql_delta_live.sh), this family's
# live proof is designed to run as its OWN standalone cell, ONCE, beside the
# shared N={1,2,4} worker-count digest loop -- NOT folded into it. Two reasons:
#
#   1. The mechanism this family needs (a bootstrap-index deferred-maintenance
#      pass) is not something any other family's live cell exercises, and
#      running it inside the shared N-loop would move every N-cell's digest
#      terminal for a reason that has nothing to do with what those cells
#      test (worker-count invariance for the demo-org/synth/SQL/code-call
#      graph). An own-cell design leaves every existing cell's digest
#      terminal untouched.
#   2. Unlike sql_relationships' delta proof (a second generation deposited
#      into an ALREADY-converged cell), this family's first drain is expected
#      to legitimately produce ZERO edges -- see
#      ifa_deployable_unit_live_assert_empty_before_maintenance below. Folding
#      that into a loop whose every other assertion expects non-empty,
#      converged output invites a future reader to "fix" the empty result as
#      a regression.
#
# WHY A MAINTENANCE PASS IS REQUIRED AT ALL (read before changing this file).
# Traced read-only against origin/main as of #5993: neither
# scripts/verify-ifa-determinism.sh nor scripts/verify-ifa-fault-injection.sh
# builds or runs eshu-ingester or eshu-bootstrap-index; both build exactly
# bootstrap-data-plane, ifa, projector, reducer, golden-corpus-gate. Without
# bootstrap-index in that set, TWO things this family depends on never
# happen in these gates today:
#
#   - CrossRepoRelationshipHandler.Resolve (go/internal/reducer/
#     cross_repo_resolution.go), which deployment_mapping's handler calls,
#     gates itself on a GraphProjectionPhaseBackwardEvidenceCommitted
#     readiness row. As of #6136, the ONLY writer of that row is
#     publishDeferredBackfillPartition (go/internal/storage/postgres/
#     ingestion_backfill_pool.go) -- writeDeferredBackfillBatch (same
#     package, ingestion_backfill.go) deliberately publishes NOTHING
#     partition-wide any more; it only persists evidence and returns the
#     partitions it contributed to, and the caller's separate fan-in step
#     (publishDeferredBackfillPartition, re-reading the active generation
#     under a fresh lock) publishes readiness once every batch of the pass
#     has committed. Still reachable only through
#     BackfillAllRelationshipEvidence, called only from cmd/ingester and
#     cmd/bootstrap-index. eshu-reducer's own per-commit backfill path
#     (runPostCommitRelationshipBackfill, ingestion.go) persists evidence but
#     publishes ZERO phase rows. Without the maintenance pass, resolution is
#     gated shut EVERY time, deterministically -- not a race, a permanently
#     closed gate.
#   - deployable_unit_correlation has "no readiness retry of its own" (see
#     the doc comment on crossScopeCorrelationReopenDomains,
#     go/internal/storage/postgres/ingestion_reopen_correlation.go:43-46,
#     quoted here so this file's rationale does not silently drift from that
#     comment): its first pass, before resolution has committed anything,
#     legitimately correlates nothing. It needs to be REOPENED (marked
#     pending again) once RelDeploysFrom resolved relationships exist --
#     the reopen only fires from RunDeferredRelationshipMaintenance
#     (ingester) or bootstrap-index's own maintenance phase.
#
# scripts/lib/golden-corpus-maintenance-drains.sh's run_maintenance_drain_cycles
# hit this exact gap for the B-7 gate and fixed it by adding an
# eshu-bootstrap-index maintenance pass; its own header comment documents the
# prior false green in plain words ("the correlation reopen had only the
# bootstrap caller... these assertions passed while normal ingestion replayed
# nothing"). This file is the same fix applied to the Ifá live gates, which
# never got that leg. bootstrap-index's maintenance phase does BOTH things a
# single bare invocation needs: it backfills relationship evidence (which also
# publishes the readiness row) AND reopens crossScopeCorrelationReopenDomains
# (including deployable_unit_correlation) in the same pass, per
# RunDeferredRelationshipMaintenance's own doc comment.
#
# RESOLVED (first live run, #5993): a truly bare invocation is not the right
# shape. bootstrap-index's collector defaults to the GitHub App auth path
# (go/internal/collector/git_selection_github.go's mintGitHubAppToken) unless
# told otherwise, and this cell has neither those credentials nor any repo
# source it wants collected -- the facts it needs already landed via `ifa
# drive`. scripts/verify-golden-corpus-gate.sh (lines ~196-212 as of #5993)
# gets a credential-free run by exporting a filesystem-mode collector
# configuration; ifa_deployable_unit_live_run_maintenance_pass below applies
# the same block, scoped to this one command via `env` (not `export`) so it
# cannot leak into `eshu-ifa drive`, the reducer, or the projector sharing
# this shell. The filesystem root is a FRESH, EMPTY scratch directory, never
# B-7's populated corpus_dir: reusing that would inject 20 unrelated repos
# into a cassette-shaped cell and move every digest, silently invalidating
# the determinism/fault-injection comparisons this cell exists to protect.
# Collection over an empty root discovers zero repos and no-ops, so the
# pipeline proceeds straight to the deferred-maintenance leg, which is
# scope-agnostic: BackfillAllRelationshipEvidence walks the repo generations
# already in Postgres (the ifa-driven ones), publishing their readiness rows,
# and the reopen replays the correlation domains. Production bootstrap-index
# also only ever collects what its configured source has, so a zero-repo
# collection is an honest degenerate case, not a trick.
#
# STILL UNPROVEN (the successor unknown, not resolved by reading code): that
# zero-repo filesystem collection is inert rather than actively harmful --
# specifically, that no reconcile path reads "a repo generation present in
# Postgres but absent from an empty collection source" as a removal and
# retracts the cassette-deposited facts this cell depends on. Judged unlikely
# (reconciliation is scoped to the collector's own sync scope, and an
# empty-root first run has no prior generations of its own to reconcile
# against), but not proven. This cell's own tripwires cover it: the
# post-maintenance drain's exact-set assertion and the fault-injection
# baseline's whole-graph digest would both fail loudly if the pass retracted
# anything.

# ifa_deployable_unit_live_drive replays the family cassette (all four
# repositories: the admitted app+deploy pair and the two negative-case
# repositories) into the current stack at -workers 1. This family's live
# proof is not a worker-count invariance test (that is what the shared N-loop
# proves for the other families); one worker is sufficient and keeps the
# ordering within this cell's own drain simple to reason about.
ifa_deployable_unit_live_drive() {
	local bin_dir="$1" cassette="$2" log_dir="$3"
	printf '\n=== deployable_unit_edges: drive family cassette through eshu-ifa drive -workers 1 ===\n'
	if ! "${bin_dir}/eshu-ifa" drive -cassette "${cassette}" -workers 1 \
		>"${log_dir}/ifa-drive-deployable-unit.log" 2>&1; then
		tail -40 "${log_dir}/ifa-drive-deployable-unit.log" >&2 || true
		echo "deployable_unit_edges: eshu-ifa drive failed" >&2
		return 1
	fi
	cat "${log_dir}/ifa-drive-deployable-unit.log"
}

# ifa_deployable_unit_live_drain_retry adapts ifa_deployable_unit_live_drain
# to the drain_cmd calling convention ifa_deployable_unit_live_converge_edges
# invokes ("$@" plus one appended pass-label argument, see that function's doc
# comment): it turns the appended per-retry pass label into a UNIQUE drain
# label ("post-${pass_label}") instead of the constant "post"
# ifa_deployable_unit_live_run_standalone_cell used before this existed. Only
# the standalone determinism-gate cell needs this -- the fault-injection
# cells pass run_drain_gate, which polls an already-running projector/reducer
# rather than starting fresh ones per retry, so it never truncates a log
# (see this function's own header two functions below for the truncation this
# fixes).
ifa_deployable_unit_live_drain_retry() {
	local bin_dir="$1" log_dir="$2" drain_timeout="$3" pass_label="$4"
	ifa_deployable_unit_live_drain "post-${pass_label}" "${bin_dir}" "${log_dir}" "${drain_timeout}"
}

# ifa_deployable_unit_live_drain runs projector + reducer in the background
# and polls the gate to the B-12 residual bound, exactly like every other
# Ifá live cell's drain step. label distinguishes the primary drain (before
# the maintenance pass) from the post-maintenance drain in the logs.
#
# label MUST be unique per invocation within a cell: ifa_det_start_bg opens
# its log file with `>` (truncate), so calling this twice with the same label
# -- e.g. the constant "post" on every convergence retry, the bug this
# comment now documents -- overwrites reducer-deployable-unit-${label}.log /
# projector-deployable-unit-${label}.log each time. On failure only the LAST
# call's log survives; the pass where the family should have converged is
# gone. ifa_deployable_unit_live_drain_retry above is how convergence retries
# get a unique label instead of reusing this one.
ifa_deployable_unit_live_drain() {
	local label="$1" bin_dir="$2" log_dir="$3" drain_timeout="$4"
	local projector_pid reducer_pid
	printf '\n=== deployable_unit_edges (%s): drain projector + reducer ===\n' "${label}"
	ifa_det_start_bg "${log_dir}" "projector-deployable-unit-${label}" projector_pid "${bin_dir}/eshu-projector"
	ifa_det_start_bg "${log_dir}" "reducer-deployable-unit-${label}" reducer_pid "${bin_dir}/eshu-reducer"
	if ! "${bin_dir}/eshu-golden-corpus-gate" \
		-phase=drains \
		-snapshot=testdata/golden/e2e-20repo-snapshot.json \
		-drain-timeout="${drain_timeout}"; then
		tail -30 "${log_dir}/reducer-deployable-unit-${label}.log" || true
		tail -30 "${log_dir}/projector-deployable-unit-${label}.log" || true
		echo "deployable_unit_edges (${label}): drain did not reach the snapshot's residual bound within ${drain_timeout}" >&2
		return 1
	fi
	kill "${projector_pid}" "${reducer_pid}" >/dev/null 2>&1 || true
}

# ifa_deployable_unit_live_assert_empty_before_maintenance proves the
# documented first-pass state explicitly: BEFORE any maintenance pass runs,
# the live graph must carry ZERO CORRELATES_DEPLOYABLE_UNIT edges, because
# CrossRepoRelationshipHandler.Resolve is gated shut (see this file's header)
# and deployable_unit_correlation's first pass has no resolved relationship to
# read. Asserting this is not optional scaffolding -- it is the non-vacuity
# proof that the maintenance pass which follows is what actually opens the
# gate, not some unrelated ordering accident.
#
# THIS FUNCTION USED TO COUNT shared_projection_intents ROWS FOR THIS DOMAIN.
# That check was vacuous, not merely redundant, and it is why this comment
# now spells out the trace instead of asserting a one-line claim about it.
# DeployableUnitCorrelationHandler.Handle (go/internal/reducer/
# deployable_unit_correlation.go) writes, in order: (1) the graph edge
# (materializeDeployableUnitEdges -> EdgeWriter.WriteEdges, a Cypher MERGE),
# (2) admission_decisions + admission_decision_evidence in Postgres
# (writeDeployableUnitAdmissionDecisions), then (3) graph_projection_phase_state
# (publishIntentGraphPhase). It has NO IntentWriter field and never calls
# UpsertIntents. DomainDeployableUnitEdges is also absent from
# sharedProjectionDomains (go/internal/reducer/shared_projection_runner.go);
# the only `INSERT INTO shared_projection_intents` in the repository is
# go/internal/storage/postgres/shared_intents_upsert.go, reachable only
# through UpsertIntents. This family therefore NEVER writes a row to
# shared_projection_intents -- a count for projection_domain =
# 'deployable_unit_edges' is always 0, before the maintenance pass, after it,
# admitted or rejected. The old version of this function called that count
# "the right non-vacuity check" on the premise that "any row present for this
# domain is, by construction, an admitted row" -- inverted: no row is EVER
# present, by construction, so the old assertion could never fail and proved
# nothing about whether the readiness gate was shut.
#
# This now asserts against the GRAPH instead, the only place these edges
# actually exist. `ifa assert-edges` itself rejects an empty expected-edge
# fixture as vacuous for the identical reason (a check that can only pass
# proves nothing) -- it has no "expect empty" mode, and adding one would
# weaken that vacuity rejection for every other family's live cell, so this
# uses a direct graph-dump edge count instead. It dumps the live graph with
# `eshu-ifa graph-dump -out` (the same read path capture_digest,
# ifa_fault_injection_driver.sh, uses) to a scratch file and counts
# CORRELATES_DEPLOYABLE_UNIT edges with jq.
#
# Args: bin_dir
#
# Same fail-closed shape as the query-based version it replaces, extended by
# one more distinct failure mode (jq missing) the old version did not need:
# jq's presence, then the dump command's own exit status, then whether jq's
# count computation itself succeeded, then whether the result is numeric, and
# only once all four pass does it compare to the literal string "0". A dump
# or count that never ran must never silently read as "0" -- that would blame
# the readiness gate for a graph-dump/jq hiccup that has nothing to do with
# it.
ifa_deployable_unit_live_assert_empty_before_maintenance() {
	local bin_dir="$1"
	local dump_path count
	printf '\n=== deployable_unit_edges: assert zero CORRELATES_DEPLOYABLE_UNIT edges before the maintenance pass ===\n'
	if ! command -v jq >/dev/null 2>&1; then
		echo "deployable_unit_edges: before-maintenance precondition requires jq, which is not on PATH; treat this as unknown, not as a verdict" >&2
		return 1
	fi
	dump_path="$(mktemp)" || {
		echo "deployable_unit_edges: before-maintenance precondition could not create a scratch file for the graph dump; treat this as unknown, not as a verdict" >&2
		return 1
	}
	trap 'rm -f "${dump_path}"' RETURN
	if ! "${bin_dir}/eshu-ifa" graph-dump -out "${dump_path}"; then
		echo "deployable_unit_edges: before-maintenance precondition graph-dump FAILED; treat this as unknown, not as a verdict" >&2
		return 1
	fi
	if ! count="$(jq '[.edges[] | select(.type == "CORRELATES_DEPLOYABLE_UNIT")] | length' "${dump_path}")"; then
		echo "deployable_unit_edges: before-maintenance precondition could not count CORRELATES_DEPLOYABLE_UNIT edges in the graph dump; treat this as unknown, not as a verdict" >&2
		return 1
	fi
	count="$(printf '%s' "${count}" | tr -d '[:space:]')"
	if [[ -z "${count}" ]]; then
		echo "deployable_unit_edges: before-maintenance precondition edge count came back empty; treat this as unknown, not as zero" >&2
		return 1
	fi
	if [[ ! "${count}" =~ ^[0-9]+$ ]]; then
		echo "deployable_unit_edges: before-maintenance precondition edge count '${count}' is non-numeric; treat this as unknown, not as zero" >&2
		return 1
	fi
	if [[ "${count}" != "0" ]]; then
		echo "deployable_unit_edges: expected 0 CORRELATES_DEPLOYABLE_UNIT edges before the maintenance pass, got ${count} -- either the readiness gate is not actually closed in this runtime (re-verify this file's header claim before trusting the rest of this cell), or a prior cell's state leaked into this one" >&2
		return 1
	fi
	printf 'deployable_unit_edges: confirmed 0 CORRELATES_DEPLOYABLE_UNIT edges before the maintenance pass (the readiness gate is closed, as documented)\n'
}

# ifa_deployable_unit_live_assert_readiness_opened is the exact inverse of
# ifa_deployable_unit_live_assert_empty_before_maintenance: it reads the
# reducer log from the POST-maintenance drain and confirms
# CrossRepoRelationshipHandler.Resolve actually ran the readiness-gated
# branch open, rather than inferring it from a downstream edge count alone.
# The two source log lines (go/internal/reducer/cross_repo_resolution.go)
# are "cross-repo relationship resolution started" (always logged on entry)
# and "cross-repo resolution gated" (logged only when the readiness check
# fails and the handler returns early without resolving anything). Before
# the maintenance pass this reducer log would show the started line paired
# with the gated line on every attempt; after it, the started line must
# appear with no paired gated line. Having both -- the shut-gate line from a
# pre-maintenance drain and this open-gate confirmation from the
# post-maintenance drain -- is the complete before/after proof of the
# mechanism, not just a downstream inference from edge counts.
#
# Bash substring match, NOT rg -- #5974 proved rg is absent on the
# fault-injection CI runner: a check built on it exits "command not found",
# which reads exactly like a negative match, and a mechanism that fired
# correctly gets reported as broken. This function is called from both
# gates, so it follows ifa_fault_assert_once_fault_marker's fix rather than
# reintroducing the same failure mode in a new place. The fan-in-skip check
# added below (readiness-review follow-up, #5993 item 3) follows the SAME
# constraint: it is a bash substring comparison, not another `rg` call.
#
# The "gated" branch below also names one otherwise-silent cause: the
# deferred-backfill fan-in (publishDeferredBackfillPartition,
# go/internal/storage/postgres/ingestion_backfill_pool.go -- NOT
# writeDeferredBackfillBatch, ingestion_backfill.go, which as of #6136
# deliberately publishes nothing partition-wide) can decline to publish a
# partition and still exit zero, logging
# deferred_backfill_fanin_partition_skipped=true with a
# reason (e.g. "generation_advanced_since_batch") instead of an error. When
# that fires, the readiness row this family depends on never gets published,
# CrossRepoRelationshipHandler.Resolve still logs "started" (it always does
# on entry) then finds the readiness check false and logs "gated" -- from
# this reducer log ALONE that is indistinguishable from the designed
# pre-maintenance-gate case. Low probability in this cell (the maintenance
# pass collects zero repos), but non-zero if a projector or reducer from a
# drain phase commits during the pass. Checking the bootstrap-index
# maintenance-pass log (not the reducer log) for that marker turns a mystery
# into a named cause instead of leaving the "did not open the readiness
# gate" message to mean two different things.
#
# Args: log_dir label pass_label (the reducer log is
# ${log_dir}/${label}.log; the bootstrap-index maintenance-pass log this
# checks for the fan-in-skip marker is
# ${log_dir}/bootstrap-index-deployable-unit-${pass_label}.log, matching
# ifa_deployable_unit_live_run_maintenance_pass's own naming below)
ifa_deployable_unit_live_assert_readiness_opened() {
	local log_dir="$1" label="$2" pass_label="$3"
	local reducer_log="${log_dir}/${label}.log"
	local contents
	[[ -f "${reducer_log}" ]] || {
		echo "deployable_unit_edges: readiness-opened check: missing reducer log ${reducer_log}" >&2
		return 1
	}
	contents="$(cat "${reducer_log}")" || {
		echo "deployable_unit_edges: readiness-opened check: could not read ${reducer_log}; treat this as unknown, not as a verdict" >&2
		return 1
	}
	if [[ "${contents}" != *"cross-repo relationship resolution started"* ]]; then
		echo "deployable_unit_edges: readiness-opened check: post-maintenance reducer log never logged cross-repo relationship resolution started -- CrossRepoRelationshipHandler.Resolve may not have run at all" >&2
		return 1
	fi
	if [[ "${contents}" == *"cross-repo resolution gated"* ]]; then
		local bootstrap_index_log="${log_dir}/bootstrap-index-deployable-unit-${pass_label}.log"
		local bootstrap_contents="" fanin_note=""
		if [[ -f "${bootstrap_index_log}" ]]; then
			bootstrap_contents="$(cat "${bootstrap_index_log}")" || bootstrap_contents=""
		fi
		if [[ "${bootstrap_contents}" == *"deferred_backfill_fanin_partition_skipped=true"* ]]; then
			fanin_note=" -- the bootstrap-index maintenance-pass log (${bootstrap_index_log}) shows the deferred-backfill fan-in declined to publish this partition (deferred_backfill_fanin_partition_skipped=true), which is why readiness never opened, not because resolution ran and genuinely found nothing"
		fi
		echo "deployable_unit_edges: readiness-opened check: post-maintenance reducer log still shows cross-repo resolution gated -- the maintenance pass did not open the readiness gate${fanin_note}" >&2
		return 1
	fi
	printf 'deployable_unit_edges: confirmed cross-repo relationship resolution started with no gated line after the maintenance pass (the readiness gate is open)\n'
}

# ifa_deployable_unit_live_run_maintenance_pass runs ONE bootstrap-index
# maintenance pass -- not the B-7 gate's three cycles. This family's
# dependency chain is two links deep (deployment_mapping's cross-repo
# resolution, then deployable_unit_correlation's reopen), matching
# golden-corpus-maintenance-drains.sh's own account of what its first
# maintenance cycle produces and what its second consumes for this exact
# family ("Cycle 1's drain produces the resolved DEPLOYS_FROM relationships
# ... cycle 2's drain re-runs deployable_unit_correlation now that the
# resolved relationships it consumes exist").
#
# The invocation is credential-free filesystem-mode collection over a fresh,
# empty scratch root (see this file's header) rather than a truly bare call:
# without ESHU_REPO_SOURCE_MODE=filesystem the collector defaults to the
# GitHub App auth path and fails closed with no credentials configured. The
# env vars are scoped to this one command via `env`, not `export`, so they
# cannot leak into `eshu-ifa drive` or the reducer/projector processes
# sharing this shell across the other cells in the same gate run.
ifa_deployable_unit_live_run_maintenance_pass() {
	local pass_label="$1" bin_dir="$2" log_dir="$3"
	local scratch_root scratch_repos_dir
	printf '\n=== deployable_unit_edges: bootstrap-index maintenance pass (%s) ===\n' "${pass_label}"
	scratch_root="$(mktemp -d)"
	scratch_repos_dir="$(mktemp -d)"
	if ! env \
		ESHU_REPO_SOURCE_MODE="filesystem" \
		ESHU_FILESYSTEM_DIRECT="false" \
		ESHU_FILESYSTEM_ROOT="${scratch_root}" \
		ESHU_REPOS_DIR="${scratch_repos_dir}" \
		ESHU_GIT_AUTH_METHOD="none" \
		ESHU_GITHUB_ORG="acme" \
		ESHU_REPOSITORY_RULES_JSON="[]" \
		"${bin_dir}/eshu-bootstrap-index" >"${log_dir}/bootstrap-index-deployable-unit-${pass_label}.log" 2>&1; then
		tail -40 "${log_dir}/bootstrap-index-deployable-unit-${pass_label}.log" >&2 || true
		echo "deployable_unit_edges: bootstrap-index maintenance pass (${pass_label}) failed" >&2
		rm -rf "${scratch_root}" "${scratch_repos_dir}"
		return 1
	fi
	cat "${log_dir}/bootstrap-index-deployable-unit-${pass_label}.log"
	rm -rf "${scratch_root}" "${scratch_repos_dir}"
}

# ifa_deployable_unit_live_converge_bound and
# ifa_deployable_unit_live_converge_edges (the convergence-retry loop this
# file's standalone cell below uses) now live in
# ifa_deployable_unit_live_converge.sh, split out to keep this file under the
# repository's 500-line cap. Sourced alongside this file wherever it is
# sourced (verify-ifa-determinism.sh, verify-ifa-fault-injection.sh), after
# this file since it calls ifa_deployable_unit_live_run_maintenance_pass and
# ifa_deployable_unit_live_assert defined below.

# ifa_deployable_unit_live_assert asserts the family's expected-edge-set
# fixture as an exact set against the live graph, exactly like every other
# family's assert-edges call.
ifa_deployable_unit_live_assert() {
	local bin_dir="$1" expected_edges="$2"
	printf '\n=== deployable_unit_edges: assert materialized edges (one-edge exact set) ===\n'
	"${bin_dir}/eshu-ifa" assert-edges \
		-domain deployable_unit_edges \
		-expected "${expected_edges}"
}

# ifa_deployable_unit_live_run_standalone_cell runs this family's full
# standalone determinism-gate cell: own compose lifecycle (up, wait for
# backends, apply schema) -> drive -> pre-maintenance drain + empty-edges
# assertion -> ONE bootstrap-index maintenance pass -> post-maintenance
# drain -> exact-set assert -> teardown. Extracted into one function (rather
# than left inline in verify-ifa-determinism.sh) so that script stays under
# the repo's 500-line file cap. Uses the caller's own log()/die() directly --
# this function is sourced into the same shell as verify-ifa-determinism.sh,
# same as every other function in this file -- except this one returns
# non-zero on failure instead of calling die() itself, so the caller's own
# die() message names the failing script, matching the ifa_det_run_sql_delta_live
# / ifa_code_call_drive convention of "library functions return status,
# callers decide whether to die".
#
# Args: bin_dir cassette expected_edges log_dir compose_project use_compose
#       postgres_dsn compose_file drain_timeout
ifa_deployable_unit_live_run_standalone_cell() {
	local bin_dir="$1" cassette="$2" expected_edges="$3" log_dir="$4"
	local compose_project="$5" use_compose="$6" postgres_dsn="$7"
	local compose_file="$8" drain_timeout="$9"
	local cell_start

	log "deployable_unit_edges: standalone live-proof cell (own fresh stack, not part of the N-loop)"
	cell_start=$(date +%s)

	if [[ "${use_compose}" -eq 1 ]]; then
		docker compose -p "${compose_project}" -f "${compose_file}" up -d nornicdb postgres
		log "deployable_unit_edges: wait for backends"
		ifa_det_wait_for_backends "${compose_project}" "${compose_file}" || {
			echo "deployable_unit_edges: Postgres + NornicDB did not become ready within budget" >&2
			return 1
		}
	fi

	log "deployable_unit_edges: apply Postgres + graph schema (eshu-bootstrap-data-plane)"
	"${bin_dir}/eshu-bootstrap-data-plane" >"${log_dir}/bootstrap-data-plane-deployable-unit.log" 2>&1 || {
		tail -40 "${log_dir}/bootstrap-data-plane-deployable-unit.log"
		echo "deployable_unit_edges: bootstrap-data-plane failed" >&2
		return 1
	}

	ifa_deployable_unit_live_drive "${bin_dir}" "${cassette}" "${log_dir}" || return 1

	ifa_deployable_unit_live_drain pre "${bin_dir}" "${log_dir}" "${drain_timeout}" || return 1
	ifa_deployable_unit_live_assert_empty_before_maintenance "${bin_dir}" || return 1

	ifa_deployable_unit_live_run_maintenance_pass primary "${bin_dir}" "${log_dir}" || return 1

	ifa_deployable_unit_live_drain post "${bin_dir}" "${log_dir}" "${drain_timeout}" || return 1
	ifa_deployable_unit_live_assert_readiness_opened "${log_dir}" "reducer-deployable-unit-post" "primary" || return 1
	ifa_deployable_unit_live_report_intents_after_maintenance "${compose_project}" "${use_compose}" "${postgres_dsn}" "${compose_file}"
	ifa_deployable_unit_live_report_resolved_deploys_from_count "${compose_project}" "${use_compose}" "${postgres_dsn}" "${compose_file}"
	ifa_deployable_unit_live_report_correlation_reopen "${log_dir}" "primary"

	if ! ifa_deployable_unit_live_assert "${bin_dir}" "${expected_edges}"; then
		local converge_rc=0
		ifa_deployable_unit_live_converge_edges "primary" "${bin_dir}" "${log_dir}" "${expected_edges}" \
			ifa_deployable_unit_live_drain_retry "${bin_dir}" "${log_dir}" "${drain_timeout}" || converge_rc=$?
		case "${converge_rc}" in
		0) ;;
		2)
			echo "deployable_unit_edges: standalone cell: a maintenance-pass convergence retry crashed (bootstrap-index itself failed), not an eventual-consistency timeout" >&2
			return 1
			;;
		3)
			echo "deployable_unit_edges: standalone cell: a maintenance-pass convergence retry's drain failed, not an eventual-consistency timeout" >&2
			return 1
			;;
		*)
			echo "deployable_unit_edges: standalone cell: deployable_unit_edges did not converge within the maintenance-pass convergence bound" >&2
			return 1
			;;
		esac
	fi

	if [[ "${use_compose}" -eq 1 ]]; then
		log "deployable_unit_edges: tear down cell"
		docker compose -p "${compose_project}" -f "${compose_file}" down -v >/dev/null 2>&1 || true
	fi

	printf 'deployable_unit_edges: standalone cell wall time: %ss\n' "$(( $(date +%s) - cell_start ))"
}
