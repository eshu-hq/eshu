#!/usr/bin/env bash
# verify-read-api-latency-gate.sh — issue #6797: backend-backed read-API
# latency gate.
#
# Brings up the Compose Postgres+Neo4j pair, builds the in-tree eshu-core-api,
# the frozen satellite eshu-api, and read-api-latency-gate, seeds a synthetic
# corpus (ingestion scopes, generations, and fact_work_items across every
# collector kind, infra-labeled graph nodes, plus supply-chain impact finding
# facts rebuilt into the winners read model), then sweeps every no-arg GET
# route's p95 latency against testdata/benchmarks/read-api-route-budgets.txt.
#
# Serving order is seed -> core (backend-gated /readyz) -> satellite (proxied
# /readyz against --core-base-url) -> MCP -> sweep (issue #11170: the
# satellite serves nothing coreless, so starting it without a core can never
# pass). A second sweep leg restarts the core with
# ESHU_SUPPLY_CHAIN_IMPACT_WINNERS_READ=true and re-sweeps the same corpus
# into separate -winners reports after proving the two legs serve
# byte-identical findings.
#
# Unlike the golden corpus gate, this gate seeds directly via bulk SQL/Cypher
# writes (SeedPostgres/SeedGraph) instead of driving the real
# collect/reduce/project pipeline — the point is corpus SCALE for a latency
# proof, not pipeline correctness, and running ~800 scopes' worth of real
# collectors/reducer/projector through the pipeline would blow the CI time
# budget this gate has to stay inside.
#
# Usage:
#   scripts/verify-read-api-latency-gate.sh [--no-compose] [--keep]
#     --no-compose  assume Postgres + graph are already running; skip
#                   compose up/down here.
#     --keep        leave Postgres and the graph backend running (with the
#                   seeded corpus) and the work dir in place on exit, for
#                   debugging a failed run. eshu-api is stopped on exit; start
#                   your own build against the kept stack.
#
# GATE_STACK_DIR=<dir> takes docker-compose.yaml (and so the db-migrate image build
# context: migrations and graph schema) from that checkout instead of this one.
# The in-tree core (eshu-core-api) is always built from THIS checkout, so to
# measure a candidate fix to the core, run the script from a worktree at that
# commit; a database migrated by a different checkout than the core reads from
# would silently take the old read path.
#
# GATE_API_BIN=<path> uses that pre-built satellite eshu-api binary instead of
# building the pinned one from eshu-hq/eshu-api (see build_bin api) — for a
# RED/GREEN comparison against a different satellite commit.
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "${repo_root}"

# Neo4j is the supported backend (#7331) and the only one eshu-core-api
# starts on (anything else fails its startup guard); the satellite leg needs
# a core, so the gate runs the Neo4j leg. There is no nornicdb leg: a core
# cannot serve on it, and the satellite cannot serve without a core.
: "${ESHU_GRAPH_BACKEND:=neo4j}"
# Port defaults below are deliberately NOT 15532/7789/7576/18082: 15532 is
# scripts/verify-ifa-replay-drive.sh's own default (a real, confirmed
# collision), and 18082 is docker-compose.yaml's own
# ESHU_WORKFLOW_COORDINATOR_HTTP_PORT default, so a dev box with that
# profile enabled would have this gate's /readyz answered by a DIFFERENT
# service.
: "${GATE_POSTGRES_PORT:=15537}"
: "${GATE_NEO4J_BOLT_PORT:=7797}"
: "${GATE_NEO4J_HTTP_PORT:=7585}"
: "${GATE_NEO4J_PASSWORD:=change-me}"
: "${GATE_POSTGRES_PASSWORD:=change-me}"
: "${GATE_API_PORT:=18097}"
: "${GATE_MCP_PORT:=18098}"
: "${GATE_CORE_PORT:=18096}"
: "${GATE_API_KEY:=read-api-latency-gate-local-key}"
: "${GATE_COMPOSE_PROJECT:=eshu-read-api-latency-gate-$$}"
: "${GATE_WINNERS_FACT_COUNT:=20000}"
: "${GATE_TOTAL_SCOPES:=800}"
: "${GATE_NODES_PER_LABEL:=150000}"
: "${GATE_IAC_FACT_COUNT:=150000}"
: "${GATE_SHARED_INTENT_COUNT:=2500000}"
: "${GATE_ITERATIONS:=20}"
# GATE_RUNS defaults to 1: one counted pass after discarded warmups, with
# no later-pass data. #6965 phase 6's cross-backend
# comparison recipe sets it to 5 or more; see
# docs/public/reference/local-testing/read-api-latency-gate.md.
: "${GATE_RUNS:=1}"
: "${GATE_CONCURRENT_WORKERS:=0}"
: "${GATE_CONCURRENT_REQUESTS:=20}"
: "${GATE_CONCURRENT_REPORT:=}"
: "${GATE_BUDGETS:=testdata/benchmarks/read-api-route-budgets.txt}"
: "${GATE_WORK_BUDGETS:=testdata/benchmarks/read-api-route-work-budgets.txt}"
# The winners leg enforces its own generated table (#11170): the same routes
# read far less Postgres work from the maintained winners table, so a table
# derived from the legacy read cannot guard it, and the legacy table must keep
# guarding the default (flag off) configuration.
: "${GATE_WINNERS_WORK_BUDGETS:=testdata/benchmarks/read-api-route-work-budgets-winners.txt}"
: "${GATE_WORK_REPORT:=}"
: "${GATE_LATENCY_REPORT:=}"
: "${GATE_STATUS_DIAGNOSTIC_DIR:=}"

