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
delta_lib="${repo_root}/scripts/lib/ifa_sql_delta_live.sh"

fail() { printf 'test-verify-ifa-determinism: %s\n' "$*" >&2; exit 1; }

[[ -f "${script}" ]] || fail "missing ${script}"
[[ -x "${script}" ]] || fail "verify-ifa-determinism.sh must be executable"
[[ -f "${lib}" ]] || fail "missing ${lib}"
[[ -f "${delta_lib}" ]] || fail "missing ${delta_lib}"

# Both files parse under bash -n.
bash -n "${script}" || fail "verify-ifa-determinism.sh has a syntax error"
bash -n "${lib}" || fail "ifa_determinism_common.sh has a syntax error"
bash -n "${delta_lib}" || fail "ifa_sql_delta_live.sh has a syntax error"
[[ "$(wc -l <"${script}" | tr -d '[:space:]')" -lt 500 ]] \
	|| fail "verify-ifa-determinism.sh must stay under 500 lines"

require() {
	local label="$1" needle="$2"
	rg --fixed-strings --quiet -- "${needle}" "${script}" || fail "missing ${label}: ${needle}"
}
require_lib() {
	local label="$1" needle="$2"
	rg --fixed-strings --quiet -- "${needle}" "${lib}" || fail "missing ${label} (lib): ${needle}"
}
require_delta_lib() {
	local label="$1" needle="$2"
	rg --fixed-strings --quiet -- "${needle}" "${delta_lib}" || fail "missing ${label} (delta lib): ${needle}"
}

# Strict mode and self-cleanup.
require "strict mode" "set -euo pipefail"
require "exit trap" "trap cleanup EXIT"
# The bash>=4.4 precondition guard MUST stay: under bash 3.2 a nounset abort is
# masked by the exit trap above as a false PASS. Pin the exact check so a
# refactor cannot silently drop it.
require "bash>=4.4 guard (masking-safe)" "requires bash >= 4.4"
require "sources shared lib" "scripts/lib/ifa_determinism_common.sh"
require "sources SQL delta-live lib" "scripts/lib/ifa_sql_delta_live.sh"
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

# Synth-multiscope cassette (issue #4396 slice 6b): generated ONCE before the
# cell loop via `ifa synth-cassette`, then driven into every cell via a
# SECOND `ifa drive` call alongside the unmodified demo-org cassette — this is
# what makes -workers N non-inert (a single-scope cassette gives the driver
# exactly one work unit for any N).
require "synth-cassette verb invocation" '"${bin_dir}/eshu-ifa" synth-cassette'
require "synth-cassette seed flag" "-seed \"\${SYNTH_MULTISCOPE_SEED}\""
require "synth-cassette projects flag" "-projects \"\${SYNTH_MULTISCOPE_PROJECTS}\""
require "synth-cassette resources flag" "-resources \"\${SYNTH_MULTISCOPE_RESOURCES}\""
require "synth-cassette generated before the cell loop" "synth_cassette=\"\${work_dir}/synth-multiscope.json\""
require "second drive invocation into the same cell" 'eshu-ifa" drive -cassette "${synth_cassette}" -workers "${n}"'
require "combined-graph digest framing" "demo-org + synth-multiscope + SQL family"

# SQL relationship family cassette (#5351): the committed cassette driven into
# every cell so the ifa-determinism lane actually replays the SQL relationship
# materialization family (backing the materialized_edges:sql_relationships
# manifest row's proof_gate: ifa-determinism claim), plus the per-cell
# absolute-set assertion (`ifa assert-edges`) that the P2 digest cannot make: a
# family silently empty in ALL cells has an identical digest in every cell and
# passes the digest comparison vacuously; the absolute expected set catches it.
require "SQL cassette path" "testdata/cassettes/sqlrelationships/ifa-sql-family.json"
require "SQL expected-edge set path" "go/internal/ifa/testdata/sqlrelationships/ifa-sql-family-expected-edges.json"
require "SQL delta cassette path" "testdata/cassettes/sqlrelationships/ifa-sql-family-delta.json"
require "SQL delta-live expected-edge set path" "go/internal/ifa/testdata/sqlrelationships/ifa-sql-family-delta-live-expected-edges.json"
require "SQL cassette existence guard" 'SQL cassette not found'
require "SQL expected-edge set existence guard" 'SQL expected-edge set not found'
require "SQL delta cassette existence guard" 'SQL delta cassette not found'
require "SQL delta expected-edge set existence guard" 'SQL delta expected-edge set not found'
require "SQL baseline helper invocation in every cell" "ifa_det_drive_sql_baseline"
require_delta_lib "SQL cassette drive into every cell" 'eshu-ifa" drive -cassette "${sql_cassette}" -workers "${n}"'
require "SQL delta helper invocation in every cell" "ifa_det_run_sql_delta_live"
require_delta_lib "SQL delta cassette drive into every cell" 'eshu-ifa" drive -cassette "${sql_delta_cassette}" -workers "${n}"'
require_delta_lib "SQL delta populated guard" "SQL delta drive enqueued 0 new fact_work_items rows"
require "SQL baseline assertion helper invocation in every cell" "ifa_det_assert_sql_baseline"
require_delta_lib "assert-edges verb invocation" '"${bin_dir}/eshu-ifa" assert-edges'
require_delta_lib "assert-edges domain flag" "-domain sql_relationships"
require_delta_lib "assert-edges expected flag" '-expected "${sql_expected_edges}"'
require_delta_lib "assert-edges non-vacuity framing" "non-vacuity"
require_delta_lib "assert-edges no-normalize-away directive" "do NOT normalize this away"
require_delta_lib "delta assert-edges expected flag" '-expected "${sql_delta_expected_edges}"'
require_delta_lib "delta assert-edges exactness framing" "SQL delta-live materialized edge set did not match the expected accumulated set"

