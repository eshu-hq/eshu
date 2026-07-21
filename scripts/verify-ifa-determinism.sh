#!/usr/bin/env bash
# Ifá P3 (#4396) graph-determinism matrix (design doc
# docs/internal/design/4389-ifa-conformance-platform.md, Layer 2, "the
# determinism matrix"). Drives the SAME unmodified demo-org GCP cassette
# (testdata/cassettes/gcpcloud/supply-chain-demo.json) PLUS a generated
# multi-scope synthetic GCP cassette (go/internal/synth/gcp.GenerateMultiScope
# via `ifa synth-cassette`, slice 6b) through `eshu-ifa drive -workers N` for
# N in {1, 2, 4}, each against an INDEPENDENT, FRESH Postgres + NornicDB
# Compose stack (`docker compose down -v` between every cell — no state, no
# volume, no container survives from one N to the next), drains the
# projector/reducer to the exact B-12 residual bound
# scripts/verify-ifa-replay-drive.sh already proves via
# `eshu-golden-corpus-gate -phase=drains`, then canonicalizes the resulting
# graph with `ifa graph-dump` (go/internal/ifa/graphdump.Canonicalize, a
# content-addressed, order-independent byte form — see that package's doc.go).
#
# Why both cassettes: the demo-org cassette alone has exactly one scope and
# one generation, so `concurrentreplay.Driver` has exactly one work unit for
# ANY worker count — varying N over it proves repeatability, not a worker
# matrix (see go/internal/reducer/gcp_resource_materialization_teeth.go's doc
# for the measured-inert finding this exposed). The generated synth-multiscope
# cassette (`ifa synth-cassette -projects 8 -resources 64`) adds 8 disjoint
# GCP project scopes — disjoint by construction, since every generated
# resource's full_resource_name embeds its own scope's distinct ProjectID
# (go/internal/synth/gcp/README.md's "Multi-scope generation" section) — so
# the combined cassette set gives the driver 9 genuinely independent work
# units, and `-workers N` actually varies commit interleaving. The synth
# cassette is generated ONCE (one fixed seed) before the cell loop, then
# driven into every cell identically, exactly like the demo-org cassette.
#
# This automates the exact 3-run shim go/internal/ifa/graphdump/README.md's
# "Benchmark Evidence" section already proved manually: N=1 and N=4 digests on
# a fresh DB were byte-identical, and a single mutated payload value changed
# the digest (proving the check is not vacuous). This script is that proof as
# a repeatable gate instead of a one-off manual rerun.
#
# Acceptance: all three digests are byte-identical. A mismatch is a real
# concurrency defect in the reducer/projector's graph write path (a MERGE
# race, an ordering-dependent projection, a dropped or duplicated write) —
# NOT a scan-order or backend-ID artifact, since Canonicalize is already
# content-addressed and order-independent, and NOT a cross-scope identity
# collision, since the synth-multiscope cassette's scopes are disjoint by
# construction (see above). On mismatch this script prints the full byte diff
# between the two divergent canonical dumps and exits non-zero. Per this
# platform's flake policy (design doc P4, "no retry-to-green, ever") and the
# repo's Serialization-Is-Not-A-Fix doctrine: a real divergence here must be
# root-caused, never normalized away by lowering N, retrying, or reducing
# worker counts. A red matrix on a NORMAL (non---teeth) build here means the
# synth scopes are not actually disjoint — fix the ProjectID derivation in
# go/internal/synth/gcp, do not "fix" this script by normalizing the digest.
#
# Slice scope (#4396 slice 6, "the teeth"): this file also owns --teeth, the
# acceptance clause's negative-path proof that the matrix actually catches "a
# deliberately non-idempotent write" instead of passing vacuously. --teeth
# builds every host binary with `-tags ifadeterminismteeth`, which links in
# exactly one build-tag-gated fault: go/internal/reducer/gcp_resource_
# materialization_teeth.go stamps TWO properties onto each CloudResource row —
# `ifa_teeth_seq` (a process-global monotonic sequence number, reintroduced in
# slice 6b now that the multi-scope cassette above makes it interleaving-
# sensitive again instead of inert) and `ifa_teeth_write_order` (wall-clock
# nanoseconds, the guaranteed-red floor) — and go/internal/storage/cypher/
# cloud_resource_node_writer_teeth.go appends the two matching SET clauses
# that persist them. At least one of those values depends on this run's own
# commit/processing order, so it genuinely differs across independent N=1/
# N=2/N=4 cells, changing `ifa graph-dump`'s canonical digest and failing the
# SAME comparison this script already runs unmodified. No normal, CI, or
# production build ever compiles that fault: cloud_resource_node_writer_
# teeth_off.go and gcp_resource_materialization_teeth_off.go (tag
# !ifadeterminismteeth) are its zero-cost, zero-behavior default.
#
# --teeth reuses every other line of this script's matrix logic unchanged;
# the only difference is the build tag and the final message. Per the
# acceptance clause, "exit non-zero = caught = teeth pass": a --teeth run
# that reaches the existing "graph-determinism matrix FAILED" branch below
# has done its job — do NOT read that failure as a real regression, and do
# NOT respond to it (or to a real one) by lowering N, retrying, or shrinking
# worker counts (Serialization-Is-Not-A-Fix). The one case --teeth treats as
# its OWN failure is the opposite: if the fault fails to manifest and all
# three digests still match, that is the teeth being broken, not the matrix
# being healthy, and this script reports that distinctly.
#
# The failure-path leg (malformed Odù dead-lettering identically across N)
# stays in scripts/verify-ifa-dead-letter-determinism.sh (slice 4); this
# script does not duplicate it.
#
# Usage:
#   scripts/verify-ifa-determinism.sh [--no-compose] [--keep] [--teeth]
#     --no-compose  assume Postgres + NornicDB are already running on the
#                   configured ports; skip compose up/down here. Because
#                   each cell still needs a FRESH database, --no-compose
#                   pairs only with a caller that resets both backends
#                   between cells itself; this script cannot do that for you.
#     --keep        leave the last cell's work dir (all three digests + full
#                   canonical dumps, for a mismatch diff) in place on exit.
#     --teeth       build every host binary with -tags ifadeterminismteeth
#                   (see above) and expect the matrix to go RED, proving a
#                   deliberately non-idempotent write is caught. Never pass
#                   this in a normal verification run.
#     --contention  ALSO drive the #5007 overlapping-identity cassette
#                   (gcpsynth.GenerateOverlappingScope via `ifa synth-cassette
#                   -overlap -divergent`) into every cell. Its K scopes share
#                   one CloudResource uid set, so the cross-scope writers
#                   contend on one node; the #5007 owner ledger must keep the
#                   digest identical across N=1/2/4. A digest divergence that
#                   appears ONLY under --contention is a real owner-ledger
#                   regression (the graph-level contention Odù).