# Backend stacks moved to eshu-deploy under #8871: the base Compose file
# and the latency overlay both come from the ESHU_DEPLOY_CHECKOUT checkout
# (CI pins the extraction commit). GATE_STACK_DIR still overrides the base
# file for local candidate-fix measurement.
: "${ESHU_DEPLOY_CHECKOUT:?ESHU_DEPLOY_CHECKOUT must point at an eshu-hq/eshu-deploy checkout; the gate workflow checks it out at the pinned extraction commit}"
stack_dir="${GATE_STACK_DIR:-${ESHU_DEPLOY_CHECKOUT}/compose}"
compose_file="${stack_dir}/docker-compose.yaml"
database="nornic"
if [[ "${ESHU_GRAPH_BACKEND}" == "neo4j" ]]; then
	compose_file="${stack_dir}/docker-compose.neo4j.yml"
	database="neo4j"
fi
[[ -f "${compose_file}" ]] || { printf 'verify-read-api-latency-gate: compose file not found: %s\n' "${compose_file}" >&2; exit 1; }
# The override loads pg_stat_statements into postgres so the gate can budget
# per-route Postgres work. It is passed on EVERY compose call below (up, logs,
# down): a call without it recomputes a different postgres config and recreates
# the running container mid-run (same failure class as the port env vars above).
compose_args=(-p "${GATE_COMPOSE_PROJECT}" -f "${compose_file}" -f "${ESHU_DEPLOY_CHECKOUT}/compose/docker-compose.read-api-latency-gate.yaml")

use_compose=1
keep=0
for arg in "$@"; do
	case "${arg}" in
		--no-compose) use_compose=0 ;;
		--keep) keep=1 ;;
		-h|--help) sed -n '2,25p' "${BASH_SOURCE[0]}"; exit 0 ;;
		*) echo "verify-read-api-latency-gate: unknown argument: ${arg}" >&2; exit 2 ;;
	esac
done

log() { printf '\n=== %s ===\n' "$*"; }
die() { printf 'verify-read-api-latency-gate: %s\n' "$*" >&2; exit 1; }

command -v go >/dev/null 2>&1 || die "missing required tool: go"
command -v curl >/dev/null 2>&1 || die "missing required tool: curl"
if [[ "${use_compose}" -eq 1 ]]; then
	command -v docker >/dev/null 2>&1 || die "missing required tool: docker"
	command -v nc >/dev/null 2>&1 || die "missing required tool: nc (used to wait for the graph backend's Bolt port)"
fi

# shellcheck source=scripts/lib/live-gate-lock.sh
. "${repo_root}/scripts/lib/live-gate-lock.sh"
# shellcheck source=scripts/lib/compose_verification_runtime_common.sh
. "${repo_root}/scripts/lib/compose_verification_runtime_common.sh"
COMPOSE_CMD=(bash "${repo_root}/scripts/ci/compose.sh" "${compose_args[@]}" --)

