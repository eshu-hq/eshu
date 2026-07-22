#!/usr/bin/env bash
# Ifá P6 part 2 (#4580) deterministic fault-injection Docker gate (design doc
# docs/internal/design/4389-ifa-conformance-platform.md, Layer 4). Drives the
# SAME demo-org GCP cassette (testdata/cassettes/gcpcloud/supply-chain-demo.json)
# PLUS a generated synth-multiscope GCP cassette (`eshu-ifa synth-cassette`,
# same non-inert rationale as scripts/verify-ifa-determinism.sh) through a
# FRESH Postgres + NornicDB Compose stack per cell (`down -v` between every
# cell, mirroring every sibling verify-ifa-*.sh script), then injects one
# scripted fault per cell into the real eshu-reducer binary and asserts that,
# after the fault and a full drain, the canonicalized graph
# (`ifa graph-dump -digest`) is BYTE-IDENTICAL to the fault-free baseline and
# fact_work_items carries ZERO durable dead_letter rows -- Layer 4's
# unchanged acceptance clause: "still correct" is the same digest comparison
# Layers 1-2 already define, applied along the failure axis instead of the
# scheduling axis.
#
# Five cells, each hitting a genuinely different recovery seam:
#
#   1. baseline                              -- fault-free; establishes the
#      digest cells 2-5 are compared against.
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
#
# Cells 2 and 3 do NOT go through faultreplay's kill-worker-after-claim /
# expire-lease-mid-handler fault kinds: those two kinds only have a hermetic,
# in-process WorkSource decorator (go/internal/replay/faultreplay's
# FaultingWorkSource, consumed by faultreplay.RunFault) -- there is no
# ifafaultinjection-tagged wiring of FaultingWorkSource into go/cmd/reducer
# against real Postgres. Acting directly on the live process/row is this
# gate's own mechanism for those two cells, matching the T2/T3 manual proofs
# this gate automates (see issue #4580 history): kill -9 the host reducer mid-
# drain converges via the 1-minute lease reclaim in ~65s with zero dead
# letters, and a forced lease expiry converges the same way from the
# handler-side trigger.
#
# fail-terminal (a sixth possible cell) is deliberately NOT included: it has
# no live seam either -- go/internal/storage/cypher/fault_executor.go's
# applyFault leaves it explicitly inert at the graph-executor seam ("a
# different decorator owns them"), and that different decorator is the SAME
# hermetic-only FaultingWorkSource cells 2/3 already can't use live. Building
# a live fail-terminal seam is out of this slice's scope (S5: the gate script
# only); this is reported as an explicit, honest gap, not silently dropped.
#
# Flake policy: NO retry-to-green, ever. A digest mismatch or a non-zero
# dead_letter count after a cell's drain is a real concurrency/recovery
# defect -- root-cause it, never lower workers, retry, or otherwise normalize
# it away (Serialization-Is-Not-A-Fix). A fault that never fires (checked
# per-cell below: a claimed-row proof for cells 2/3, a reducer-log grep for
# cell 4, a sentinel-fired proof for cell 5) is an inert script, not a pass.
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
# 1-minute reducer lease (cell 2/3) and the default 30s (+jitter) reducer
# retry delay (cell 4's queue-retry lane) -- see go/cmd/reducer/
# main_helpers.go and go/internal/runtime/retry_policy.go.
: "${GATE_DRAIN_TIMEOUT:=4m}"
: "${CLAIMED_ROW_WAIT_TIMEOUT:=60}"
: "${RESTART_SENTINEL_WAIT_TIMEOUT:=90}"

compose_file="docker-compose.yaml"
cassette="${repo_root}/testdata/cassettes/gcpcloud/supply-chain-demo.json"
drive_workers=4

