#!/usr/bin/env bash
# Static structural test for verify-ifa-fault-injection.sh (issue #4580 P6
# slice S5, extended by issue #5555's SQL-targeted cells 6-7). The gate
# itself needs Docker + a built toolchain and takes significantly longer
# than the sibling determinism matrix (seven fresh Postgres + NornicDB
# stacks, two of them building AND running a -tags ifafaultinjection
# reducer), so this mirror validates the contract that cannot silently
# drift: strict mode and the bash>=4.4 guard, an isolated Compose project and
# port triple distinct from every sibling verify-ifa-*.sh script, the
# seven-cell shape (baseline + the six live cells; fail-terminal
# deliberately absent with its rationale documented), each cell's own
# recovery mechanism, the digest/dead_letter/non-vacuity assertions, the
# tagged-reducer + fault-script wiring this gate is the first thing to
# exercise live, and (since #5555) that the two SQL-targeted cells provably
# target sql_relationship_materialization / sql_relationships rather than
# whichever domain the driven cassettes happen to schedule first. The driver
# script itself was split into scripts/lib/ifa_fault_injection_driver.sh
# (shared per-cell plumbing), scripts/lib/ifa_fault_injection_cells.sh (the
# five original cells), and scripts/lib/ifa_fault_injection_sql_cells.sh (the
# two SQL-targeted cells) to stay under the repo's 500-line cap; checks below
# point at whichever file now actually holds the content.
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
script="${repo_root}/scripts/verify-ifa-fault-injection.sh"
det_lib="${repo_root}/scripts/lib/ifa_determinism_common.sh"
fault_lib="${repo_root}/scripts/lib/ifa_fault_injection_common.sh"
driver_lib="${repo_root}/scripts/lib/ifa_fault_injection_driver.sh"
cells_lib="${repo_root}/scripts/lib/ifa_fault_injection_cells.sh"
sql_cells_lib="${repo_root}/scripts/lib/ifa_fault_injection_sql_cells.sh"
delivery_cells_lib="${repo_root}/scripts/lib/ifa_fault_injection_delivery_cells.sh"

fail() { printf 'test-verify-ifa-fault-injection: %s\n' "$*" >&2; exit 1; }

for f in "${script}" "${fault_lib}" "${det_lib}" "${driver_lib}" "${cells_lib}" "${sql_cells_lib}" "${delivery_cells_lib}"; do
	[[ -f "${f}" ]] || fail "missing ${f}"
done
[[ -x "${script}" ]] || fail "verify-ifa-fault-injection.sh must be executable"

bash -n "${script}" || fail "verify-ifa-fault-injection.sh has a syntax error"
bash -n "${fault_lib}" || fail "ifa_fault_injection_common.sh has a syntax error"
bash -n "${driver_lib}" || fail "ifa_fault_injection_driver.sh has a syntax error"
bash -n "${cells_lib}" || fail "ifa_fault_injection_cells.sh has a syntax error"
bash -n "${sql_cells_lib}" || fail "ifa_fault_injection_sql_cells.sh has a syntax error"
bash -n "${delivery_cells_lib}" || fail "ifa_fault_injection_delivery_cells.sh has a syntax error"

require() {
	local label="$1" needle="$2"
	rg --fixed-strings --quiet -- "${needle}" "${script}" || fail "missing ${label}: ${needle}"
}
require_lib() {
	local label="$1" needle="$2"
	rg --fixed-strings --quiet -- "${needle}" "${fault_lib}" || fail "missing ${label} (lib): ${needle}"
}
require_driver() {
	local label="$1" needle="$2"
	rg --fixed-strings --quiet -- "${needle}" "${driver_lib}" || fail "missing ${label} (driver lib): ${needle}"
}
require_cells() {
	local label="$1" needle="$2"
	rg --fixed-strings --quiet -- "${needle}" "${cells_lib}" || fail "missing ${label} (cells lib): ${needle}"
}
require_sql_cells() {
	local label="$1" needle="$2"
	rg --fixed-strings --quiet -- "${needle}" "${sql_cells_lib}" || fail "missing ${label} (sql cells lib): ${needle}"
}
require_delivery_cells() {
	local label="$1" needle="$2"
	rg --fixed-strings --quiet -- "${needle}" "${delivery_cells_lib}" || fail "missing ${label} (delivery cells lib): ${needle}"
}
# require_delivery_cells_multiline binds a needle that spans a line break.
# Deleting just the function name from a continued call leaves its argument
# lines behind, so an argument-only needle stays green -- proven by seeding
# exactly that deletion. -U makes the match span the continuation.
require_delivery_cells_multiline() {
	local label="$1" needle="$2"
	rg -U --fixed-strings --quiet -- "${needle}" "${delivery_cells_lib}" || fail "missing ${label} (delivery cells lib, multiline): ${needle}"
}

