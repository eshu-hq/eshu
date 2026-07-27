#!/usr/bin/env bash
# B-7 golden end-to-end corpus gate (#3800). The ONE command that runs the full
# pipeline (sync -> discover -> parse -> collect -> reduce -> query) over a
# minimal repo corpus with the B-10 cassette collectors, drains every queue, and
# diffs the result against the B-12 golden snapshot via golden-corpus-gate.
#
# Mirrors the e2e-tests.yml substrate: Docker Compose brings up Postgres and the
# graph backend, host binaries drive the pipeline. The reducer and projector
# have no finite drain mode, so they run as background processes that the gate's
# drain phase polls to terminal, then this script stops them.
#
# Usage:
#   scripts/verify-golden-corpus-gate.sh [--no-compose] [--keep]
#     --no-compose  assume Postgres + graph are already running (CI brings them
#                   up in a separate step); skip compose up/down here.
#     --keep        leave services running and the work dir in place on exit
#                   (for debugging a failed run).
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "${repo_root}"

# B-11 (#3804) per-phase timing/baseline helper (defines emit_phase_timings_and_flags).
# shellcheck source=scripts/lib/golden-corpus-phase-timings.sh
. "${repo_root}/scripts/lib/golden-corpus-phase-timings.sh"
# shellcheck source=scripts/lib/golden-corpus-dead-code-fixtures.sh
. "${repo_root}/scripts/lib/golden-corpus-dead-code-fixtures.sh"
# host_tcp_port_open: HOST-side TCP readiness probe (#5813) — see the lib's
# header for why the "wait for backends" loop below needs this in addition
# to the in-container pg_isready check.
# shellcheck source=scripts/lib/golden-corpus-readiness.sh
. "${repo_root}/scripts/lib/golden-corpus-readiness.sh"
# shellcheck source=scripts/lib/golden-corpus-vulnerability-suppression.sh
. "${repo_root}/scripts/lib/golden-corpus-vulnerability-suppression.sh"

# ----------------------------------------------------------------------------
# Configuration (override via environment).
#
# #5813: NEO4J_HTTP_PORT/NEO4J_BOLT_PORT/ESHU_POSTGRES_PORT/GATE_API_PORT/
# GATE_MCP_PORT below isolate this run from the DEFAULT ports, but they are a
# SHARED CONVENTION, not per-run isolation — the compose PROJECT name (derived
# from this directory) isolates containers and volumes, but Docker still binds
# these to fixed HOST ports. Two concurrent runs on one machine that both set
# the same override values collide. The benign failure is
# `Bind for 0.0.0.0:<port> failed: port is already allocated` at `docker
# compose up`. The DANGEROUS failure is silent: if a foreign, unrelated
# stack already owns ESHU_POSTGRES_PORT, this run's HOST binaries
# (bootstrap-index, collectors, reducer/projector) happily connect over TCP
# to THAT stack's Postgres and read/write its data — producing dead-lettered
# work items and query-phase assertion failures that look exactly like a
# flake. Concurrent runs MUST pick a genuinely private port set and verify
# each port is free before starting.
# ----------------------------------------------------------------------------
: "${ESHU_GRAPH_BACKEND:=nornicdb}"
: "${ESHU_POSTGRES_PORT:=15432}"
: "${NEO4J_BOLT_PORT:=7687}"
: "${NEO4J_HTTP_PORT:=7474}"
: "${ESHU_NEO4J_PASSWORD:=change-me}"
: "${ESHU_POSTGRES_PASSWORD:=change-me}"
: "${GATE_API_PORT:=18080}"   # off the default 8080 so a sibling stack does not collide
: "${GATE_MCP_PORT:=18091}"   # eshu-mcp-server http transport for B-7(c) MCP query truth
: "${GATE_API_KEY:=golden-corpus-gate-local-key}"
: "${GATE_DRAIN_TIMEOUT:=10m}"
: "${GATE_BUDGET_SECONDS:=900}"   # baseline wall-time budget; ceiling is 2x.
: "${GATE_BUDGET_MULTIPLIER:=2}"
: "${GATE_COLLECTOR_SETTLE_SECONDS:=20}"

