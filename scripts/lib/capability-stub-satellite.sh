#!/usr/bin/env bash
# capability-stub-satellite.sh — run one command with the
# capability-inventory satellite inputs pointed at the offline double.
#
# verify/report/generate need the satellite's served OpenAPI document plus an
# admin/all-scope bearer token (ESHU_API_BASE_URL + ESHU_API_KEY), which
# commit-time hooks and the schema-drift CI job do not have. Wherever
# ESHU_API_BASE_URL is already set — a live satellite the operator provided —
# this wrapper passes the command through untouched. Otherwise it builds and
# starts the in-module stub source (the in-tree OpenAPI
# assembly behind the RequireAuth-shaped gate, the same double the capinv
# tests serve), points both variables at it, runs the command, and stops the
# server. The fetch path is identical either way, so the drift gate keeps
# biting without credentials, Docker, or network.
#
# Usage: capability-stub-satellite.sh command [args...]
#   The command inherits ESHU_API_BASE_URL/ESHU_API_KEY for its lifetime.
#   Exit status is the command's. Needs `go` (module toolchain) to build the
#   stub and `curl` for the readiness probe.
#
# Refs eshu-hq/eshu#9669, row 7.
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
go_dir="${repo_root}/go"
# The stub source lives in-module (not next to this wrapper): it imports
# go/internal/query/openapi, which the Go internal rule forbids from outside
# the module tree. The `//go:build ignore` tag keeps normal module builds
# from compiling it; only the explicit build below does.
stub_src="${go_dir}/cmd/capability-inventory/stub_satellite.go"

# Default dummy bearer the stub expects, mirroring the capinv test double
# (admin-all-scope-token in go/cmd/capability-inventory/*_test.go): a fixed,
# publicly-known, scope-less placeholder — never a real credential. Honored
# only when ESHU_API_KEY is unset; a preset key is passed to both the stub
# and the child so an explicitly configured token keeps working.
default_stub_token="admin-all-scope-token"

die() { printf 'capability-stub-satellite: %s\n' "$*" >&2; exit 1; }

[[ $# -gt 0 ]] || die "usage: capability-stub-satellite.sh command [args...]"
[[ -f "${stub_src}" ]] || die "missing stub source ${stub_src}"
command -v curl >/dev/null 2>&1 || die "needs curl for the stub readiness probe"

# A live satellite is already configured: run the command as-is. The stub
# must not shadow an operator-provided backend.
if [[ -n "${ESHU_API_BASE_URL:-}" ]]; then
	"$@"
	exit "$?"
fi

command -v go >/dev/null 2>&1 || die "needs go to build the stub satellite (no live ESHU_API_BASE_URL set)"

stub_token="${ESHU_API_KEY:-${default_stub_token}}"
work_dir="$(mktemp -d -t capability-stub-satellite.XXXXXX)"
server_pid=""

cleanup() {
	if [[ -n "${server_pid}" ]]; then
		kill "${server_pid}" >/dev/null 2>&1 || true
		wait "${server_pid}" 2>/dev/null || true
		server_pid=""
	fi
	if [[ -n "${work_dir}" && -d "${work_dir}" ]]; then
		rm -rf "${work_dir}"
	fi
}
trap cleanup EXIT INT TERM

# Built from inside the go module so the stub's query/openapi import resolves
# in module context (the file itself carries `//go:build ignore`, so normal
# module builds never compile it).
( cd "${go_dir}" && go build -o "${work_dir}/stub-satellite" "${stub_src}" ) \
	|| die "failed to build the stub satellite"

# The expected bearer travels via the stub's environment, not its argv:
# process arguments are visible to other local users via ps.
CAPABILITY_STUB_TOKEN="${stub_token}" "${work_dir}/stub-satellite" >"${work_dir}/url" 2>"${work_dir}/err.log" &
server_pid=$!

# Wait for the URL line, then for the RequireAuth shape itself: an untokened
# GET must 401, which proves the server is up AND gating without ever sending
# a bearer through this probe.
stub_url=""
ready=0
for _ in $(seq 1 50); do
	if [[ -z "${stub_url}" && -s "${work_dir}/url" ]]; then
		stub_url="$(cat "${work_dir}/url")"
	fi
	if [[ -n "${stub_url}" ]]; then
		code="$(curl -s -o /dev/null -w '%{http_code}' "${stub_url}/api/v0/openapi.json" 2>/dev/null || true)"
		if [[ "${code}" == "401" ]]; then
			ready=1
			break
		fi
	fi
	sleep 0.2
done
[[ "${ready}" == "1" ]] || die "stub satellite never gated (see ${work_dir}/err.log)"

export ESHU_API_BASE_URL="${stub_url}"
export ESHU_API_KEY="${stub_token}"

child_status=0
"$@" || child_status=$?
exit "${child_status}"