# Strict mode, self-cleanup, and the masking-safe bash>=4.4 guard.
require "strict mode" "set -euo pipefail"
require "exit trap" "trap cleanup EXIT"
require "bash>=4.4 guard (masking-safe)" "requires bash >= 4.4"
require "sources determinism lib" "scripts/lib/ifa_determinism_common.sh"
require "sources fault-injection lib" "scripts/lib/ifa_fault_injection_common.sh"
require "sources driver lib" "scripts/lib/ifa_fault_injection_driver.sh"
require "sources cells lib" "scripts/lib/ifa_fault_injection_cells.sh"
require "sources sql cells lib" "scripts/lib/ifa_fault_injection_sql_cells.sh"
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

# Both GCP cassettes, generated synth-multiscope once, and the drive verb
# (now in the driver lib's drive_all_cassettes helper).
require "demo-org cassette" "testdata/cassettes/gcpcloud/supply-chain-demo.json"
require "synth-cassette verb invocation" '"${bin_dir}/eshu-ifa" synth-cassette'
require_driver "drive verb invocation" 'eshu-ifa" drive -cassette'
require_driver "vacuous-drive guard" "vacuous drain proof"

# SQL relationship family cassette (#5351): driven into every cell so cells
# 2/3/6 (lease-expiry / kill-worker, including the SQL-targeted kill-worker)
# exercise the SQL relationship materialization handler's replay through the
# real durable fault path, plus a baseline absolute-set assertion
# (`ifa assert-edges`) proving the fault-free graph carries all nine SQL
# edges before the recovery cells compare against it. Backs the
# materialized_edges:sql_relationships manifest row's proof_gate:
# ifa-fault-injection claim.
require "SQL cassette path" "testdata/cassettes/sqlrelationships/ifa-sql-family.json"
require "SQL expected-edge set path" "go/internal/ifa/testdata/sqlrelationships/ifa-sql-family-expected-edges.json"
require "SQL cassette existence guard" 'SQL cassette not found'
require "SQL expected-edge set existence guard" 'SQL expected-edge set not found'
require_driver "SQL cassette driven into every cell" 'eshu-ifa" drive -cassette "${sql_cassette}" -workers "${drive_workers}"'
require_driver "drive helper defined" "drive_all_cassettes() {"
require_cells "assert-edges verb invocation on baseline" '"${bin_dir}/eshu-ifa" assert-edges'
require_cells "assert-edges domain flag" "-domain sql_relationships"
require_cells "assert-edges expected flag" '-expected "${sql_expected_edges}"'
require_cells "assert-edges non-vacuity framing" "non-vacuity"

# Untagged binaries plus a SEPARATE tagged reducer build for the queue-retry /
# restart cells (4, 5, 7).
require "untagged reducer build" "ifa_det_build_bin \"\${bin_dir}\" reducer"
require "tagged reducer build" "ifa_det_build_bin \"\${tagged_bin_dir}\" reducer \"ifafaultinjection\""
require "gate binary build" "ifa_det_build_bin \"\${bin_dir}\" golden-corpus-gate"
require_driver "gate binary invocation" "eshu-golden-corpus-gate"
require_driver "drains phase" "-phase=drains"
require_driver "snapshot contract" "testdata/golden/e2e-20repo-snapshot.json"