# Refuse to run under bash < 4.4 (or a non-bash shell). This gate relies on bash
# 4.4's fix for expanding an empty array under `set -u` (bash 4.0-4.3 and 3.2
# still raise "unbound variable"). Under a shell without that fix — notably
# macOS's bundled bash 3.2 — a nounset abort during an array expansion is masked
# by the EXIT trap below (whose `local status=$?` reads 0 from a prior success),
# turning a real mid-script crash into a clean exit 0 — a FALSE PASS that
# silently defeats this determinism gate. Fail loudly instead. This check runs
# before `set -euo pipefail` so it can never itself trip nounset; BASH_VERSINFO[0]
# and BASH_VERSINFO[1] are the running bash's major and minor version.
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
source "${repo_root}/scripts/lib/ifa_determinism_common.sh"
source "${repo_root}/scripts/lib/ifa_sql_delta_live.sh"

# ----------------------------------------------------------------------------
# Configuration (override via environment). One Compose project + one port
# triple reused across all three cells (torn down with `down -v` between
# cells for a genuinely fresh database each time) — distinct from
# verify-golden-corpus-gate.sh's defaults (15432/7687/7474),
# verify-ifa-replay-drive.sh's defaults (15532/7788/7575), and
# verify-ifa-dead-letter-determinism.sh's defaults (15635/7792/7679).
# ----------------------------------------------------------------------------
: "${DETERMINISM_COMPOSE_PROJECT:=eshu-ifa-determinism-$$}"
# These three MUST be exported (not just set): docker-compose.yaml's "ports"
# mapping interpolates them from the child process environment `docker
# compose` inherits, not from this script's own shell variables. An
# unexported override here would silently fall back to docker-compose.yaml's
# own hardcoded default port instead of this script's isolated one.
export ESHU_POSTGRES_PORT="${ESHU_POSTGRES_PORT:-15636}"
export NEO4J_BOLT_PORT="${NEO4J_BOLT_PORT:-7793}"
export NEO4J_HTTP_PORT="${NEO4J_HTTP_PORT:-7680}"
: "${ESHU_POSTGRES_PASSWORD:=change-me}"
: "${ESHU_NEO4J_PASSWORD:=change-me}"
: "${GATE_DRAIN_TIMEOUT:=3m}"

