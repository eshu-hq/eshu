#!/usr/bin/env bash
# Static structural test for verify-ifa-replay-drive.sh. The verifier itself
# needs Docker + a built toolchain to run end to end, so this mirror validates
# the contract that cannot silently drift: the script parses, sets strict
# mode, uses a unique Compose project + non-default ports, drives the demo-org
# GCP cassette through `eshu-ifa drive`, proves the drive enqueued work before
# draining, and polls the exact drains.go SQL via eshu-golden-corpus-gate,
# then tears its stack down.
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
script="${repo_root}/scripts/verify-ifa-replay-drive.sh"

fail() { printf 'test-verify-ifa-replay-drive: %s\n' "$*" >&2; exit 1; }

[[ -f "${script}" ]] || fail "missing ${script}"
[[ -x "${script}" ]] || fail "verify-ifa-replay-drive.sh must be executable"

# Parses under bash -n.
bash -n "${script}" || fail "verify-ifa-replay-drive.sh has a syntax error"

require() {
	local label="$1" needle="$2"
	rg --fixed-strings --quiet -- "${needle}" "${script}" || fail "missing ${label}: ${needle}"
}

# Strict mode and self-cleanup.
require "strict mode" "set -euo pipefail"
require "exit trap" "trap cleanup EXIT"
# The bash>=4.4 precondition guard MUST stay: under bash 3.2 a nounset abort is
# masked by the exit trap above as a false PASS. Pin the exact check so a
# refactor cannot silently drop it.
require "bash>=4.4 guard (masking-safe)" "requires bash >= 4.4"
# Background pids must be recorded in the PARENT shell (printf -v), or the
# cleanup trap reaps nothing on a failure path and leaks host processes.
require "parent-shell pid capture" "printf -v"
# Failure must surface the host-binary logs before the work dir is removed.
require "failure log dump" "host binary logs (failure)"
require "--no-compose flag" "--no-compose"
require "--keep flag" "--keep"

# Isolation: a unique Compose project name and non-default ports, so this
# script cannot collide with verify-golden-corpus-gate.sh's own defaults
# (15432/7687/7474) or another stack already running on the host.
require "unique compose project default" 'REPLAY_DRIVE_COMPOSE_PROJECT:=eshu-replay-drive-$$'
require "compose -p flag on up" '-p "${REPLAY_DRIVE_COMPOSE_PROJECT}"'
require "compose -p flag on down" 'docker compose -p "${REPLAY_DRIVE_COMPOSE_PROJECT}"'
if rg --fixed-strings --quiet -- 'ESHU_POSTGRES_PORT:=15432' "${script}"; then
	fail "must not reuse verify-golden-corpus-gate.sh's default Postgres port 15432"
fi
if rg --fixed-strings --quiet -- 'NEO4J_BOLT_PORT:=7687' "${script}"; then
	fail "must not reuse verify-golden-corpus-gate.sh's default Neo4j bolt port 7687"
fi
# The port overrides MUST be exported, not just set: docker-compose.yaml's
# "ports" mapping interpolates them from the environment `docker compose`
# inherits, not from this script's own shell variables. An unexported
# `: "${VAR:=n}"` silently falls back to docker-compose.yaml's own hardcoded
# default port instead of this script's isolated one (regression proven live
# on 2026-07-09: bootstrap-data-plane connection-refused on the intended
# non-default Postgres port because it was never actually published).
require "exported Postgres port override" 'export ESHU_POSTGRES_PORT='
require "exported Neo4j bolt port override" 'export NEO4J_BOLT_PORT='
require "exported Neo4j http port override" 'export NEO4J_HTTP_PORT='

# Drives every pipeline stage end to end.
require "schema bootstrap" "eshu-bootstrap-data-plane"
require "ifa binary build" "build_bin ifa"
require "drive verb invocation" 'eshu-ifa" drive -cassette'
require "demo-org cassette" "testdata/cassettes/gcpcloud/supply-chain-demo.json"
require "N=1 default workers" "REPLAY_DRIVE_WORKERS:=1"
require "projector drain" "eshu-projector"
require "reducer drain" "eshu-reducer"
require "gate binary" "eshu-golden-corpus-gate"
require "drains phase" "-phase=drains"
require "snapshot contract" "testdata/golden/e2e-20repo-snapshot.json"

# Populated-then-drained guard: the drive must be proven to have enqueued at
# least one fact_work_items row before the residual=0 poll runs, or a 0/0
# reading before anything was ever enqueued would pass on a vacuous drain.
require "drive-populated guard" "vacuous drain proof"
require "fact_work_items populated check" "SELECT count(*) FROM fact_work_items;"

# The drain must be polled by the gate binary, not slept.
if rg --quiet --pcre2 'sleep\s+\$\{?GATE_DRAIN' "${script}"; then
	fail "drain must be polled by the gate, not slept"
fi

# No private data: hostnames, IPs, cloud account IDs, keys, internal paths.
private_pattern='ghp_|github_pat_|glpat-|AKIA|ASIA|xox[baprs]-|arn:aws:|(^|[^0-9])[0-9]{12}([^0-9]|$)|/Users/|/home/[a-z]'
if rg --pcre2 --quiet -- "${private_pattern}" "${script}"; then
	fail "verify-ifa-replay-drive.sh looks like it contains private data"
fi

printf 'test-verify-ifa-replay-drive: pass\n'
