#!/usr/bin/env bash
# shellcheck disable=SC2034  # drive_workers and the *_operation_match
# anchors are assigned here and read by the cell functions in
# scripts/lib/ifa_fault_injection_*.sh, which shellcheck cannot see from
# this file. The mirror of the SC2154 case the libraries disable.
# Ifá P6 part 2 (#4580) deterministic fault-injection Docker gate (design doc
# docs/internal/design/4389-ifa-conformance-platform.md, Layer 4). Drives the
# SAME demo-org GCP cassette (testdata/cassettes/gcpcloud/supply-chain-demo.json)
# PLUS a generated synth-multiscope GCP cassette (`eshu-ifa synth-cassette`,
# same non-inert rationale as scripts/verify-ifa-determinism.sh) PLUS the SQL
# relationship and code-call family cassettes through a FRESH Postgres + NornicDB Compose
# stack per cell (`down -v` between every cell, mirroring every sibling
# verify-ifa-*.sh script), then injects one scripted fault per cell into the
# real eshu-reducer binary and asserts that, after the fault and a full
# drain, the canonicalized graph (`ifa graph-dump -digest`) is
# BYTE-IDENTICAL to the fault-free baseline and fact_work_items carries ZERO
# durable dead_letter rows -- Layer 4's unchanged acceptance clause: "still
# correct" is the same digest comparison Layers 1-2 already define, applied
# along the failure axis instead of the scheduling axis.
#
# Fourteen cells, each hitting a genuinely different recovery or delivery
# seam. All fourteen run by default. Cell
# functions live in scripts/lib/ifa_fault_injection_cells.sh (cells 1-5),
# scripts/lib/ifa_fault_injection_sql_cells.sh (cells 6 and 10, issue #5555),
# scripts/lib/ifa_fault_injection_code_call_cells.sh (cells 7 and 11, issue
# #5991), scripts/lib/ifa_fault_injection_delivery_cells.sh (cells 8-9, issue
# #5544), and scripts/lib/ifa_fault_injection_deployable_unit_cells.sh
# (cells 12-14, issue #5993). The delta cell's full-node collateral comparator
# is split into scripts/lib/ifa_fault_injection_collateral_nodes.sh:
#
#   1. baseline                              -- fault-free; establishes the
#      digest every non-delta recovery cell is compared against. Cell 9
#      deliberately does not compare against it; see that entry.
#   2. kill-worker-after-claim                -- `kill -9` the live host
#      eshu-reducer process after a row is genuinely claimed, then start a
#      fresh reducer process and let the fixed 1-minute lease
#      (postgres.NewReducerQueue's hardcoded time.Minute,
#      go/cmd/reducer/main_helpers.go) expire and get reclaimed.
#   3. expire-lease-mid-handler                -- force `claim_until = now()`
#      directly via SQL on a genuinely claimed row (no kill), so the running
#      reducer's OWN claim query (claimReducerWorkQuery's
#      `claim_until <= $1`) reclaims it on the next poll while the original
#      handler goroutine is still in flight.
#   4. fail-graph-write-once-then-succeed      -- the tagged
#      (-tags ifafaultinjection) eshu-reducer with ESHU_IFA_FAULT_SCRIPT
#      pointed at a queue-retry fault script that fails the CloudResource
#      MERGE exactly once via go/internal/storage/cypher.FaultingExecutor.
#   5. restart-backend-between-phase-groups    -- the same tagged reducer
#      with a fault script that pauses after the first completed graph-write
#      group; this gate restarts the nornicdb Compose service while the
#      reducer is blocked on that pause, then releases it.
#   6. kill-worker-after-claim-sql (#5555)     -- mirrors cell 2, but
#      wait_for_claimed is scoped to domain=sql_relationship_materialization
#      specifically, provably targeting SQL work instead of whichever domain
#      the driven cassettes happen to schedule first (in practice GCP).
#   7. kill-worker-after-claim-code-calls (#5991) -- mirrors cells 2 and 6,
#      but waits specifically for a claimed code_call_materialization row and
#      exact-asserts the five code-call edges after reclaim.
#   8. duplicate-delivery (#5544)             -- drain once cleanly, then force
#      every succeeded reducer row back to a claimable pending state in SQL and
#      drain again. Proves the write path is idempotent under at-least-once
#      redelivery: the graph after the second drain must equal the fault-free
#      baseline exactly. The redelivery count is asserted > 0, so an UPDATE
#      that stops matching fails loudly instead of making the second drain a
#      no-op that passes vacuously.
#   9. delta-retract (#5544)                  -- drive the committed
#      generation-2 SQL cassette through ifa_det_run_sql_delta_live, the same
#      helper scripts/verify-ifa-determinism.sh calls, so the two gates cannot
#      drift on what a correctly-landed delta means. Generation 1 is asserted
#      to have materialized BEFORE generation 2 is driven, otherwise "the
#      retract removed it" and "it never arrived" are indistinguishable.
#      This is the ONE cell that does not compare to the baseline digest:
#      generation 2 changes the graph on purpose (it retracts one INDEXES edge
#      and adds another), so that comparison would fail correctly and invite
#      the wrong fix. Its proof is the exact expected-v2 edge set, which is
#      stronger than digest equality because it names the edges.
#  10. fail-graph-write-once-then-succeed-sql (#5555) -- mirrors cell 4, but
#      the fault is anchored to a SQL edge MERGE (QUERIES_TABLE) instead of
#      CloudResource. Fired-fault proof is the once-fired marker the fault
#      decorator writes at injection time, not a log
#      line, not fact_work_items attempt_count: sql_relationship_
#      materialization's graph writes ride the async shared-projection
#      intent path, which has no attempt_count column (see
#      go/internal/reducer/shared_projection_runner.go's
#      TestSharedProjectionRunnerLogsPartitionProcessingError).
#      Runs by default since #5974. It was held out for months on the belief
#      that the fault did not fire in CI; it always did, and the assertion was
#      calling a binary the runner lacks. See the call site below.
#  11. fail-graph-write-once-then-succeed-code-calls (#5991) -- mirrors cells
#      4 and 10, but anchors the one-shot queue-retry fault to the code-call
#      CALLS MERGE, proves the durable marker names that operation, and
#      exact-asserts the five code-call edges after recovery.
#  12. baseline-deployable-unit (#5993) -- a FAMILY-SCOPED fault-free
#      baseline, not a recovery cell: deployable_unit_edges materializes
#      nothing without a bootstrap-index maintenance pass this gate's other
#      cells never run (see scripts/lib/ifa_deployable_unit_live.sh's header),
#      so the shared cell 1 baseline's digest has zero deployable_unit_edges
#      materialization by construction and cannot serve cells 13-14 below.
#  13. kill-worker-after-claim-deployable-unit (#5993) -- mirrors cells 6-7,
#      scoped to domain=deployable_unit_correlation, run AFTER a maintenance
#      pass opens the readiness gate CrossRepoRelationshipHandler.Resolve
#      checks.
#  14. fail-graph-write-once-then-succeed-deployable-unit (#5993) -- mirrors
#      cells 10-11, anchored to the CORRELATES_DEPLOYABLE_UNIT MERGE, also
#      run after the same maintenance pass.
#
# Cells 2, 3, 6, and 7 do NOT go through faultreplay's kill-worker-after-claim /
# expire-lease-mid-handler fault kinds: those two kinds only have a hermetic,
# in-process WorkSource decorator (go/internal/replay/faultreplay's
# FaultingWorkSource, consumed by faultreplay.RunFault) -- there is no
# ifafaultinjection-tagged wiring of FaultingWorkSource into go/cmd/reducer
# against real Postgres. Acting directly on the live process/row is this
# gate's own mechanism for those cells, matching the T2/T3 manual proofs this
# gate automates (see issue #4580 history): kill -9 the host reducer mid-
# drain converges via the 1-minute lease reclaim in ~65s with zero dead
# letters, and a forced lease expiry converges the same way from the
# handler-side trigger.
#
# fail-terminal (a twelfth possible cell) is deliberately NOT included: it
# has no live seam either -- go/internal/storage/cypher/fault_executor.go's
# applyFault leaves it explicitly inert at the graph-executor seam ("a
# different decorator owns them"), and that different decorator is the SAME
# hermetic-only FaultingWorkSource cells 2/3/6/7 already can't use live.
# Building a live fail-terminal seam is out of scope; this is reported as an
# explicit, honest gap, not silently dropped.
#
# Flake policy: NO retry-to-green, ever. A digest mismatch or a non-zero
# dead_letter count after a cell's drain is a real concurrency/recovery
# defect -- root-cause it, never lower workers, retry, or otherwise normalize
# it away (Serialization-Is-Not-A-Fix). A fault that never fires (checked
# per-cell: a claimed-row proof for cells 2/3/6/7, a once-fired marker for
# cells 4/10/11, a sentinel-fired proof for cell 5) is an inert script, not a
# pass.
#
# Usage:
#   scripts/verify-ifa-fault-injection.sh [--no-compose] [--keep]
#     --no-compose  assume Postgres + NornicDB are already running on the
#                   configured ports; skip compose up/down here. Cell 5
#                   (restart-backend-between-phase-groups) needs this script
#                   to own the compose lifecycle to restart nornicdb, so
#                   --no-compose SKIPS cell 5 with an explicit warning rather
#                   than silently no-op'ing the restart.
#     --keep        leave the last cell's work dir (every digest + full
#                   canonical dump + logs, for a mismatch diff) in place.

