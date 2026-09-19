#!/usr/bin/env bash
# verify-read-api-latency-gate.sh — issue #6797: backend-backed read-API
# latency gate.
#
# Brings up the same Docker Compose Postgres+NornicDB pair
# scripts/verify-golden-corpus-gate.sh uses, builds eshu-api and
# read-api-latency-gate, starts eshu-api against the live backends, then runs
# read-api-latency-gate to seed a synthetic corpus (ingestion scopes,
# generations, and fact_work_items across every collector kind, plus
# infra-labeled graph nodes) and sweep every no-arg GET route's p95 latency
# against testdata/benchmarks/read-api-route-budgets.txt.
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
# context: migrations and graph schema) from that checkout instead of this one. Use
# it with GATE_API_BIN built from the SAME checkout when measuring a candidate fix
# that ships a migration: an eshu-api from the fix against a database migrated by
# this worktree would silently fall back to the old read path.
#
# GATE_API_BIN=<path> uses that pre-built eshu-api binary instead of building
# one from this worktree — for a RED/GREEN comparison against a different
# commit's eshu-api (e.g. main with a candidate fix merged) without rebasing
# or disturbing this worktree's checkout.
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "${repo_root}"

: "${ESHU_GRAPH_BACKEND:=nornicdb}"
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
: "${GATE_API_KEY:=read-api-latency-gate-local-key}"
: "${GATE_COMPOSE_PROJECT:=eshu-read-api-latency-gate-$$}"
: "${GATE_TOTAL_SCOPES:=800}"
: "${GATE_NODES_PER_LABEL:=150000}"
: "${GATE_IAC_FACT_COUNT:=150000}"
: "${GATE_ITERATIONS:=20}"
: "${GATE_BUDGETS:=testdata/benchmarks/read-api-route-budgets.txt}"
: "${GATE_WORK_BUDGETS:=testdata/benchmarks/read-api-route-work-budgets.txt}"
: "${GATE_WORK_REPORT:=}"

stack_dir="${GATE_STACK_DIR:-${repo_root}}"
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
compose_args=(-p "${GATE_COMPOSE_PROJECT}" -f "${compose_file}" -f "${repo_root}/docker-compose.read-api-latency-gate.yaml")

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
COMPOSE_CMD=(docker compose "${compose_args[@]}")

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
# shellcheck source=scripts/lib/golden-corpus-host-helpers.sh
. "${repo_root}/scripts/lib/golden-corpus-host-helpers.sh"

# api_log_preserve_path is where cleanup copies the running eshu-api's log
# before deleting work_dir, so a CI failure still has the log available (the
# "Dump service logs on failure" workflow step reads it) instead of losing
# it to the same rm -rf that clears everything else.
api_log_preserve_path="${TMPDIR:-/tmp}/read-api-latency-gate-api.log"

cleanup() {
	local status=$?
	for pid in "${bg_pids[@]:-}"; do
		[[ -n "${pid}" ]] && kill "${pid}" >/dev/null 2>&1 || true
	done
	if [[ -n "${work_dir}" && -f "${work_dir}/logs/api.log" ]]; then
		cp "${work_dir}/logs/api.log" "${api_log_preserve_path}" 2>/dev/null || true
	fi
	if [[ "${keep}" -eq 1 ]]; then
		printf 'verify-read-api-latency-gate: --keep set; leaving %s and the compose stack up\n' "${work_dir}" >&2
		retain_live_gate_lock
		exit "${status}"
	fi
	if [[ "${use_compose}" -eq 1 && "${stack_up}" -eq 1 ]]; then
		docker compose "${compose_args[@]}" down -v --remove-orphans >/dev/null 2>&1 || true
	fi
	[[ -n "${work_dir}" ]] && rm -rf "${work_dir}"
	release_live_gate_lock
	exit "${status}"
}
trap cleanup EXIT

acquire_live_gate_lock

work_dir="$(mktemp -d -t read-api-latency-gate.XXXXXX)"
bin_dir="${work_dir}/bin"
log_dir="${work_dir}/logs"
mkdir -p "${bin_dir}" "${log_dir}"

