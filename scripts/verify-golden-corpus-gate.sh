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

# ----------------------------------------------------------------------------
# Configuration (override via environment).
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
	# On failure, dump every host-binary log to stdout BEFORE the work dir is
	# removed, so a CI failure (which captures this script's stdout) preserves the
	# api/collector/projector/reducer logs that explain the break — not just the
	# Docker logs the workflow dumps separately.
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

build_bin() {
	local cmd="$1"
	CGO_ENABLED=1 go -C go build -o "${bin_dir}/eshu-${cmd}" "./cmd/${cmd}" \
		|| die "build ${cmd} failed"
}

# start_bg <name> <pidvar> <cmd...> launches cmd in the background, records its
# pid in bg_pids (so the cleanup trap can reap it on EVERY exit path), and writes
# the pid into the caller-named variable pidvar. The pid is assigned via
# printf -v in the PARENT shell — a previous version returned it through command
# substitution, whose subshell discarded the bg_pids append, leaving the trap a
# no-op that leaked processes on failure.
start_bg() {
	local name="$1" pidvar="$2"; shift 2
	"$@" >"${log_dir}/${name}.log" 2>&1 &
	local pid=$!
	bg_pids+=("${pid}")
	printf -v "${pidvar}" '%s' "${pid}"
}

# pg runs a single-value SQL query against the gate's Postgres, working in both
# compose mode (via the postgres container) and --no-compose mode (via a local
# psql client). Used to assert the cassette collectors actually committed.
pg() {
	local sql="$1"
	if [[ "${use_compose}" -eq 1 ]]; then
		docker compose -f "${compose_file}" exec -T postgres \
			psql -U eshu -d eshu -tA -c "${sql}" 2>/dev/null
	else
		command -v psql >/dev/null 2>&1 || die "psql client required in --no-compose mode"
		psql "${ESHU_POSTGRES_DSN}" -tA -c "${sql}" 2>/dev/null
	fi
}

# ----------------------------------------------------------------------------
log "stage minimal corpus (${#corpus_fixtures[@]} repos)"
# Copy (not symlink) each fixture: the filesystem discovery walker treats each
# immediate child of ESHU_FILESYSTEM_ROOT as a repo and does not follow symlinks,
# so symlinked fixtures collapse to a single scope and break cross-repo edges.
for fixture in "${corpus_fixtures[@]}"; do
	src="${repo_root}/tests/fixtures/ecosystems/${fixture}"
	[[ -d "${src}" ]] || die "corpus fixture not found: ${src}"
	cp -R "${src}" "${corpus_dir}/${fixture}"
	# deployable-config needs a git repo so localGitRefs can discover tags
	# for the B-12 query_shape.http branches endpoint assertion.
		if [[ "${fixture}" = "deployable-config" ]]; then
		git -C "${corpus_dir}/${fixture}" -c init.defaultBranch=main init >/dev/null 2>&1
		git -C "${corpus_dir}/${fixture}" config user.email "gate@eshu.local" >/dev/null 2>&1
		git -C "${corpus_dir}/${fixture}" config user.name "Golden Gate" >/dev/null 2>&1
		git -C "${corpus_dir}/${fixture}" add -A >/dev/null 2>&1
		git -C "${corpus_dir}/${fixture}" commit -m "initial" >/dev/null 2>&1
		# Annotated tag for peeled-SHA coverage.
		git -C "${corpus_dir}/${fixture}" tag -a v1.0.0-annotated -m "annotated tag" HEAD >/dev/null 2>&1
		# Lightweight tag.
		git -C "${corpus_dir}/${fixture}" tag lightweight HEAD >/dev/null 2>&1
	fi
done
printf 'staged: %s\n' "${corpus_fixtures[*]}"

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
		if [[ "${graph_service}" == "nornicdb" ]]; then
			docker compose -f "${compose_file}" exec -T nornicdb wget --spider -q http://localhost:7474/health >/dev/null 2>&1 && graph_ready=true
		else
			docker compose -f "${compose_file}" exec -T neo4j cypher-shell -u neo4j -p "${ESHU_NEO4J_PASSWORD}" "RETURN 1" >/dev/null 2>&1 && graph_ready=true
		fi
		if [[ "${graph_ready}" == "true" ]] && \
			docker compose -f "${compose_file}" exec -T postgres pg_isready -U eshu -d eshu >/dev/null 2>&1; then
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