# Refuse to run under bash < 4.4 (or a non-bash shell): see
# scripts/verify-ifa-determinism.sh's identical guard for the false-pass
# hazard this avoids (a nounset abort masked by the EXIT trap as exit 0).
if (( BASH_VERSINFO[0] < 4 || (BASH_VERSINFO[0] == 4 && BASH_VERSINFO[1] < 4) )); then
	printf '%s: requires bash >= 4.4 (running under %s).
' "${0##*/}" "${BASH_VERSION:-non-bash shell}" >&2
	printf '  On bash < 4.4 a nounset abort can be masked by the EXIT trap as a false PASS;
' >&2
	printf '  re-run under bash >= 4.4 (e.g. /opt/homebrew/bin/bash, or run `brew install bash`).
' >&2
	exit 3
fi
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "${repo_root}"

# shellcheck source=scripts/lib/ifa_determinism_common.sh
source "${repo_root}/scripts/lib/ifa_determinism_common.sh"
# shellcheck source=scripts/lib/ifa_fault_injection_common.sh
source "${repo_root}/scripts/lib/ifa_fault_injection_common.sh"
# shellcheck source=scripts/lib/ifa_fault_injection_driver.sh
source "${repo_root}/scripts/lib/ifa_fault_injection_driver.sh"
# shellcheck source=scripts/lib/ifa_fault_injection_cells.sh
source "${repo_root}/scripts/lib/ifa_fault_injection_cells.sh"
# shellcheck source=scripts/lib/ifa_fault_injection_sql_cells.sh
source "${repo_root}/scripts/lib/ifa_fault_injection_sql_cells.sh"
# shellcheck source=scripts/lib/ifa_fault_injection_code_call_cells.sh
source "${repo_root}/scripts/lib/ifa_fault_injection_code_call_cells.sh"
# shellcheck source=scripts/lib/ifa_fault_injection_collateral_nodes.sh
source "${repo_root}/scripts/lib/ifa_fault_injection_collateral_nodes.sh"
# shellcheck source=scripts/lib/ifa_fault_injection_delivery_cells.sh
source "${repo_root}/scripts/lib/ifa_fault_injection_delivery_cells.sh"
# shellcheck source=scripts/lib/ifa_sql_delta_live.sh
# Shared with scripts/verify-ifa-determinism.sh so both gates agree on what a
# correctly-landed generation-2 delta looks like (#5544 cell_deltaretract).
source "${repo_root}/scripts/lib/ifa_sql_delta_live.sh"
# shellcheck source=scripts/lib/ifa_code_call_live.sh
source "${repo_root}/scripts/lib/ifa_code_call_live.sh"
# shellcheck source=scripts/lib/ifa_documentation_live.sh
source "${repo_root}/scripts/lib/ifa_documentation_live.sh"
# shellcheck source=scripts/lib/ifa_fault_injection_documentation_cells.sh
source "${repo_root}/scripts/lib/ifa_fault_injection_documentation_cells.sh"
# shellcheck source=scripts/lib/ifa_deployable_unit_live.sh
source "${repo_root}/scripts/lib/ifa_deployable_unit_live.sh"
# shellcheck source=scripts/lib/ifa_deployable_unit_live_diagnostics.sh
source "${repo_root}/scripts/lib/ifa_deployable_unit_live_diagnostics.sh"
# shellcheck source=scripts/lib/ifa_deployable_unit_live_converge.sh
source "${repo_root}/scripts/lib/ifa_deployable_unit_live_converge.sh"
# shellcheck source=scripts/lib/ifa_fault_injection_deployable_unit_cells.sh
source "${repo_root}/scripts/lib/ifa_fault_injection_deployable_unit_cells.sh"

