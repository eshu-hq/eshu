#!/usr/bin/env bash
# Live-backend test runner (#6784). Runs every CI-class row from
# specs/live-tests.v1.yaml against NornicDB and Neo4j in turn and reports.
#
# Each ledger FILE gets a fresh, isolated backend: several CI-class tests
# assert exact whole-graph counts and assume an empty database (see
# graph_row_tokens_live_test.go), so sharing one database across files
# fails closed on pollution, and the main compose stack's own
# bootstrap-index seeds a demo graph on boot. Single-service compose
# files with pinned images (no eshu services: the tests only speak Bolt)
# give that isolation via `down -v` + `up` per file; the stack is removed
# after its file unless --keep.
#
# The two backends share the fixed Bolt port, so they run sequentially.
# The shared live-gate lock serializes this script against the other
# Docker-heavy gates on the machine.
#
#   scripts/run-live-backend-tests.sh [--backend nornicdb|neo4j|both]
#       [--tags <go build tags>] [--keep] [--no-compose]
#       [--shard 1/4|2/4|3/4|4/4] [--results <JSON path>]
#
#     --shard      isolated CI partition; default all (full suite)
#     --results    exact-source coverage and per-target timing report
#     --backend    which backend(s) to exercise (default: both)
#     --tags       go test tags to run (default: live_nornicdb_answer_truth)
#     --keep       leave the LAST file's stack up after the run
#                  (debugging; earlier files' stacks are still removed
#                  because they share the fixed host port)
#     --no-compose run the tests against the ambient backends as-is: no
#                  containers are started and no database is fresh
#                  (local iteration; skips the container lifecycle entirely)
#
# Test files come from specs/live-tests.v1.yaml rows with class: ci, so
# promoting a test to blocking CI is a ledger edit, not a script edit.
# Credential-free: both bare backends run with auth disabled and the
# driver uses NoAuth (ESHU_NEO4J_USERNAME/PASSWORD are unset so tests
# never take the BasicAuth branch).
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ledger="${repo_root}/specs/live-tests.v1.yaml"

# Backend lifecycle uses docker compose (not bare `docker run`): the
# --keep marker reclaim path checks `docker compose -p <project> ps -q`,
# which does not see bare-run containers, so a legitimately retained
# stack would read as stale and the next run would collide on the fixed
# ports. Compose files (single bare service each):
#   nornicdb → docker-compose.live-backend-nornicdb.yml
#   neo4j    → docker-compose.live-backend-neo4j.yml
# Their image pins MUST match docker-compose.yaml / docker-compose.neo4j.yml;
# scripts/test-run-live-backend-tests.sh fails on drift.
nornicdb_compose="docker-compose.live-backend-nornicdb.yml"
neo4j_compose="docker-compose.live-backend-neo4j.yml"

backend="both"
tags="live_nornicdb_answer_truth"
keep=0
use_compose=1
shard="all"
results=""
while [[ "$#" -gt 0 ]]; do
	case "$1" in
		--backend=*) backend="${1#--backend=}" ; shift ;;
		--backend) backend="${2:?--backend needs a value}" ; shift 2 ;;
		--tags=*) tags="${1#--tags=}" ; shift ;;
		--tags) tags="${2:?--tags needs a value}" ; shift 2 ;;
		--shard) shard="${2:?--shard needs a value}" ; shift 2 ;;
		--results) results="${2:?--results needs a path}" ; shift 2 ;;
		--keep) keep=1 ; shift ;;
		--no-compose) use_compose=0 ; shift ;;
		*) printf 'run-live-backend-tests: unknown argument %s\n' "$1" >&2; exit 2 ;;
	esac
done
case "${backend}" in
	nornicdb|neo4j|both) ;;
	*) printf 'run-live-backend-tests: --backend must be nornicdb, neo4j, or both\n' >&2; exit 2 ;;
esac

case "${shard}" in
	all|1/4|2/4|3/4|4/4) ;;
	*) printf 'run-live-backend-tests: invalid shard %s\n' "${shard}" >&2; exit 2 ;;
esac
if [[ "${shard}" != all && "${backend}" != both ]]; then
	printf 'run-live-backend-tests: --shard requires --backend both\n' >&2; exit 2