# work_dir/bin_dir/log_dir/bg_pids are referenced by cleanup() below but not
# populated until after the lock is held; declared empty here (rather than
# after acquire_live_gate_lock) so the trap can be installed FIRST — a
# mktemp/mkdir failure between acquiring the lock and installing the trap
# would otherwise leak the lock until it is later detected as stale.
work_dir=""
bg_pids=()
# stack_up is set immediately before this run's own `docker compose up` (so a
# partially created project is still torn down) and never earlier: an early exit
# (a held lock, a --keep marker) must not tear down a Compose project this run
# did not create, e.g. a retained --keep stack whose name was exported as
# GATE_COMPOSE_PROJECT.
stack_up=0
api_pid=""
status_diagnostic_resource_pid=""
if [[ -n "${GATE_STATUS_DIAGNOSTIC_DIR}" ]]; then
 [[ "${GATE_CONCURRENT_WORKERS}" -ge 2 ]] || die "status diagnostics require concurrent workers"
 command -v python3 >/dev/null 2>&1 || die "status diagnostics require python3"
 [[ "$use_compose" -eq 1 ]] || die "status resource diagnostics require this runner's Compose stack"
 # shellcheck source=scripts/lib/status-diagnostic-runner.sh
 . "${repo_root}/scripts/lib/status-diagnostic-runner.sh"
 status_diagnostic_capacity "${GATE_ITERATIONS}" "${GATE_RUNS}" "${GATE_CONCURRENT_REQUESTS}" || die "status diagnostic target requests exceed collector capacity or are invalid"
fi
# shellcheck source=scripts/lib/golden-corpus-host-helpers.sh
. "${repo_root}/scripts/lib/golden-corpus-host-helpers.sh"
# shellcheck source=scripts/lib/read-api-latency-core.sh
. "${repo_root}/scripts/lib/read-api-latency-core.sh"

# api_log_preserve_path is where cleanup copies the running eshu-api's log
# before deleting work_dir, so a CI failure still has the log available (the
# "Dump service logs on failure" workflow step reads it) instead of losing
# it to the same rm -rf that clears everything else.
api_log_preserve_path="${TMPDIR:-/tmp}/read-api-latency-gate-api.log"
mcp_log_preserve_path="${TMPDIR:-/tmp}/read-api-latency-gate-mcp.log"
core_log_preserve_dir="${TMPDIR:-/tmp}"

cleanup() {
	local status=$?
 if [[ -n "${GATE_STATUS_DIAGNOSTIC_DIR}" ]]; then
  if [[ -n "${status_diagnostic_resource_pid}" ]]; then
   if ! status_diagnostic_resource_stop "${GATE_STATUS_DIAGNOSTIC_DIR}" "${status_diagnostic_resource_pid}"; then
    printf 'verify-read-api-latency-gate: diagnostic resource capture failed\n' >&2
    [[ "$status" -ne 0 ]] || status=1
   fi
  fi
  if [[ -n "${api_pid}" ]]; then
   if ! status_diagnostic_stop "${api_pid}" 30; then
    printf 'verify-read-api-latency-gate: diagnostic API shutdown failed\n' >&2
    [[ "$status" -ne 0 ]] || status=1
   fi
  fi
  if ! status_diagnostic_validate "${repo_root}" "${GATE_STATUS_DIAGNOSTIC_DIR}" "${GATE_CONCURRENT_REQUESTS}"; then
   [[ "$status" -ne 0 ]] || status=1
  fi
 fi
	for pid in "${bg_pids[@]:-}"; do
		if [[ -n "${GATE_STATUS_DIAGNOSTIC_DIR}" && "${pid}" == "${api_pid}" ]]; then
   continue
  fi
  [[ -n "${pid}" ]] && kill "${pid}" >/dev/null 2>&1 || true
	done
	if [[ -n "${work_dir}" && -f "${work_dir}/logs/api.log" ]]; then
		cp "${work_dir}/logs/api.log" "${api_log_preserve_path}" 2>/dev/null || true
	fi
	if [[ -n "${work_dir}" && -f "${work_dir}/logs/mcp-server.log" ]]; then
		cp "${work_dir}/logs/mcp-server.log" "${mcp_log_preserve_path}" 2>/dev/null || true
	fi
	for leg in legacy winners; do
		if [[ -n "${work_dir}" && -f "${work_dir}/logs/core-${leg}.log" ]]; then
			cp "${work_dir}/logs/core-${leg}.log" "${core_log_preserve_dir}/read-api-latency-gate-core-${leg}.log" 2>/dev/null || true
		fi
	done
	if [[ "${keep}" -eq 1 ]]; then
		printf 'verify-read-api-latency-gate: --keep set; leaving %s and the compose stack up\n' "${work_dir}" >&2
		retain_live_gate_lock
		exit "${status}"
	fi
	if [[ "${use_compose}" -eq 1 && "${stack_up}" -eq 1 ]]; then
		bash "${repo_root}/scripts/ci/compose.sh" "${compose_args[@]}" -- down -v --remove-orphans >/dev/null 2>&1 || true
	fi
	if declare -F eshu_ci_compose_cleanup >/dev/null; then
		eshu_ci_compose_cleanup
	fi
	[[ -n "${work_dir}" ]] && rm -rf "${work_dir}"
	release_live_gate_lock
	exit "${status}"
}
trap cleanup EXIT