# Drain-must-be-polled-not-slept, mirroring the determinism gate's own check.
if rg --quiet --pcre2 'sleep\s+\$\{?GATE_DRAIN' "${driver_lib}"; then
	fail "drain must be polled by the gate, not slept"
fi

# The nine-cell shape: baseline plus eight cells with a live seam -- four
# original recovery cells, two SQL-targeted (#5555), two delivery-shaped
# (#5544). Eight of the nine run by default; cell_failgraphwrite_sql is
# defined but held out until #5974 proves it fires in CI.
for cell in baseline killworker expirelease failgraphwrite restartbackend; do
	require "cell present: ${cell}" "cell_${cell}"
done
# Anchored to the invocation line, not the bare name: the verifier's
# source-helper comments mention cell_deltaretract, so a bare-name needle stays
# green after the call is deleted. rg without --fixed-strings so ^...$ binds.
# cell_failgraphwrite_sql remains held out: the marker proved the fault does not
# fire in CI at all, which the old log poll could not distinguish from a late
# log line. The other three run.
for cell in cell_killworker_sql cell_duplicatedelivery cell_deltaretract; do
	rg --quiet -- "^${cell}\$" "${script}" || fail "verifier does not INVOKE ${cell} on its own line"
done
# #5974 probes. A missing marker had two explanations and the gate had to guess
# between them, which is how this issue stayed open on a wrong root cause. These
# three close the gap: the stack is provably fresh, the edge provably exists,
# and a failed marker write is no longer silent.
require_driver "fresh_stack fails loudly when teardown fails" "the stack is NOT fresh"
require_driver "fresh_stack captures teardown output instead of discarding it" 'compose-down-${cell}.log'
require_sql_cells "probe 1: fresh-stack intent precondition" "survived fresh_stack"
require_sql_cells "probe 2: SQL edges asserted after the drain" "the QUERIES_TABLE MERGE never ran in this cell"
require_sql_cells "probe 2: this cell's intent window is reported" "projection_domain = 'sql_relationships'"
require_sql_cells "die message names the marker-write-failure alternative" "once-fired marker write failed"
# The old message asserted a single cause. It must not come back.
if rg --fixed-strings --quiet -- "the scripted fault never fired -- no once-fired marker" "${sql_cells_lib}"; then
	fail "the SQL cell's die message asserts the fault never fired; a missing marker also means the marker write failed (#5974)"
fi

require "failgraphwrite_sql enabled for the #5974 probe experiment" "runs HERE ON PURPOSE, for one experiment"
# The library must DEFINE both cells. The needles below check implementation
# details that could still match if the function wrapper were renamed away.
require_delivery_cells "delivery lib defines cell_duplicatedelivery" "cell_duplicatedelivery() {"
require_delivery_cells "delivery lib defines cell_deltaretract" "cell_deltaretract() {"

# Cell 8 (duplicate-delivery, #5544) -- numbered as the gate header numbers
# it, not by #5544's own 'cell 6' wording, which collides with #5555's cells.
# The redelivery must actually reset rows.
# Without the >0 assertion the second drain is a no-op and every downstream
# digest comparison passes vacuously -- the inert-gate defect #5974 records.
require_delivery_cells "duplicate-delivery redelivers via the shared helper" "ifa_fault_redeliver_succeeded"
require_delivery_cells "duplicate-delivery asserts the redelivery was non-vacuous" '[[ -n "${reset_count}" && "${reset_count}" -gt 0 ]]'
require_delivery_cells "duplicate-delivery drains a second time after redelivery" "run_drain_gate duplicatedelivery"
require_delivery_cells "duplicate-delivery proves idempotency against the baseline" "assert_matches_baseline duplicatedelivery"
require_lib "redelivery clears the lease, not only the status" "lease_owner = NULL"
require_lib "redelivery makes the row visible again" "visible_at = now()"
require_lib "redelivery counts what it actually wrote (CTE, not a second SELECT)" "SELECT count(*) FROM redelivered;"

