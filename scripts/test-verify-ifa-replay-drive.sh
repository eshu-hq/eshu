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
pins_lib="${repo_root}/scripts/lib/ifa_mirror_pins.sh"

fail() { printf 'test-verify-ifa-replay-drive: %s\n' "$*" >&2; exit 1; }

[[ -f "${script}" ]] || fail "missing ${script}"
[[ -x "${script}" ]] || fail "verify-ifa-replay-drive.sh must be executable"
[[ -f "${pins_lib}" ]] || fail "missing ${pins_lib}"
bash -n "${pins_lib}" || fail "ifa_mirror_pins.sh has a syntax error"

# Parses under bash -n.
bash -n "${script}" || fail "verify-ifa-replay-drive.sh has a syntax error"

# Pin helpers are shared with the sibling mirror so there is ONE matcher, not a
# private copy per mirror that drifts. `require` binds LIVE CODE: the bare
# whole-file match this used to be was satisfied by a comment, which is #6161.
# shellcheck source=scripts/lib/ifa_mirror_pins.sh
source "${pins_lib}"

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
# Bind the PARSER cases, not the flag names -- the usage text names them too.
require "--no-compose flag is parsed" '--no-compose) use_compose=0 ;;'
require "--keep flag is parsed" '--keep) keep=1 ;;'

# Isolation: a unique Compose project name and non-default ports, so this
# script cannot collide with verify-golden-corpus-gate.sh's own defaults
# (15432/7687/7474) or another stack already running on the host.
require "unique compose project default" 'REPLAY_DRIVE_COMPOSE_PROJECT:=eshu-replay-drive-$$'
# Bind the BRING-UP line, not the bare -p flag: the teardown and the two exec
# probes carry the same flag, so deleting `up -d` entirely left this pin green
# with the gate never starting its stack (#6161). Same defect as the determinism
# and dead-letter mirrors carried.
require "compose -p flag on up" 'docker compose -p "${REPLAY_DRIVE_COMPOSE_PROJECT}" -f "${compose_file}" up -d nornicdb postgres'
# Bind the TEARDOWN line for the same reason, which the round above missed: the
# bare -p flag has five code occurrences (the project default, this teardown, the
# bring-up, and two exec probes), so deleting the teardown outright left this pin
# green on any of the other four. Losing it leaks the containers and the volume,
# which is exactly the isolation the comment block above claims.
require "compose -p flag on down" 'docker compose -p "${REPLAY_DRIVE_COMPOSE_PROJECT}" -f "${compose_file}" down -v'
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
# Bind the INVOCATION: the log() line above names the binary too, so replacing
# the real call with `true` left this pin green with the schema never applied.
require "schema bootstrap" '"${bin_dir}/eshu-bootstrap-data-plane"'
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
# Bind the guard's own comparison, not the prose above it. "vacuous drain proof"
# appears in verify-ifa-replay-drive.sh ONLY in the explanatory comment, so this
# pin was satisfied by that comment while the guard itself could be deleted
# outright -- proven by deleting it and watching this mirror stay green (#6161).
# The sibling dead-letter-matrix mirror pins the same guard by a phrase that
# lives in its die() message, which is code; this one had no such phrase.
require "drive-populated guard" '"${work_items_after_drive}" -gt 0'
require "fact_work_items populated check" "SELECT count(*) FROM fact_work_items;"

# The drain must be polled by the gate binary, not slept.
if rg --quiet --pcre2 'sleep\s+\$\{?GATE_DRAIN' "${script}"; then
	fail "drain must be polled by the gate, not slept"
fi

# No private data: hostnames, IPs, cloud account IDs, keys, internal paths.
private_pattern='ghp_|github_pat_|glpat-|AKIA|ASIA|xox[baprs]-|arn:aws:|(^|[^0-9])[0-9]{12}([^0-9]|$)|/Users/|/home/[a-z]'
# Scans the shared pin lib too, not just the gate: it was added by #6161 and
# nothing else in the tree covers it -- the determinism mirror's derived scan
# only reaches *_lib variables bound in ITS scope.
for private_target in "${script}" "${pins_lib}"; do
	if rg --pcre2 --quiet -- "${private_pattern}" "${private_target}"; then
		fail "$(basename "${private_target}") looks like it contains private data"
	fi
done

# Every pin helper above must BIND CODE. Run last, so `compgen -A function`
# sees them all. This is what stops #6161 from being reintroduced: a helper
# added later is discovered and executed, not trusted.
ifa_mirror_assert_pins_bind_code
# Pin this gate's OWN call site. EXACTLY TWO -- the call and this needle --
# because an in-file pin is always satisfied by its own line, which is the
# defect #6173 had to fix twice.
[[ "$(ifa_mirror_count_code_matches 'ifa_mirror_assert_pins_bind_code' "${BASH_SOURCE[0]}")" -eq 2 ]] \
	|| fail "this mirror no longer runs the pin-helper behaviour check -- nothing would prove its pins bind code"

printf 'test-verify-ifa-replay-drive: pass\n'