acquire_live_gate_lock
if [[ -n "${GATE_STATUS_DIAGNOSTIC_DIR}" ]]; then
 status_diagnostic_prepare "${GATE_STATUS_DIAGNOSTIC_DIR}" || die "status diagnostics require a new report directory"
 GATE_STATUS_DIAGNOSTIC_DIR="$(cd "${GATE_STATUS_DIAGNOSTIC_DIR}" && pwd)"
fi

work_dir="$(mktemp -d -t read-api-latency-gate.XXXXXX)"
bin_dir="${work_dir}/bin"
log_dir="${work_dir}/logs"
mkdir -p "${bin_dir}" "${log_dir}"

export ESHU_GRAPH_BACKEND
# Fail fast on a backend eshu-core-api cannot start on (issue #11170): the
# satellite leg needs a core, and a core fails closed at startup anywhere but
# Neo4j. Waiting until the core /readyz poll to discover this would burn the
# Compose stack and 240s per run.
require_neo4j_backend_for_core
# Exported (not just inline-prefixed on one `docker compose up` call): every
# docker compose invocation below (the postgres/graph up, the separate
# db-migrate up, logs, down) re-resolves docker-compose.yaml's
# ${ESHU_POSTGRES_PORT:-...}-style interpolation from the CURRENT shell
# environment. A var only prefixed on the first `up` call is invisible to the
# later ones, so compose sees a changed config for postgres/nornicdb and
# RECREATES them out from under db-migrate/eshu-api — this is what actually
# caused a live-gate "connect: connection refused" on the Bolt port right
# after a clean db-migrate exit (issue #6797 live-gate incident): the second
# `up -d db-migrate` call recomputed postgres+nornicdb's config with these
# unset (falling back to docker-compose.yaml's defaults) and recreated both
# running containers.
export ESHU_POSTGRES_PORT="${GATE_POSTGRES_PORT}"
export NEO4J_BOLT_PORT="${GATE_NEO4J_BOLT_PORT}"
export NEO4J_HTTP_PORT="${GATE_NEO4J_HTTP_PORT}"
export ESHU_NEO4J_PASSWORD="${GATE_NEO4J_PASSWORD}"
export ESHU_POSTGRES_PASSWORD="${GATE_POSTGRES_PASSWORD}"
export NEO4J_URI="bolt://localhost:${GATE_NEO4J_BOLT_PORT}"
export NEO4J_USERNAME="neo4j"
export NEO4J_PASSWORD="${GATE_NEO4J_PASSWORD}"
export NEO4J_DATABASE="${database}"
export DEFAULT_DATABASE="${database}"
export ESHU_POSTGRES_DSN="postgresql://eshu:${GATE_POSTGRES_PASSWORD}@localhost:${GATE_POSTGRES_PORT}/eshu"
export ESHU_CONTENT_STORE_DSN="${ESHU_POSTGRES_DSN}"
export ESHU_API_KEY="${GATE_API_KEY}"
export ESHU_API_ADDR=":${GATE_API_PORT}"
# The SSM host inventory readback fails closed without freshness bounds
# (#9558: the handler 503s when any bound is unset, which the latency
# gate counts as a HardFailed budget breach regardless of latency).
# Values mirror the coherent test limits (skew within capture age).
export ESHU_SSM_HOST_MAX_CAPTURE_AGE="2h"
export ESHU_SSM_HOST_MAX_CROSS_TYPE_SKEW="30m"
export ESHU_SSM_HOST_MAX_PLATFORM_OBSERVATION_AGE="1h"
export ESHU_MCP_TRANSPORT="http"
export ESHU_MCP_QUERY_PROFILE="${ESHU_QUERY_PROFILE:-}"
# NOTE: the satellite eshu-mcp replaced the in-tree server (#9670) and does
# not read ESHU_MCP_ADDR; the listen address travels on the -addr flag below.
export ESHU_AUTH_SECRET_ENC_KEY="AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA="
export ESHU_LISTEN_ADDR="127.0.0.1:0"
export ESHU_METRICS_ADDR="127.0.0.1:0"
# Without ESHU_COMPONENT_HOME, ComponentExtensionsHandler.readbackOrUnavailable
# (go/internal/query/component_extensions.go) 503s GET /api/v0/component-extensions
# unconditionally -- a gate-environment gap, not a product bug (issue #6797
# live-gate incident: this route HardFailed on every run). An empty directory
# is a legitimate, supported registry state: Registry.Readback/List/load all
# treat a missing/empty registry file as zero installed components, not an
# error (go/internal/component/registry.go), so this makes the route return a
# real empty-list 200 instead of a fabricated pass/fail signal.
export ESHU_COMPONENT_HOME="${work_dir}/component-home"
mkdir -p "${ESHU_COMPONENT_HOME}"
unset ESHU_PPROF_ADDR || true