compose_file="docker-compose.yaml"
cassette="${repo_root}/testdata/cassettes/gcpcloud/supply-chain-demo.json"
worker_counts=(1 2 4)

# SQL relationship family cassette (#5351): a committed cassette (unlike the
# generated synth cassette) that exercises the reducer's SQL relationship edge
# materialization across all seven materialized edge types (QUERIES_TABLE,
# READS_FROM, HAS_COLUMN, TRIGGERS, EXECUTES, INDEXES, MIGRATES). It is driven
# into every cell alongside the demo-org + synth-multiscope cassettes, and the
# materialized_edges:sql_relationships manifest row's proof_gate: ifa-determinism
# claim is backed by an ADDITIONAL per-cell absolute-set assertion (see the
# `ifa assert-edges` call after the drain): digest equality across N cannot
# catch a family silently empty in ALL cells, the absolute expected set can.
sql_cassette="${repo_root}/testdata/cassettes/sqlrelationships/ifa-sql-family.json"
sql_expected_edges="${repo_root}/go/internal/ifa/testdata/sqlrelationships/ifa-sql-family-expected-edges.json"
sql_delta_cassette="${repo_root}/testdata/cassettes/sqlrelationships/ifa-sql-family-delta.json"
sql_delta_expected_edges="${repo_root}/go/internal/ifa/testdata/sqlrelationships/ifa-sql-family-delta-live-expected-edges.json"

# synth-multiscope cassette settings (issue #4396 slice 6b): a fixed seed so
# the generated cassette is byte-identical across every cell (and across
# repeated runs of this script), 8 disjoint GCP project scopes, 64 resources
# each. Generated ONCE below (before the cell loop) into work_dir and never
# checked into testdata/ — every run regenerates it from scratch.
: "${SYNTH_MULTISCOPE_SEED:=4396}"
: "${SYNTH_MULTISCOPE_PROJECTS:=8}"
: "${SYNTH_MULTISCOPE_RESOURCES:=64}"

# #5007 contention cassette settings (opt-in via --contention). Unlike the
# disjoint synth-multiscope cassette above, these scopes deliberately SHARE one
# resource-identity set (gcpsynth.GenerateOverlappingScope): all
# SYNTH_CONTENTION_PROJECTS scopes fold to the SAME CloudResource node uids, so
# the reducer's cross-scope writers contend on one shared node, which the #5007
# owner ledger must resolve to the max-(observed_at, source_fact_id) contributor
# identically across N=1/2/4 (GREEN); without the ledger the contended node is
# last-writer-wins by commit order and the digest diverges (RED).
: "${SYNTH_CONTENTION_SEED:=5007}"
: "${SYNTH_CONTENTION_PROJECTS:=4}"
: "${SYNTH_CONTENTION_RESOURCES:=32}"

use_compose=1
keep=0
teeth=0
contention=0
for arg in "$@"; do
	case "${arg}" in
	--no-compose) use_compose=0 ;;
	--keep) keep=1 ;;
	--teeth) teeth=1 ;;
	--contention) contention=1 ;;
	-h | --help)
		sed -n '2,106p' "${BASH_SOURCE[0]}"
		exit 0
		;;
	*)
		echo "verify-ifa-determinism: unknown argument: ${arg}" >&2
		exit 2
		;;
	esac
done

