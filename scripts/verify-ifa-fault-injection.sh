#!/usr/bin/env bash
# shellcheck disable=SC2034,SC2154  # drive_workers and the *_operation_match
# anchors are assigned here and read by the cell functions in
# scripts/lib/ifa_fault_injection_*.sh, which shellcheck cannot see from
# this file. The mirror of the SC2154 case the libraries disable. SC2154
# added for use_compose/keep/list_cells/shard_spec/shard_k/shard_n: they are
# assigned by scripts/lib/ifa_fault_shard.sh's ifa_fault_shard_parse_args and
# read here, a cross-file assignment shellcheck likewise cannot see from
# this file alone.
# Ifá P6 part 2 (#4580) deterministic fault-injection Docker gate (design doc
# docs/internal/design/4389-ifa-conformance-platform.md, Layer 4). Drives the
# SAME demo-org GCP cassette (testdata/cassettes/gcpcloud/supply-chain-demo.json)
# PLUS a generated synth-multiscope GCP cassette (`eshu-ifa synth-cassette`) PLUS
# every other registered materialized-edge family's own committed cassette (see
# the numbered cell list below -- deliberately not enumerated by name here, since a fixed family list here goes stale)
# through a FRESH Postgres + NornicDB Compose stack per cell (`down -v` between every cell,
# mirroring every sibling verify-ifa-*.sh script), then injects one scripted fault per cell into the
# real eshu-reducer binary and asserts that, after the fault and a full
# drain, the canonicalized graph (`ifa graph-dump -digest`) is
# BYTE-IDENTICAL to the fault-free baseline and fact_work_items carries ZERO
# durable dead_letter rows -- Layer 4's unchanged acceptance clause: "still
# correct" is the same digest comparison Layers 1-2 already define, applied
# along the failure axis instead of the scheduling axis.
#
# The shard mirror derives the current cell count from its hand-authored
# roster and cross-checks this script's dispatch block. Cell libraries are
# split by family under scripts/lib/ifa_fault_injection_*_cells.sh; the
# symbol-runtime library owns cells 37-43 for issues #5995/#6000/#5997.
# The delta cell's full-node collateral comparator is split into
# scripts/lib/ifa_fault_injection_collateral_nodes.sh:
#
# The numbered catalog of every cell this gate dispatches lives in
# docs/internal/ifa-fault-cell-catalog.md. It moved out when this script hit its
# 500-line cap; scripts/test-verify-ifa-fault-injection.sh asserts the entry
# count there matches the ifa_fault_shard_run dispatch count below, so the two
# cannot drift apart silently.
#
# Cells 2, 3, 6, 7, 8, and 9 do NOT go through faultreplay's kill-worker-after-claim /
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
# fail-terminal (a thirty-fourth possible cell) is deliberately NOT included: it
# has no live seam either -- go/internal/storage/cypher/fault_executor.go's
# applyFault leaves it explicitly inert at the graph-executor seam ("a
# different decorator owns them"), and that different decorator is the SAME
# hermetic-only FaultingWorkSource cells 2/3/6/7/8/9/17/20/23/26/29/32 already can't use live.
# Building a live fail-terminal seam is out of scope; this is reported as an
# explicit, honest gap, not silently dropped.
#
# Flake policy: NO retry-to-green, ever. A digest mismatch or a non-zero
# dead_letter count after a cell's drain is a real concurrency/recovery
# defect -- root-cause it, never lower workers, retry, or otherwise normalize
# it away (Serialization-Is-Not-A-Fix). A fault that never fires is an inert
# script, not a pass, so every cell checks that its own fault actually fired:
#   - a claimed-row proof for cells 2/3/6/7/8/9/17/20/23/26/29/32/35
#   - an exact runner-waiter proof for cells 41/42/43
#   - a once-fired marker for cells 4/12/13/14/15/18/21/24/27/30/33/36/38/39/40
#   - a sentinel-fired proof for cell 5
#
# Usage:
#   scripts/verify-ifa-fault-injection.sh [--no-compose] [--keep] [--list-cells] [--shard k/n]
#     --no-compose  assume Postgres + NornicDB are already running on the
#                   configured ports; skip compose up/down here. Cell 5
#                   (restart-backend-between-phase-groups) needs this script
#                   to own the compose lifecycle to restart nornicdb, so
#                   --no-compose SKIPS cell 5 with an explicit warning rather
#                   than silently no-op'ing the restart.
#     --keep        leave the last cell's work dir (every digest + full
#                   canonical dump + logs, for a mismatch diff) in place.
#     --list-cells  print the ordered cell list (or, combined with --shard,
#                   just that shard's cells) and exit 0. Fully hermetic: no
#                   Docker, compose, build, or Postgres step runs first.
#     --shard k/n   run only the atomic cell groups assigned to shard k of n.
#                   Both k and n are required -- there is no default; a bare
#                   "--shard 2" is rejected as malformed. CI drives this gate
#                   with n=4, scripts/lib/ifa_fault_shard.sh's
#                   IFA_FAULT_SHARD_DEFAULT_N. cell_baseline still runs in
#                   every shard. See that file for the partition table.

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
# shellcheck source=scripts/lib/ifa_fault_injection_sources.sh
source "${repo_root}/scripts/lib/ifa_fault_injection_sources.sh"

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
# 1-minute reducer lease (cells 2/3/6/7/8/9/17/20/23/26/29/32/35) and the default 30s (+jitter)
# reducer retry delay (cells 4/12/13/14/15/18/21/24/27/30/33/36/38/39/40's queue-retry lane) -- see go/cmd/reducer/
# main_helpers.go and go/internal/runtime/retry_policy.go.
: "${GATE_DRAIN_TIMEOUT:=4m}"
# 120s general CI margin; lock-vs-projector ordering fixed the CI codeowners failure, not this budget.
: "${CLAIMED_ROW_WAIT_TIMEOUT:=120}"
: "${RESTART_SENTINEL_WAIT_TIMEOUT:=90}"