# ----------------------------------------------------------------------------
# Configuration. One Compose project + one port triple reused across every
# cell (torn down with `down -v` between cells) -- distinct from every
# sibling verify-ifa-*.sh default: golden-corpus-gate (15432/7687/7474),
# replay-drive (15532/7788/7575), dead-letter-determinism (15635/7792/7679),
# determinism (15636/7793/7680), dead-letter-matrix (15637/7794/7681).
# ----------------------------------------------------------------------------
: "${FAULT_COMPOSE_PROJECT:=eshu-ifa-fault-injection-$$}"
export ESHU_POSTGRES_PORT="${ESHU_POSTGRES_PORT:-15642}"
export NEO4J_BOLT_PORT="${NEO4J_BOLT_PORT:-7801}"
export NEO4J_HTTP_PORT="${NEO4J_HTTP_PORT:-7688}"
: "${ESHU_POSTGRES_PASSWORD:=change-me}"
: "${ESHU_NEO4J_PASSWORD:=change-me}"
# Headroom over this gate's two slowest natural recovery mechanics: the fixed
# 1-minute reducer lease (cells 2/3/6/7) and the default 30s (+jitter) reducer
# retry delay (cells 4/10/11's queue-retry lane) -- see go/cmd/reducer/
# main_helpers.go and go/internal/runtime/retry_policy.go.
: "${GATE_DRAIN_TIMEOUT:=4m}"
: "${CLAIMED_ROW_WAIT_TIMEOUT:=60}"
: "${RESTART_SENTINEL_WAIT_TIMEOUT:=90}"