# build_tags is threaded through every ifa_det_build_bin call below. Empty
# (the default) builds exactly what every normal/CI/production build links;
# --teeth is the only thing that ever sets it, to compile in the
# ifadeterminismteeth build-tag-gated fault described above.
build_tags=""
if [[ "${teeth}" -eq 1 ]]; then
	build_tags="ifadeterminismteeth"
fi

[[ -f "${cassette}" ]] || { echo "verify-ifa-determinism: cassette not found: ${cassette}" >&2; exit 1; }
[[ -f "${sql_cassette}" ]] || { echo "verify-ifa-determinism: SQL cassette not found: ${sql_cassette}" >&2; exit 1; }
[[ -f "${sql_expected_edges}" ]] || { echo "verify-ifa-determinism: SQL expected-edge set not found: ${sql_expected_edges}" >&2; exit 1; }
[[ -f "${sql_delta_cassette}" ]] || { echo "verify-ifa-determinism: SQL delta cassette not found: ${sql_delta_cassette}" >&2; exit 1; }
[[ -f "${sql_delta_expected_edges}" ]] || { echo "verify-ifa-determinism: SQL delta expected-edge set not found: ${sql_delta_expected_edges}" >&2; exit 1; }

work_dir="$(mktemp -d -t ifa-determinism.XXXXXX)"
bin_dir="${work_dir}/bin"
log_dir="${work_dir}/logs"
mkdir -p "${bin_dir}" "${log_dir}"

bg_pids=()

log() { printf '\n=== %s ===\n' "$*"; }
die() { printf 'verify-ifa-determinism: %s\n' "$*" >&2; exit 1; }