compose_file="docker-compose.yaml"
cassette="${repo_root}/testdata/cassettes/gcpcloud/supply-chain-demo.json"
drive_workers=4

# Committed materialized-edge family cassettes and their expected-set files,
# shared verbatim with scripts/verify-ifa-determinism.sh. Every family below
# is driven into every cell alongside the demo-org + synth-multiscope
# cassettes (the deployable-unit family excepted -- see drive_all_cassettes'
# header), and the fault-free baseline's own graph is exact-set asserted, so a
# fault that silently dropped an edge on recovery diverges the digest and
# fails instead of passing vacuously.
# shellcheck source=scripts/lib/ifa_family_fixtures.sh
source "${repo_root}/scripts/lib/ifa_family_fixtures.sh"

: "${SYNTH_MULTISCOPE_SEED:=4580}"
: "${SYNTH_MULTISCOPE_PROJECTS:=8}"
: "${SYNTH_MULTISCOPE_RESOURCES:=64}"

# The CloudResource MERGE anchor cell_failgraphwrite (cell 4) targets
# (go/internal/storage/cypher/cloud_resource_node_writer.go's
# baseCloudResourceUpsertCypher): a fixed, grep-stable substring regardless of
# this run's own call interleaving, unlike a statement_ordinal.
cloud_resource_operation_match="MERGE (r:CloudResource"

# The SQL edge MERGE anchor cell_failgraphwrite_sql (cell 12, #5555) targets:
# go/internal/storage/cypher/canonical.go's batchCanonicalSQLQueriesTableUpsertCypher
# and edge_writer_sql.go's buildLabelScopedSQLRelationshipCypher both emit
# this exact MERGE clause text for a QUERIES_TABLE edge regardless of which
# source/target node labels the label-scoped writer path picks (only the
# preceding MATCH clauses vary by label) -- a fixed, grep-stable substring,
# same rationale as cloud_resource_operation_match above. QUERIES_TABLE is
# one of the SQL family's nine materialized edge types and is present in the
# committed sql_cassette, so this fault genuinely fires during that drive.
sql_edge_operation_match="MERGE (source)-[rel:QUERIES_TABLE]->(target)"
codeowners_edge_operation_match="MERGE (repo)-[rel:DECLARES_CODEOWNER"  # PREFIX, verified vs canonical_codeowners_edges.go:35; see the cells lib header
submodule_pin_edge_operation_match="MERGE (parent)-[rel:PINS_SUBMODULE"  # PREFIX, verified vs canonical_submodule_edges.go:32; see the cells lib header

# The TARGETS_ENVIRONMENT MERGE anchor cell_failgraphwrite_kubernetes_namespace_environment
# (#6309) targets: go/internal/storage/cypher/kubernetes_namespace_node_writer.go:90 emits
# this exact MERGE clause text -- read off the writer's executed statement, same
# rationale as cloud_resource_operation_match above.
kubernetes_namespace_environment_edge_operation_match="MERGE (n)-[env_rel:TARGETS_ENVIRONMENT]->(env)"
# The HAS_ROLE MERGE anchor cell_failgraphwrite_iam_instance_profile_role (#6309)
# targets: go/internal/storage/cypher/iam_instance_profile_role_edge_writer.go:22
# emits MERGE (profile)-[rel:%s]->(role), filled from a closed single-member
# vocabulary, so the anchor carries the INTERPOLATED type. A literal %s would
# match no executed statement and the fault would never fire. It is NOT
# IAM_INSTANCE_PROFILE_HAS_ROLE, which is statement metadata beside the query.
iam_instance_profile_role_edge_operation_match="MERGE (profile)-[rel:HAS_ROLE]->(role)"

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

