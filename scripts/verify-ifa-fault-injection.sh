#!/usr/bin/env bash
# Ifá P6 part 2 (#4580) deterministic fault-injection Docker gate (design doc
# docs/internal/design/4389-ifa-conformance-platform.md, Layer 4). Drives the
# SAME demo-org GCP cassette (testdata/cassettes/gcpcloud/supply-chain-demo.json)
# PLUS a generated synth-multiscope GCP cassette (`eshu-ifa synth-cassette`,
# same non-inert rationale as scripts/verify-ifa-determinism.sh) PLUS the SQL
# relationship family cassette through a FRESH Postgres + NornicDB Compose
# stack per cell (`down -v` between every cell, mirroring every sibling
# verify-ifa-*.sh script), then injects one scripted fault per cell into the
# real eshu-reducer binary and asserts that, after the fault and a full
# drain, the canonicalized graph (`ifa graph-dump -digest`) is
# BYTE-IDENTICAL to the fault-free baseline and fact_work_items carries ZERO
# durable dead_letter rows -- Layer 4's unchanged acceptance clause: "still
# correct" is the same digest comparison Layers 1-2 already define, applied
# along the failure axis instead of the scheduling axis.
#
# Seven cells, each hitting a genuinely different recovery seam. Cell
# functions live in scripts/lib/ifa_fault_injection_cells.sh (cells 1-5) and
# scripts/lib/ifa_fault_injection_sql_cells.sh (cells 6-7, issue #5555):
#
#   1. baseline                              -- fault-free; establishes the
#      digest cells 2-7 are compared against.
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
#   7. fail-graph-write-once-then-succeed-sql (#5555) -- mirrors cell 4, but
#      the fault is anchored to a SQL edge MERGE (QUERIES_TABLE) instead of
#      CloudResource. Fired-fault proof is a shared-projection error log
#      line, not fact_work_items attempt_count: sql_relationship_
#      materialization's graph writes ride the async shared-projection
#      intent path, which has no attempt_count column (see
#      go/internal/reducer/shared_projection_runner.go's
#      TestSharedProjectionRunnerLogsPartitionProcessingError).
#
# Cells 2, 3, and 6 do NOT go through faultreplay's kill-worker-after-claim /
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
# fail-terminal (an eighth possible cell) is deliberately NOT included: it
# has no live seam either -- go/internal/storage/cypher/fault_executor.go's
# applyFault leaves it explicitly inert at the graph-executor seam ("a
# different decorator owns them"), and that different decorator is the SAME
# hermetic-only FaultingWorkSource cells 2/3/6 already can't use live.
# Building a live fail-terminal seam is out of scope; this is reported as an
# explicit, honest gap, not silently dropped.
#
# Flake policy: NO retry-to-green, ever. A digest mismatch or a non-zero
# dead_letter count after a cell's drain is a real concurrency/recovery
# defect -- root-cause it, never lower workers, retry, or otherwise normalize
# it away (Serialization-Is-Not-A-Fix). A fault that never fires (checked
# per-cell: a claimed-row proof for cells 2/3/6, a reducer-log poll for
# cells 4/7, a sentinel-fired proof for cell 5) is an inert script, not a
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
# 1-minute reducer lease (cell 2/3/6) and the default 30s (+jitter) reducer
# retry delay (cell 4/7's queue-retry lane) -- see go/cmd/reducer/
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

: "${SYNTH_MULTISCOPE_SEED:=4580}"
: "${SYNTH_MULTISCOPE_PROJECTS:=8}"
: "${SYNTH_MULTISCOPE_RESOURCES:=64}"

# The CloudResource MERGE anchor cell_failgraphwrite (cell 4) targets
# (go/internal/storage/cypher/cloud_resource_node_writer.go's
# baseCloudResourceUpsertCypher): a fixed, grep-stable substring regardless of
# this run's own call interleaving, unlike a statement_ordinal.
cloud_resource_operation_match="MERGE (r:CloudResource"

# The SQL edge MERGE anchor cell_failgraphwrite_sql (cell 7, #5555) targets:
# go/internal/storage/cypher/canonical.go's batchCanonicalSQLQueriesTableUpsertCypher
# and edge_writer_sql.go's buildLabelScopedSQLRelationshipCypher both emit
# this exact MERGE clause text for a QUERIES_TABLE edge regardless of which
# source/target node labels the label-scoped writer path picks (only the
# preceding MATCH clauses vary by label) -- a fixed, grep-stable substring,
# same rationale as cloud_resource_operation_match above. QUERIES_TABLE is
# one of the SQL family's nine materialized edge types and is present in the
# committed sql_cassette, so this fault genuinely fires during that drive.
sql_edge_operation_match="MERGE (source)-[rel:QUERIES_TABLE]->(target)"

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
log "build tagged host reducer (-tags ifafaultinjection, cells 4/5/7 only)"
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
cell_failgraphwrite_sql

log "PASS: fault-injection matrix green (project ${FAULT_COMPOSE_PROJECT}, postgres:${ESHU_POSTGRES_PORT}, neo4j-bolt:${NEO4J_BOLT_PORT})"
for cell in "${!digests[@]}"; do
	printf '  %s: digest=%s wall=%ss\n' "${cell}" "${digests[${cell}]}" "${wall_times[${cell}]:-n/a}"
done