if [[ "${use_compose}" -eq 1 ]]; then
	# shellcheck source=scripts/lib/ci-image-compose.sh
	. "${repo_root}/scripts/lib/ci-image-compose.sh"
	# scripts/ci/compose.sh renders against its own repo root. Prepare once here, so
	# the wrapper takes its signature path, only when the stack is that root;
	# otherwise the wrapper prepares per call.
	if [[ "${stack_dir}" == "${repo_root}" ]]; then
		eshu_ci_compose_prepare "${repo_root}" "${compose_args[@]}" || die "CI image overlay preflight failed"
	fi
fi

if [[ "${use_compose}" -eq 1 ]]; then
	log "bring up Postgres + ${ESHU_GRAPH_BACKEND} (Compose project ${GATE_COMPOSE_PROJECT})"
	# Set before `up` so a partially created project is still torn down.
	stack_up=1
	bash "${repo_root}/scripts/ci/compose.sh" "${compose_args[@]}" -- up -d --wait postgres "${ESHU_GRAPH_BACKEND}" \
		|| { bash "${repo_root}/scripts/ci/compose.sh" "${compose_args[@]}" -- logs --tail=200 || true; die "compose up failed"; }
	# db-migrate is a one-shot job that exits(0) once schema bootstrap
	# completes; it must not be a `--wait` target above alongside the
	# long-running postgres/graph services, because compose's --wait treats
	# a target leaving the running/healthy state as failure regardless of
	# exit code, so a clean db-migrate exit was reported as "compose up
	# failed" (issue #6797 live-gate incident). Start it separately and wait
	# for its own clean exit via the shared one-shot-service helper.
	bash "${repo_root}/scripts/ci/compose.sh" "${compose_args[@]}" -- up -d db-migrate \
		|| { bash "${repo_root}/scripts/ci/compose.sh" "${compose_args[@]}" -- logs --tail=200 || true; die "compose up failed"; }
	eshu_compose_wait_for_named_exit db-migrate 120 \
		|| { bash "${repo_root}/scripts/ci/compose.sh" "${compose_args[@]}" -- logs --tail=200 db-migrate || true; die "db-migrate failed"; }

	# nornicdb's healthcheck only probes its HTTP port (7474/health), not
	# its Bolt listener (7687) — the two ports come up at slightly
	# different times inside the container. eshu-api's own Neo4j
	# connectivity preflight is a single dial with no retry, so starting
	# it right after compose reports nornicdb "Healthy" races the Bolt
	# port and can hard-fail eshu-api on a fresh container even though
	# db-migrate (which retries its own connect) succeeded moments
	# earlier against the same port (issue #6797 live-gate incident: RED
	# run failed "connect: connection refused" on port 7797 immediately
	# after a clean db-migrate exit). Wait for the Bolt port to actually
	# accept a connection before starting eshu-api.
	bolt_deadline=$((SECONDS + 60))
	until nc -z 127.0.0.1 "${GATE_NEO4J_BOLT_PORT}" >/dev/null 2>&1; do
		if ((SECONDS >= bolt_deadline)); then
			die "${ESHU_GRAPH_BACKEND} Bolt port ${GATE_NEO4J_BOLT_PORT} never accepted a connection"
		fi
		/bin/sleep 1
	done
