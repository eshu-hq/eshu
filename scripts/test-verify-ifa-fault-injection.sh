#!/usr/bin/env bash
# Static structural test for verify-ifa-fault-injection.sh (issue #4580 P6
# slice S5). The gate itself needs Docker + a built toolchain and takes
# significantly longer than the sibling determinism matrix (five fresh
# Postgres + NornicDB stacks, one of them building AND running a
# -tags ifafaultinjection reducer twice), so this mirror validates the
# contract that cannot silently drift: strict mode and the bash>=4.4 guard,
# an isolated Compose project and port triple distinct from every sibling
# verify-ifa-*.sh script, the five-cell shape (baseline + the four live
# cells; fail-terminal deliberately absent with its rationale documented),
# each cell's own recovery mechanism, the digest/dead_letter/non-vacuity
# assertions, and the tagged-reducer + fault-script wiring this gate is the
# first thing to exercise live.
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
script="${repo_root}/scripts/verify-ifa-fault-injection.sh"
det_lib="${repo_root}/scripts/lib/ifa_determinism_common.sh"
fault_lib="${repo_root}/scripts/lib/ifa_fault_injection_common.sh"

fail() { printf 'test-verify-ifa-fault-injection: %s\n' "$*" >&2; exit 1; }

[[ -f "${script}" ]] || fail "missing ${script}"
[[ -x "${script}" ]] || fail "verify-ifa-fault-injection.sh must be executable"
[[ -f "${fault_lib}" ]] || fail "missing ${fault_lib}"
[[ -f "${det_lib}" ]] || fail "missing ${det_lib}"

bash -n "${script}" || fail "verify-ifa-fault-injection.sh has a syntax error"
bash -n "${fault_lib}" || fail "ifa_fault_injection_common.sh has a syntax error"

require() {
	local label="$1" needle="$2"
	rg --fixed-strings --quiet -- "${needle}" "${script}" || fail "missing ${label}: ${needle}"
}
require_lib() {
	local label="$1" needle="$2"
	rg --fixed-strings --quiet -- "${needle}" "${fault_lib}" || fail "missing ${label} (lib): ${needle}"
}

# Strict mode, self-cleanup, and the masking-safe bash>=4.4 guard.
require "strict mode" "set -euo pipefail"
require "exit trap" "trap cleanup EXIT"
require "bash>=4.4 guard (masking-safe)" "requires bash >= 4.4"
require "sources determinism lib" "scripts/lib/ifa_determinism_common.sh"
require "sources fault-injection lib" "scripts/lib/ifa_fault_injection_common.sh"
require "failure log dump" "host binary logs (failure)"
require "--no-compose flag" "--no-compose"
require "--keep flag" "--keep"

# Isolation: a Compose project name and port triple distinct from every
# sibling verify-ifa-*.sh script and verify-golden-corpus-gate.sh.
require "isolated compose project default" 'FAULT_COMPOSE_PROJECT:=eshu-ifa-fault-injection-$$'
for reserved in \
	'ESHU_POSTGRES_PORT:-15432' 'NEO4J_BOLT_PORT:-7687' 'NEO4J_HTTP_PORT:-7474' \
	'ESHU_POSTGRES_PORT:-15532' 'NEO4J_BOLT_PORT:-7788' 'NEO4J_HTTP_PORT:-7575' \
	'ESHU_POSTGRES_PORT:-15635' 'NEO4J_BOLT_PORT:-7792' 'NEO4J_HTTP_PORT:-7679' \
	'ESHU_POSTGRES_PORT:-15636' 'NEO4J_BOLT_PORT:-7793' 'NEO4J_HTTP_PORT:-7680' \
	'ESHU_POSTGRES_PORT:-15637' 'NEO4J_BOLT_PORT:-7794' 'NEO4J_HTTP_PORT:-7681'; do
	if rg --fixed-strings --quiet -- "${reserved}" "${script}"; then
		fail "must not reuse a sibling verify-ifa-*.sh / verify-golden-corpus-gate.sh default port: ${reserved}"
	fi
done
require "exported Postgres port override" 'export ESHU_POSTGRES_PORT='
require "exported Neo4j bolt port override" 'export NEO4J_BOLT_PORT='
require "exported Neo4j http port override" 'export NEO4J_HTTP_PORT='

# Both cassettes, generated synth-multiscope once, and the drive verb.
require "demo-org cassette" "testdata/cassettes/gcpcloud/supply-chain-demo.json"
require "synth-cassette verb invocation" '"${bin_dir}/eshu-ifa" synth-cassette'
require "drive verb invocation" 'eshu-ifa" drive -cassette'
require "vacuous-drive guard" "vacuous drain proof"