# SQL relationship family cassette (#5351): driven into every cell alongside
# the demo-org + synth-multiscope cassettes, so cells 2/3 (lease-expiry / kill-
# worker) exercise the SQL relationship materialization handler's replay
# through the REAL durable fault path, and the fault-free baseline's own graph
# is asserted to carry exactly the seven expected SQL edges (the non-vacuity
# check backing the materialized_edges:sql_relationships manifest row's
# proof_gate: ifa-fault-injection claim). Every cell's post-recovery graph is
# then compared byte-identical to that baseline, so a fault that silently
# dropped a SQL edge on recovery diverges the digest and fails.
sql_cassette="${repo_root}/testdata/cassettes/sqlrelationships/ifa-sql-family.json"
sql_expected_edges="${repo_root}/go/internal/ifa/testdata/sqlrelationships/ifa-sql-family-expected-edges.json"

: "${SYNTH_MULTISCOPE_SEED:=4580}"
: "${SYNTH_MULTISCOPE_PROJECTS:=8}"
: "${SYNTH_MULTISCOPE_RESOURCES:=64}"

# The CloudResource MERGE anchor fail-graph-write-once-then-succeed targets
# (go/internal/storage/cypher/cloud_resource_node_writer.go's
# baseCloudResourceUpsertCypher): a fixed, grep-stable substring regardless of
# this run's own call interleaving, unlike a statement_ordinal.
cloud_resource_operation_match="MERGE (r:CloudResource"

use_compose=1
keep=0
for arg in "$@"; do
	case "${arg}" in
	--no-compose) use_compose=0 ;;
	--keep) keep=1 ;;
	-h | --help)
		sed -n '2,72p' "${BASH_SOURCE[0]}"
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
log "build tagged host reducer (-tags ifafaultinjection, cells 4-5 only)"
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

# fresh_stack tears down any prior cell's stack and brings up a genuinely
# fresh Postgres + NornicDB pair, then applies schema.
fresh_stack() {
	local cell="$1"
	if [[ "${use_compose}" -eq 1 ]]; then
		docker compose -p "${FAULT_COMPOSE_PROJECT}" -f "${compose_file}" down -v >/dev/null 2>&1 || true
		docker compose -p "${FAULT_COMPOSE_PROJECT}" -f "${compose_file}" up -d nornicdb postgres
		log "${cell}: wait for backends"
		ifa_det_wait_for_backends "${FAULT_COMPOSE_PROJECT}" "${compose_file}" \
			|| die "${cell}: Postgres + NornicDB did not become ready within budget"
	fi
	log "${cell}: apply Postgres + graph schema (eshu-bootstrap-data-plane)"
	"${bin_dir}/eshu-bootstrap-data-plane" >"${log_dir}/bootstrap-data-plane-${cell}.log" 2>&1 \
		|| { tail -40 "${log_dir}/bootstrap-data-plane-${cell}.log"; die "${cell}: bootstrap-data-plane failed"; }
}

# drive_all_cassettes drives the demo-org + synth-multiscope + SQL relationship
# family cassettes into the fresh stack and asserts the drive actually enqueued
# work (never a vacuous drain proof). The SQL family cassette (#5351) makes
# cells 2/3 exercise the SQL relationship materialization handler's replay
# through the real durable fault path, not only the GCP resource path.
drive_all_cassettes() {
	local cell="$1"
	log "${cell}: drive demo-org cassette (-workers ${drive_workers})"
	"${bin_dir}/eshu-ifa" drive -cassette "${cassette}" -workers "${drive_workers}" \
		>"${log_dir}/ifa-drive-${cell}.log" 2>&1 \
		|| { tail -40 "${log_dir}/ifa-drive-${cell}.log" >&2; die "${cell}: eshu-ifa drive (demo-org) failed"; }
	log "${cell}: drive synth-multiscope cassette (-workers ${drive_workers})"
	"${bin_dir}/eshu-ifa" drive -cassette "${synth_cassette}" -workers "${drive_workers}" \
		>"${log_dir}/ifa-drive-synth-${cell}.log" 2>&1 \
		|| { tail -40 "${log_dir}/ifa-drive-synth-${cell}.log" >&2; die "${cell}: eshu-ifa drive (synth-multiscope) failed"; }
	log "${cell}: drive SQL relationship family cassette (-workers ${drive_workers})"
	"${bin_dir}/eshu-ifa" drive -cassette "${sql_cassette}" -workers "${drive_workers}" \
		>"${log_dir}/ifa-drive-sql-${cell}.log" 2>&1 \
		|| { tail -40 "${log_dir}/ifa-drive-sql-${cell}.log" >&2; die "${cell}: eshu-ifa drive (SQL relationship family) failed"; }
	local enqueued
	enqueued="$(ifa_det_pg "${FAULT_COMPOSE_PROJECT}" "${use_compose}" "${ESHU_POSTGRES_DSN}" \
		'SELECT count(*) FROM fact_work_items;' "${compose_file}" | tr -d '[:space:]')"
	[[ -n "${enqueued}" && "${enqueued}" -gt 0 ]] \
		|| die "${cell}: eshu-ifa drive committed but enqueued 0 fact_work_items rows (vacuous drain proof)"
	printf '%s: fact_work_items enqueued: %s\n' "${cell}" "${enqueued}"
}