fi
if [[ -n "${results}" && ( "${use_compose}" == 0 || "${keep}" == 1 ) ]]; then
	printf 'run-live-backend-tests: coverage results require fresh, cleaned Compose stacks\n' >&2; exit 2
fi

# Neo4j runs on the Docker host's native platform (#7353). The pinned
# neo4j image is a multi-arch index (linux/amd64 and linux/arm64, same
# release), but the compose file defaults to linux/amd64, which on an
# arm64 host runs the JVM under emulation: cold Cypher planning was
# several times slower (about 6x on one Apple silicon host) and the
# first entity-context read overran its 10s budget.
# An explicit NEO4J_PLATFORM always wins. An arch this does not map, or a
# docker that cannot report one, leaves the compose default. CI's
# ubuntu-latest runners are amd64, so CI still resolves linux/amd64.
# NornicDB is untouched: its image is amd64-only.
neo4j_native_platform() {
	local arch
	arch="$(docker version -f '{{.Server.Arch}}' 2>/dev/null)" || return 0
	case "${arch}" in
		amd64|x86_64) printf 'linux/amd64' ;;
		arm64|aarch64) printf 'linux/arm64' ;;
	esac
}
# --backend nornicdb never starts Neo4j, so it skips the docker probe.
if [[ -z "${NEO4J_PLATFORM:-}" && "${backend}" != "nornicdb" ]]; then
	NEO4J_PLATFORM="$(neo4j_native_platform)"
fi
if [[ -n "${NEO4J_PLATFORM:-}" ]]; then
	export NEO4J_PLATFORM
else
	unset NEO4J_PLATFORM
fi

# Probe hook for the static self-test: parse args, print the resolved
# config, and exit before touching the lock or starting containers.
if [[ "${ESHU_LIVE_RUNNER_SELFTEST:-0}" == "1" ]]; then
	printf 'backend=%s tags=%s keep=%s use_compose=%s\n' "${backend}" "${tags}" "${keep}" "${use_compose}"
	printf 'nornicdb_compose=%s\nneo4j_compose=%s\n' "${nornicdb_compose}" "${neo4j_compose}"
	printf 'neo4j_platform=%s\n' "${NEO4J_PLATFORM:-}"
	exit 0
fi

: "${NEO4J_BOLT_PORT:=7687}"
: "${NEO4J_HTTP_PORT:=7474}"

die() { printf 'run-live-backend-tests: %s\n' "$*" >&2; exit 1; }

# shellcheck disable=SC1091
. "${repo_root}/scripts/lib/live-gate-lock.sh"

[[ -f "${ledger}" ]] || die "ledger not found: ${ledger}"

# Capture the extractor output explicitly: mapfile succeeds even when the
# process substitution fails, which used to degrade a validator crash to
# "no ledger targets selected" (wrong debugger direction).
targets_raw="$(python3 "${script_dir}/lib/live_backend_test_targets.py" "${ledger}" "${repo_root}")" || die "could not extract live-test targets from ${ledger}"
mapfile -t targets <<<"${targets_raw}"

# One compose project for every stack this script starts: the --keep
# marker records GATE_COMPOSE_PROJECT, and the reclaim path checks
# `docker compose -p <project> ps -q` (bare `docker run` containers are
# invisible to it, so lifecycle goes through compose files).
compose_project="${ESHU_LIVE_COMPOSE_PROJECT:-eshu-live-backend}"
[[ "${compose_project}" =~ ^[a-z0-9][a-z0-9_-]*$ ]] || die "invalid Compose project"
# Exported: retain_live_gate_lock records it in the --keep marker.
export GATE_COMPOSE_PROJECT="${compose_project}"
active_compose_file=""
report_dir=""
source_head=""
shard_helper="${script_dir}/lib/live_backend_shards.py"
compose_args=()
# shellcheck source=scripts/lib/ci-image-compose.sh
. "${repo_root}/scripts/lib/ci-image-compose.sh"

compose_down() {
	local containers volumes
	docker compose "${compose_args[@]}" down -v >/dev/null 2>&1 || return 1
	containers="$(docker ps -aq --filter "label=com.docker.compose.project=${compose_project}")" || return 1
	volumes="$(docker volume ls -q --filter "label=com.docker.compose.project=${compose_project}")" || return 1
	[[ -z "${containers}" && -z "${volumes}" ]]
}