# SQL relationship family cassette (#5351): driven into every cell so cells 2/3
# (lease-expiry / kill-worker) exercise the SQL relationship materialization
# handler's replay through the real durable fault path, plus a baseline
# absolute-set assertion (`ifa assert-edges`) proving the fault-free graph
# carries all nine SQL edges before the recovery cells compare against it.
# Backs the materialized_edges:sql_relationships manifest row's proof_gate:
# ifa-fault-injection claim.
require "SQL cassette path" "testdata/cassettes/sqlrelationships/ifa-sql-family.json"
require "SQL expected-edge set path" "go/internal/ifa/testdata/sqlrelationships/ifa-sql-family-expected-edges.json"
require "SQL cassette existence guard" 'SQL cassette not found'
require "SQL expected-edge set existence guard" 'SQL expected-edge set not found'
require "SQL cassette driven into every cell" 'eshu-ifa" drive -cassette "${sql_cassette}" -workers "${drive_workers}"'
require "drive helper covers all three cassettes" "drive_all_cassettes"
require "assert-edges verb invocation on baseline" '"${bin_dir}/eshu-ifa" assert-edges'
require "assert-edges domain flag" "-domain sql_relationships"
require "assert-edges expected flag" '-expected "${sql_expected_edges}"'
require "assert-edges non-vacuity framing" "non-vacuity"

# Untagged binaries plus a SEPARATE tagged reducer build for cells 4-5.
require "untagged reducer build" "ifa_det_build_bin \"\${bin_dir}\" reducer"
require "tagged reducer build" "ifa_det_build_bin \"\${tagged_bin_dir}\" reducer \"ifafaultinjection\""
require "gate binary" "eshu-golden-corpus-gate"
require "drains phase" "-phase=drains"
require "snapshot contract" "testdata/golden/e2e-20repo-snapshot.json"

# Drain-must-be-polled-not-slept, mirroring the determinism gate's own check.
if rg --quiet --pcre2 'sleep\s+\$\{?GATE_DRAIN' "${script}"; then
	fail "drain must be polled by the gate, not slept"
fi

# The five-cell shape: baseline plus the four cells with a live seam.
for cell in baseline killworker expirelease failgraphwrite restartbackend; do
	require "cell present: ${cell}" "${cell}"
done
require "fail-terminal explicitly excluded with rationale" "fail-terminal (a sixth possible cell) is deliberately NOT included"

# Cell 2 (kill-worker-after-claim): real kill -9 + a fresh process, not the
# hermetic-only faultreplay kind.
require "claimed-row wait before kill" "ifa_fault_wait_for_claimed"
require "kill -9 the live reducer" "kill -9 \"\${reducer_pid_before}\""
require "fresh reducer process after kill" "reducer-killworker-after"

# Cell 3 (expire-lease-mid-handler): direct SQL forced expiry, no kill.
require "forced lease expiry SQL" "UPDATE fact_work_items SET claim_until = now()"
require "expire-lease targets claimed/running" "status IN ('claimed', 'running');\""

# Cell 4 (fail-graph-write-once-then-succeed): queue-retry lane, CloudResource
# MERGE anchor, ESHU_IFA_FAULT_SCRIPT wiring, and a durable non-vacuity retry
# check (Postgres attempt_count, not the reducer log — see the helper doc for
# why the log grep raced the buffered-stderr flush in CI).
require "once-then-succeed script writer" "ifa_fault_write_once_script"
require "CloudResource MERGE operation_match anchor" 'cloud_resource_operation_match="MERGE (r:CloudResource"'
require "queue-retry lane selected" '"queue-retry"'
require "ESHU_IFA_FAULT_SCRIPT env wiring" "ESHU_IFA_FAULT_SCRIPT=\${fault_once_script}"
require "non-vacuity retry check for cell 4 (baseline differential)" "ifa_fault_assert_retried_above"
require "fault-free baseline retry snapshot in cell 1" "baseline_retried="
require_lib "durable retry-signal query" "SELECT count(*) FROM fact_work_items WHERE stage = 'reducer' AND status = 'succeeded' AND attempt_count > 1"
require_lib "baseline-differential assert helper" "ifa_fault_assert_retried_above"
require_lib "once-script JSON kind" "fail-graph-write-once-then-succeed"