# run_drain_gate polls the gate binary to the B-12 residual bound (0), which
# folds in this gate's own dead_letter=0 requirement: factWorkItemsResidualSQL
# (go/cmd/golden-corpus-gate/drains.go) counts a dead_letter row AS residual,
# so a PASS here already proves no durable dead letter survived.
run_drain_gate() {
	local cell="$1"
	log "${cell}: drain projector + reducer (gate polls to the B-12 residual bound)"
	if ! "${bin_dir}/eshu-golden-corpus-gate" \
		-phase=drains \
		-snapshot=testdata/golden/e2e-20repo-snapshot.json \
		-drain-timeout="${GATE_DRAIN_TIMEOUT}"; then
		tail -40 "${log_dir}"/reducer-*"${cell}"*.log 2>/dev/null || true
		tail -40 "${log_dir}/projector-${cell}.log" 2>/dev/null || true
		die "${cell}: drain did not reach the snapshot's residual bound within ${GATE_DRAIN_TIMEOUT}"
	fi
}

# assert_no_dead_letters is a second, explicit dead_letter=0 check independent
# of run_drain_gate's implicit one, so a failure here names the actual count
# instead of only "the gate timed out".
assert_no_dead_letters() {
	local cell="$1"
	local count
	count="$(ifa_fault_dead_letter_count "${FAULT_COMPOSE_PROJECT}" "${use_compose}" "${ESHU_POSTGRES_DSN}" "${compose_file}")"
	[[ "${count}" -eq 0 ]] || die "${cell}: expected 0 dead_letter rows after recovery, got ${count}"
	printf '%s: dead_letter rows: 0 (recovery converged)\n' "${cell}"
}

# capture_digest canonicalizes the post-drain graph and stores it in digests[cell].
capture_digest() {
	local cell="$1"
	log "${cell}: canonicalize graph (ifa graph-dump)"
	"${bin_dir}/eshu-ifa" graph-dump -out "${work_dir}/graph-${cell}.dump" \
		|| die "${cell}: ifa graph-dump (canonical bytes) failed"
	local d
	d="$("${bin_dir}/eshu-ifa" graph-dump -digest | tr -d '[:space:]')"
	[[ -n "${d}" ]] || die "${cell}: ifa graph-dump -digest returned empty output"
	digests[${cell}]="${d}"
	printf '%s: digest: %s\n' "${cell}" "${d}"
}

