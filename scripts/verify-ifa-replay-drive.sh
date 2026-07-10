#!/usr/bin/env bash
# Ifá P2 (#4395) concurrent replay driver drain proof. Brings up an isolated
# Postgres + NornicDB Compose stack, applies schema, drives the demo-org GCP
# cassette through `eshu-ifa drive` at -workers 1, runs the projector and
# reducer as background host processes, and polls the EXACT fact_work_items /
# shared_projection_intents drain SQL go/cmd/golden-corpus-gate/drains.go
# defines (via eshu-golden-corpus-gate -phase=drains) until both reach the
# B-12 snapshot's bound or a timeout — proving #4395's acceptance clause:
# "driver passes -race; same Odù drains (fact_work_items.residual_max:0) at
# N=1".
#
# Mirrors scripts/verify-golden-corpus-gate.sh's host-binaries-over-compose
# shape, scoped down to the single demo-org GCP cassette instead of the full
# 20-repo corpus: no bootstrap-index, no filesystem repo corpus, no API/MCP
# query truth, no other B-10 cassette collectors. Runs on a UNIQUE Compose
# project name and host ports so it does not collide with another stack
# already running on this machine (the golden-corpus gate's own defaults, or
# an unrelated local product stack).
#
# Usage:
#   scripts/verify-ifa-replay-drive.sh [--no-compose] [--keep]
#     --no-compose  assume Postgres + NornicDB are already running on the
#                   configured ports; skip compose up/down here.
#     --keep        leave services running and the work dir in place on exit
#                   (for debugging a failed run).

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

# ----------------------------------------------------------------------------
# Configuration (override via environment). Defaults deliberately differ from
# verify-golden-corpus-gate.sh's own defaults (15432/7687/7474) so both scripts
# can run at the same time on one machine without colliding.
# ----------------------------------------------------------------------------
: "${REPLAY_DRIVE_COMPOSE_PROJECT:=eshu-replay-drive-$$}"
# These three MUST be exported (not just set): docker-compose.yaml's "ports"
# mapping interpolates them from the child process environment it hands to
# `docker compose`, not from this script's own shell variables. An unexported
# override here would silently fall back to docker-compose.yaml's own hardcoded
# defaults (15432/7687/7474) instead of this script's isolated ports.
export ESHU_POSTGRES_PORT="${ESHU_POSTGRES_PORT:-15532}"
export NEO4J_BOLT_PORT="${NEO4J_BOLT_PORT:-7788}"
export NEO4J_HTTP_PORT="${NEO4J_HTTP_PORT:-7575}"
: "${ESHU_POSTGRES_PASSWORD:=change-me}"
: "${ESHU_NEO4J_PASSWORD:=change-me}"
: "${REPLAY_DRIVE_WORKERS:=1}"
: "${GATE_DRAIN_TIMEOUT:=3m}"

compose_file="docker-compose.yaml"
cassette="${repo_root}/testdata/cassettes/gcpcloud/supply-chain-demo.json"

use_compose=1
keep=0
for arg in "$@"; do
	case "${arg}" in
		--no-compose) use_compose=0 ;;
		--keep) keep=1 ;;
		-h|--help) sed -n '2,24p' "${BASH_SOURCE[0]}"; exit 0 ;;
		*) echo "verify-ifa-replay-drive: unknown argument: ${arg}" >&2; exit 2 ;;
	esac
done

[[ -f "${cassette}" ]] || { echo "verify-ifa-replay-drive: cassette not found: ${cassette}" >&2; exit 1; }

work_dir="$(mktemp -d -t ifa-replay-drive.XXXXXX)"
bin_dir="${work_dir}/bin"
log_dir="${work_dir}/logs"
mkdir -p "${bin_dir}" "${log_dir}"

bg_pids=()

log() { printf '\n=== %s ===\n' "$*"; }
die() { printf 'verify-ifa-replay-drive: %s\n' "$*" >&2; exit 1; }

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
			docker compose -p "${REPLAY_DRIVE_COMPOSE_PROJECT}" -f "${compose_file}" down -v >/dev/null 2>&1 || true
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
# verify-golden-corpus-gate.sh).
export ESHU_LISTEN_ADDR="127.0.0.1:0"
export ESHU_METRICS_ADDR="127.0.0.1:0"
unset ESHU_PPROF_ADDR || true

build_bin() {
	local cmd="$1"
	CGO_ENABLED=1 go -C go build -o "${bin_dir}/eshu-${cmd}" "./cmd/${cmd}" \
		|| die "build ${cmd} failed"
}

