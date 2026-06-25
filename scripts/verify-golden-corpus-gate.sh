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

# ----------------------------------------------------------------------------
# Configuration (override via environment).
# ----------------------------------------------------------------------------
: "${ESHU_GRAPH_BACKEND:=nornicdb}"
: "${ESHU_POSTGRES_PORT:=15432}"
: "${NEO4J_BOLT_PORT:=7687}"
: "${NEO4J_HTTP_PORT:=7474}"
: "${ESHU_NEO4J_PASSWORD:=change-me}"
: "${ESHU_POSTGRES_PASSWORD:=change-me}"
: "${GATE_API_PORT:=8080}"
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

# Minimal 5-repo corpus. Chosen to produce cross-repo DEPENDS_ON (rc-3) and a
# source/deployment-config deployable-unit correlation (rc-1).
corpus_fixtures=(
	go_comprehensive
	python_comprehensive
	terraform_comprehensive
	kubernetes_comprehensive
	helm_argocd_platform
)

# 9 credentialed collectors and their B-10 cassette directories.
collector_specs=(
	"collector-kubernetes-live:kuberneteslive"
	"collector-aws-cloud:awscloud"
	"collector-azure-cloud:azurecloud"
	"collector-gcp-cloud:gcpcloud"
	"collector-vault-live:vaultlive"
	"collector-oci-registry:ociregistry"
	"collector-package-registry:packageregistry"
	"collector-terraform-state:terraformstate"
	"collector-prometheus-mimir:prometheusmimir"
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

work_dir="$(mktemp -d -t golden-corpus-gate.XXXXXX)"
bin_dir="${work_dir}/bin"
corpus_dir="${work_dir}/corpus"
home_dir="${work_dir}/home"
log_dir="${work_dir}/logs"
mkdir -p "${bin_dir}" "${corpus_dir}" "${home_dir}" "${log_dir}"

bg_pids=()

log() { printf '\n=== %s ===\n' "$*"; }
die() { printf 'verify-golden-corpus-gate: %s\n' "$*" >&2; exit 1; }

cleanup() {
	local status=$?
	for pid in "${bg_pids[@]:-}"; do
		[[ -n "${pid}" ]] && kill "${pid}" >/dev/null 2>&1 || true
	done
	if [[ "${keep}" -eq 1 ]]; then
		printf '\n[--keep] work dir retained: %s\n' "${work_dir}" >&2
	else
		if [[ "${use_compose}" -eq 1 ]]; then
			docker compose -f "${compose_file}" down -v >/dev/null 2>&1 || true
		fi
		rm -rf "${work_dir}"
	fi
	exit "${status}"
}
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
export ESHU_REPOSITORY_RULES_JSON="[]"
export ESHU_QUERY_PROFILE="local_full_stack"
export ESHU_API_KEY="${GATE_API_KEY}"
mkdir -p "${ESHU_REPOS_DIR}"

build_bin() {
	local cmd="$1"
	CGO_ENABLED=1 go -C go build -o "${bin_dir}/eshu-${cmd}" "./cmd/${cmd}" \
		|| die "build ${cmd} failed"
}

start_bg() {
	local name="$1"; shift
	"$@" >"${log_dir}/${name}.log" 2>&1 &
	local pid=$!
	bg_pids+=("${pid}")
	printf '%s' "${pid}"
}

# ----------------------------------------------------------------------------
log "stage minimal corpus (${#corpus_fixtures[@]} repos)"
for fixture in "${corpus_fixtures[@]}"; do
	src="${repo_root}/tests/fixtures/ecosystems/${fixture}"
	[[ -d "${src}" ]] || die "corpus fixture not found: ${src}"
	ln -s "${src}" "${corpus_dir}/${fixture}"
done
printf 'staged: %s\n' "${corpus_fixtures[*]}"

log "build host binaries"
build_bin bootstrap-index
build_bin projector
build_bin reducer
build_bin api
build_bin golden-corpus-gate
for spec in "${collector_specs[@]}"; do build_bin "${spec%%:*}"; done

if [[ "${use_compose}" -eq 1 ]]; then
	log "start Postgres + ${graph_service}"
	ESHU_NEO4J_PASSWORD="${ESHU_NEO4J_PASSWORD}" ESHU_POSTGRES_PASSWORD="${ESHU_POSTGRES_PASSWORD}" \
		docker compose -f "${compose_file}" up -d "${graph_service}" postgres

	log "wait for backends"
	for _ in $(seq 1 60); do
		graph_ready=false
		if [[ "${graph_service}" == "nornicdb" ]]; then
			docker compose -f "${compose_file}" exec -T nornicdb wget --spider -q http://localhost:7474/health >/dev/null 2>&1 && graph_ready=true
		else
			docker compose -f "${compose_file}" exec -T neo4j cypher-shell -u neo4j -p "${ESHU_NEO4J_PASSWORD}" "RETURN 1" >/dev/null 2>&1 && graph_ready=true
		fi
		if [[ "${graph_ready}" == "true" ]] && \
			docker compose -f "${compose_file}" exec -T postgres pg_isready -U eshu -d eshu >/dev/null 2>&1; then
			break
		fi
		sleep 2
	done
fi

pipeline_start="$(date +%s)"

log "bootstrap-index over minimal corpus (schema + filesystem facts + projection)"
"${bin_dir}/eshu-bootstrap-index" >"${log_dir}/bootstrap-index.log" 2>&1 \
	|| { tail -40 "${log_dir}/bootstrap-index.log"; die "bootstrap-index failed"; }

log "replay B-10 cassette collectors (credential-free)"
collector_pids=()
for spec in "${collector_specs[@]}"; do
	cmd="${spec%%:*}"
	dir="${spec##*:}"
	cassette="${repo_root}/testdata/cassettes/${dir}/${cassette_recording}"
	[[ -f "${cassette}" ]] || die "cassette not found: ${cassette}"
	pid="$(start_bg "${cmd}" "${bin_dir}/eshu-${cmd}" -mode=cassette -cassette-file="${cassette}")"
	collector_pids+=("${pid}")
done
printf 'launched %d collectors; settling %ss for first-pass commit\n' "${#collector_pids[@]}" "${GATE_COLLECTOR_SETTLE_SECONDS}"
sleep "${GATE_COLLECTOR_SETTLE_SECONDS}"
for pid in "${collector_pids[@]}"; do kill "${pid}" >/dev/null 2>&1 || true; done

log "drain projector + reducer (background; gate polls to terminal)"
projector_pid="$(start_bg projector "${bin_dir}/eshu-projector")"
reducer_pid="$(start_bg reducer "${bin_dir}/eshu-reducer")"

log "B-7(a) drains"
if ! "${bin_dir}/eshu-golden-corpus-gate" \
	-phase=drains \
	-snapshot=testdata/golden/e2e-20repo-snapshot.json \
	-drain-timeout="${GATE_DRAIN_TIMEOUT}"; then
	tail -30 "${log_dir}/reducer.log" || true
	tail -30 "${log_dir}/projector.log" || true
	die "drain phase failed"
fi
kill "${projector_pid}" "${reducer_pid}" >/dev/null 2>&1 || true

pipeline_end="$(date +%s)"
elapsed=$(( pipeline_end - pipeline_start ))

log "start eshu-api for query truth"
ESHU_RUNTIME_ROLE="api" api_pid="$(start_bg api "${bin_dir}/eshu-api")"
for _ in $(seq 1 30); do
	curl -fsS "http://localhost:${GATE_API_PORT}/readyz" >/dev/null 2>&1 && break
	sleep 1
done

log "B-7(b) graph truth + B-7(c) query truth + B-7(d) timing"
gate_status=0
"${bin_dir}/eshu-golden-corpus-gate" \
	-phase=graph,query,timing \
	-snapshot=testdata/golden/e2e-20repo-snapshot.json \
	-api-base-url="http://localhost:${GATE_API_PORT}" \
	-graph-required-only=true \
	-required-correlations="rc-1,rc-3" \
	-budget-seconds="${GATE_BUDGET_SECONDS}" \
	-budget-multiplier="${GATE_BUDGET_MULTIPLIER}" \
	-elapsed-seconds="${elapsed}" || gate_status=$?
kill "${api_pid}" >/dev/null 2>&1 || true

if [[ "${gate_status}" -ne 0 ]]; then
	die "graph/query/timing phase failed (elapsed ${elapsed}s)"
fi

log "PASS: B-7 golden corpus gate green (elapsed ${elapsed}s, budget ceiling $((GATE_BUDGET_SECONDS * GATE_BUDGET_MULTIPLIER))s)"