# #5007 contention cassette (opt-in --contention): the overlapping-identity
# fixture whose K scopes share one CloudResource uid set, so the cross-scope
# writers contend and the owner ledger must keep the digest identical across
# N=1/2/4. Generated via `ifa synth-cassette -overlap -divergent` and driven as
# a THIRD drive, behind --contention so it can never break the default matrix.
require "--contention flag" "--contention"
require "contention overlap generation" '-overlap -divergent'
require "contention seed flag" "-seed \"\${SYNTH_CONTENTION_SEED}\""
require "contention projects flag" "-projects \"\${SYNTH_CONTENTION_PROJECTS}\""
require "contention cassette generated once" 'contention_cassette="${work_dir}/contention.json"'
require "third drive of the contention cassette" 'eshu-ifa" drive -cassette "${contention_cassette}" -workers "${n}"'
require "contention ledger-regression framing" "graph-level contention"

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

# --teeth (#4396 slice 6): the acceptance clause's negative-path proof that
# the matrix catches a deliberately non-idempotent write, built behind a Go
# build tag so it never ships in a normal/CI/production binary.
require "--teeth flag" "--teeth"
require "teeth build tag" "ifadeterminismteeth"
require "teeth threads tags through every build call" 'ifa_det_build_bin "${bin_dir}" reducer "${build_tags}"'
require "teeth caught framing" "TEETH: CAUGHT"
require "teeth-not-caught is its own failure" "TEETH FAILED"
require "teeth still forbids lowering N" "lower N, retry, or otherwise normalize this away"
require_lib "build_bin accepts an optional tags argument" 'local bin_dir="$1" cmd="$2" tags="${3:-}"'
require_lib "tags become -tags args only when non-empty" 'tag_args=(-tags "${tags}")'

# The build-tag-gated fault itself must exist exactly where the script's own
# doc says it does, and must not be reachable without the tag.
teeth_reducer_on="${repo_root}/go/internal/reducer/gcp_resource_materialization_teeth.go"
teeth_reducer_off="${repo_root}/go/internal/reducer/gcp_resource_materialization_teeth_off.go"
teeth_cypher_on="${repo_root}/go/internal/storage/cypher/cloud_resource_node_writer_teeth.go"
teeth_cypher_off="${repo_root}/go/internal/storage/cypher/cloud_resource_node_writer_teeth_off.go"
for f in "${teeth_reducer_on}" "${teeth_reducer_off}" "${teeth_cypher_on}" "${teeth_cypher_off}"; do
	[[ -f "${f}" ]] || fail "missing teeth build-tag file: ${f}"
done
rg --fixed-strings --quiet -- '//go:build ifadeterminismteeth' "${teeth_reducer_on}" \
	|| fail "${teeth_reducer_on} must carry the ifadeterminismteeth build tag"
rg --fixed-strings --quiet -- '//go:build !ifadeterminismteeth' "${teeth_reducer_off}" \
	|| fail "${teeth_reducer_off} must carry the !ifadeterminismteeth build tag"
rg --fixed-strings --quiet -- '//go:build ifadeterminismteeth' "${teeth_cypher_on}" \
	|| fail "${teeth_cypher_on} must carry the ifadeterminismteeth build tag"
rg --fixed-strings --quiet -- '//go:build !ifadeterminismteeth' "${teeth_cypher_off}" \
	|| fail "${teeth_cypher_off} must carry the !ifadeterminismteeth build tag"

# No private data: hostnames, IPs, cloud account IDs, keys, internal paths.
private_pattern='ghp_|github_pat_|glpat-|AKIA|ASIA|xox[baprs]-|arn:aws:|(^|[^0-9])[0-9]{12}([^0-9]|$)|/Users/|/home/[a-z]'
if rg --pcre2 --quiet -- "${private_pattern}" "${script}"; then
	fail "verify-ifa-determinism.sh looks like it contains private data"
fi
if rg --pcre2 --quiet -- "${private_pattern}" "${lib}"; then
	fail "ifa_determinism_common.sh looks like it contains private data"
fi

printf 'test-verify-ifa-determinism: pass\n'