fi

log "build host binaries"
if [[ -n "${GATE_API_BIN:-}" ]]; then
	# RED/GREEN comparison mode: measure this branch's gate against an
	# eshu-api built from a DIFFERENT commit (e.g. main with a candidate
	# fix merged) without rebasing or disturbing this worktree.
	[[ -x "${GATE_API_BIN}" ]] || die "GATE_API_BIN=${GATE_API_BIN} is not an executable file"
	cp "${GATE_API_BIN}" "${bin_dir}/eshu-api"
	log "using pre-built eshu-api from GATE_API_BIN=${GATE_API_BIN} (not building from this worktree)"
	command -v shasum >/dev/null 2>&1 && shasum -a 256 "${bin_dir}/eshu-api"
else
	build_bin api
fi
build_bin core-api
build_bin read-api-latency-gate
build_bin mcp-server

# Recorded in the latency report's identity block (-api-binary-sha256 and
# -core-binary-sha256 below) so a cross-backend comparison can refuse two
# legs measured against different eshu-api or eshu-core-api builds -- see
# scripts/compare-backend-latency.sh. Only computed (and only then does its
# own log line print) when a report was actually asked for:
# GATE_LATENCY_REPORT is the only consumer, and CI's default invocation never
# sets it, so computing/logging this unconditionally would add a shasum cost
# and stdout lines to every default run.
# Best effort: an empty string when shasum is
# unavailable, same as the already-conditional lsof provenance print below.
gate_api_binary_sha256=""
gate_core_binary_sha256=""
# Best effort: the commit this worktree's HEAD resolves to, recorded in the
# same identity block. Empty when git is unavailable or HEAD is detached with
# no resolvable ref -- never fatal, the identity block is comparison metadata,
# not a gate input. Same GATE_LATENCY_REPORT gating as the sha256 above, for
# the same reason.
gate_eshu_commit=""
if [[ -n "${GATE_LATENCY_REPORT}" ]]; then
	if command -v shasum >/dev/null 2>&1; then
		gate_api_binary_sha256="$(shasum -a 256 "${bin_dir}/eshu-api" | awk '{print $1}')"
		log "eshu-api binary sha256: ${gate_api_binary_sha256}"
		gate_core_binary_sha256="$(shasum -a 256 "${bin_dir}/eshu-core-api" | awk '{print $1}')"
		log "eshu-core-api binary sha256: ${gate_core_binary_sha256}"
	fi
	gate_eshu_commit="$(git -C "${repo_root}" rev-parse HEAD 2>/dev/null || true)"
fi

gate_report_args=()
if [[ -n "${GATE_WORK_REPORT}" ]]; then
	gate_report_args+=(-work-report "${GATE_WORK_REPORT}")
fi
if [[ -n "${GATE_LATENCY_REPORT}" ]]; then
	gate_report_args+=(-latency-report "${GATE_LATENCY_REPORT}")
fi
if [[ -n "${GATE_CONCURRENT_REPORT}" ]]; then
	gate_report_args+=(-concurrent-report "${GATE_CONCURRENT_REPORT}")
fi

# Two phases, like a real deploy: seed first, THEN start eshu-api. eshu-api's
# startup backfill of the infra read model derives whatever content_entities
# already holds, once (it records a marker and never re-runs), so an API started
# before the seed would backfill an empty table and leave the seeded rows to the
# dirty-repository fence, which sends reads back to the graph (issue #6797,
# measured live against the #6793 candidate).
gate_common_args=(
	-postgres-dsn "${ESHU_POSTGRES_DSN}"
	-graph-uri "${NEO4J_URI}"
	-graph-database "${database}"
	-graph-username "${NEO4J_USERNAME}"
	-graph-password "${NEO4J_PASSWORD}"
	-api-base-url "http://localhost:${GATE_API_PORT}"
	-mcp-base-url "http://127.0.0.1:${GATE_MCP_PORT}"
	-api-key "${GATE_API_KEY}"
	-total-scopes "${GATE_TOTAL_SCOPES}"
	-nodes-per-label "${GATE_NODES_PER_LABEL}"
	-iac-fact-count "${GATE_IAC_FACT_COUNT}"
	-shared-intent-count "${GATE_SHARED_INTENT_COUNT}"
	-iterations "${GATE_ITERATIONS}"
	-runs "${GATE_RUNS}"
	-concurrent-workers "${GATE_CONCURRENT_WORKERS}"
	-concurrent-requests "${GATE_CONCURRENT_REQUESTS}"
	-eshu-commit "${gate_eshu_commit}"
	-api-binary-sha256 "${gate_api_binary_sha256}"
	-core-binary-sha256 "${gate_core_binary_sha256}"
	-winners-fact-count "${GATE_WINNERS_FACT_COUNT}"
)