start_bg() {
	local name="$1" pidvar="$2"; shift 2
	"$@" >"${log_dir}/${name}.log" 2>&1 &
	local pid=$!
	bg_pids+=("${pid}")
	printf -v "${pidvar}" '%s' "${pid}"
}

# pg runs a single-value SQL query against this script's own Postgres, working
# in both compose mode (via the postgres container) and --no-compose mode (via
# a local psql client).
pg() {
	local sql="$1"
	if [[ "${use_compose}" -eq 1 ]]; then
		docker compose -p "${REPLAY_DRIVE_COMPOSE_PROJECT}" -f "${compose_file}" exec -T postgres \
			psql -U eshu -d eshu -tA -c "${sql}" 2>/dev/null
	else
		command -v psql >/dev/null 2>&1 || die "psql client required in --no-compose mode"
		psql "${ESHU_POSTGRES_DSN}" -tA -c "${sql}" 2>/dev/null
	fi
}

log "build host binaries"
build_bin bootstrap-data-plane
build_bin ifa
build_bin projector
build_bin reducer
build_bin golden-corpus-gate

if [[ "${use_compose}" -eq 1 ]]; then
	log "start Postgres + NornicDB (project ${REPLAY_DRIVE_COMPOSE_PROJECT})"
	docker compose -p "${REPLAY_DRIVE_COMPOSE_PROJECT}" -f "${compose_file}" up -d nornicdb postgres

	log "wait for backends"
	backends_ready=false
	for _ in $(seq 1 60); do
		if docker compose -p "${REPLAY_DRIVE_COMPOSE_PROJECT}" -f "${compose_file}" exec -T nornicdb \
			wget --spider -q http://localhost:7474/health >/dev/null 2>&1 && \
			docker compose -p "${REPLAY_DRIVE_COMPOSE_PROJECT}" -f "${compose_file}" exec -T postgres \
			pg_isready -U eshu -d eshu >/dev/null 2>&1; then
			backends_ready=true
			break
		fi
		sleep 2
	done
	[[ "${backends_ready}" == "true" ]] || die "Postgres + NornicDB did not become ready within budget"
fi

log "apply Postgres + graph schema (eshu-bootstrap-data-plane)"
"${bin_dir}/eshu-bootstrap-data-plane" >"${log_dir}/bootstrap-data-plane.log" 2>&1 \
	|| { tail -40 "${log_dir}/bootstrap-data-plane.log"; die "bootstrap-data-plane failed"; }

log "drive demo-org GCP cassette through the concurrent replay Driver at -workers ${REPLAY_DRIVE_WORKERS}"
if ! "${bin_dir}/eshu-ifa" drive -cassette "${cassette}" -workers "${REPLAY_DRIVE_WORKERS}" \
	>"${log_dir}/ifa-drive.log" 2>&1; then
	tail -40 "${log_dir}/ifa-drive.log" >&2 || true
	die "eshu-ifa drive failed"
fi
cat "${log_dir}/ifa-drive.log"

# Prove the drive actually enqueued something: a residual=0 reading over a
# queue nothing was ever put in would be a vacuous drain proof, not evidence
# the driver committed the demo-org cassette (mirrors
# verify-golden-corpus-gate.sh's cassette-facts-landed check).
work_items_after_drive="$(pg 'SELECT count(*) FROM fact_work_items;' | tr -d '[:space:]')"
[[ -n "${work_items_after_drive}" && "${work_items_after_drive}" -gt 0 ]] \
	|| die "eshu-ifa drive committed but enqueued 0 fact_work_items rows (nothing to drain would make the residual=0 proof vacuous)"
printf 'fact_work_items enqueued by the drive: %s\n' "${work_items_after_drive}"

log "drain projector + reducer (background; gate polls to terminal)"
start_bg projector projector_pid "${bin_dir}/eshu-projector"
start_bg reducer reducer_pid "${bin_dir}/eshu-reducer"

if ! "${bin_dir}/eshu-golden-corpus-gate" \
	-phase=drains \
	-snapshot=testdata/golden/e2e-20repo-snapshot.json \
	-drain-timeout="${GATE_DRAIN_TIMEOUT}"; then
	tail -30 "${log_dir}/reducer.log" || true
	tail -30 "${log_dir}/projector.log" || true
	die "drain did not reach the snapshot's residual bound within ${GATE_DRAIN_TIMEOUT}"
fi
kill "${projector_pid}" "${reducer_pid}" >/dev/null 2>&1 || true

log "PASS: eshu-ifa drive N=${REPLAY_DRIVE_WORKERS} drain proof green (project ${REPLAY_DRIVE_COMPOSE_PROJECT}, postgres:${ESHU_POSTGRES_PORT}, neo4j-bolt:${NEO4J_BOLT_PORT})"
