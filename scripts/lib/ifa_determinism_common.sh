#!/usr/bin/env bash
# Shared helpers for Ifá P3 (#4396) Docker-backed determinism verifiers:
# building host binaries, waiting for the Postgres + NornicDB Compose
# backends, querying Postgres (compose or --no-compose), and starting/
# tracking background host processes. Extracted so
# scripts/verify-ifa-determinism.sh (the N-cell graph-determinism matrix,
# slice 5) does not reimplement the same build/wait/pg/start_bg idiom
# scripts/verify-ifa-replay-drive.sh (slice 2) and
# scripts/verify-ifa-dead-letter-determinism.sh (slice 4) each already prove
# standalone.
#
# This file is a plain function library, not a script: it deliberately does
# NOT set `set -euo pipefail` (sourcing it would rebind the caller's shell
# options). The calling script owns strict mode, its own log/die helpers, and
# its own exit trap. Every function here returns a non-zero status and prints
# its own diagnostic to stderr on failure; callers decide whether to `die`.
#
# Not sourced by scripts/verify-ifa-replay-drive.sh or
# scripts/verify-ifa-dead-letter-determinism.sh: those slice 2/4 scripts stay
# exactly as already proven; only scripts/verify-ifa-determinism.sh (slice 5)
# sources this file.

# ifa_det_build_bin builds go/cmd/<cmd> into <bin_dir>/eshu-<cmd> with cgo
# enabled (NornicDB's Bolt driver needs cgo). Returns non-zero on build
# failure; the caller's own `go build` stderr already explains why.
ifa_det_build_bin() {
	local bin_dir="$1" cmd="$2"
	CGO_ENABLED=1 go -C go build -o "${bin_dir}/eshu-${cmd}" "./cmd/${cmd}"
}

# ifa_det_start_bg starts "$@" as a background process, redirecting its
# stdout/stderr to <log_dir>/<name>.log, appends its PID to the caller's
# global `bg_pids` array (so the caller's own exit trap can reap it), and
# stores the PID in the variable named by pidvar via `printf -v` — required
# because `local pid=$!` inside this function would otherwise be invisible to
# the caller's shell once this function returns.
ifa_det_start_bg() {
	local log_dir="$1" name="$2" pidvar="$3"
	shift 3
	"$@" >"${log_dir}/${name}.log" 2>&1 &
	local pid=$!
	bg_pids+=("${pid}")
	printf -v "${pidvar}" '%s' "${pid}"
}

# ifa_det_pg runs a single-value SQL query against the stack's Postgres,
# working in both compose mode (via the postgres container) and --no-compose
# mode (via a local psql client against dsn).
#
# Args: compose_project use_compose dsn sql [compose_file=docker-compose.yaml]
ifa_det_pg() {
	local compose_project="$1" use_compose="$2" dsn="$3" sql="$4"
	local compose_file="${5:-docker-compose.yaml}"
	if [[ "${use_compose}" -eq 1 ]]; then
		docker compose -p "${compose_project}" -f "${compose_file}" exec -T postgres \
			psql -U eshu -d eshu -tA -c "${sql}" 2>/dev/null
	else
		command -v psql >/dev/null 2>&1 || {
			echo "ifa_det_pg: psql client required in --no-compose mode" >&2
			return 1
		}
		psql "${dsn}" -tA -c "${sql}" 2>/dev/null
	fi
}

# ifa_det_wait_for_backends polls the compose project's nornicdb + postgres
# containers (over their in-container ports, not the host-mapped ones — the
# health checks run via `docker compose exec`, so they never see the host
# port remapping) until both report ready, or returns non-zero after the
# ~120s budget (60 iterations * 2s).
#
# Args: compose_project [compose_file=docker-compose.yaml]
ifa_det_wait_for_backends() {
	local compose_project="$1"
	local compose_file="${2:-docker-compose.yaml}"
	local i
	for i in $(seq 1 60); do
		if docker compose -p "${compose_project}" -f "${compose_file}" exec -T nornicdb \
			wget --spider -q http://localhost:7474/health >/dev/null 2>&1 &&
			docker compose -p "${compose_project}" -f "${compose_file}" exec -T postgres \
				pg_isready -U eshu -d eshu >/dev/null 2>&1; then
			return 0
		fi
		sleep 2
	done
	echo "ifa_det_wait_for_backends: Postgres + NornicDB did not become ready within budget" >&2
	return 1
}