log "seed (${GATE_TOTAL_SCOPES} scopes, ${GATE_NODES_PER_LABEL} nodes/infra-label, ${GATE_IAC_FACT_COUNT} IaC facts, ${GATE_SHARED_INTENT_COUNT} shared projection intents, ${GATE_WINNERS_FACT_COUNT} winners finding facts)"
"${bin_dir}/eshu-read-api-latency-gate" "${gate_common_args[@]}" -seed-only \
	|| die "seed failed (see the reason printed above)"

# Two phases, like a real deploy: seed first, THEN start the core. The
# satellite proxies every read to the core, so the core must be serving
# before the satellite's /readyz can answer (issue #11170).
log "start eshu-core-api (legacy leg: winners-read off)"
start_core_api false
start_satellite_api
start_mcp_server

if [[ -n "${GATE_STATUS_DIAGNOSTIC_DIR}" ]]; then
 status_diagnostic_resource_start "${repo_root}" "${GATE_STATUS_DIAGNOSTIC_DIR}" "${api_pid}" "${GATE_COMPOSE_PROJECT}" || die "cannot identify diagnostic Postgres process"
fi

run_sweep_leg legacy "${GATE_BUDGETS}" "${GATE_WORK_BUDGETS}" ${gate_report_args[@]+"${gate_report_args[@]}"}

# The legacy leg is the pre-winners truth the winners leg must reproduce:
# snapshot it before restarting the core with the flag on.
log "prove the legacy leg's findings truth"
snapshot_findings_page "${work_dir}/findings-legacy.json"
# Negative control: while the flag is off, the winners-sourced grouped
# dimensions fail closed (env-registry contract), so a 200 here would mean
# the leg is not measuring the legacy path it claims.
require_grouped_inventory 503 service_id
require_grouped_inventory 503 workload_id
require_grouped_inventory 503 environment

# The winners flag is read once at mux build, so the leg needs its own core
# process. The satellite and MCP stay up: they forward to the core address,
# which does not move between legs.
log "restart eshu-core-api (winners leg: winners-read on)"
stop_core_api
start_core_api true
wait_for_satellite_readyz
kill -0 "${api_pid}" 2>/dev/null || die "eshu-api (pid ${api_pid}) did not survive the core restart"
kill -0 "${mcp_pid}" 2>/dev/null || die "MCP server (pid ${mcp_pid}) did not survive the core restart"

log "prove the winners leg serves the same truth"
snapshot_findings_page "${work_dir}/findings-winners.json"
require_findings_byte_identical "${work_dir}/findings-legacy.json" "${work_dir}/findings-winners.json"
# Positive control: the seeded service/workload/environment dimensions must
# answer 200 from the rebuilt winners table, proving the leg measured the
# read model rather than an empty fallback.
require_grouped_inventory 200 service_id
require_grouped_inventory 200 workload_id
require_grouped_inventory 200 environment

# The winners leg re-sweeps the same corpus into separate reports (never
# merged with the legacy leg's): each report's identity block carries
# -winners-read, so a reader can tell which read model produced it.
winners_extra_args=(-winners-read)
if [[ -n "${GATE_LATENCY_REPORT}" ]]; then
	winners_extra_args+=(-latency-report "$(winners_report_path "${GATE_LATENCY_REPORT}")")
fi
if [[ -n "${GATE_WORK_REPORT}" ]]; then
	winners_extra_args+=(-work-report "$(winners_report_path "${GATE_WORK_REPORT}")")
fi
if [[ -n "${GATE_CONCURRENT_REPORT}" ]]; then
	winners_extra_args+=(-concurrent-report "$(winners_report_path "${GATE_CONCURRENT_REPORT}")")
fi
run_sweep_leg winners "${GATE_BUDGETS}" "${GATE_WINNERS_WORK_BUDGETS}" "${winners_extra_args[@]}"

fail_if_any_leg_failed
log "PASS: read-api-latency-gate green (legacy + winners legs)"