# Cell 9 (delta-retract, #5544; #5544 calls it 'cell 7'): shares the
# determinism gate's helper so the
# two gates cannot drift on what a correctly-landed delta means, and asserts
# generation 1 landed BEFORE driving generation 2 -- otherwise "the retract
# removed it" and "it never arrived" look identical.
# Match the CALL, not the bare helper name: this file's own comment names
# ifa_det_run_sql_delta_live, so a bare-name needle stays green when the
# invocation is deleted. Proven by seeding exactly that deletion -- the
# bare-name form passed, this argument-shaped form fails.
require_delivery_cells_multiline "delta-retract drives gen 2 through the shared helper" $'ifa_det_run_sql_delta_live \\\n\t\t1 "${bin_dir}" "${sql_delta_cassette}"'

require_delivery_cells "delta-retract proves it changed no non-SQL edges" "changed edges outside the nine SQL relationship types"
require_delivery_cells "delta-retract compares structurally, not by line" "objects | select(has("
require_delivery_cells "delta-retract asserts generation 1 landed first" "generation-1 SQL edge set did not match before the delta was driven"
require "gate sources the shared delta-live helper" "scripts/lib/ifa_sql_delta_live.sh"
require "gate defines the delta expected-edge set" "sql_delta_expected_edges="
# Cell 7 CHANGES the graph on purpose (gen 2 adds and retracts edges), so a
# baseline-digest comparison would fail correctly and invite the wrong fix.
# Its exactness assertion is the expected-v2 set, which names the edges.
if rg --fixed-strings --quiet -- "assert_matches_baseline deltaretract" "${delivery_cells_lib}"; then
	fail "cell_deltaretract must NOT compare to the baseline digest: generation 2 intentionally changes the graph, so its proof is the expected-v2 edge set, not digest equality"
fi

require "fail-terminal explicitly excluded with rationale" "fail-terminal (a tenth possible cell) is deliberately NOT included"

# Cell 2 / cell 6 (kill-worker-after-claim[-sql]): real kill -9 + a fresh
# process, not the hermetic-only faultreplay kind.
require_cells "claimed-row wait before kill" "ifa_fault_wait_for_claimed"
require_cells "kill -9 the live reducer" "kill -9 \"\${reducer_pid_before}\""
require_cells "fresh reducer process after kill" "reducer-killworker-after"
require_sql_cells "SQL-targeted claimed-row wait before kill" "ifa_fault_wait_for_claimed"
require_sql_cells "SQL-targeted claimed-wait scoped to the SQL domain, not any item" '"sql_relationship_materialization")"'
require_sql_cells "SQL-targeted kill -9 the live reducer" "kill -9 \"\${reducer_pid_before}\""

# Cell 3 (expire-lease-mid-handler): direct SQL forced expiry, no kill.
require_cells "forced lease expiry SQL" "UPDATE fact_work_items SET claim_until = now()"
require_cells "expire-lease targets claimed/running" "status IN ('claimed', 'running');\""

# Cell 4 (fail-graph-write-once-then-succeed): queue-retry lane, CloudResource
# MERGE anchor, ESHU_IFA_FAULT_SCRIPT wiring, and a durable non-vacuity retry
# check (Postgres attempt_count, not the reducer log -- see the helper doc for
# why the log grep raced the buffered-stderr flush in CI).
require_cells "once-then-succeed script writer" "ifa_fault_write_once_script"
require "CloudResource MERGE operation_match anchor" 'cloud_resource_operation_match="MERGE (r:CloudResource"'
require_cells "queue-retry lane selected" '"queue-retry"'
require_cells "ESHU_IFA_FAULT_SCRIPT env wiring" "ESHU_IFA_FAULT_SCRIPT=\${fault_once_script}"
require_cells "non-vacuity retry check for cell 4 (baseline differential)" "ifa_fault_assert_retried_above"
require_cells "fault-free baseline retry snapshot in cell 1" "baseline_retried="
require_lib "durable retry-signal query" "SELECT count(*) FROM fact_work_items WHERE stage = 'reducer' AND status = 'succeeded' AND attempt_count > 1"
require_lib "baseline-differential assert helper" "ifa_fault_assert_retried_above"
require_lib "once-script JSON kind" "fail-graph-write-once-then-succeed"