# assert_matches_baseline compares digests[cell] to digests[baseline], printing
# the full canonical-dump diff (never hiding it) on a mismatch.
assert_matches_baseline() {
	local cell="$1"
	[[ "${digests[${cell}]}" == "${digests[baseline]}" ]] && return 0
	printf 'MISMATCH: %s digest (%s) != baseline digest (%s)\n' \
		"${cell}" "${digests[${cell}]}" "${digests[baseline]}" >&2
	printf '\n=== full canonical graph diff: baseline vs %s (failure artifact) ===\n' "${cell}" >&2
	diff -u "${work_dir}/graph-baseline.dump" "${work_dir}/graph-${cell}.dump" >&2 || true
	die "${cell}: graph diverged from the fault-free baseline -- a real recovery/concurrency defect; do NOT retry, lower workers, or otherwise normalize this away"
}

teardown_cell() {
	local cell="$1"
	for pid in "${bg_pids[@]:-}"; do
		[[ -n "${pid}" ]] && kill "${pid}" >/dev/null 2>&1 || true
	done
	wait 2>/dev/null || true
	bg_pids=()
	if [[ "${use_compose}" -eq 1 ]]; then
		log "${cell}: tear down cell (fresh stack for the next cell)"
		docker compose -p "${FAULT_COMPOSE_PROJECT}" -f "${compose_file}" down -v >/dev/null 2>&1 || true
	fi
}

cell_start=$(date +%s)

# --- Cell 1: baseline (fault-free) ------------------------------------------
log "cell baseline: fresh stack"
fresh_stack baseline
drive_all_cassettes baseline
ifa_det_start_bg "${log_dir}" "projector-baseline" projector_pid "${bin_dir}/eshu-projector"
ifa_det_start_bg "${log_dir}" "reducer-baseline" reducer_pid "${bin_dir}/eshu-reducer"
run_drain_gate baseline
assert_no_dead_letters baseline
capture_digest baseline
# Non-vacuity assertion for the SQL relationship family (#5351): the fault-free
# baseline graph must carry EXACTLY the seven expected SQL edges. This is what
# gives the per-cell "identical to baseline" digest comparison teeth for this
# family — if the SQL family materialized zero edges, the baseline digest and
# every recovery-cell digest would still match (empty == empty) and pass
# vacuously; asserting the absolute set here proves the baseline is non-empty,
# so a fault that drops a SQL edge on recovery then diverges from a KNOWN-good
# baseline. Backs the materialized_edges:sql_relationships manifest row's
# proof_gate: ifa-fault-injection claim.
log "baseline: assert SQL relationship family materialized edges (absolute set, non-vacuity)"
"${bin_dir}/eshu-ifa" assert-edges \
	-domain sql_relationships \
	-expected "${sql_expected_edges}" \
	|| die "baseline: SQL relationship family materialized edge set did not match the expected set (fault-free baseline must materialize all nine SQL edges before the recovery cells compare against it)"
# Snapshot the fault-free retry count so cell 4 can prove the injected fault
# ADDED a retry this identical drive did not produce on its own (guards the
# non-vacuity check against a natural counting-class retry greening it while the
# decorator sits inert). Captured before teardown while this cell's stack is up.
baseline_retried="$(ifa_fault_count_retried "${FAULT_COMPOSE_PROJECT}" "${use_compose}" "${ESHU_POSTGRES_DSN}" "${compose_file}")"
baseline_retried="${baseline_retried:-0}"
printf 'baseline: fault-free gcp_resource_materialization retried rows (attempt_count>1): %s\n' "${baseline_retried}"
teardown_cell baseline
wall_times[baseline]=$(( $(date +%s) - cell_start ))
printf 'baseline: cell wall time: %ss\n' "${wall_times[baseline]}"

# --- Cell 2: kill-worker-after-claim -----------------------------------------
cell_start=$(date +%s)
log "cell kill-worker-after-claim: fresh stack"
fresh_stack killworker
drive_all_cassettes killworker
ifa_det_start_bg "${log_dir}" "projector-killworker" projector_pid "${bin_dir}/eshu-projector"
ifa_det_start_bg "${log_dir}" "reducer-killworker-before" reducer_pid_before "${bin_dir}/eshu-reducer"
claimed_before="$(ifa_fault_wait_for_claimed "${FAULT_COMPOSE_PROJECT}" "${use_compose}" "${ESHU_POSTGRES_DSN}" "${compose_file}" "${CLAIMED_ROW_WAIT_TIMEOUT}")" \
	|| die "kill-worker-after-claim: no row was ever claimed before the kill -- non-vacuous precondition failed"