ifa_fault_shard_parse_args "${BASH_SOURCE[0]}" "$@"

[[ -f "${cassette}" ]] || { echo "verify-ifa-fault-injection: cassette not found: ${cassette}" >&2; exit 1; }
ifa_family_fixtures_require verify-ifa-fault-injection

work_dir="$(mktemp -d -t ifa-fault-injection.XXXXXX)"
bin_dir="${work_dir}/bin"
tagged_bin_dir="${work_dir}/bin-fault"
log_dir="${work_dir}/logs"
mkdir -p "${bin_dir}" "${tagged_bin_dir}" "${log_dir}"
ifa_deployable_unit_live_init_maintenance_scratch "${work_dir}"

bg_pids=()
ifa_documentation_ack_barrier_active=0
ifa_documentation_ack_holder_owned=0
ifa_documentation_ack_ddl_owned=0
ifa_documentation_ack_barrier_cell=""
ifa_documentation_ack_holder_pid=""
ifa_documentation_ack_holder_backend_pid=""
ifa_documentation_ack_waiter_pid=""
ifa_documentation_ack_run_id=""
ifa_documentation_ack_producers_safe=1
ifa_documentation_ack_producer_pids=()

log() { printf '\n=== %s ===\n' "$*"; }
die() { printf 'verify-ifa-fault-injection: %s\n' "$*" >&2; exit 1; }