# Cell 5 (restart-backend-between-phase-groups): sentinel-driven backend
# restart, --no-compose skip, and a non-vacuity fired check.
require_cells "restart script writer" "ifa_fault_write_restart_script"
require_cells "sentinel suffix matches Go wiring" '.restart-sentinel"'
require_cells "sentinel watcher invocation" "ifa_fault_watch_restart_sentinel"
require_cells "no-compose skips cell 5" "SKIPPED (--no-compose cannot restart a backend it does not own)"
require_cells "non-vacuity fired check for cell 5" '"${restart_fired}" == "fired"'
require_lib "restart script JSON kind" "restart-backend-between-phase-groups"
require_lib "nornicdb restart command" "docker compose -p \"\${compose_project}\" -f \"\${compose_file}\" restart nornicdb"

# Cell 6 (kill-worker-after-claim-sql, #5555): see the Cell 2 checks above --
# require_sql_cells asserts the SQL-scoped variant exists distinctly.

# Cell 7 (fail-graph-write-once-then-succeed-sql, #5555/#5974): SQL edge MERGE
# anchor (not CloudResource), queue-retry lane, and a fired-fault proof read
# from the marker the fault decorator writes at injection time. It does NOT
# read the reducer log: fact_work_items attempt_count does not exist for this
# domain's async graph writes, and the log route raced the logger's flush,
# which is what made this cell inert in CI while passing locally (#5974).
require "SQL edge MERGE operation_match anchor" 'sql_edge_operation_match="MERGE (source)-[rel:QUERIES_TABLE]->(target)"'
if rg --pcre2 --quiet -- 'sql_edge_operation_match="[^"]*CloudResource' "${script}"; then
	fail "sql_edge_operation_match must not be anchored to CloudResource -- that is issue #5555's exact complaint"
fi
require_sql_cells "SQL-targeted once-then-succeed script writer" "ifa_fault_write_once_script"
require_sql_cells "SQL-targeted queue-retry lane selected" '"queue-retry"'
require_sql_cells "SQL-targeted ESHU_IFA_FAULT_SCRIPT env wiring" "ESHU_IFA_FAULT_SCRIPT=\${fault_once_script_sql}"
require_sql_cells "SQL fired-fault proof reads the durable marker" "ifa_fault_assert_once_fault_marker"
require_sql_cells "SQL fired-fault proof non-vacuity framing" "non-vacuity"
require_lib "once-fired marker function signature" 'ifa_fault_assert_once_fault_marker() {'
require_lib "marker assertion names the retired log route and why" "races the logger's flush"
# The log-poll helper is retired, not merely unused: leaving a mechanism that
# is known to go inert in CI invites the next cell to reach for it.
if rg --fixed-strings --quiet -- "ifa_fault_assert_sql_graph_write_fired" "${fault_lib}" "${sql_cells_lib}" "${script}"; then
	fail "the retired log-poll fired-fault helper is still referenced; #5974 replaced it with ifa_fault_assert_once_fault_marker"
fi
require_lib "count_retried generalized with a domain arg" 'domain="${5:-gcp_resource_materialization}"'
require_lib "assert_retried_above generalized with a domain arg" 'domain="${7:-gcp_resource_materialization}"'
require_lib "wait_for_claimed generalized with a domain arg" 'domain="${6:-}"'

# The unchanged Layer 4 acceptance: digest equality against baseline plus a
# hard failure (never a retry) on divergence.
require_driver "baseline digest capture" "digests[baseline]"
require_driver "digest comparison helper" "assert_matches_baseline"
require_driver "mismatch framing" "MISMATCH:"
require_driver "full-bytes diff on divergence" "diff -u"
require_driver "no-normalize-away directive" "do NOT retry, lower workers, or otherwise normalize this away"
require_driver "dead-letter zero assertion" "assert_no_dead_letters() {"
require_lib "dead-letter count query" "SELECT count(*) FROM fact_work_items WHERE status = 'dead_letter';"

