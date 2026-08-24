#!/usr/bin/env bash
# Static structural test for verify-ifa-dead-letter-matrix.sh. The matrix
# itself needs Docker + a built toolchain and three fresh Postgres + NornicDB
# stacks (sequential), so this mirror validates the contract that cannot
# silently drift: the script parses, sets strict mode, reuses an isolated
# Compose project + non-default ports distinct from every sibling
# verify-ifa-*.sh / verify-golden-corpus-gate.sh script, mutates the demo-org
# cassette with -mutation schema-major, drives N ∈ {1, 2, 4}, tears down and
# rebuilds a FRESH stack between every cell, drains via this script's own
# failure-path terminal condition (not -phase=drains), captures the ordered
# dead-letter row set per cell, asserts all three sets are byte-identical,
# prints a diff on mismatch instead of hiding it, and tears down its own
# stack on exit. This is the credential-free lane CI runs per PR; the full
# Docker matrix runs on demand/nightly, not on every PR.
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
script="${repo_root}/scripts/verify-ifa-dead-letter-matrix.sh"
lib="${repo_root}/scripts/lib/ifa_determinism_common.sh"
pins_lib="${repo_root}/scripts/lib/ifa_mirror_pins.sh"
private_data_pattern_lib="${repo_root}/scripts/lib/ifa_private_data_pattern.sh"

fail() { printf 'test-verify-ifa-dead-letter-matrix: %s\n' "$*" >&2; exit 1; }

[[ -f "${script}" ]] || fail "missing ${script}"
[[ -x "${script}" ]] || fail "verify-ifa-dead-letter-matrix.sh must be executable"
[[ -f "${lib}" ]] || fail "missing ${lib}"
[[ -f "${pins_lib}" ]] || fail "missing ${pins_lib}"
[[ -f "${private_data_pattern_lib}" ]] || fail "missing ${private_data_pattern_lib}"
bash -n "${pins_lib}" || fail "ifa_mirror_pins.sh has a syntax error"
bash -n "${private_data_pattern_lib}" || fail "ifa_private_data_pattern.sh has a syntax error"

bash -n "${script}" || fail "verify-ifa-dead-letter-matrix.sh has a syntax error"

# Pin helpers are shared with the sibling mirror so there is ONE matcher, not a
# private copy per mirror that drifts. `require` binds LIVE CODE: the bare
# whole-file match this used to be was satisfied by a comment, which is #6161.
# shellcheck source=scripts/lib/ifa_mirror_pins.sh
source "${pins_lib}"
# shellcheck source=scripts/lib/ifa_private_data_pattern.sh
source "${private_data_pattern_lib}"

# Strict mode and self-cleanup, shared with every sibling verify-ifa-*.sh.
require "strict mode" "set -euo pipefail"
require "exit trap" "trap cleanup EXIT"
# The bash>=4.4 precondition guard MUST stay: under bash 3.2 a nounset abort is
# masked by the exit trap above as a false PASS. Pin the exact check so a
# refactor cannot silently drop it.
require "bash>=4.4 guard (masking-safe)" "requires bash >= 4.4"
# verify-ifa-dead-letter-determinism.sh is the single-N failure-classification
# sibling this matrix reuses (its mutation + terminal-condition SQL). It has no
# test mirror of its own but shares the same masking-capable set -u + EXIT-trap
# pattern, so assert its bash>=4 guard here too.
dld_sibling="${repo_root}/scripts/verify-ifa-dead-letter-determinism.sh"
rg --fixed-strings --quiet -- 'requires bash >= 4.4' "${dld_sibling}" \
	|| fail "verify-ifa-dead-letter-determinism.sh missing the bash>=4 guard"
require "sources shared lib" "scripts/lib/ifa_determinism_common.sh"
require "failure log dump" "host binary logs (failure)"
# Bind the PARSER cases, not the flag names: both names also appear in the usage
# text, so the parser could be deleted and the flags become silently inert. This
# is the same defect the determinism gate's `--teeth` pin had (#6161).
require "--no-compose flag is parsed" '--no-compose) use_compose=0 ;;'
require "--keep flag is parsed" '--keep) keep=1 ;;'

# Isolation: a distinct Compose project name and port triple, so a run of
# this script cannot collide with verify-ifa-determinism.sh,
# verify-ifa-replay-drive.sh, verify-ifa-dead-letter-determinism.sh, or
# verify-golden-corpus-gate.sh.
require "isolated compose project default" 'DEADLETTER_MATRIX_COMPOSE_PROJECT:=eshu-ifa-deadletter-matrix-$$'
# Bind the BRING-UP line, not the bare -p flag: both teardowns carry the same
# flag, so deleting `up -d` left this pin green with no stack ever started.
require "compose -p flag on up" 'docker compose -p "${DEADLETTER_MATRIX_COMPOSE_PROJECT}" -f "${compose_file}" up -d nornicdb postgres'
for reserved in \
	'ESHU_POSTGRES_PORT:=15432' 'NEO4J_BOLT_PORT:=7687' 'NEO4J_HTTP_PORT:=7474' \
	'ESHU_POSTGRES_PORT:=15532' 'NEO4J_BOLT_PORT:=7788' 'NEO4J_HTTP_PORT:=7575' \
	'ESHU_POSTGRES_PORT:=15635' 'NEO4J_BOLT_PORT:=7792' 'NEO4J_HTTP_PORT:=7679' \
	'ESHU_POSTGRES_PORT:=15636' 'NEO4J_BOLT_PORT:=7793' 'NEO4J_HTTP_PORT:=7680'; do
	if rg --fixed-strings --quiet -- "${reserved}" "${script}"; then
		fail "must not reuse a sibling verify-ifa-*.sh / verify-golden-corpus-gate.sh default port: ${reserved}"
	fi