compose_file="docker-compose.yaml"
cassette="${repo_root}/testdata/cassettes/gcpcloud/supply-chain-demo.json"
drive_workers=4

# SQL relationship family cassette (#5351): driven into every cell alongside
# the demo-org + synth-multiscope cassettes, so cells 2/3/6 (lease-expiry /
# kill-worker) exercise the SQL relationship materialization handler's
# replay through the REAL durable fault path, and the fault-free baseline's
# own graph is asserted to carry exactly the nine expected SQL edges (the
# non-vacuity check backing the materialized_edges:sql_relationships
# manifest row's proof_gate: ifa-fault-injection claim). Every cell's
# post-recovery graph is then compared byte-identical to that baseline, so a
# fault that silently dropped a SQL edge on recovery diverges the digest and
# fails.
sql_cassette="${repo_root}/testdata/cassettes/sqlrelationships/ifa-sql-family.json"
sql_expected_edges="${repo_root}/go/internal/ifa/testdata/sqlrelationships/ifa-sql-family-expected-edges.json"
sql_delta_cassette="${repo_root}/testdata/cassettes/sqlrelationships/ifa-sql-family-delta.json"
sql_delta_expected_edges="${repo_root}/go/internal/ifa/testdata/sqlrelationships/ifa-sql-family-delta-live-expected-edges.json"
code_call_cassette="${repo_root}/testdata/cassettes/codecalls/ifa-code-call-family.json"
code_call_expected_edges="${repo_root}/go/internal/ifa/testdata/codecalls/ifa-code-call-family-expected-edges.json"

# documentation_edges family cassette (#5994): driven into every cell
# alongside the SQL and code-call families, including the SqlTable-target
# DOCUMENTS edge (batchCanonicalDocumentationEntityEdgeCypher's MATCH label
# alternation). cell_killworker_documentation / cell_failgraphwrite_documentation
# below back the materialized_edges:documentation_edges manifest row's
# proof_gate: ifa-fault-injection claim.
documentation_cassette="${repo_root}/testdata/cassettes/documentation/ifa-documentation-family.json"
documentation_expected_edges="${repo_root}/go/internal/ifa/testdata/documentation/ifa-documentation-family-live-expected-edges.json"

# deployable_unit_edges family cassette (#5993), driven by
# cell_baseline_deployable_unit/cell_killworker_deployable_unit/
# cell_failgraphwrite_deployable_unit (scripts/lib/
# ifa_fault_injection_deployable_unit_cells.sh) via drive_all_cassettes.
deployable_unit_cassette="${repo_root}/testdata/cassettes/deployableunit/ifa-deployable-unit-family.json"
deployable_unit_expected_edges="${repo_root}/go/internal/ifa/testdata/deployableunit/ifa-deployable-unit-family-expected-edges.json"