cleanup() {
	local status=$?
	if [[ "${status}" -ne 0 && -d "${log_dir}" ]]; then
		printf '\n=== host binary logs (failure) ===\n' >&2
		for logf in "${log_dir}"/*.log; do
			[[ -f "${logf}" ]] || continue
			printf '\n--- %s ---\n' "$(basename "${logf}")" >&2
			tail -40 "${logf}" >&2 || true
		done
	fi
	for pid in "${bg_pids[@]:-}"; do
		[[ -n "${pid}" ]] && kill "${pid}" >/dev/null 2>&1 || true
	done
	if [[ "${keep}" -eq 1 ]]; then
		printf '\n[--keep] work dir retained: %s\n' "${work_dir}" >&2
	else
		if [[ "${use_compose}" -eq 1 ]]; then
			docker compose -p "${DETERMINISM_COMPOSE_PROJECT}" -f "${compose_file}" down -v >/dev/null 2>&1 || true
		fi
		rm -rf "${work_dir}"
	fi
	exit "${status}"
}
trap cleanup EXIT

# ----------------------------------------------------------------------------
# Shared runtime environment for every host binary. Every URL/DSN below points
# at localhost on this script's own ports — never the in-Compose-network
# hostnames docker-compose.yaml's own db-migrate service uses.
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
# Every Lifecycle binary (projector, reducer) starts an operator status server
# and a metrics scrape server, both defaulting to fixed ports; run concurrently
# they would collide, so each process gets an ephemeral port (mirrors
# verify-golden-corpus-gate.sh / verify-ifa-replay-drive.sh).
export ESHU_LISTEN_ADDR="127.0.0.1:0"
export ESHU_METRICS_ADDR="127.0.0.1:0"
unset ESHU_PPROF_ADDR || true

if [[ "${teeth}" -eq 1 ]]; then
	log "build host binaries (--teeth: -tags ${build_tags})"
else
	log "build host binaries"
fi
ifa_det_build_bin "${bin_dir}" bootstrap-data-plane "${build_tags}" || die "build bootstrap-data-plane failed"
ifa_det_build_bin "${bin_dir}" ifa "${build_tags}" || die "build ifa failed"
ifa_det_build_bin "${bin_dir}" projector "${build_tags}" || die "build projector failed"
ifa_det_build_bin "${bin_dir}" reducer "${build_tags}" || die "build reducer failed"
ifa_det_build_bin "${bin_dir}" golden-corpus-gate "${build_tags}" || die "build golden-corpus-gate failed"

# Generate the synth-multiscope cassette ONCE, before the cell loop, so every
# cell drives the exact same byte-identical fixture (issue #4396 slice 6b).
# This is pure disk-and-memory work (go/internal/synth/gcp.GenerateMultiScope
# via `ifa synth-cassette`): no database or graph backend is touched, so it
# does not need to run per-cell or after the compose stack comes up.
log "generate synth-multiscope cassette (seed=${SYNTH_MULTISCOPE_SEED} projects=${SYNTH_MULTISCOPE_PROJECTS} resources=${SYNTH_MULTISCOPE_RESOURCES})"
synth_cassette="${work_dir}/synth-multiscope.json"
"${bin_dir}/eshu-ifa" synth-cassette \
	-seed "${SYNTH_MULTISCOPE_SEED}" \
	-projects "${SYNTH_MULTISCOPE_PROJECTS}" \
	-resources "${SYNTH_MULTISCOPE_RESOURCES}" \
	-out "${synth_cassette}" \
	|| die "ifa synth-cassette failed"
[[ -s "${synth_cassette}" ]] || die "ifa synth-cassette produced an empty or missing file: ${synth_cassette}"

# #5007 contention cassette (opt-in): K scopes sharing one resource-identity
# set, so the reducer's cross-scope writers contend on the same CloudResource
# node. -divergent gives the shared-uid scopes divergent observed state.
# Generated ONCE, like the synth-multiscope cassette, and driven into every cell.
contention_cassette=""
if [[ "${contention}" -eq 1 ]]; then
	log "generate #5007 contention cassette (seed=${SYNTH_CONTENTION_SEED} projects=${SYNTH_CONTENTION_PROJECTS} resources=${SYNTH_CONTENTION_RESOURCES} -overlap -divergent)"
	contention_cassette="${work_dir}/contention.json"
	"${bin_dir}/eshu-ifa" synth-cassette \
		-overlap -divergent \
		-seed "${SYNTH_CONTENTION_SEED}" \
		-projects "${SYNTH_CONTENTION_PROJECTS}" \
		-resources "${SYNTH_CONTENTION_RESOURCES}" \
		-out "${contention_cassette}" \
		|| die "ifa synth-cassette -overlap failed"
	[[ -s "${contention_cassette}" ]] || die "ifa synth-cassette -overlap produced an empty or missing file: ${contention_cassette}"
fi

declare -A digests
declare -A wall_times

for n in "${worker_counts[@]}"; do
	log "cell N=${n}: fresh stack"
	cell_start=$(date +%s)

	if [[ "${use_compose}" -eq 1 ]]; then
		docker compose -p "${DETERMINISM_COMPOSE_PROJECT}" -f "${compose_file}" up -d nornicdb postgres
		log "N=${n}: wait for backends"
		ifa_det_wait_for_backends "${DETERMINISM_COMPOSE_PROJECT}" "${compose_file}" \
			|| die "N=${n}: Postgres + NornicDB did not become ready within budget"
	fi

	log "N=${n}: apply Postgres + graph schema (eshu-bootstrap-data-plane)"
	"${bin_dir}/eshu-bootstrap-data-plane" >"${log_dir}/bootstrap-data-plane-n${n}.log" 2>&1 \
		|| { tail -40 "${log_dir}/bootstrap-data-plane-n${n}.log"; die "N=${n}: bootstrap-data-plane failed"; }

	log "N=${n}: drive demo-org GCP cassette through eshu-ifa drive -workers ${n}"
	if ! "${bin_dir}/eshu-ifa" drive -cassette "${cassette}" -workers "${n}" \
		>"${log_dir}/ifa-drive-n${n}.log" 2>&1; then
		tail -40 "${log_dir}/ifa-drive-n${n}.log" >&2 || true
		die "N=${n}: eshu-ifa drive failed"
	fi
	cat "${log_dir}/ifa-drive-n${n}.log"

	# Second drive into the SAME cell stack: the generated synth-multiscope
	# cassette (8 disjoint GCP project scopes), driven at the SAME -workers N
	# as the demo-org drive above. This is what makes -workers N non-inert
	# (issue #4396 slice 6b): the demo-org cassette alone is one work unit for
	# any N, so this second drive adds the K genuinely independent work units
	# concurrentreplay.Driver needs to actually vary commit interleaving with
	# N. Both drives commit into the same fact_work_items queue before the
	# single drain below runs, so the resulting graph is demo-org truth PLUS
	# the K disjoint synth projects, and the digest below covers the whole
	# combined graph.
	log "N=${n}: drive synth-multiscope cassette through eshu-ifa drive -workers ${n}"
	if ! "${bin_dir}/eshu-ifa" drive -cassette "${synth_cassette}" -workers "${n}" \
		>"${log_dir}/ifa-drive-synth-n${n}.log" 2>&1; then
		tail -40 "${log_dir}/ifa-drive-synth-n${n}.log" >&2 || true
		die "N=${n}: eshu-ifa drive (synth-multiscope) failed"
	fi
	cat "${log_dir}/ifa-drive-synth-n${n}.log"

	# Add the committed seven-edge SQL family to the same durable cell.
	ifa_det_drive_sql_baseline "${n}" "${bin_dir}" "${sql_cassette}" "${log_dir}" \
		|| die "N=${n}: SQL relationship baseline drive failed"

	# Fourth drive (opt-in --contention): the #5007 overlapping-identity cassette.
	# Its K scopes all contend on the same CloudResource nodes; the owner ledger
	# must make the contended node's final state identical across N=1/2/4. A
	# digest divergence that appears ONLY with --contention is a real ledger
	# regression, not a scan-order artifact.
	if [[ "${contention}" -eq 1 ]]; then
		log "N=${n}: drive #5007 contention cassette through eshu-ifa drive -workers ${n}"
		if ! "${bin_dir}/eshu-ifa" drive -cassette "${contention_cassette}" -workers "${n}" \
			>"${log_dir}/ifa-drive-contention-n${n}.log" 2>&1; then
			tail -40 "${log_dir}/ifa-drive-contention-n${n}.log" >&2 || true
			die "N=${n}: eshu-ifa drive (#5007 contention) failed"
		fi
		cat "${log_dir}/ifa-drive-contention-n${n}.log"
	fi

	# Prove both drives actually enqueued something before the drain runs: a
	# residual=0 reading over a queue nothing was ever put in would be a
	# vacuous drain proof.
	work_items="$(ifa_det_pg "${DETERMINISM_COMPOSE_PROJECT}" "${use_compose}" "${ESHU_POSTGRES_DSN}" \
		'SELECT count(*) FROM fact_work_items;' "${compose_file}" | tr -d '[:space:]')"
	[[ -n "${work_items}" && "${work_items}" -gt 0 ]] \
		|| die "N=${n}: eshu-ifa drive committed but enqueued 0 fact_work_items rows (vacuous drain proof)"
	printf 'N=%s fact_work_items enqueued (demo-org + synth-multiscope + SQL family): %s\n' "${n}" "${work_items}"

	log "N=${n}: drain projector + reducer (gate polls to the B-12 residual bound)"
	bg_pids=()
	ifa_det_start_bg "${log_dir}" "projector-n${n}" projector_pid "${bin_dir}/eshu-projector"
	ifa_det_start_bg "${log_dir}" "reducer-n${n}" reducer_pid "${bin_dir}/eshu-reducer"

	if ! "${bin_dir}/eshu-golden-corpus-gate" \
		-phase=drains \
		-snapshot=testdata/golden/e2e-20repo-snapshot.json \
		-drain-timeout="${GATE_DRAIN_TIMEOUT}"; then
		tail -30 "${log_dir}/reducer-n${n}.log" || true
		tail -30 "${log_dir}/projector-n${n}.log" || true
		die "N=${n}: drain did not reach the snapshot's residual bound within ${GATE_DRAIN_TIMEOUT}"
	fi
	kill "${projector_pid}" "${reducer_pid}" >/dev/null 2>&1 || true

	ifa_det_assert_sql_baseline "${n}" "${bin_dir}" "${sql_expected_edges}" \
		|| die "N=${n}: SQL relationship baseline assertion failed"

	# #5554: gen 2 reuses source_run_id in this same durable cell and retargets
	# INDEXES, exercising the generation-aware refresh fence end to end.
	ifa_det_run_sql_delta_live \
		"${n}" "${bin_dir}" "${sql_delta_cassette}" "${sql_delta_expected_edges}" "${log_dir}" \
		"${DETERMINISM_COMPOSE_PROJECT}" "${use_compose}" "${ESHU_POSTGRES_DSN}" "${compose_file}" "${GATE_DRAIN_TIMEOUT}" \
		|| die "N=${n}: SQL delta-live proof failed"

	log "N=${n}: canonicalize post-delta graph (ifa graph-dump)"
	"${bin_dir}/eshu-ifa" graph-dump -out "${work_dir}/graph-n${n}.dump" \
		|| die "N=${n}: ifa graph-dump (canonical bytes) failed"
	digest_n="$("${bin_dir}/eshu-ifa" graph-dump -digest | tr -d '[:space:]')"
	[[ -n "${digest_n}" ]] || die "N=${n}: ifa graph-dump -digest returned empty output"
	digests[${n}]="${digest_n}"
	printf 'N=%s post-delta digest: %s\n' "${n}" "${digest_n}"

	if [[ "${use_compose}" -eq 1 ]]; then
		log "N=${n}: tear down cell (fresh stack for the next cell)"
		docker compose -p "${DETERMINISM_COMPOSE_PROJECT}" -f "${compose_file}" down -v >/dev/null 2>&1 || true
	fi

	cell_end=$(date +%s)
	wall_times[${n}]=$((cell_end - cell_start))
	printf 'N=%s cell wall time: %ss\n' "${n}" "${wall_times[${n}]}"
done

log "compare digests across N=${worker_counts[*]}"
first_n="${worker_counts[0]}"
mismatch=0
for n in "${worker_counts[@]}"; do
	[[ "${n}" == "${first_n}" ]] && continue
	if [[ "${digests[${n}]}" != "${digests[${first_n}]}" ]]; then
		mismatch=1
		printf 'MISMATCH: N=%s digest (%s) != N=%s digest (%s)\n' \
			"${n}" "${digests[${n}]}" "${first_n}" "${digests[${first_n}]}" >&2
		printf '\n=== full canonical graph diff: N=%s vs N=%s (failure artifact) ===\n' \
			"${first_n}" "${n}" >&2
		diff -u "${work_dir}/graph-n${first_n}.dump" "${work_dir}/graph-n${n}.dump" >&2 || true
	fi
done

if [[ "${teeth}" -eq 1 ]]; then
	if [[ "${mismatch}" -eq 1 ]]; then
		log "TEETH: CAUGHT — digests diverged across N=${worker_counts[*]} under -tags ${build_tags} (see the full-dump diff above). This is the EXPECTED outcome of --teeth, not a real regression: the deliberately non-idempotent CloudResource write (r.ifa_teeth_write_order) made the matrix go red, proving it actually catches a non-idempotent write instead of passing vacuously. Per the acceptance clause, exit non-zero here IS teeth pass."
		for n in "${worker_counts[@]}"; do
			printf '  N=%s digest=%s wall=%ss\n' "${n}" "${digests[${n}]}" "${wall_times[${n}]}"
		done
		die "graph-determinism matrix FAILED as --teeth expected (digests diverged under the ifadeterminismteeth build tag) — this exit is intentional; see the TEETH: CAUGHT line above. Do not lower N, retry, or otherwise normalize this away, in --teeth mode or in a real failure."
	fi
	die "TEETH FAILED: matrix stayed GREEN across N=${worker_counts[*]} even under -tags ${build_tags} (digests: ${digests[${worker_counts[0]}]}) — the deliberately non-idempotent CloudResource write did not manifest this run, so --teeth did not prove anything. This is the teeth being broken (or too weak to reach), not the matrix being healthy; do not treat this as a passing verification."
fi

[[ "${mismatch}" -eq 0 ]] || die "graph-determinism matrix FAILED: digests diverged across worker counts (see the full-dump diff above) — this is a real concurrency defect in the reducer/projector graph write path; do NOT lower N, retry, or otherwise normalize this away"

log "PASS: graph-determinism matrix green across N=${worker_counts[*]} (project ${DETERMINISM_COMPOSE_PROJECT}, postgres:${ESHU_POSTGRES_PORT}, neo4j-bolt:${NEO4J_BOLT_PORT})"
for n in "${worker_counts[@]}"; do
	printf '  N=%s digest=%s wall=%ss\n' "${n}" "${digests[${n}]}" "${wall_times[${n}]}"
done