compose_file="docker-compose.yaml"
graph_service="nornicdb"
database="nornic"
if [[ "${ESHU_GRAPH_BACKEND}" == "neo4j" ]]; then
	compose_file="docker-compose.neo4j.yml"
	graph_service="neo4j"
	database="neo4j"
fi

# shellcheck source=scripts/lib/golden-corpus-fixtures.sh
source "${repo_root}/scripts/lib/golden-corpus-fixtures.sh"
# 9 credentialed collectors and their B-10 cassette directories.
collector_specs=(
	"collector-kubernetes-live:kuberneteslive"
	"collector-aws-cloud:awscloud"
	"collector-azure-cloud:azurecloud"
	"collector-gcp-cloud:gcpcloud"
	"collector-vault-live:vaultlive"
	"collector-oci-registry:ociregistry"
	"collector-cicd-run:cicdrun"
	"collector-package-registry:packageregistry"
	"collector-terraform-state:terraformstate"
	"collector-prometheus-mimir:prometheusmimir"
	"collector-tempo:tempo"
	"collector-grafana:grafana"
	"collector-loki:loki"
	"collector-security-alerts:securityalerts"
	"collector-jira:jira"
	"collector-pagerduty:pagerduty"
	"collector-sbom-attestation:sbomattestation"
	"collector-vulnerability-intelligence:vulnerabilityintelligence"
)
cassette_recording="supply-chain-demo.json"

use_compose=1
keep=0
for arg in "$@"; do
	case "${arg}" in
		--no-compose) use_compose=0 ;;
		--keep) keep=1 ;;
		-h|--help) sed -n '2,20p' "${BASH_SOURCE[0]}"; exit 0 ;;
		*) echo "verify-golden-corpus-gate: unknown argument: ${arg}" >&2; exit 2 ;;
	esac
done

bg_pids=()

log() { printf '\n=== %s ===\n' "$*"; }
die() { printf 'verify-golden-corpus-gate: %s\n' "$*" >&2; exit 1; }

. "${repo_root}/scripts/lib/live-gate-lock.sh"
acquire_live_gate_lock

# After the mutex, so a refused run leaves no temp dir. The EXIT trap stays
# further down on purpose: cleanup runs `docker compose down -v`, and arming it
# before the lock is held would let a blocked run kill the live holder's stack.
work_dir="$(mktemp -d -t golden-corpus-gate.XXXXXX)"
bin_dir="${work_dir}/bin"
corpus_dir="${work_dir}/corpus"
home_dir="${work_dir}/home"
log_dir="${work_dir}/logs"
mkdir -p "${bin_dir}" "${corpus_dir}" "${home_dir}" "${log_dir}"

# Extracted to scripts/lib/golden-corpus-cleanup.sh (defines cleanup()) to keep
# this orchestrator under the 500-line cap.
# shellcheck source=scripts/lib/golden-corpus-cleanup.sh
. "${repo_root}/scripts/lib/golden-corpus-cleanup.sh"
trap cleanup EXIT