: "${SYNTH_MULTISCOPE_SEED:=4580}"
: "${SYNTH_MULTISCOPE_PROJECTS:=8}"
: "${SYNTH_MULTISCOPE_RESOURCES:=64}"

# The CloudResource MERGE anchor cell_failgraphwrite (cell 4) targets
# (go/internal/storage/cypher/cloud_resource_node_writer.go's
# baseCloudResourceUpsertCypher): a fixed, grep-stable substring regardless of
# this run's own call interleaving, unlike a statement_ordinal.
cloud_resource_operation_match="MERGE (r:CloudResource"

# The SQL edge MERGE anchor cell_failgraphwrite_sql (cell 10, #5555) targets:
# go/internal/storage/cypher/canonical.go's batchCanonicalSQLQueriesTableUpsertCypher
# and edge_writer_sql.go's buildLabelScopedSQLRelationshipCypher both emit
# this exact MERGE clause text for a QUERIES_TABLE edge regardless of which
# source/target node labels the label-scoped writer path picks (only the
# preceding MATCH clauses vary by label) -- a fixed, grep-stable substring,
# same rationale as cloud_resource_operation_match above. QUERIES_TABLE is
# one of the SQL family's nine materialized edge types and is present in the
# committed sql_cassette, so this fault genuinely fires during that drive.
sql_edge_operation_match="MERGE (source)-[rel:QUERIES_TABLE]->(target)"
code_call_edge_operation_match="MERGE (source)-[rel:CALLS]->(target)"

# The DOCUMENTS edge MERGE anchor cell_failgraphwrite_documentation targets:
# go/internal/storage/cypher/canonical_documentation_edges.go's
# batchCanonicalDocumentationEntityEdgeCypher emits this exact MERGE clause
# text regardless of which target label (Function/Class/.../SqlTable) the
# preceding MATCH picked -- a fixed, grep-stable substring, same rationale as
# cloud_resource_operation_match/sql_edge_operation_match above.
documentation_edge_operation_match="MERGE (section)-[rel:DOCUMENTS]->(target)"

# The CORRELATES_DEPLOYABLE_UNIT MERGE anchor cell_failgraphwrite_deployable_unit
# (#5993) targets: go/internal/storage/cypher/canonical_deployable_unit_edges.go's
# batchCanonicalDeployableUnitCorrelationUpsertCypher emits this exact MERGE
# clause text -- a fixed, grep-stable substring, same rationale as
# cloud_resource_operation_match above. Byte-exact copy of that file's line 9.
deployable_unit_edge_operation_match="MERGE (source_repo)-[rel:CORRELATES_DEPLOYABLE_UNIT]->(deployment_repo)"

use_compose=1
keep=0
for arg in "$@"; do
	case "${arg}" in
	--no-compose) use_compose=0 ;;
	--keep) keep=1 ;;
	-h | --help)
		sed -n '2,91p' "${BASH_SOURCE[0]}"
		exit 0
		;;
	*)
		echo "verify-ifa-fault-injection: unknown argument: ${arg}" >&2
		exit 2
		;;
	esac
done