cleanup() {
	local status=$?
	local barrier_cleanup_rc=0
	if [[ "${status}" -ne 0 && -d "${log_dir}" ]]; then
		# A dead-lettered row's failure_message exists ONLY in Postgres, and the
		# tail below is routinely flooded by INFO chatter (one real CI failure
		# spent all 60 of its lines on "drift finding admitted"), so a red cell
		# could name its own cause nowhere. Print the durable rows first and
		# unelided. Never allowed to change the exit status: status was captured
		# above, and the dump cannot fail the trap.
		printf '\n=== durable work-item failures (Postgres) ===\n' >&2
		ifa_det_pg "${FAULT_COMPOSE_PROJECT:-}" "${use_compose:-0}" "${ESHU_POSTGRES_DSN:-}" \
			"SELECT stage, domain, scope_id, status, attempt_count, failure_class, failure_message FROM fact_work_items WHERE status NOT IN ('succeeded', 'superseded') ORDER BY status, domain;" \
			"${compose_file:-}" >&2 || printf '(durable work-item dump unavailable)\n' >&2
		printf '\n=== host binary logs (failure) ===\n' >&2
		for logf in "${log_dir}"/*.log; do
			[[ -f "${logf}" ]] || continue
			printf '\n--- %s ---\n' "$(basename "${logf}")" >&2
			tail -60 "${logf}" >&2 || true
		done
	fi
	if declare -F ifa_documentation_stop_ack_producers >/dev/null; then
		ifa_documentation_stop_ack_producers || barrier_cleanup_rc=$?
	else
		for pid in "${bg_pids[@]:-}"; do
			[[ -n "${pid}" ]] && kill "${pid}" >/dev/null 2>&1 || true
		done
	fi
	if declare -F ifa_documentation_cleanup_ack_barrier >/dev/null; then
		ifa_documentation_cleanup_ack_barrier "${ifa_documentation_ack_barrier_cell:-killworkerdocumentation}" \
			|| { local cleanup_rc=$?; [[ "${barrier_cleanup_rc}" -ne 0 ]] || barrier_cleanup_rc="${cleanup_rc}"; }
	fi
	if declare -F ifa_fault_cleanup_runner_lease_audit >/dev/null; then
		ifa_fault_cleanup_runner_lease_audit \
			|| { local cleanup_rc=$?; [[ "${barrier_cleanup_rc}" -ne 0 ]] || barrier_cleanup_rc="${cleanup_rc}"; }
	fi
	if [[ "${barrier_cleanup_rc}" -ne 0 ]]; then
		printf 'verify-ifa-fault-injection: database fixture EXIT cleanup failed\n' >&2
		[[ "${status}" -ne 0 ]] || status="${barrier_cleanup_rc}"
	fi
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

ifa_fault_shard_run cell_baseline
ifa_fault_shard_run cell_killworker
ifa_fault_shard_run cell_expirelease
ifa_fault_shard_run cell_failgraphwrite
ifa_fault_shard_run cell_restartbackend
ifa_fault_shard_run cell_killworker_sql
ifa_fault_shard_run cell_killworker_code_calls
ifa_fault_shard_run cell_killworker_documentation
ifa_fault_shard_run cell_killworker_rationale
ifa_fault_shard_run cell_duplicatedelivery
ifa_fault_shard_run cell_deltaretract
# cell_failgraphwrite_sql is a permanent member of the matrix as of #5974.
# The full account of why it was held out for months on three successive wrong
# diagnoses -- and the lesson that outlives the cell -- now lives beside the
# cell itself, in scripts/lib/ifa_fault_injection_sql_cells.sh's header.
ifa_fault_shard_run cell_failgraphwrite_sql
ifa_fault_shard_run cell_failgraphwrite_code_calls
ifa_fault_shard_run cell_failgraphwrite_documentation
ifa_fault_shard_run cell_failgraphwrite_rationale

# deployable_unit_edges cells (#5993). Ordering matters: the family-scoped
# baseline must precede its two fault cells. The full ruling on why a scoped
# baseline is required at all lives in that family's own cell library.
ifa_fault_shard_run cell_baseline_deployable_unit
ifa_fault_shard_run cell_killworker_deployable_unit
ifa_fault_shard_run cell_failgraphwrite_deployable_unit

# codeowners_ownership_edges (#5992), cells 19-21; baseline first (it sets digests[baseline_codeowners] + baseline_codeowners_retried).
# Wrapped: #6160 landed these before sharding, so bare dispatch would run in all shards and the partition would be false.
# mirror's exact-cover proof no longer described. The shard-cases count pin
# caught it on the merge.
ifa_fault_shard_run cell_baseline_codeowners
ifa_fault_shard_run cell_killworker_codeowners
ifa_fault_shard_run cell_failgraphwrite_codeowners
ifa_fault_shard_run cell_baseline_repo_dependency
ifa_fault_shard_run cell_killworker_repo_dependency
ifa_fault_shard_run cell_failgraphwrite_repo_dependency
# submodule_pin_edges (#6002), cells 25-27; baseline first (it sets
# digests[baseline_submodule_pin] + baseline_submodule_pin_retried), same trio
# shape as codeowners above.
ifa_fault_shard_run cell_baseline_submodule_pin
ifa_fault_shard_run cell_killworker_submodule_pin
ifa_fault_shard_run cell_failgraphwrite_submodule_pin
# kubernetes_namespace_environment (#6309) and iam_instance_profile_role
# (#6309): first direct-materialization families in this gate. Each trio is
# baseline first (sole writer of that family's digest and retry baseline),
# same shape as codeowners above.
ifa_fault_shard_run cell_baseline_kubernetes_namespace_environment
ifa_fault_shard_run cell_killworker_kubernetes_namespace_environment
ifa_fault_shard_run cell_failgraphwrite_kubernetes_namespace_environment
ifa_fault_shard_run cell_baseline_iam_instance_profile_role
ifa_fault_shard_run cell_killworker_iam_instance_profile_role
ifa_fault_shard_run cell_failgraphwrite_iam_instance_profile_role
# inheritance_edges (#5996) and shell_exec (#6001): generic dispatch, baseline
# first in each trio (sole writer of that family's digest and retry baseline;
# the atomic-group ordering check enforces it). Both are FAULT_SHARED_DRIVE=0,
# which is why each needs its own baseline -- see ifa_fault_generic_cells.sh.
ifa_fault_shard_run cell_baseline_inheritance
ifa_fault_shard_run cell_killworker_inheritance
ifa_fault_shard_run cell_failgraphwrite_inheritance
ifa_fault_shard_run cell_baseline_shell_exec
ifa_fault_shard_run cell_killworker_shell_exec
ifa_fault_shard_run cell_failgraphwrite_shell_exec

# workload_dependency (#6003, cells 34-36) and the handles_route/runs_in/
# invokes_cloud_action trio (#5995/#6000/#5997, cells 37-43,
# scripts/lib/ifa_fault_injection_symbol_runtime_cells.sh): each baseline
# must run before its recovery cells in the same atomic shard group. The
# trio's runner-lease kill cells follow the three graph-write cells.
ifa_fault_shard_run cell_baseline_workload_dependency
ifa_fault_shard_run cell_killworker_workload_dependency
ifa_fault_shard_run cell_failgraphwrite_workload_dependency
ifa_fault_shard_run cell_baseline_symbol_runtime
ifa_fault_shard_run cell_failgraphwrite_handles_route
ifa_fault_shard_run cell_failgraphwrite_runs_in
ifa_fault_shard_run cell_failgraphwrite_invokes_cloud_action
ifa_fault_shard_run cell_killworker_handles_route
ifa_fault_shard_run cell_killworker_runs_in
ifa_fault_shard_run cell_killworker_invokes_cloud_action

log "PASS: fault-injection matrix green (project ${FAULT_COMPOSE_PROJECT}, postgres:${ESHU_POSTGRES_PORT}, neo4j-bolt:${NEO4J_BOLT_PORT})"
for cell in "${!digests[@]}"; do
	printf '  %s: digest=%s wall=%ss\n' "${cell}" "${digests[${cell}]}" "${wall_times[${cell}]:-n/a}"
done