# ----------------------------------------------------------------------------
# Shared runtime environment for every host binary.
# ----------------------------------------------------------------------------
export ESHU_GRAPH_BACKEND
export NEO4J_URI="bolt://localhost:${NEO4J_BOLT_PORT}"
export NEO4J_USERNAME="neo4j"
export NEO4J_PASSWORD="${ESHU_NEO4J_PASSWORD}"
export NEO4J_DATABASE="${database}"
export ESHU_NEO4J_DATABASE="${database}"
export DEFAULT_DATABASE="${database}"
export ESHU_POSTGRES_DSN="postgresql://eshu:${ESHU_POSTGRES_PASSWORD}@localhost:${ESHU_POSTGRES_PORT}/eshu"
export ESHU_CONTENT_STORE_DSN="${ESHU_POSTGRES_DSN}"
export ESHU_HOME="${home_dir}"
export ESHU_REPOS_DIR="${work_dir}/repos"
export ESHU_REPO_SOURCE_MODE="filesystem"
export ESHU_FILESYSTEM_ROOT="${corpus_dir}"
export ESHU_GIT_AUTH_METHOD="none"
# Filesystem repos have no real git remote; the collector synthesizes one from the
# repo id + this org (repoRemoteURL in git_selection_discovery.go). Pinning it to a
# fictional org makes the corpus repos' remotes deterministic
# (github.com/acme/<repo>) so package-registry source_hint URLs resolve to the
# in-corpus owner repo, producing cross-repo DEPENDS_ON (rc-3).
export ESHU_GITHUB_ORG="acme"
export ESHU_REPOSITORY_RULES_JSON="[]"
export ESHU_QUERY_PROFILE="local_full_stack"
export ESHU_API_KEY="${GATE_API_KEY}"
export ESHU_API_ADDR=":${GATE_API_PORT}"
# ESHU_AUTH_BOOTSTRAP_MODE defaults to "generated" (#4963), which requires a
# configured data-encryption key to seal the one-time admin credential;
# without it eshu-api fails closed at startup and /readyz never returns.
# Fixed, publicly-known, all-zero dev-only placeholder — never a real secret.
export ESHU_AUTH_SECRET_ENC_KEY="AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA="
# Every Lifecycle binary (the 9 collectors, projector, reducer) starts an
# operator status server on ESHU_LISTEN_ADDR and a metrics scrape server on
# ESHU_METRICS_ADDR, both defaulting to fixed ports (8080 / 9464). Run
# concurrently they would all collide on those ports and exit on startup, so each
# process gets an ephemeral port. The api's data server is separate
# (ESHU_API_ADDR, set above) and is the one the gate queries. pprof stays off
# because ESHU_PPROF_ADDR is left unset.
export ESHU_LISTEN_ADDR="127.0.0.1:0"
export ESHU_METRICS_ADDR="127.0.0.1:0"
unset ESHU_PPROF_ADDR || true
mkdir -p "${ESHU_REPOS_DIR}"

# Extracted to scripts/lib/golden-corpus-host-helpers.sh (defines build_bin,
# start_bg, pg) to keep this orchestrator under the 500-line cap.
# shellcheck source=scripts/lib/golden-corpus-host-helpers.sh
. "${repo_root}/scripts/lib/golden-corpus-host-helpers.sh"

# ----------------------------------------------------------------------------
log "stage minimal corpus (${#corpus_fixtures[@]} repos)"
# Extracted to scripts/lib/golden-corpus-stage.sh (defines
# stage_minimal_corpus) to keep this orchestrator under the 500-line cap.
# shellcheck source=scripts/lib/golden-corpus-stage.sh
. "${repo_root}/scripts/lib/golden-corpus-stage.sh"
stage_minimal_corpus

log "build host binaries"
build_bin bootstrap-index
build_bin projector
build_bin reducer
build_bin api
build_bin mcp-server
build_bin golden-corpus-gate
for spec in "${collector_specs[@]}"; do build_bin "${spec%%:*}"; done