[[ -f "${cassette}" ]] || { echo "verify-ifa-fault-injection: cassette not found: ${cassette}" >&2; exit 1; }
[[ -f "${sql_cassette}" ]] || { echo "verify-ifa-fault-injection: SQL cassette not found: ${sql_cassette}" >&2; exit 1; }
[[ -f "${sql_expected_edges}" ]] || { echo "verify-ifa-fault-injection: SQL expected-edge set not found: ${sql_expected_edges}" >&2; exit 1; }
[[ -f "${sql_delta_cassette}" ]] || { echo "verify-ifa-fault-injection: SQL delta cassette not found: ${sql_delta_cassette}" >&2; exit 1; }
[[ -f "${sql_delta_expected_edges}" ]] || { echo "verify-ifa-fault-injection: SQL delta expected-edge set not found: ${sql_delta_expected_edges}" >&2; exit 1; }
[[ -f "${code_call_cassette}" ]] || { echo "verify-ifa-fault-injection: code-call cassette not found: ${code_call_cassette}" >&2; exit 1; }
[[ -f "${code_call_expected_edges}" ]] || { echo "verify-ifa-fault-injection: code-call expected-edge set not found: ${code_call_expected_edges}" >&2; exit 1; }
[[ -f "${documentation_cassette}" ]] || { echo "verify-ifa-fault-injection: documentation cassette not found: ${documentation_cassette}" >&2; exit 1; }
[[ -f "${documentation_expected_edges}" ]] || { echo "verify-ifa-fault-injection: documentation expected-edge set not found: ${documentation_expected_edges}" >&2; exit 1; }
[[ -f "${deployable_unit_cassette}" ]] || { echo "verify-ifa-fault-injection: deployable-unit cassette not found: ${deployable_unit_cassette}" >&2; exit 1; }
[[ -f "${deployable_unit_expected_edges}" ]] || { echo "verify-ifa-fault-injection: deployable-unit expected-edge set not found: ${deployable_unit_expected_edges}" >&2; exit 1; }

work_dir="$(mktemp -d -t ifa-fault-injection.XXXXXX)"
bin_dir="${work_dir}/bin"
tagged_bin_dir="${work_dir}/bin-fault"
log_dir="${work_dir}/logs"
mkdir -p "${bin_dir}" "${tagged_bin_dir}" "${log_dir}"

bg_pids=()

log() { printf '\n=== %s ===\n' "$*"; }
die() { printf 'verify-ifa-fault-injection: %s\n' "$*" >&2; exit 1; }