done
require "exported Postgres port override" 'export ESHU_POSTGRES_PORT='
require "exported Neo4j bolt port override" 'export NEO4J_BOLT_PORT='
require "exported Neo4j http port override" 'export NEO4J_HTTP_PORT='

# The failure-path matrix itself: N in {1, 2, 4}, same demo-org cassette,
# mutated with -mutation schema-major (the corruption slice 4 proved reaches
# a durable dead-letter row).
require "worker-count matrix N in {1,2,4}" "worker_counts=(1 2 4)"
require "demo-org cassette" "testdata/cassettes/gcpcloud/supply-chain-demo.json"
require "schema-major mutation" "-kind schema-major"
# Bind the INVOCATION: the tee target `mutate-cassette.log` two lines below is
# code too, so replacing the real call with `true` left this pin green and the
# matrix driving an UNMUTATED cassette (#6161).
require "mutate-cassette invocation" '"${bin_dir}/eshu-ifa" mutate-cassette'
require "drive verb invocation" 'eshu-ifa" drive -cassette'
require "projector drain" "eshu-projector"
require "reducer drain" "eshu-reducer"

# The failure-path terminal condition MUST NOT be cmd/golden-corpus-gate's
# -phase=drains: that gate counts a dead_letter row as residual by design, so
# it would never reach zero on a deliberately malformed cassette.
require "own terminal condition, not -phase=drains" "status NOT IN ('succeeded', 'superseded', 'dead_letter')"
# The script must not actually INVOKE cmd/golden-corpus-gate's -phase=drains
# flag (it counts a dead_letter row as residual by design); its own doc
# comment referencing that flag by name for contrast is fine.
if rg --fixed-strings --quiet -- 'golden-corpus-gate" -phase=drains' "${script}"; then
	fail "must not reuse golden-corpus-gate's -phase=drains terminal condition (it treats dead_letter as residual)"
fi

# Populated-then-drained guard per cell.
require "drive-populated guard" "vacuous drain proof"
require "fact_work_items populated check" "SELECT count(*) FROM fact_work_items;"

# Fresh-DB-per-cell teardown, matching every sibling matrix script.
require "per-cell fresh-stack teardown" "fresh stack for the next cell"
# EXACTLY TWO, not "at least one": the exit trap and the per-cell teardown are
# the same line character for character, so no single-line needle can tell them
# apart and `require` was satisfied by whichever one survived. Deleting the
# per-cell one left this pin green with cells N=1/2/4 running against a
# contaminated stack -- so the cross-worker dead-letter-set identity comparison,
# the whole point of the matrix, compared accumulated state (#6161).
require_count "exit-trap and per-cell down -v" 'docker compose -p "${DEADLETTER_MATRIX_COMPOSE_PROJECT}" -f "${compose_file}" down -v' 2

# The dead-letter-set capture and identity assertion.
# Bind the SQL ASSIGNMENT: the die message on divergence repeats the same tuple
# in prose-shaped but live code, so a column could be dropped from the real query
# -- silently narrowing what the determinism comparison compares -- with this pin
# still green (#6161).
require "dead-letter set SQL selects the ordered contract" 'dead_letter_set_sql="SELECT work_item_id, stage, domain, failure_class FROM fact_work_items'
require "dead-letter set ordered by work_item_id" "ORDER BY work_item_id"
require "per-N dead-letter set storage" "dead_letter_sets[\${n}]="
require "mismatch detection" "MISMATCH:"
require "diff on divergence" "diff -u"
require "failure-artifact framing" "failure artifact"
require "hard failure on divergence" "dead-letter-set determinism FAILED"
require "no-normalize-away directive" "do NOT lower N, retry, or otherwise normalize this away"
require "at-least-one-dead-letter guard" "produced 0 durable dead-letter rows"

# No private data: hostnames, IPs, cloud account IDs, keys, internal paths.
# The pattern and its positive control live in ifa_private_data_pattern.sh, and
# the only way to reach the pattern is through the helper, which proves it still
# detects one planted token per alternative before it hands it over. This mirror
# carried a hand-copied literal instead: nothing measured it, so it could drift
# from the hardened copy, and replacing it with anything uncompilable left the
# mirror at exit 0 scanning nothing (#6161).
ifa_private_data_pattern private_pattern
# Scans the shared pin lib too, not just the gate: it was added by #6161 and
# nothing else in the tree covers it -- the determinism mirror's derived scan
# only reaches *_lib variables bound in ITS scope.
for private_target in "${script}" "${pins_lib}"; do
	# rc is CAPTURED, not tested through `if`: rg exits 2 on a pattern it cannot
	# compile, `if` reads that as "no match", and `set -e` does not apply inside an
	# `if` condition -- so one uncompilable pattern made every file read as clean
	# and this mirror still passed (#6161).
	private_rc=0
	rg --pcre2 --quiet -- "${private_pattern}" "${private_target}" || private_rc=$?
	[[ "${private_rc}" -ne 0 ]] \
		|| fail "$(basename "${private_target}") looks like it contains private data"
	[[ "${private_rc}" -eq 1 ]] \
		|| fail "the private-data scan could not run over $(basename "${private_target}") (rg exit ${private_rc}); a scanner that cannot run must never read as clean"
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

printf 'test-verify-ifa-dead-letter-matrix: pass\n'