wait_healthy() {
	local port="$1" path="$2" deadline=180 waited=0
	while ! curl -sf -o /dev/null "http://127.0.0.1:${port}${path}" 2>/dev/null; do
		[[ "${waited}" -lt "${deadline}" ]] || return 1
		sleep 2
		waited=$((waited + 2))
	done
	return 0
}

# Single EXIT trap (installed once after lock acquisition): mid-run trap
# juggling clobbers the lock release, and arming teardown on the --keep
# path destroys the stack --keep promised to leave up. The active stack
# is torn down unless --keep retained it; the mutex is retained with the
# stack on --keep (so the next run refuses while the ports are held) and
# released otherwise. Retention needs a stack we own: --keep with
# --no-compose owns nothing, so it releases.
cleanup() {
	local status=$?
	if [[ -n "${active_compose_file}" && "${keep}" == "0" ]]; then
		compose_down "${active_compose_file}" || status=1
	fi
	if [[ "${keep}" == "0" ]]; then
		eshu_ci_compose_cleanup
	fi
	if [[ "${keep}" == "1" && "${use_compose}" == "1" ]]; then
		retain_live_gate_lock
	else
		release_live_gate_lock
	fi
	if [[ -n "${report_dir}" ]]; then
		python3 "${shard_helper}" report --head "${source_head}" --shard "${shard}" \
			--plan "${report_dir}/full.plan" --events "${report_dir}/events.jsonl" \
			--output "${results:-${report_dir}/result.json}" || status=1
		rm -rf "${report_dir}"
	fi
	exit "${status}"
}

# Run one ledger file against one backend with a fresh database: `down -v`
# clears the previous file's volume, `up` recreates it empty.
# Args: backend, compose file, file, package, tests(|-joined), is_last(0/1).
run_one() {
	local name="$1" compose_file="$2" file="$3" package="$4" tests="$5" is_last="$6"
	local health_path started=$SECONDS setup_seconds test_seconds cleanup_started cleanup_seconds status=0
	if [[ "${name}" == "nornicdb" ]]; then
		health_path="/health"
	else
		health_path="/"
	fi
	printf 'run-live-backend-tests: %s %s (%s)\n' "${name}" "${file}" "${package}"
	if [[ "${name}" == "neo4j" && "${use_compose}" == "1" ]]; then
		printf 'run-live-backend-tests: neo4j platform %s\n' "${NEO4J_PLATFORM:-compose default (linux/amd64)}"
	fi
	if [[ "${use_compose}" == "1" ]]; then
		compose_args=(-p "${compose_project}" -f "${repo_root}/${compose_file}")
		eshu_ci_compose_prepare "${repo_root}" "${compose_args[@]}" || die "CI image overlay preflight failed"
		if [[ -n "${ESHU_CI_COMPOSE_OVERLAY}" ]]; then
			compose_args+=(-f "${ESHU_CI_COMPOSE_OVERLAY}")
		fi
		compose_down "${compose_file}" || die "could not establish fresh ${name} stack"
		# A failed up can leave a half-started stack behind, and the EXIT
		# trap only knows stacks recorded in active_compose_file (assigned
		# after a successful start), so tear the half-started stack down
		# here before dying instead of littering it.
		docker compose "${compose_args[@]}" up -d --wait --wait-timeout 180 ||
			{ compose_down "${compose_file}" || true; die "could not start ${name} stack"; }
		active_compose_file="${compose_file}"
		wait_healthy "${NEO4J_HTTP_PORT}" "${health_path}" || die "${name} stack never became healthy"
	fi
	# Several tests default the database to "nornic" when the env is empty,
	# which does not exist on Neo4j: pin it per backend so both backends
	# run the same files against their own database.
	local database="nornic"
	[[ "${name}" == "nornicdb" ]] || database="neo4j"
	export ESHU_NEO4J_URI="bolt://127.0.0.1:${NEO4J_BOLT_PORT}" ESHU_LIVE_GRAPH_BACKEND="${name}"
	export ESHU_NEO4J_DATABASE="${database}" ESHU_LIVE_GRAPH_DATABASE="${database}"
	unset ESHU_NEO4J_USERNAME ESHU_NEO4J_PASSWORD || true
	setup_seconds=$((SECONDS - started))
	started=$SECONDS
	(cd "${repo_root}/go" && go test -tags "${tags}" -count=1 "${package}" -run "^(${tests})$") || status=$?
	test_seconds=$((SECONDS - started))
	cleanup_started=$SECONDS
	if [[ "${use_compose}" == "1" ]]; then
		# --keep retains only the last file's stack: earlier ones share
		# the fixed host port, so keeping them all would collide.
		if [[ "${keep}" == "0" || "${is_last}" == "0" ]]; then
			compose_down "${compose_file}" || die "could not clean ${name} stack after test"
		else
			printf 'run-live-backend-tests: --keep: stack left running (bolt 127.0.0.1:%s)\n' \
				"${NEO4J_BOLT_PORT}"
			docker compose "${compose_args[@]}" ps
		fi
		active_compose_file=""
		if [[ "${keep}" == "0" || "${is_last}" == "0" ]]; then
			eshu_ci_compose_cleanup
		fi
	fi
	cleanup_seconds=$((SECONDS - cleanup_started))
	python3 "${shard_helper}" record --run "${name}|${compose_file}|${file}|${package}|${tests}" \
		--status "${status}" --setup-seconds "${setup_seconds}" --test-seconds "${test_seconds}" \
		--cleanup-seconds "${cleanup_seconds}" --events "${report_dir}/events.jsonl" || die "could not record terminal run"
	printf 'live-backend-target: shard=%s backend=%s file=%s setup=%ss test=%ss cleanup=%ss status=%s\n' \
		"${shard}" "${name}" "${file}" "${setup_seconds}" "${test_seconds}" "${cleanup_seconds}" "${status}"
	return "${status}"
}