cleanup() {
	local status=$?
	if [[ "${status}" -ne 0 && -d "${log_dir}" ]]; then
		printf '\n=== host binary logs (failure) ===\n' >&2
		for logf in "${log_dir}"/*.log; do
			[[ -f "${logf}" ]] || continue
			printf '\n--- %s ---\n' "$(basename "${logf}")" >&2
			tail -60 "${logf}" >&2 || true
		done
	fi
	for pid in "${bg_pids[@]:-}"; do
		[[ -n "${pid}" ]] && kill "${pid}" >/dev/null 2>&1 || true
	done
	if [[ "${keep}" -eq 1 ]]; then
		printf '\n[--keep] work dir retained: %s\n' "${work_dir}" >&2
	else
		if [[ "${use_compose}" -eq 1 ]]; then
			docker compose -p "${FAULT_COMPOSE_PROJECT}" -f "${compose_file}" down -v >/dev/null 2>&1 || true
		fi
		rm -rf "${work_dir}"
	fi
	exit "${status}"
}
trap cleanup EXIT

# ----------------------------------------------------------------------------
# Shared runtime environment (mirrors verify-ifa-determinism.sh's block).
# ESHU_REDUCER_WORKERS is pinned >1 so cell 3 (expire-lease-mid-handler) can
# be reclaimed by a DIFFERENT worker goroutine in the same process while the
# original handler is still in flight, mirroring faultreplay.Config's own
# Workers>=2 requirement for the hermetic equivalent (runner.go's validate).
# ----------------------------------------------------------------------------
export ESHU_GRAPH_BACKEND=nornicdb
export NEO4J_URI="bolt://localhost:${NEO4J_BOLT_PORT}"
export NEO4J_USERNAME=neo4j
export NEO4J_PASSWORD="${ESHU_NEO4J_PASSWORD}"
export NEO4J_DATABASE=nornic
export ESHU_NEO4J_DATABASE=nornic
export DEFAULT_DATABASE=nornic
export ESHU_POSTGRES_DSN="postgresql://eshu:${ESHU_POSTGRES_PASSWORD}@localhost:${ESHU_POSTGRES_PORT}/eshu"
export ESHU_CONTENT_STORE_DSN="${ESHU_POSTGRES_DSN}"
export ESHU_LISTEN_ADDR="127.0.0.1:0"
export ESHU_METRICS_ADDR="127.0.0.1:0"
export ESHU_REDUCER_WORKERS=4
unset ESHU_PPROF_ADDR || true

log "build host binaries"
ifa_det_build_bin "${bin_dir}" bootstrap-data-plane || die "build bootstrap-data-plane failed"
ifa_det_build_bin "${bin_dir}" ifa || die "build ifa failed"
ifa_det_build_bin "${bin_dir}" projector || die "build projector failed"
ifa_det_build_bin "${bin_dir}" reducer || die "build reducer failed"
ifa_det_build_bin "${bin_dir}" golden-corpus-gate || die "build golden-corpus-gate failed"
# Sixth binary (#5993): cell_baseline_deployable_unit/cell_killworker_deployable_unit/
# cell_failgraphwrite_deployable_unit each run ONE bootstrap-index maintenance
# pass (backfills relationship evidence AND reopens
# crossScopeCorrelationReopenDomains) -- required for deployable_unit_edges to
# materialize anything in this gate's runtime; see
# scripts/lib/ifa_deployable_unit_live.sh's header for the full traced
# rationale. Untagged, matching the other five plain (non ifafaultinjection)
# builds above -- only the reducer needs the tagged build.
ifa_det_build_bin "${bin_dir}" bootstrap-index || die "build bootstrap-index failed"
log "build tagged host reducer (-tags ifafaultinjection, graph-fault and restart cells)"
ifa_det_build_bin "${tagged_bin_dir}" reducer "ifafaultinjection" || die "build tagged reducer failed"

log "generate synth-multiscope cassette (seed=${SYNTH_MULTISCOPE_SEED} projects=${SYNTH_MULTISCOPE_PROJECTS} resources=${SYNTH_MULTISCOPE_RESOURCES})"
synth_cassette="${work_dir}/synth-multiscope.json"
"${bin_dir}/eshu-ifa" synth-cassette \
	-seed "${SYNTH_MULTISCOPE_SEED}" \
	-projects "${SYNTH_MULTISCOPE_PROJECTS}" \
	-resources "${SYNTH_MULTISCOPE_RESOURCES}" \
	-out "${synth_cassette}" \
	|| die "ifa synth-cassette failed"
[[ -s "${synth_cassette}" ]] || die "ifa synth-cassette produced an empty or missing file: ${synth_cassette}"

declare -A digests
declare -A wall_times

cell_baseline
cell_killworker
cell_expirelease
cell_failgraphwrite
cell_restartbackend
cell_killworker_sql
cell_killworker_code_calls
cell_killworker_documentation
cell_duplicatedelivery
cell_deltaretract
# cell_failgraphwrite_sql is a permanent member of the matrix as of #5974.
#
# It spent months held out under three successive diagnoses -- a stderr-flush
# race, then "the fault does not fire in CI", then "the emitted Cypher does not
# contain the anchor" -- and all three were wrong. When the marker was finally
# read correctly it was present, naming the anchored statement: the injection
# worked and the reading of it did not. What failed was the assertion:
# it matched the marker with `rg`, which is not installed on this runner, so
# "command not found" was read as "the marker does not name the operation".
# The fix lives in ifa_fault_injection_common.sh.
#
# The lesson outlives the cell: a checker that cannot run must never look like a
# checker that ran and said no. The assertion now matches in bash and returns
# three distinct verdicts -- 0 the targeted write, 2 a different write, 1 no
# marker -- so no single exit code carries two meanings.
#
# Do not hold this cell out again on a red run without first proving the
# assertion itself can execute.
cell_failgraphwrite_sql
cell_failgraphwrite_code_calls
cell_failgraphwrite_documentation

# deployable_unit_edges cells (#5993). cell_baseline_deployable_unit MUST run
# before the two fault cells below: it populates digests[baseline_deployable_unit]
# and baseline_deployable_unit_retried, shell state the fault cells' own
# assert_matches_baseline/ifa_fault_assert_retried_above calls read. This is a
# family-scoped baseline, not the shared cell_baseline above: cell_baseline
# never runs a bootstrap-index maintenance pass, so its digest has ZERO
# deployable_unit_edges materialization by construction and would never match
# a cell that does run the pass -- see scripts/lib/
# ifa_fault_injection_deployable_unit_cells.sh's header for the full ruling.
cell_baseline_deployable_unit
cell_killworker_deployable_unit
cell_failgraphwrite_deployable_unit

log "PASS: fault-injection matrix green (project ${FAULT_COMPOSE_PROJECT}, postgres:${ESHU_POSTGRES_PORT}, neo4j-bolt:${NEO4J_BOLT_PORT})"
for cell in "${!digests[@]}"; do
	printf '  %s: digest=%s wall=%ss\n' "${cell}" "${digests[${cell}]}" "${wall_times[${cell}]:-n/a}"
done