if [[ "${use_compose}" -eq 1 ]]; then
	log "start Postgres + ${graph_service}"
	ESHU_NEO4J_PASSWORD="${ESHU_NEO4J_PASSWORD}" ESHU_POSTGRES_PASSWORD="${ESHU_POSTGRES_PASSWORD}" \
		docker compose -f "${compose_file}" up -d "${graph_service}" postgres

	log "wait for backends"
	backends_ready=false
	for _ in $(seq 1 60); do
		graph_ready=false
		# NornicDB has no analogue to Postgres's initdb temp-server dance below:
		# it is a single Go binary whose HTTP and Bolt listeners both come up in
		# one startup sequence (docker logs show the HTTP "ready" banner and the
		# "bolt server listening" line land within the same startup burst, not
		# across a shutdown/restart), and this check performs a real TCP+HTTP
		# round trip rather than a socket-only probe like pg_isready's default.
		# So an in-container success here is not a false positive relative to
		# the host mapping — verified live: a host `curl` against the mapped
		# port succeeds the moment this in-container check does. No
		# host-vs-container fix needed for this branch.
		if [[ "${graph_service}" == "nornicdb" ]]; then
			docker compose -f "${compose_file}" exec -T nornicdb wget --spider -q http://localhost:7474/health >/dev/null 2>&1 && graph_ready=true
		else
			docker compose -f "${compose_file}" exec -T neo4j cypher-shell -u neo4j -p "${ESHU_NEO4J_PASSWORD}" "RETURN 1" >/dev/null 2>&1 && graph_ready=true
		fi
		# Postgres readiness requires BOTH the in-container socket probe AND a
		# HOST-side TCP connect (host_tcp_port_open, golden-corpus-readiness.sh)
		# — belt and braces. Do NOT drop either: pg_isready alone races the
		# initdb temp-server window (see the lib's header); the TCP probe alone
		# would drop the existing container-side signal for no reason.
		if [[ "${graph_ready}" == "true" ]] && \
			docker compose -f "${compose_file}" exec -T postgres pg_isready -U eshu -d eshu >/dev/null 2>&1 && \
			host_tcp_port_open 127.0.0.1 "${ESHU_POSTGRES_PORT}"; then
			backends_ready=true
			break
		fi
		sleep 2
	done
	[[ "${backends_ready}" == "true" ]] || die "Postgres + ${graph_service} did not become ready within budget"
fi

pipeline_start="$(date +%s)"
# B-11 (#3804) macro per-phase wall-clock capture. Integer-second epochs are used
# (portable across GNU and BSD date; %N is GNU-only) — the 10-15% regression band
# absorbs the sub-second rounding. Phase boundaries mirror the orchestration
# below; the deltas are emitted to phase-timings.json just before the final gate,
# which compares them against testdata/golden/e2e-baseline.json.
phase_bootstrap_start="${pipeline_start}"

log "bootstrap-index over minimal corpus (schema + filesystem facts + projection)"
"${bin_dir}/eshu-bootstrap-index" >"${log_dir}/bootstrap-index.log" 2>&1 \
	|| { tail -40 "${log_dir}/bootstrap-index.log"; die "bootstrap-index failed"; }
phase_bootstrap_end="$(date +%s)"
phase_collect_start="${phase_bootstrap_end}"

log "replay B-10 cassette collectors (credential-free)"
collector_pids=()
collector_names=()
for spec in "${collector_specs[@]}"; do
	cmd="${spec%%:*}"
	dir="${spec##*:}"
	cassette="${repo_root}/testdata/cassettes/${dir}/${cassette_recording}"
	[[ -f "${cassette}" ]] || die "cassette not found: ${cassette}"
	start_bg "${cmd}" cpid "${bin_dir}/eshu-${cmd}" -mode=cassette -cassette-file="${cassette}"
	collector_pids+=("${cpid}")
	collector_names+=("${cmd}")
done
printf 'launched %d collectors; settling %ss for first-pass commit\n' "${#collector_pids[@]}" "${GATE_COLLECTOR_SETTLE_SECONDS}"
sleep "${GATE_COLLECTOR_SETTLE_SECONDS}"
# A collector that crashed on startup (cassette parse, Postgres connect) exited
# during the settle. Catch that before killing, so a silently-dead collector does
# not let the gate pass with the cassette half of the pipeline unverified.
for i in "${!collector_pids[@]}"; do
	if ! kill -0 "${collector_pids[$i]}" >/dev/null 2>&1; then
		tail -20 "${log_dir}/${collector_names[$i]}.log" >&2 || true
		die "collector ${collector_names[$i]} exited during settle (did not stay up to commit)"
	fi
done
for pid in "${collector_pids[@]}"; do kill "${pid}" >/dev/null 2>&1 || true; done

# Prove the cassette facts actually landed: each credentialed collector must have
# produced at least one ingestion scope. Without this, all 9 collectors could
# no-op and the gate would still pass (Repository nodes come from filesystem
# discovery, not collectors).
collector_sources="$(pg "SELECT count(DISTINCT source_system) FROM ingestion_scopes WHERE source_system <> 'git';" | tr -d '[:space:]')"
: "${GATE_MIN_COLLECTOR_SOURCES:=${#collector_specs[@]}}"
if [[ -z "${collector_sources}" ]] || (( collector_sources < GATE_MIN_COLLECTOR_SOURCES )); then
	die "only ${collector_sources:-0} credentialed collector source(s) landed facts; want >= ${GATE_MIN_COLLECTOR_SOURCES} (cassette replay did not commit)"