# Shared pure planner preserves backend pins and complete multi-Test fields.
full_runs="$(printf '%s\n' "${targets[@]}" | python3 "${shard_helper}" expand --backend "${backend}")" || die "could not expand live-test targets"
mapfile -t runs <<<"${full_runs}"
selected_runs="$(printf '%s\n' "${runs[@]}" | python3 "${shard_helper}" plan --shard "${shard}")" || die "could not partition full live plan"
mapfile -t runs <<<"${selected_runs}"

# Plan probe for the static self-test: list the scheduled runs without
# touching Docker or the lock.
if [[ "${ESHU_LIVE_RUNNER_SELFTEST:-0}" == "plan" ]]; then
	printf '%s\n' "${runs[@]}"
	exit 0
fi

command -v docker >/dev/null 2>&1 || die "docker is required"
command -v go >/dev/null 2>&1 || die "go is required"
command -v curl >/dev/null 2>&1 || die "curl is required for backend health checks"
acquire_live_gate_lock
trap cleanup EXIT

source_head="$(git -C "${repo_root}" rev-parse HEAD)" || die "cannot identify tested source"
report_dir="$(mktemp -d)"
printf '%s\n' "${full_runs}" >"${report_dir}/full.plan"
: >"${report_dir}/events.jsonl"
printf 'live-backend-plan: head=%s shard=%s selected=%s full=%s\n' \
	"${source_head}" "${shard}" "${#runs[@]}" "$(printf '%s\n' "${full_runs}" | wc -l | tr -d ' ')"
failed=0
total="${#runs[@]}"
[[ "${total}" -gt 0 ]] || die "no ledger targets selected for --backend ${backend}"
for (( i = 0; i < total; i++ )); do
	name="${runs[$i]%%|*}"
	rest="${runs[$i]#*|}"
	compose_file="${rest%%|*}"
	rest="${rest#*|}"
	file="${rest%%|*}"
	rest="${rest#*|}"
	package="${rest%%|*}"
	tests="${rest#*|}"
	is_last=0
	[[ "$((i + 1))" -lt "${total}" ]] || is_last=1
	run_one "${name}" "${compose_file}" "${file}" "${package}" "${tests}" "${is_last}" || failed=1
done

[[ "${failed}" == "0" ]] || die "live-backend tests failed"
printf 'run-live-backend-tests: all backends green\n'