# Cell 5 (restart-backend-between-phase-groups): sentinel-driven backend
# restart, --no-compose skip, and a non-vacuity fired check.
require "restart script writer" "ifa_fault_write_restart_script"
require "sentinel suffix matches Go wiring" '.restart-sentinel"'
require "sentinel watcher invocation" "ifa_fault_watch_restart_sentinel"
require "no-compose skips cell 5" "SKIPPED (--no-compose cannot restart a backend it does not own)"
require "non-vacuity fired check for cell 5" '"${restart_fired}" == "fired"'
require_lib "restart script JSON kind" "restart-backend-between-phase-groups"
require_lib "nornicdb restart command" "docker compose -p \"\${compose_project}\" -f \"\${compose_file}\" restart nornicdb"

# The unchanged Layer 4 acceptance: digest equality against baseline plus a
# hard failure (never a retry) on divergence.
require "baseline digest capture" "digests[baseline]"
require "digest comparison helper" "assert_matches_baseline"
require "mismatch framing" "MISMATCH:"
require "full-bytes diff on divergence" "diff -u"
require "no-normalize-away directive" "do NOT retry, lower workers, or otherwise normalize this away"
require "dead-letter zero assertion" "assert_no_dead_letters"
require_lib "dead-letter count query" "SELECT count(*) FROM fact_work_items WHERE status = 'dead_letter';"

# Per-cell wall time is reported.
require "per-cell wall time capture" "cell_start"
require "wall time in summary" "wall=%ss"

# The lib functions this script depends on all exist with the expected shape.
require_lib "once-script function signature" 'ifa_fault_write_once_script() {'
require_lib "restart-script function signature" 'ifa_fault_write_restart_script() {'
require_lib "claimed-wait function signature" 'ifa_fault_wait_for_claimed() {'
require_lib "claimed-wait uses one server-side polling connection" 'pg_temp.ifa_wait_for_claimed'
require_lib "claimed-wait validates the SQL budget" 'budget must be a positive integer'
require_lib "sentinel-watch function signature" 'ifa_fault_watch_restart_sentinel() {'
require_lib "dead-letter-count function signature" 'ifa_fault_dead_letter_count() {'

# The tagged-build-only fault decorator files this gate is the first live
# integration test of must actually exist where the design says they do.
fault_executor="${repo_root}/go/internal/storage/cypher/fault_executor.go"
fault_executor_off="${repo_root}/go/internal/storage/cypher/fault_executor_off.go"
reducer_wiring="${repo_root}/go/cmd/reducer/ifa_fault_wiring.go"
reducer_wiring_off="${repo_root}/go/cmd/reducer/ifa_fault_wiring_off.go"
for f in "${fault_executor}" "${fault_executor_off}" "${reducer_wiring}" "${reducer_wiring_off}"; do
	[[ -f "${f}" ]] || fail "missing ifafaultinjection build-tag file: ${f}"
done
rg --fixed-strings --quiet -- '//go:build ifafaultinjection' "${fault_executor}" \
	|| fail "${fault_executor} must carry the ifafaultinjection build tag"
rg --fixed-strings --quiet -- '//go:build !ifafaultinjection' "${fault_executor_off}" \
	|| fail "${fault_executor_off} must carry the !ifafaultinjection build tag"
rg --fixed-strings --quiet -- 'ESHU_IFA_FAULT_SCRIPT' "${reducer_wiring}" \
	|| fail "${reducer_wiring} must read ESHU_IFA_FAULT_SCRIPT"

# No private data: hostnames, IPs, cloud account IDs, keys, internal paths.
private_pattern='ghp_|github_pat_|glpat-|AKIA|ASIA|xox[baprs]-|arn:aws:|(^|[^0-9])[0-9]{12}([^0-9]|$)|/Users/|/home/[a-z]'
if rg --pcre2 --quiet -- "${private_pattern}" "${script}"; then
	fail "verify-ifa-fault-injection.sh looks like it contains private data"
fi
if rg --pcre2 --quiet -- "${private_pattern}" "${fault_lib}"; then
	fail "ifa_fault_injection_common.sh looks like it contains private data"
fi

# The wait budget is interpolated into the server-side function call. Reject a
# malformed environment override before it can reach psql.
# shellcheck source=scripts/lib/ifa_fault_injection_common.sh
source "${fault_lib}"
ifa_det_pg() { printf '1\n'; }
if ifa_fault_wait_for_claimed test-project 1 test-dsn test-compose.yml '1; SELECT 1'; then
	fail "claimed-wait accepted a non-integer SQL budget"
fi

printf 'test-verify-ifa-fault-injection: pass\n'