fi
printf 'cassette facts landed: %s credentialed collector sources\n' "${collector_sources}"
phase_collect_end="$(date +%s)"
phase_first_drain_start="${phase_collect_end}"

log "drain projector + reducer — first pass (background; gate polls to terminal)"
start_bg projector projector_pid "${bin_dir}/eshu-projector"
start_bg reducer reducer_pid "${bin_dir}/eshu-reducer"

# First drain: let the reducer reach terminal. This produces the cross-scope
# package-registry ownership facts (reducer_package_ownership_correlation) from
# the cassette source hints, and runs the code_import_repo_edge projection once —
# which resolves no owner yet (the ownership fact is produced in the same drain)
# and so succeeds as a retraction. We do not assert here; this is the shard-drain
# whose completion the deferred maintenance below keys off.
if ! "${bin_dir}/eshu-golden-corpus-gate" \
	-phase=drains \
	-snapshot=testdata/golden/e2e-20repo-snapshot.json \
	-require-populated-domains="repo_dependency" \
	-drain-timeout="${GATE_DRAIN_TIMEOUT}"; then
	tail -30 "${log_dir}/reducer.log" || true
	tail -30 "${log_dir}/projector.log" || true
	die "first drain pass failed"
fi
kill "${projector_pid}" "${reducer_pid}" >/dev/null 2>&1 || true
phase_first_drain_end="$(date +%s)"
phase_maintenance_start="${phase_first_drain_end}"

# Deferred maintenance + drain cycles. Extracted to
# scripts/lib/golden-corpus-maintenance-drains.sh (defines
# run_maintenance_drain_cycles, and carries the full rationale for the cycle
# count and for what the loop does and does not prove) to keep this
# orchestrator under the 500-line cap.
# shellcheck source=scripts/lib/golden-corpus-maintenance-drains.sh
. "${repo_root}/scripts/lib/golden-corpus-maintenance-drains.sh"
run_maintenance_drain_cycles

pipeline_end="$(date +%s)"
elapsed=$(( pipeline_end - pipeline_start ))
phase_maintenance_end="${pipeline_end}"
phase_graph_query_start="${pipeline_end}"

log "seed cross-repo dead-code query fixture"
seed_cross_repo_dead_code_fixture

log "start eshu-api for query truth"
start_bg api api_pid "${bin_dir}/eshu-api"
api_ready=false
for _ in $(seq 1 30); do
	if curl -fsS "http://localhost:${GATE_API_PORT}/readyz" >/dev/null 2>&1; then
		api_ready=true
		break
	fi
	sleep 1
done
[[ "${api_ready}" == "true" ]] || { tail -30 "${log_dir}/api.log" >&2 || true; die "eshu-api /readyz never returned on port ${GATE_API_PORT}"; }

log "B-7 suppression producer truth: active -> hidden -> expired"
golden_suppression_verify_producer_truth

log "start eshu-mcp-server (http) for MCP query truth"
ESHU_MCP_TRANSPORT=http ESHU_MCP_ADDR=":${GATE_MCP_PORT}" \
	start_bg mcp-server mcp_pid "${bin_dir}/eshu-mcp-server"
mcp_ready=false
for _ in $(seq 1 30); do
	if curl -fsS "http://localhost:${GATE_MCP_PORT}/health" >/dev/null 2>&1; then
		mcp_ready=true
		break
	fi
	sleep 1
done
[[ "${mcp_ready}" == "true" ]] || { tail -30 "${log_dir}/mcp-server.log" >&2 || true; die "eshu-mcp-server /health never returned on port ${GATE_MCP_PORT}"; }