printf 'kill-worker-after-claim: non-vacuous: %s claimed/running row(s) observed before kill\n' "${claimed_before}"
log "kill-worker-after-claim: kill -9 the live reducer (pid ${reducer_pid_before})"
kill -9 "${reducer_pid_before}" >/dev/null 2>&1 || true
log "kill-worker-after-claim: start a fresh reducer process (1-minute lease expiry reclaim)"
ifa_det_start_bg "${log_dir}" "reducer-killworker-after" reducer_pid_after "${bin_dir}/eshu-reducer"
run_drain_gate killworker
assert_no_dead_letters killworker
capture_digest killworker
assert_matches_baseline killworker
teardown_cell killworker
wall_times[killworker]=$(( $(date +%s) - cell_start ))
printf 'kill-worker-after-claim: cell wall time: %ss\n' "${wall_times[killworker]}"

# --- Cell 3: expire-lease-mid-handler -----------------------------------------
cell_start=$(date +%s)
log "cell expire-lease-mid-handler: fresh stack"
fresh_stack expirelease
drive_all_cassettes expirelease
ifa_det_start_bg "${log_dir}" "projector-expirelease" projector_pid "${bin_dir}/eshu-projector"
ifa_det_start_bg "${log_dir}" "reducer-expirelease" reducer_pid "${bin_dir}/eshu-reducer"
claimed_before="$(ifa_fault_wait_for_claimed "${FAULT_COMPOSE_PROJECT}" "${use_compose}" "${ESHU_POSTGRES_DSN}" "${compose_file}" "${CLAIMED_ROW_WAIT_TIMEOUT}")" \
	|| die "expire-lease-mid-handler: no row was ever claimed before the forced expiry -- non-vacuous precondition failed"
printf 'expire-lease-mid-handler: non-vacuous: %s claimed/running row(s) observed before forced expiry\n' "${claimed_before}"
log "expire-lease-mid-handler: force claim_until = now() on every claimed/running reducer row (SQL, no kill)"
ifa_det_pg "${FAULT_COMPOSE_PROJECT}" "${use_compose}" "${ESHU_POSTGRES_DSN}" \
	"UPDATE fact_work_items SET claim_until = now() WHERE stage = 'reducer' AND status IN ('claimed', 'running');" \
	"${compose_file}" >/dev/null
run_drain_gate expirelease
assert_no_dead_letters expirelease
capture_digest expirelease
assert_matches_baseline expirelease
teardown_cell expirelease
wall_times[expirelease]=$(( $(date +%s) - cell_start ))
printf 'expire-lease-mid-handler: cell wall time: %ss\n' "${wall_times[expirelease]}"

# --- Cell 4: fail-graph-write-once-then-succeed (queue-retry lane) ----------
cell_start=$(date +%s)
log "cell fail-graph-write-once-then-succeed: fresh stack"
fresh_stack failgraphwrite
drive_all_cassettes failgraphwrite
fault_once_script="${work_dir}/fault-once-then-succeed.json"
ifa_fault_write_once_script "${fault_once_script}" "${cloud_resource_operation_match}" "queue-retry"
ifa_det_start_bg "${log_dir}" "projector-failgraphwrite" projector_pid "${bin_dir}/eshu-projector"
ifa_det_start_bg "${log_dir}" "reducer-failgraphwrite" reducer_pid \
	env "ESHU_IFA_FAULT_SCRIPT=${fault_once_script}" "${tagged_bin_dir}/eshu-reducer"
