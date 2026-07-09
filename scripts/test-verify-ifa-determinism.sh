#!/usr/bin/env bash
# Static structural test for verify-ifa-determinism.sh. The matrix itself
# needs Docker + a built toolchain and takes ~30-45 minutes (three fresh
# Postgres + NornicDB stacks, sequential), so this mirror validates the
# contract that cannot silently drift: the script parses, sets strict mode,
# reuses an isolated Compose project + non-default ports distinct from every
# sibling verify-ifa-*.sh script, drives N ∈ {1, 2, 4}, tears down and
# rebuilds a FRESH stack between every cell, asserts the drive actually
# enqueued work before draining, drains via the same B-12 residual bound
# verify-ifa-replay-drive.sh proves, canonicalizes the graph at each cell,
# asserts all three digests are byte-identical, prints the full-dump diff on
# a mismatch instead of hiding it, and tears down its own stack on exit. This
# is the credential-free lane CI runs per PR; the full Docker matrix runs on
# demand/nightly, not on every PR.
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
script="${repo_root}/scripts/verify-ifa-determinism.sh"
lib="${repo_root}/scripts/lib/ifa_determinism_common.sh"

fail() { printf 'test-verify-ifa-determinism: %s\n' "$*" >&2; exit 1; }

[[ -f "${script}" ]] || fail "missing ${script}"
[[ -x "${script}" ]] || fail "verify-ifa-determinism.sh must be executable"
[[ -f "${lib}" ]] || fail "missing ${lib}"

# Both files parse under bash -n.
bash -n "${script}" || fail "verify-ifa-determinism.sh has a syntax error"
bash -n "${lib}" || fail "ifa_determinism_common.sh has a syntax error"

require() {
	local label="$1" needle="$2"
	rg --fixed-strings --quiet -- "${needle}" "${script}" || fail "missing ${label}: ${needle}"
}
require_lib() {
	local label="$1" needle="$2"
	rg --fixed-strings --quiet -- "${needle}" "${lib}" || fail "missing ${label} (lib): ${needle}"
}

# Strict mode and self-cleanup.
require "strict mode" "set -euo pipefail"
require "exit trap" "trap cleanup EXIT"
require "sources shared lib" "scripts/lib/ifa_determinism_common.sh"
# Background pids must be recorded in the PARENT shell (printf -v in the lib),
# or the cleanup trap reaps nothing on a failure path and leaks host processes.
require_lib "parent-shell pid capture" "printf -v"
# Failure must surface the host-binary logs before the work dir is removed.
require "failure log dump" "host binary logs (failure)"
require "--no-compose flag" "--no-compose"
require "--keep flag" "--keep"

# Isolation: a Compose project name and non-default ports distinct from every
# sibling verify-ifa-*.sh script and verify-golden-corpus-gate.sh's own
# defaults, so a run of this script cannot collide with any of them.
require "isolated compose project default" 'DETERMINISM_COMPOSE_PROJECT:=eshu-ifa-determinism-$$'
require "compose -p flag on up" '-p "${DETERMINISM_COMPOSE_PROJECT}"'
for reserved in \
	'ESHU_POSTGRES_PORT:=15432' 'NEO4J_BOLT_PORT:=7687' 'NEO4J_HTTP_PORT:=7474' \
	'ESHU_POSTGRES_PORT:=15532' 'NEO4J_BOLT_PORT:=7788' 'NEO4J_HTTP_PORT:=7575' \
	'ESHU_POSTGRES_PORT:=15635' 'NEO4J_BOLT_PORT:=7792' 'NEO4J_HTTP_PORT:=7679'; do
	if rg --fixed-strings --quiet -- "${reserved}" "${script}"; then
		fail "must not reuse a sibling verify-ifa-*.sh / verify-golden-corpus-gate.sh default port: ${reserved}"
	fi
done
# The port overrides MUST be exported, not just set: docker-compose.yaml's
# "ports" mapping interpolates them from the environment `docker compose`
# inherits, not from this script's own shell variables.
require "exported Postgres port override" 'export ESHU_POSTGRES_PORT='
require "exported Neo4j bolt port override" 'export NEO4J_BOLT_PORT='
require "exported Neo4j http port override" 'export NEO4J_HTTP_PORT='

# The determinism matrix itself: N in {1, 2, 4}, driven through the same
# cassette every sibling Ifá P3 script uses.
require "worker-count matrix N in {1,2,4}" "worker_counts=(1 2 4)"
require "demo-org cassette" "testdata/cassettes/gcpcloud/supply-chain-demo.json"
require "drive verb invocation" 'eshu-ifa" drive -cassette'
require "ifa binary build" "ifa_det_build_bin \"\${bin_dir}\" ifa"
require "projector drain" "eshu-projector"
require "reducer drain" "eshu-reducer"
require "gate binary" "eshu-golden-corpus-gate"
require "drains phase" "-phase=drains"
require "snapshot contract" "testdata/golden/e2e-20repo-snapshot.json"

# Populated-then-drained guard per cell: a 0/0 reading before anything was
# ever enqueued would pass on a vacuous drain.
require "drive-populated guard" "vacuous drain proof"
require "fact_work_items populated check" "SELECT count(*) FROM fact_work_items;"

# Fresh-DB-per-cell: every cell must tear its OWN stack down before the next
# cell starts, not only once at the very end — this is what makes each N a
# genuinely independent, fresh-database run rather than an incremental replay
# onto the previous cell's data.
require "per-cell fresh-stack teardown" "fresh stack for the next cell"
require "per-cell down -v inside the loop" 'docker compose -p "${DETERMINISM_COMPOSE_PROJECT}" -f "${compose_file}" down -v'

# Graph-truth capture and the digest-equality assertion.
require "graph-dump full-bytes capture" "graph-dump -out"
require "graph-dump digest capture" "graph-dump -digest"
require "digest storage per N" "digests[\${n}]="
require "digest mismatch detection" "MISMATCH:"
require "full-bytes diff on divergence" "diff -u"
require "failure-artifact framing" "failure artifact"
require "hard failure on divergence" "graph-determinism matrix FAILED"
require "no-normalize-away directive" "do NOT lower N, retry, or otherwise normalize this away"

# Per-cell wall time is reported.
require "per-cell wall time capture" "cell_end - cell_start"
require "wall time in PASS reporting" "wall=%ss"

# The drain must be polled by the gate binary, not slept.
if rg --quiet --pcre2 'sleep\s+\$\{?GATE_DRAIN' "${script}"; then
	fail "drain must be polled by the gate, not slept"
fi

# No private data: hostnames, IPs, cloud account IDs, keys, internal paths.
private_pattern='ghp_|github_pat_|glpat-|AKIA|ASIA|xox[baprs]-|arn:aws:|(^|[^0-9])[0-9]{12}([^0-9]|$)|/Users/|/home/[a-z]'
if rg --pcre2 --quiet -- "${private_pattern}" "${script}"; then
	fail "verify-ifa-determinism.sh looks like it contains private data"
fi
if rg --pcre2 --quiet -- "${private_pattern}" "${lib}"; then
	fail "ifa_determinism_common.sh looks like it contains private data"
fi

printf 'test-verify-ifa-determinism: pass\n'