export ESHU_GRAPH_BACKEND
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
	log "bring up Postgres + ${ESHU_GRAPH_BACKEND} (Compose project ${GATE_COMPOSE_PROJECT})"
	# Set before `up` so a partially created project is still torn down.
	stack_up=1
	docker compose "${compose_args[@]}" up -d --wait postgres "${ESHU_GRAPH_BACKEND}" \
		|| { docker compose "${compose_args[@]}" logs --tail=200 || true; die "compose up failed"; }
	# db-migrate is a one-shot job that exits(0) once schema bootstrap
	# completes; it must not be a `--wait` target above alongside the
	# long-running postgres/graph services, because compose's --wait treats
	# a target leaving the running/healthy state as failure regardless of
	# exit code, so a clean db-migrate exit was reported as "compose up
	# failed" (issue #6797 live-gate incident). Start it separately and wait
	# for its own clean exit via the shared one-shot-service helper.
	docker compose "${compose_args[@]}" up -d db-migrate \
		|| { docker compose "${compose_args[@]}" logs --tail=200 || true; die "compose up failed"; }
	eshu_compose_wait_for_named_exit db-migrate 120 \
		|| { docker compose "${compose_args[@]}" logs --tail=200 db-migrate || true; die "db-migrate failed"; }

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
build_bin read-api-latency-gate

gate_report_args=()
if [[ -n "${GATE_WORK_REPORT}" ]]; then
	gate_report_args=(-work-report "${GATE_WORK_REPORT}")
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
	-api-key "${GATE_API_KEY}"
	-total-scopes "${GATE_TOTAL_SCOPES}"
	-nodes-per-label "${GATE_NODES_PER_LABEL}"
	-iac-fact-count "${GATE_IAC_FACT_COUNT}"
	-iterations "${GATE_ITERATIONS}"
)

log "seed (${GATE_TOTAL_SCOPES} scopes, ${GATE_NODES_PER_LABEL} nodes/infra-label, ${GATE_IAC_FACT_COUNT} IaC facts)"
"${bin_dir}/eshu-read-api-latency-gate" "${gate_common_args[@]}" -seed-only \
	|| die "seed failed (see the reason printed above)"

log "start eshu-api"
start_bg api api_pid "${bin_dir}/eshu-api"
if command -v lsof >/dev/null 2>&1; then
	sleep 0.2 # give the process a moment to finish exec() before lsof inspects it
	log "eshu-api binary provenance (lsof -p ${api_pid})"
	lsof -p "${api_pid}" 2>/dev/null | grep -E 'txt|TEXT' || echo "lsof reported no txt mapping for pid ${api_pid}"
fi
api_ready=false
for _ in $(seq 1 240); do
	if curl -fsS "http://localhost:${GATE_API_PORT}/readyz" >/dev/null 2>&1; then
		api_ready=true
		break
	fi
	sleep 1
done
[[ "${api_ready}" == "true" ]] || { tail -40 "${log_dir}/api.log" >&2 || true; die "eshu-api /readyz never returned on port ${GATE_API_PORT}"; }
# A responder on GATE_API_PORT that is not our spawned process (a stale
# service left on a shared/dev port) would pass the curl check above and
# then measure something other than this run's eshu-api. Ports are now
# unique to this gate (see the port comment above), but this is a cheap extra
# check that the process we started is still the one alive.
kill -0 "${api_pid}" 2>/dev/null || die "eshu-api (pid ${api_pid}) is not running even though /readyz answered — a different process is likely bound to port ${GATE_API_PORT}"

log "sweep (${GATE_ITERATIONS} requests/route)"
gate_status=0
"${bin_dir}/eshu-read-api-latency-gate" "${gate_common_args[@]}" -skip-seed \
	-budgets "${GATE_BUDGETS}" \
	-work-budgets "${GATE_WORK_BUDGETS}" \
	${gate_report_args[@]+"${gate_report_args[@]}"} || gate_status=$?

if [[ "${gate_status}" -ne 0 ]]; then
	# main.go already printed the specific reason (budget breach, coverage
	# floor, or a required route not exercised) to stderr above; this
	# message stays neutral rather than naming one specific cause for every
	# nonzero exit.
	die "read-api-latency-gate failed with exit ${gate_status} (see the reason printed above)"
fi

log "PASS: read-api-latency-gate green"