# Deferred maintenance + drain, run TWICE — the ingester's post-shard-drain
# pattern (RunDeferredRelationshipMaintenance) loops continuously; the gate
# approximates that with two cycles so additive correlation domains converge.
# Each maintenance pass re-runs bootstrap-index's post-collection phase: it
# re-backfills relationship evidence with the collector facts now present, replays
# the succeeded code_import_repo_edge work items, and replays the correlation
# domains (deployable_unit_correlation). Cycle 1's drain produces the resolved
# DEPLOYS_FROM relationships (deployment_mapping -> cross-repo resolution) and the
# cross-repo DEPENDS_ON edge; cycle 2's drain re-runs deployable_unit_correlation
# now that the resolved relationships it consumes exist, producing
# CORRELATES_DEPLOYABLE_UNIT. Collection/projection re-runs are idempotent (facts
# dedupe by stable key; schema is IF NOT EXISTS). The final cycle is the asserted
# B-7(a) drain.
for maintenance_pass in 1 2; do
	log "deferred maintenance pass ${maintenance_pass}: re-run bootstrap-index maintenance"
	"${bin_dir}/eshu-bootstrap-index" >"${log_dir}/bootstrap-index-maint-${maintenance_pass}.log" 2>&1 \
		|| { tail -40 "${log_dir}/bootstrap-index-maint-${maintenance_pass}.log"; die "deferred maintenance pass ${maintenance_pass} failed"; }

	log "B-7(a) drains — maintenance drain ${maintenance_pass} (background; gate polls to terminal)"
	# Every shared_projection_intents domain must reach terminal — including
	# code_calls, whose held-intent deadlock (#3865) is fixed. No domain is
	# quarantined as advisory. -require-populated-domains guards against premature
	# convergence: the reducer runs in the background and the poll could otherwise
	# read an empty 0/0 before it emits any intents and pass on an unreduced
	# pipeline. repo_dependency is the domain the corpus reliably produces (and the
	# B-13/#3859 gate), so the drain is accepted only once it has been observed
	# populated and then drained.
	# Do NOT add platform_infra here: -require-populated-domains counts
	# shared_projection_intents domains, but nothing emits into the platform_infra
	# shared-projection domain (EmitPlatformInfraIntents has no live caller).
	# rc-26 PROVISIONS_PLATFORM is materialized inside the deployment_mapping
	# handler (a fact_work_items domain), which the fact_work_items residual=0
	# drain assertion already gates on — so requiring platform_infra here can
	# never be satisfied and the rc-26 graph assertion already proves the edge.
	start_bg projector projector_pid "${bin_dir}/eshu-projector"
	start_bg reducer reducer_pid "${bin_dir}/eshu-reducer"
	if ! "${bin_dir}/eshu-golden-corpus-gate" \
		-phase=drains \
		-snapshot=testdata/golden/e2e-20repo-snapshot.json \
		-require-populated-domains="repo_dependency" \
		-drain-timeout="${GATE_DRAIN_TIMEOUT}"; then
		tail -30 "${log_dir}/reducer.log" || true
		tail -30 "${log_dir}/projector.log" || true
		die "maintenance drain ${maintenance_pass} failed"
	fi
	kill "${projector_pid}" "${reducer_pid}" >/dev/null 2>&1 || true