run_drain_gate failgraphwrite
assert_no_dead_letters failgraphwrite
capture_digest failgraphwrite
assert_matches_baseline failgraphwrite
ifa_fault_assert_retried_above "${FAULT_COMPOSE_PROJECT}" "${use_compose}" "${ESHU_POSTGRES_DSN}" "${compose_file}" "${baseline_retried}" \
	|| die "fail-graph-write-once-then-succeed: the scripted fault never fired -- the count of retried gcp_resource_materialization intents (succeeded, attempt_count > 1) did not exceed the fault-free baseline (${baseline_retried}). An inert script, not a pass. This is the live integration proof of the ifafaultinjection decorator; if the fault never fires, root-cause the wiring (ESHU_IFA_FAULT_SCRIPT read, NewFaultingExecutor construction, operation_match against the real CloudResource MERGE text) before treating this gate as usable."
printf 'fail-graph-write-once-then-succeed: non-vacuous: retried gcp_resource_materialization intents exceed the fault-free baseline (%s)\n' "${baseline_retried}"
teardown_cell failgraphwrite
wall_times[failgraphwrite]=$(( $(date +%s) - cell_start ))
printf 'fail-graph-write-once-then-succeed: cell wall time: %ss\n' "${wall_times[failgraphwrite]}"

# --- Cell 5: restart-backend-between-phase-groups ---------------------------
if [[ "${use_compose}" -eq 0 ]]; then
	log "cell restart-backend-between-phase-groups: SKIPPED (--no-compose cannot restart a backend it does not own)"
else
	cell_start=$(date +%s)
	log "cell restart-backend-between-phase-groups: fresh stack"
	fresh_stack restartbackend
	drive_all_cassettes restartbackend
	fault_restart_script="${work_dir}/fault-restart-backend.json"
	ifa_fault_write_restart_script "${fault_restart_script}" 1
	restart_sentinel="${fault_restart_script}.restart-sentinel"
	restart_result="${work_dir}/restart-watch-result"
	ifa_det_start_bg "${log_dir}" "projector-restartbackend" projector_pid "${bin_dir}/eshu-projector"
	ifa_det_start_bg "${log_dir}" "reducer-restartbackend" reducer_pid \
		env "ESHU_IFA_FAULT_SCRIPT=${fault_restart_script}" "${tagged_bin_dir}/eshu-reducer"
	ifa_fault_watch_restart_sentinel "${restart_sentinel}" "${FAULT_COMPOSE_PROJECT}" "${compose_file}" \
		"${restart_result}" "${RESTART_SENTINEL_WAIT_TIMEOUT}" &
	watcher_pid=$!
	bg_pids+=("${watcher_pid}")
	run_drain_gate restartbackend
	wait "${watcher_pid}" 2>/dev/null || true
	restart_fired="$(cat "${restart_result}" 2>/dev/null || echo missing)"
	[[ "${restart_fired}" == "fired" ]] \
		|| die "restart-backend-between-phase-groups: the scripted fault never fired (sentinel ${restart_sentinel} never appeared) -- inert script, not a pass. Root-cause the ifafaultinjection decorator's ExecuteGroup/ExecutePhaseGroup wiring before treating this gate as usable."
	printf 'restart-backend-between-phase-groups: non-vacuous: sentinel fired, nornicdb restarted mid-drain\n'
	assert_no_dead_letters restartbackend
	capture_digest restartbackend
	assert_matches_baseline restartbackend
	teardown_cell restartbackend
	wall_times[restartbackend]=$(( $(date +%s) - cell_start ))
	printf 'restart-backend-between-phase-groups: cell wall time: %ss\n' "${wall_times[restartbackend]}"
fi

log "PASS: fault-injection matrix green (project ${FAULT_COMPOSE_PROJECT}, postgres:${ESHU_POSTGRES_PORT}, neo4j-bolt:${NEO4J_BOLT_PORT})"
for cell in "${!digests[@]}"; do
	printf '  %s: digest=%s wall=%ss\n' "${cell}" "${digests[${cell}]}" "${wall_times[${cell}]:-n/a}"
done
