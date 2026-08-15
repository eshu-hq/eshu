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
#
# Query capture is NEVER piped, so this needs no pipefail reasoning at all
# (unlike the after-probe below, which pipes a whitespace trim onto its
# capture and guards that with `|| [[ -z ]]`): the raw ifa_det_pg exit status
# is captured directly into admitted_rc via the if/else `$?` idiom, matching
# ifa_deployable_unit_require_fresh_intents
# (ifa_fault_injection_deployable_unit_cells.sh) exactly, which is the
# correct pattern already proven elsewhere in this family. Unlike that
# diagnostic, THIS function is a GATE (return 1 fails the cell), so a query
# that never ran must never silently read as "0" -- that would blame the
# readiness gate for a Postgres/compose hiccup that has nothing to do with
# it. Checked in order: the query's own exit code, then whether the (now
# whitespace-trimmed) output is empty, then whether it is non-numeric, and
# only once all three pass does it compare to the literal string "0".
ifa_deployable_unit_live_assert_empty_before_maintenance() {
	local compose_project="$1" use_compose="$2" dsn="$3" compose_file="$4"
	local admitted admitted_rc
	if admitted="$(ifa_det_pg "${compose_project}" "${use_compose}" "${dsn}" \
		"SELECT count(*) FROM shared_projection_intents WHERE projection_domain = 'deployable_unit_edges';" \
		"${compose_file}")"; then
		admitted_rc=0
	else
		admitted_rc=$?
	fi
	if [[ "${admitted_rc}" -ne 0 ]]; then
		echo "deployable_unit_edges: before-maintenance precondition query FAILED (exit ${admitted_rc}); treat this as unknown, not as a verdict" >&2
		return "${admitted_rc}"
	fi
	admitted="$(printf '%s' "${admitted}" | tr -d '[:space:]')"
	if [[ -z "${admitted}" ]]; then
		echo "deployable_unit_edges: before-maintenance precondition query returned empty output; treat this as unknown, not as zero" >&2
		return 1
	fi
	if [[ ! "${admitted}" =~ ^[0-9]+$ ]]; then
		echo "deployable_unit_edges: before-maintenance precondition query returned non-numeric output '${admitted}'; treat this as unknown, not as zero" >&2
		return 1
	fi
	if [[ "${admitted}" != "0" ]]; then
		echo "deployable_unit_edges: expected 0 shared_projection_intents rows before the maintenance pass, got ${admitted} -- either the readiness gate is not actually closed in this runtime (re-verify this file's header claim before trusting the rest of this cell), or a prior cell's state leaked into this one" >&2
		return 1
	fi
	printf 'deployable_unit_edges: confirmed 0 admitted edges before the maintenance pass (the readiness gate is closed, as documented)\n'
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
# reintroducing the same failure mode in a new place.
#
# Args: log_dir label (the reducer log is ${log_dir}/reducer-<label>.log)
ifa_deployable_unit_live_assert_readiness_opened() {
	local log_dir="$1" label="$2"
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
		echo "deployable_unit_edges: readiness-opened check: post-maintenance reducer log still shows cross-repo resolution gated -- the maintenance pass did not open the readiness gate" >&2
		return 1
	fi
	printf 'deployable_unit_edges: confirmed cross-repo relationship resolution started with no gated line after the maintenance pass (the readiness gate is open)\n'
}

# ifa_deployable_unit_live_report_intents_after_maintenance is a DIAGNOSTIC
# companion to ifa_deployable_unit_live_assert_empty_before_maintenance, not a
# second pass/fail gate: ifa_deployable_unit_live_assert below remains the
# sole authority on whether this cell passes or fails. This is a real problem
# to fix, not scaffolding: the live gate's edge assertion fails as a single
# symptom, "expected 1, got 0", with two indistinguishable causes --
#
#   (a) correlation admitted rows, but the WRITER dropped them. The two MATCH
#       clauses in canonical_deployable_unit_edges.go
#       (MATCH (source_repo:Repository {id: row.repo_id}) and
#       MATCH (deployment_repo:Repository {id: row.deployment_repo_id})) are
#       filters, not outer joins: if either endpoint Repository node is
#       absent, the row is eliminated, the statement still succeeds, zero
#       rows are affected, and nothing counts it -- no error, no dead letter.
#   (b) correlation produced nothing at all (the reopen either did not fire
#       or found nothing to correlate).
#
# Re-running the SAME shared_projection_intents count query used by the
# BEFORE probe, but AFTER the maintenance pass and BEFORE the final edge
# assertion, separates them: a nonzero count here proves admitted rows
# existed, so a subsequent "expected 1, got 0" implicates the writer; a zero
# count here means correlation itself produced nothing.
#
# What this function actually guarantees: it REPORTS, it never GATES, and a
# failed query degrades to an explicit "unavailable" reading rather than
# failing the cell or silently reading as zero. The call site below has no
# `|| return 1` on purpose, which means the caller's `set -e` (this file is
# sourced into verify-ifa-fault-injection.sh's `set -euo pipefail` shell) is
# NOT suppressed at that call site -- a function called without a `||` guard
# still runs under `set -e`, so an internally-unguarded query failure here
# would abort the whole cell before the real edge assertion ran, attributed
# to an assertion that never got to run -- exactly the false-red this probe
# exists to avoid, and a worse bug than the ambiguity it fixes.
#
# Harmonized with ifa_deployable_unit_live_assert_empty_before_maintenance's
# rc-capture shape (same file, above) rather than piping the query capture
# through `tr` and guarding with `if !` / `|| [[ -z ]]`: capturing
# admitted_rc directly from a BARE (unpiped) ifa_det_pg call means success or
# failure here never depends on the caller having set pipefail at all, one
# pattern instead of two slightly different ones for the same underlying
# hazard. The three failure modes this function must never let through as a
# false "0" -- the query itself failing, empty output, and non-numeric
# output -- collapse to the SAME "unavailable" reading (unlike the BEFORE
# probe, which reports each distinctly and gates on all three, since this
# function never gates on any of them). An empty-string or non-numeric count
# read as a real value would be its own false signal -- "intents == 0
# implicates correlation" is the WRONG diagnosis if the query never ran at
# all, so any of the three failure modes gets the same distinct,
# unmistakable reading instead.
#
# Args: compose_project use_compose dsn compose_file
ifa_deployable_unit_live_report_intents_after_maintenance() {
	local compose_project="$1" use_compose="$2" dsn="$3" compose_file="$4"
	local admitted admitted_rc
	if admitted="$(ifa_det_pg "${compose_project}" "${use_compose}" "${dsn}" \
		"SELECT count(*) FROM shared_projection_intents WHERE projection_domain = 'deployable_unit_edges';" \
		"${compose_file}")"; then
		admitted_rc=0
	else
		admitted_rc=$?
	fi
	if [[ "${admitted_rc}" -eq 0 ]]; then
		admitted="$(printf '%s' "${admitted}" | tr -d '[:space:]')"
	fi
	if [[ "${admitted_rc}" -ne 0 || -z "${admitted}" || ! "${admitted}" =~ ^[0-9]+$ ]]; then
		admitted="unavailable (query failed)"
	fi
	printf 'deployable_unit_edges: post-maintenance shared_projection_intents count = %s (diagnostic only, not a gate -- intents > 0 with 0 edges implicates the writer: an endpoint MATCH miss in canonical_deployable_unit_edges.go; intents == 0 implicates correlation: no admitted rows produced; a failed query reports as unavailable rather than failing this cell or reading as zero)\n' "${admitted}"
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
	ifa_deployable_unit_live_assert_readiness_opened "${log_dir}" "reducer-deployable-unit-post" || return 1
	ifa_deployable_unit_live_report_intents_after_maintenance "${compose_project}" "${use_compose}" "${postgres_dsn}" "${compose_file}"

	ifa_deployable_unit_live_assert "${bin_dir}" "${expected_edges}" || return 1

	if [[ "${use_compose}" -eq 1 ]]; then
		log "deployable_unit_edges: tear down cell"
		docker compose -p "${compose_project}" -f "${compose_file}" down -v >/dev/null 2>&1 || true
	fi

	printf 'deployable_unit_edges: standalone cell wall time: %ss\n' "$(( $(date +%s) - cell_start ))"
}