log "B-7(b) graph truth + B-7(c) query truth + B-7(d) timing"
# Minimal-corpus graph assertions prove projection plus deterministic
# correlations from static fixtures and credential-free cassettes.
# rc-1 uses the deployable-source/deployable-config pair; rc-2 and rc-8 use the
# api-svc Flask/Kubernetes bridge, with php_comprehensive covering Laravel route
# resolution. rc-4 uses matching kuberneteslive/ociregistry digest evidence.
# rc-5/rc-6 and rc-27/rc-28 use terraform_comprehensive Atlantis and GitLab
# fixtures. rc-11..rc-23 cover the already-materialized code, workload,
# deployable, and evidence edge families. rc-9/rc-10 remain reserved until
# their fixture/projection work is complete.
# rc-164/rc-165 require #5457 PUBLISHES/BUILT_FROM provenance. rc-164 uses the
# package_registry source_hint; rc-165 uses matching cicdrun/ociregistry
# digests and pins exact-digest OCI evidence. container_image_identity remains
# BUILT_FROM's sole writer after #5428 was rescinded; see #5827 and
# docs/internal/evidence/5428-built-from-projection-rescinded.md.
# rc-167 (DERIVED_FROM) is issue #5460's base-image lineage projection. Unlike
# rc-164/rc-165 it is driven by a STATIC-PARSE fixture rather than a cassette
# join: the container-base-lineage fixture's Dockerfile pins its final stage to
# a digest the ociregistry cassette also observes, and its k8s Deployment runs a
# second observed digest, so the base and the built image resolve as two
# exact_digest decisions anchored to the same repository. It pins
# attribution_basis=repository_single_base so a future, more precise CI/SLSA
# attribution cannot silently satisfy the Dockerfile-only assertion.
# rc-168 (HAS_ARTIFACT) uses #5458's package_artifact fixture and the same
# deferred-edge-group path as rc-24/rc-25.
# -required-correlations="all" (below) single-sources the blocking set from
# the snapshot's own required_correlations ids (#4596): promoting an rc-N to
# blocking is now a one-file edit (add/confirm it in
# testdata/golden/e2e-20repo-snapshot.json) instead of also hand-editing a
# second, duplicated comma-separated id list here. To stage a newly-added rc
# as advisory-only before it is proven, pass an explicit comma-separated
# subset instead of "all".
# B-11 (#3804): the sourced phase-timing helper emits wall-clock evidence and
# sets phase_timings_file plus phase_flags from the epochs captured above.
emit_phase_timings_and_flags

gate_status=0
# demo-answers (#4776): after graph/query truth, execute the five
# specs/demo-first-answers.v1.yaml questions live with their SPECIFIC arguments
# and assert each returns a populated answer. The generic query shapes assert
# each tool/route with example arguments; this phase guards the demo oracle's
# own pinned arguments so a demo answer cannot silently regress to empty.
"${bin_dir}/eshu-golden-corpus-gate" \
	-phase=graph,query,timing,demo-answers \
	-snapshot="${golden_suppression_runtime_snapshot}" \
	-demo-manifest=specs/demo-first-answers.v1.yaml \
	-api-base-url="http://localhost:${GATE_API_PORT}" \
	-mcp-base-url="http://localhost:${GATE_MCP_PORT}" \
	-graph-required-only=false \
	-required-node-labels="Repository,Directory,File,Function,AtlantisProject,AtlantisWorkflow,Platform,GitlabPipeline,GitlabJob,CloudAction" \
	-required-correlations="all" \
	-budget-seconds="${GATE_BUDGET_SECONDS}" \
	-budget-multiplier="${GATE_BUDGET_MULTIPLIER}" \
	-elapsed-seconds="${elapsed}" \
	${phase_flags[@]+"${phase_flags[@]}"} || gate_status=$?
kill "${api_pid}" >/dev/null 2>&1 || true
kill "${mcp_pid}" >/dev/null 2>&1 || true

if [[ "${gate_status}" -ne 0 ]]; then
	die "graph/query/timing phase failed (elapsed ${elapsed}s)"
fi

log "PASS: B-7 golden corpus gate green (elapsed ${elapsed}s, budget ceiling $((GATE_BUDGET_SECONDS * GATE_BUDGET_MULTIPLIER))s)"