# Per-cell wall time is captured by every cell and reported in the driver's
# final summary.
require_cells "per-cell wall time capture" "cell_start"
require_sql_cells "per-cell wall time capture (sql cells)" "cell_start"
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
for f in "${script}" "${fault_lib}" "${driver_lib}" "${cells_lib}" "${sql_cells_lib}" "${delivery_cells_lib}"; do
	if rg --pcre2 --quiet -- "${private_pattern}" "${f}"; then
		fail "$(basename "${f}") looks like it contains private data"
	fi
done

# The wait budget is interpolated into the server-side function call. Reject a
# malformed environment override before it can reach psql.
# shellcheck source=scripts/lib/ifa_fault_injection_common.sh
source "${fault_lib}"
ifa_det_pg() { printf '1\n'; }
if ifa_fault_wait_for_claimed test-project 1 test-dsn test-compose.yml '1; SELECT 1'; then
	fail "claimed-wait accepted a non-integer SQL budget"
fi

# Domain-scoping (#5555) actually changes the SQL: stub ifa_det_pg to capture
# the query text it was asked to run, and assert the domain clause is
# present only when a domain argument is passed. ifa_fault_wait_for_claimed
# invokes ifa_det_pg inside a `$( ... )` command substitution, which runs in
# a SUBSHELL -- a plain variable assignment inside the stub would not survive
# back to this shell, so the stub writes to a file instead.
capture_file="$(mktemp)"
ifa_det_pg() { printf '%s' "$4" >"${capture_file}"; printf '1\n'; }
ifa_fault_wait_for_claimed test-project 1 test-dsn test-compose.yml 5 "sql_relationship_materialization" >/dev/null
rg --fixed-strings --quiet -- "AND domain = 'sql_relationship_materialization'" "${capture_file}" \
	|| fail "claimed-wait did not apply the domain filter when a domain argument was passed"
: >"${capture_file}"
ifa_fault_wait_for_claimed test-project 1 test-dsn test-compose.yml 5 >/dev/null
if rg --fixed-strings --quiet -- "AND domain =" "${capture_file}"; then
	fail "claimed-wait applied a domain filter when no domain argument was passed"
fi
rm -f "${capture_file}"

# ifa_fault_assert_once_fault_marker (#5974) is a real functional check, not a
# string grep. It must tell three cases apart: the fault never fired, it fired
# on the targeted write, and it fired on a DIFFERENT write. The third is the
# one that matters -- a marker alone only proves some fault fired.
marker_dir="$(mktemp -d)"
trap 'rm -rf "${marker_dir}"' EXIT
marker_script="${marker_dir}/fault.json"
sql_anchor="MERGE (source)-[rel:QUERIES_TABLE]->(target)"

# Negative: no marker at all -- the inert-script case this assertion exists for.
if ifa_fault_assert_once_fault_marker "${marker_script}" "${sql_anchor}" 2>/dev/null; then
	fail "once-fired marker check passed with no marker present -- an inert script would report as a pass"
fi

# Positive: marker names the targeted operation.
printf 'lane=queue-retry ordinal=3\noperation=%s SET rel.confidence = 0.95\n' "${sql_anchor}" \
	>"${marker_script}.restart-sentinel.once-fired"
if ! ifa_fault_assert_once_fault_marker "${marker_script}" "${sql_anchor}"; then
	fail "once-fired marker check did not accept a marker naming the targeted SQL edge MERGE"
fi

# Negative: the fault fired, but on a different write. This is the analogue of
# the split-evidence regression #5555 closed -- "a fault fired" is not the same
# claim as "the fault hit the SQL family".
printf 'lane=queue-retry ordinal=7\noperation=MERGE (r:CloudResource {uid: row.uid})\n' \
	>"${marker_script}.restart-sentinel.once-fired"
if ifa_fault_assert_once_fault_marker "${marker_script}" "${sql_anchor}" 2>/dev/null; then
	fail "once-fired marker check passed on a fault that fired against CloudResource -- exactly the wrong-target defect #5555 exists to prevent"
fi

printf 'test-verify-ifa-fault-injection: pass\n'