done

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
# Minimal-corpus posture: the required graph assertions are "the pipeline
# projected the corpus" (Repository present) and the cross-repo DEPENDS_ON
# correlation (rc-3), which the lib-common/orders-api fixture pair plus the
# package-registry cassette deterministically produce. The deployable-unit (rc-1),
# the cassette-dependent correlations, and the 20-repo node/edge tolerances:
# rc-1 (deployable-unit) is required — the deployable-source + deployable-config
# (ArgoCD) pair plus the correlation reopen produce CORRELATES_DEPLOYABLE_UNIT.
# rc-2 (RUNS_IN) is now required too — the api-svc fixture (Flask @app.route
# handlers + an in-repo k8s/deployment.yaml) produces the code->runtime bridge:
# the handler Functions bind via HANDLES_ROUTE to their Endpoints and via runs_in
# to the api-svc Workload the repository DEFINES. rc-4 (RUNS_IMAGE) is now required
# too — the kuberneteslive + ociregistry cassettes produce a live workload whose
# digest-pinned image resolves (exact decision) to the OCI manifest node, and the
# kubernetes_correlation_materialization domain (reopened in maintenance once the
# OCI generation is active) promotes that decision into the RUNS_IMAGE edge.
# rc-5 (MANAGES) and rc-6 (DEPENDS_ON between AtlantisProjects) are now required
# too — the terraform_comprehensive fixture carries a depth-1 atlantis.yaml with
# network + staging projects; the structural-edge phase emits both edges after the
# AtlantisProject and Directory nodes are written for that repo.
# rc-27 (DEFINES_JOB) and rc-28 (NEEDS between GitlabJobs) are now required too —
# the terraform_comprehensive fixture carries a depth-0 .gitlab-ci.yml with
# terraform-validate + terraform-plan jobs; the structural-edge phase emits the
# pipeline->job DEFINES_JOB edges and the plan->validate NEEDS edge after the
# GitlabPipeline and GitlabJob nodes are written for that repo. Static parse, no
# cassette, mirroring the Atlantis rc-5/rc-6 pattern.
# rc-8 (HANDLES_ROUTE) is now required too — the same api-svc fixture that drives
# rc-2 (RUNS_IN) binds its Flask @app.route handler Functions to their Endpoint
# nodes via Function-[:HANDLES_ROUTE]->Endpoint. Code-path edge, no new cassette.
# rc-11..rc-23 promote edges that already materialize in this corpus from the
# same proven machinery as rc-1..rc-8 (code structure: CALLS/INHERITS/REFERENCES/
# DEFINES/INSTANTIATES/USES_METACLASS; workload materialization: INSTANCE_OF/
# EXPOSES_ENDPOINT/RUNS_ON/DEPLOYMENT_SOURCE; deployable correlation: DEPLOYS_FROM/
# DEFINES; evidence: HAS_DEPLOYMENT_EVIDENCE/EVIDENCES_REPOSITORY_RELATIONSHIP).
# Confirmed materializing via a graph sweep; promoted to lock them against
# regression. (rc-9 DEPENDS_ON_PACKAGE / rc-10 INVOKES_CLOUD_ACTION reserved —
# those need fixture/projection work before promotion.)
# -required-correlations="all" (below) single-sources the blocking set from
# the snapshot's own required_correlations ids (#4596): promoting an rc-N to
# blocking is now a one-file edit (add/confirm it in
# testdata/golden/e2e-20repo-snapshot.json) instead of also hand-editing a
# second, duplicated comma-separated id list here. To stage a newly-added rc
# as advisory-only before it is proven, pass an explicit comma-separated
# subset instead of "all".
# B-11 (#3804): emit the observed per-phase wall-clock and decide the per-phase
# regression flags. Extracted to scripts/lib/golden-corpus-phase-timings.sh to
# keep this orchestrator under the 500-line cap; it sets phase_timings_file and
# the phase_flags array from the phase_* epochs captured inline above.
emit_phase_timings_and_flags

gate_status=0
# demo-answers (#4776): after graph/query truth, execute the five
# specs/demo-first-answers.v1.yaml questions live with their SPECIFIC arguments
# and assert each returns a populated answer. The generic query shapes assert
# each tool/route with example arguments; this phase guards the demo oracle's
# own pinned arguments so a demo answer cannot silently regress to empty.
"${bin_dir}/eshu-golden-corpus-gate" \
	-phase=graph,query,timing,demo-answers \
	-snapshot=testdata/golden/e2e-20repo-snapshot.json \
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
