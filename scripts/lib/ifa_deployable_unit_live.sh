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
#     readiness row. The ONLY writer of that row is
#     writeDeferredBackfillBatch (go/internal/storage/postgres/
#     ingestion_backfill.go), reachable only through
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
# ONE HONEST UNKNOWN, not resolved by reading code: whether a bare
# `eshu-bootstrap-index` invocation (no repo source configured) runs clean
# against a stack whose facts arrived via `eshu-ifa drive` rather than
# bootstrap-index's own collection phase. verify-golden-corpus-gate.sh calls
# it bare for both its FIRST run (against an empty DB, where it also does its
# own filesystem collection) and every subsequent maintenance pass (against a
# DB bootstrap-index itself already populated) -- this file's use is the
# untested third shape: bare invocation against a DB populated by a DIFFERENT
# committer (`ifa drive`). This must be proven the first time this cell
# actually runs, not assumed here.

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

# ifa_deployable_unit_live_drain runs projector + reducer in the background
# and polls the gate to the B-12 residual bound, exactly like every other
# Ifá live cell's drain step. label distinguishes the primary drain (before
# the maintenance pass) from the post-maintenance drain in the logs.
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
# deployable_unit_edges must materialize ZERO edges, because
# CrossRepoRelationshipHandler.Resolve is gated shut (see this file's header)
# and deployable_unit_correlation's first pass has no resolved relationship to
# read. Asserting this is not optional scaffolding -- it is the non-vacuity
# proof that the maintenance pass which follows is what actually opens the
# gate, not some unrelated ordering accident.
#
# materializeDeployableUnitEdges (go/internal/reducer/
# deployable_unit_correlation_edges.go) only ever calls EdgeWriter.WriteEdges
# with AdmittedDeployableUnitRows(rows) -- the rejected and blank-
# deployment_repo_id rows this family's Odu also carries never reach
# WriteEdges, and therefore never reach the shared_projection_intents queue
# table at all. So a plain per-domain row count is already the right
# non-vacuity check: any row present for this domain is, by construction, an
# admitted row carrying a deployment_repo_id. `ifa assert-edges` itself
# rejects an empty expected-edge fixture as vacuous, which is why this uses a
# direct Postgres count instead of the assert-edges verb for the BEFORE state.
#
# Args: compose_project use_compose dsn compose_file
ifa_deployable_unit_live_assert_empty_before_maintenance() {
	local compose_project="$1" use_compose="$2" dsn="$3" compose_file="$4"
	local admitted
	admitted="$(ifa_det_pg "${compose_project}" "${use_compose}" "${dsn}" \
		"SELECT count(*) FROM shared_projection_intents WHERE projection_domain = 'deployable_unit_edges';" \
		"${compose_file}" | tr -d '[:space:]')"
	if [[ "${admitted}" != "0" ]]; then
		echo "deployable_unit_edges: expected 0 shared_projection_intents rows before the maintenance pass, got ${admitted} -- either the readiness gate is not actually closed in this runtime (re-verify this file's header claim before trusting the rest of this cell), or a prior cell's state leaked into this one" >&2
		return 1
	fi
	printf 'deployable_unit_edges: confirmed 0 admitted edges before the maintenance pass (the readiness gate is closed, as documented)\n'
}

# ifa_deployable_unit_live_run_maintenance_pass runs ONE bootstrap-index
# maintenance pass -- not the B-7 gate's three cycles. This family's
# dependency chain is two links deep (deployment_mapping's cross-repo
# resolution, then deployable_unit_correlation's reopen), matching
# golden-corpus-maintenance-drains.sh's own account of what its first
# maintenance cycle produces and what its second consumes for this exact
# family ("Cycle 1's drain produces the resolved DEPLOYS_FROM relationships
# ... cycle 2's drain re-runs deployable_unit_correlation now that the
# resolved relationships it consumes exist"). Bare invocation, no flags --
# identical to how verify-golden-corpus-gate.sh invokes it, both for its
# first run and every subsequent maintenance pass.
ifa_deployable_unit_live_run_maintenance_pass() {
	local pass_label="$1" bin_dir="$2" log_dir="$3"
	printf '\n=== deployable_unit_edges: bootstrap-index maintenance pass (%s) ===\n' "${pass_label}"
	if ! "${bin_dir}/eshu-bootstrap-index" >"${log_dir}/bootstrap-index-deployable-unit-${pass_label}.log" 2>&1; then
		tail -40 "${log_dir}/bootstrap-index-deployable-unit-${pass_label}.log" >&2 || true
		echo "deployable_unit_edges: bootstrap-index maintenance pass (${pass_label}) failed" >&2
		return 1
	fi
	cat "${log_dir}/bootstrap-index-deployable-unit-${pass_label}.log"
}

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
	ifa_deployable_unit_live_assert_empty_before_maintenance \
		"${compose_project}" "${use_compose}" "${postgres_dsn}" "${compose_file}" || return 1

	ifa_deployable_unit_live_run_maintenance_pass primary "${bin_dir}" "${log_dir}" || return 1

	ifa_deployable_unit_live_drain post "${bin_dir}" "${log_dir}" "${drain_timeout}" || return 1

	ifa_deployable_unit_live_assert "${bin_dir}" "${expected_edges}" || return 1

	if [[ "${use_compose}" -eq 1 ]]; then
		log "deployable_unit_edges: tear down cell"
		docker compose -p "${compose_project}" -f "${compose_file}" down -v >/dev/null 2>&1 || true
	fi

	printf 'deployable_unit_edges: standalone cell wall time: %ss\n' "$(( $(date +%s) - cell_start ))"
}
