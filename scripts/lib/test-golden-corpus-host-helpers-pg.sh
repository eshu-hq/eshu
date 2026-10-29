#!/usr/bin/env bash
# SPDX-License-Identifier: MIT
# Copyright (c) 2025-2026 eshu-hq
#
# Self-test for the pg() -v forwarding in golden-corpus-host-helpers.sh.
# Single-argument callers must see byte-identical argv; leading -v pairs must
# reach psql as query variables in both compose and --no-compose modes.

set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "${repo_root}"
# shellcheck source=scripts/lib/golden-corpus-host-helpers.sh
log() { :; }
source "${repo_root}/scripts/lib/golden-corpus-host-helpers.sh"

fail() { printf 'test-golden-corpus-host-helpers-pg: %s\n' "$*" >&2; exit 1; }

case_dir="$(mktemp -d -t golden-host-helpers-pg.XXXXXX)"
trap 'rm -rf "${case_dir}"' EXIT
argv_log="${case_dir}/argv.log"

psql() { printf '%s\n' "$@" >>"${argv_log}"; printf 'row\n'; }
docker() { printf '%s\n' "$@" >>"${argv_log}"; printf 'row\n'; }
die() { fail "$@"; }

# --no-compose, single argument: legacy argv untouched.
use_compose=0
ESHU_POSTGRES_DSN="postgresql://eshu:x@localhost:1/eshu"
[[ "$(pg "SELECT 1")" == "row" ]] || fail "pg returned unexpected output"
rg -Fqx -- "ON_ERROR_STOP=1" "${argv_log}" || fail "ON_ERROR_STOP flag missing"
[[ "$(rg -c -Fx -- "-v" "${argv_log}")" == "1" ]] || fail "single-arg call must pass exactly one -v flag"

# --no-compose, -v forwarding: variables land before -tA -c.
: >"${argv_log}"
[[ "$(pg -v cfg='{"a":1}' -v id=x "SELECT :'cfg'")" == "row" ]] || fail "pg -v returned unexpected output"
rg -Fqx -- "ON_ERROR_STOP=1" "${argv_log}" || fail "ON_ERROR_STOP flag missing with -v"
rg -Fqx -- 'cfg={"a":1}' "${argv_log}" || fail "first -v pair not forwarded"
rg -Fqx -- "id=x" "${argv_log}" || fail "second -v pair not forwarded"
# shellcheck disable=SC2016 # the needle is a literal psql -c argument
rg -Fqx -- "SELECT :'cfg'" "${argv_log}" || fail "SQL text not forwarded after -v pairs"
# rg 15.1.0 prints no text-mode matches in this environment (exit codes and
# --count still work), so the ordering check below reads the log with pure
# bash instead of rg -n.
argv_content="$(cat "${argv_log}")"
argv_before_tA="${argv_content%%-tA*}"
[[ "${argv_before_tA}" == *'cfg={"a":1}'*'id=x'* ]] ||
	fail "-v pairs must precede -tA -c"

# Compose mode forwards the same way through docker compose exec.
: >"${argv_log}"
use_compose=1
compose_args=(-p test -f compose.yaml)
[[ "$(pg -v k=v "SELECT 1")" == "row" ]] || fail "compose pg -v returned unexpected output"
rg -Fqx -- "k=v" "${argv_log}" || fail "compose mode dropped the -v pair"

printf 'PASS: golden host helpers pg forwarding\n'
