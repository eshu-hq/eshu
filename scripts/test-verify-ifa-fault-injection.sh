#!/usr/bin/env bash
# Static structural test for verify-ifa-fault-injection.sh (issue #4580 P6
# slice S5, extended by #5555's SQL-targeted cells and #5991's code-call cells). The gate
# itself needs Docker + a built toolchain and takes significantly longer
# than the sibling determinism matrix (eleven fresh Postgres + NornicDB
# stacks, four of them running a -tags ifafaultinjection
# reducer), so this mirror validates the contract that cannot silently
# drift: strict mode and the bash>=4.4 guard, an isolated Compose project and
# port triple distinct from every sibling verify-ifa-*.sh script, the
# eleven-cell shape (baseline + ten live cells; fail-terminal
# deliberately absent with its rationale documented), each cell's own
# recovery mechanism, the digest/dead_letter/non-vacuity assertions, the
# tagged-reducer + fault-script wiring this gate is the first thing to
# exercise live, and (since #5555) that the two SQL-targeted cells provably
# target sql_relationship_materialization / sql_relationships rather than
# whichever domain the driven cassettes happen to schedule first. The driver
# script itself was split into scripts/lib/ifa_fault_injection_driver.sh
# (shared per-cell plumbing), scripts/lib/ifa_fault_injection_cells.sh (the
# five original cells), scripts/lib/ifa_fault_injection_sql_cells.sh (two
# SQL-targeted cells), scripts/lib/ifa_fault_injection_code_call_cells.sh (two
# code-call cells), scripts/lib/ifa_fault_injection_delivery_cells.sh, and its
# full-node collateral helper ifa_fault_injection_collateral_nodes.sh to stay
# under the repo's 500-line cap; checks below point at whichever file now holds
# the content.
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
script="${repo_root}/scripts/verify-ifa-fault-injection.sh"
det_lib="${repo_root}/scripts/lib/ifa_determinism_common.sh"
fault_lib="${repo_root}/scripts/lib/ifa_fault_injection_common.sh"
driver_lib="${repo_root}/scripts/lib/ifa_fault_injection_driver.sh"
cells_lib="${repo_root}/scripts/lib/ifa_fault_injection_cells.sh"
sql_cells_lib="${repo_root}/scripts/lib/ifa_fault_injection_sql_cells.sh"
delivery_cells_lib="${repo_root}/scripts/lib/ifa_fault_injection_delivery_cells.sh"
collateral_nodes_lib="${repo_root}/scripts/lib/ifa_fault_injection_collateral_nodes.sh"
code_call_lib="${repo_root}/scripts/lib/ifa_code_call_live.sh"
code_call_cells_lib="${repo_root}/scripts/lib/ifa_fault_injection_code_call_cells.sh"
documentation_lib="${repo_root}/scripts/lib/ifa_documentation_live.sh"
documentation_cells_lib="${repo_root}/scripts/lib/ifa_fault_injection_documentation_cells.sh"
documentation_cases_lib="${repo_root}/scripts/lib/test-ifa-fault-injection-documentation-cases.sh"
deployable_unit_live_lib="${repo_root}/scripts/lib/ifa_deployable_unit_live.sh"
deployable_unit_diagnostics_lib="${repo_root}/scripts/lib/ifa_deployable_unit_live_diagnostics.sh"
deployable_unit_converge_lib="${repo_root}/scripts/lib/ifa_deployable_unit_live_converge.sh"
deployable_unit_lock_lib="${repo_root}/scripts/lib/ifa_fault_injection_deployable_unit_lock.sh"
deployable_unit_cells_lib="${repo_root}/scripts/lib/ifa_fault_injection_deployable_unit_cells.sh"
review_cases_lib="${repo_root}/scripts/lib/test-ifa-fault-injection-review-cases.sh"
deployable_unit_cases_lib="${repo_root}/scripts/lib/test-ifa-fault-injection-deployable-unit-cases.sh"
assertions_lib="${repo_root}/scripts/lib/test-ifa-fault-injection-assertions.sh"

fail() { printf 'test-verify-ifa-fault-injection: %s\n' "$*" >&2; exit 1; }

for f in "${script}" "${fault_lib}" "${det_lib}" "${driver_lib}" "${cells_lib}" "${sql_cells_lib}" "${delivery_cells_lib}" "${collateral_nodes_lib}" "${code_call_lib}" "${code_call_cells_lib}" "${documentation_lib}" "${documentation_cells_lib}" "${documentation_cases_lib}" "${review_cases_lib}" "${deployable_unit_cases_lib}" "${assertions_lib}" "${deployable_unit_live_lib}" "${deployable_unit_diagnostics_lib}" "${deployable_unit_converge_lib}" "${deployable_unit_lock_lib}" "${deployable_unit_cells_lib}"; do
	[[ -f "${f}" ]] || fail "missing ${f}"
done
[[ -x "${script}" ]] || fail "verify-ifa-fault-injection.sh must be executable"

bash -n "${script}" || fail "verify-ifa-fault-injection.sh has a syntax error"
bash -n "${fault_lib}" || fail "ifa_fault_injection_common.sh has a syntax error"
bash -n "${driver_lib}" || fail "ifa_fault_injection_driver.sh has a syntax error"
bash -n "${cells_lib}" || fail "ifa_fault_injection_cells.sh has a syntax error"
bash -n "${sql_cells_lib}" || fail "ifa_fault_injection_sql_cells.sh has a syntax error"
bash -n "${delivery_cells_lib}" || fail "ifa_fault_injection_delivery_cells.sh has a syntax error"
bash -n "${collateral_nodes_lib}" || fail "ifa_fault_injection_collateral_nodes.sh has a syntax error"
bash -n "${code_call_lib}" || fail "ifa_code_call_live.sh has a syntax error"
bash -n "${code_call_cells_lib}" || fail "ifa_fault_injection_code_call_cells.sh has a syntax error"
bash -n "${documentation_lib}" || fail "ifa_documentation_live.sh has a syntax error"
bash -n "${documentation_cells_lib}" || fail "ifa_fault_injection_documentation_cells.sh has a syntax error"
bash -n "${documentation_cases_lib}" || fail "test-ifa-fault-injection-documentation-cases.sh has a syntax error"
bash -n "${deployable_unit_live_lib}" || fail "ifa_deployable_unit_live.sh has a syntax error"
bash -n "${deployable_unit_diagnostics_lib}" || fail "ifa_deployable_unit_live_diagnostics.sh has a syntax error"
bash -n "${deployable_unit_converge_lib}" || fail "ifa_deployable_unit_live_converge.sh has a syntax error"
bash -n "${deployable_unit_lock_lib}" || fail "ifa_fault_injection_deployable_unit_lock.sh has a syntax error"
bash -n "${deployable_unit_cells_lib}" || fail "ifa_fault_injection_deployable_unit_cells.sh has a syntax error"
bash -n "${review_cases_lib}" || fail "test-ifa-fault-injection-review-cases.sh has a syntax error"
bash -n "${deployable_unit_cases_lib}" || fail "test-ifa-fault-injection-deployable-unit-cases.sh has a syntax error"
bash -n "${assertions_lib}" || fail "test-ifa-fault-injection-assertions.sh has a syntax error"
[[ "$(wc -l <"${BASH_SOURCE[0]}" | tr -d '[:space:]')" -lt 500 ]] \
	|| fail "test-verify-ifa-fault-injection.sh must stay under 500 lines"

# shellcheck source=scripts/lib/test-ifa-fault-injection-assertions.sh
source "${assertions_lib}"

# Strict mode, self-cleanup, and the masking-safe bash>=4.4 guard.
require "strict mode" "set -euo pipefail"
require "exit trap" "trap cleanup EXIT"
require "bash>=4.4 guard (masking-safe)" "requires bash >= 4.4"
require "sources determinism lib" "scripts/lib/ifa_determinism_common.sh"
require "sources fault-injection lib" "scripts/lib/ifa_fault_injection_common.sh"
require "sources driver lib" "scripts/lib/ifa_fault_injection_driver.sh"
require "sources cells lib" "scripts/lib/ifa_fault_injection_cells.sh"
require "sources sql cells lib" "scripts/lib/ifa_fault_injection_sql_cells.sh"
require "sources code-call live lib" "scripts/lib/ifa_code_call_live.sh"
require "sources code-call cells lib" "scripts/lib/ifa_fault_injection_code_call_cells.sh"
require "sources collateral-node lib" "scripts/lib/ifa_fault_injection_collateral_nodes.sh"
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
# graph-fault and restart cells.
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

# The eleven-cell shape: baseline plus ten cells with a live seam -- four
# original recovery cells, two SQL-targeted (#5555), two delivery-shaped
# (#5544), and two code-call-targeted (#5991). All eleven run by default.
for cell in baseline killworker expirelease failgraphwrite restartbackend; do
	require "cell present: ${cell}" "cell_${cell}"
done
# Anchored to the invocation line, not the bare name: the verifier's
# source-helper comments mention cell_deltaretract, so a bare-name needle stays
# green after the call is deleted. rg without --fixed-strings so ^...$ binds.
# Pin every split-library invocation to its own line so mentioning a function
# in documentation cannot keep the test green after the call is deleted.
for cell in cell_killworker_sql cell_killworker_code_calls cell_duplicatedelivery cell_deltaretract cell_failgraphwrite_code_calls; do
	rg --quiet -- "^${cell}\$" "${script}" || fail "verifier does not INVOKE ${cell} on its own line"
done
# #5974 probes. A missing marker had two explanations and the gate had to guess
# between them, which is how this issue stayed open on a wrong root cause. These
# three close the gap: the stack is provably fresh, the edge provably exists,
# and a failed marker write is no longer silent.
require_driver "fresh_stack fails loudly when teardown fails" "the stack is NOT fresh"
require_driver "fresh_stack captures teardown output instead of discarding it" 'compose-down-${cell}.log'
require_sql_cells "probe 1: fresh-stack intent precondition" "survived fresh_stack"
require_sql_cells "probe 2: SQL edges asserted after the drain" "assert-edges is set-exact"
require_sql_cells "probe 2: this cell's intent window is reported" "projection_domain = 'sql_relationships'"
require_sql_cells "the write-failure branch uses the single-source prefix variable" "IFA_ONCE_MARKER_WRITE_FAILED_PREFIX"
require_sql_cells "the two marker failure modes are told apart by exit code" 'marker_rc}" -eq 2'
# The old message asserted a single cause. It must not come back.
if rg --fixed-strings --quiet -- "the scripted fault never fired -- no once-fired marker" "${sql_cells_lib}"; then
	fail "the SQL cell's die message asserts the fault never fired; a missing marker also means the marker write failed (#5974)"
fi

# Review round on the probes. Three of the five findings were the same defect
# this cell exists to remove: a message or a check asserting more than it knows.
require_sql_cells "probe 1 distinguishes a failed query from a count" "precondition query FAILED"
require_sql_cells "probe 1 rejects non-numeric output rather than reading it as zero" "treat that as unknown, not as zero"
require_sql_cells "probe 1 skips when --no-compose owns the stack" "fresh-stack precondition SKIPPED"
require_sql_cells "assert-edges failure names both directions" "AND an extra, duplicated, or wrong-typed edge"
require_sql_cells "the failure lists the sentinel family on disk" "sentinel family on disk"
require_sql_cells "a missing marker sends the reader to probe 2 rather than guessing" "Probe 2 above already proved"

# The root cause of #5974, pinned so it cannot come back: the marker assertion
# matched with `rg`, which the fault-injection runner does not have. The
# "command not found" exit read as "the marker does not name the operation", so
# a fault that fired correctly was reported as inert for weeks. A checker that
# cannot run must never be readable as a negative result.
require_lib "the marker assertion matches in bash, not via an external binary" "Bash substring match, NOT an external tool"
if rg --fixed-strings --quiet -- 'rg --quiet' "${fault_lib}"; then
	fail "the marker assertion shells out to rg again; it is absent on the fault-injection runner and its failure is indistinguishable from a negative match (#5974)"
fi
require_sql_cells "the gate greps for the marker-write failure itself" "the marker WRITE FAILED (line above)"
require_lib "marker-write prefix declared once in shell" 'IFA_ONCE_MARKER_WRITE_FAILED_PREFIX="ifa fault: once-fired marker write failed"'

# The shell prefix and the Go constant are greped against each other, so drift
# makes the search silently match nothing -- which reads exactly like "the
# marker write never failed".
go_marker_prefix="$(rg --no-filename -o 'OnceFiredMarkerWriteFailedPrefix = "([^"]+)"' -r '$1' \
	"${repo_root}/go/internal/storage/cypher/fault_executor_marker.go" | head -1)"
shell_marker_prefix="$(rg --no-filename -o 'IFA_ONCE_MARKER_WRITE_FAILED_PREFIX="([^"]+)"' -r '$1' \
	"${fault_lib}" | head -1)"
[[ -n "${go_marker_prefix}" ]] || fail "could not read OnceFiredMarkerWriteFailedPrefix from fault_executor_marker.go"
[[ "${go_marker_prefix}" == "${shell_marker_prefix}" ]] \
	|| fail "marker-write prefix drift: Go has ${go_marker_prefix@Q}, shell has ${shell_marker_prefix@Q} -- the gate's grep would silently find nothing"

# The SQL cell is a permanent matrix member, not a temporary experiment. Pin
# both the invocation and the reason, so re-holding it out is a deliberate edit
# to a stated rule rather than a quiet deletion on a red run (#5974).
rg --quiet --line-regexp -- 'cell_failgraphwrite_sql' "${script}" \
	|| fail "cell_failgraphwrite_sql is no longer invoked in the default matrix; it fires correctly and holding it out again needs a stated reason (#5974)"
require "failgraphwrite_sql is documented as permanent, not an experiment" "permanent member of the matrix as of #5974"
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

require_delivery_cells_multiline "delta-retract reasserts the unaffected code-call family exactly" $'ifa_code_call_assert "deltaretract" "${bin_dir}" "${code_call_expected_edges}"'
require_delivery_cells "delta-retract compares collateral graph truth outside exactly asserted families" 'ifa_fault_compare_collateral_edges'
require_delivery_cells "delta-retract asserts generation 1 landed first" "generation-1 SQL edge set did not match before the delta was driven"
require "gate sources the shared delta-live helper" "scripts/lib/ifa_sql_delta_live.sh"
require "gate defines the delta expected-edge set" "sql_delta_expected_edges="
if rg --fixed-strings --quiet -- "ifa_fault_compare_non_sql_edges" "${delivery_cells_lib}"; then
	fail "delta-retract must not compare whole non-SQL graph-dump endpoint hashes: SQL generation updates legitimately replace SQL-owned CONTAINS/REPO_CONTAINS hashes; assert unaffected covered families exactly instead"
fi
# Cell 9 CHANGES the graph on purpose (gen 2 adds and retracts edges), so a
# baseline-digest comparison would fail correctly and invite the wrong fix.
# Its exactness assertion is the expected-v2 set, which names the edges.
if rg --fixed-strings --quiet -- "assert_matches_baseline deltaretract" "${delivery_cells_lib}"; then
	fail "cell_deltaretract must NOT compare to the baseline digest: generation 2 intentionally changes the graph, so its proof is the expected-v2 edge set, not digest equality"
fi

require "fail-terminal explicitly excluded with rationale" "fail-terminal (a twelfth possible cell) is deliberately NOT included"

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

# code_calls (#5991): every cell drives the family, baseline exact-asserts it,
# and two dedicated cells prove queue reclaim and graph-write retry against the
# code-call domains rather than an unrelated row that happened to run first.
require "code-call cassette path" "testdata/cassettes/codecalls/ifa-code-call-family.json"
require "code-call expected-edge set path" "go/internal/ifa/testdata/codecalls/ifa-code-call-family-expected-edges.json"
require "code-call cassette existence guard" "code-call cassette not found"
require "code-call expected-edge set existence guard" "code-call expected-edge set not found"
require "code-call MERGE operation_match anchor" 'code_call_edge_operation_match="MERGE (source)-[rel:CALLS]->(target)"'
require_driver "code-call drive in every cell" "ifa_code_call_drive"
require_cells "code-call exact assertion in baseline" "ifa_code_call_assert"
require_cells "code-call fault-free retry baseline" '"code_call_materialization"'
require "code-call kill-reclaim cell invocation" "cell_killworker_code_calls"
require "code-call graph-write cell invocation" "cell_failgraphwrite_code_calls"
require_code_call_lib "code-call drive command" 'eshu-ifa" drive -cassette "${cassette}" -workers "${workers}"'
require_code_call_lib "code-call exact assertion domain" "-domain code_calls"
require_code_call_cells "claimed row targets code-call materialization" '"code_call_materialization"'
require_code_call_cells "kill cell proves a retry above baseline" "ifa_fault_assert_retried_above"
require_code_call_cells "graph-write cell selects queue-retry" '"queue-retry"'
require_code_call_cells "graph-write cell targets durable code-call marker" "ifa_fault_assert_once_fault_marker"
require_code_call_cells "graph-write cell probes code-call intents" "projection_domain = 'code_calls'"
require_code_call_cells "code-call precondition preserves query failure" "precondition query FAILED (exit"
require_code_call_cells "code-call precondition distinguishes empty output" "returned empty output"
require_code_call_cells "code-call precondition rejects non-numeric output" "returned non-numeric output"
require_code_call_cells "code-call precondition reports stale intents" "survived fresh_stack"
require_code_call_cells "both cells exact-assert five edges" "ifa_code_call_assert"

# documentation_edges (#5994): every cell drives the family via cell_baseline's
# drive_all_cassettes, baseline exact-asserts it, and two dedicated cells prove
# queue reclaim and graph-write retry against the documentation_materialization
# domain rather than an unrelated row that happened to run first. The family
# also proves the SqlTable target case (batchCanonicalDocumentationEntityEdgeCypher's
# MATCH label alternation, TestBuildDocumentationRowMapTableTargetMatchesSqlTableLabel).
require "documentation cassette path" "testdata/cassettes/documentation/ifa-documentation-family.json"
require "documentation expected-edge set path" "go/internal/ifa/testdata/documentation/ifa-documentation-family-live-expected-edges.json"
require "documentation cassette existence guard" "documentation cassette not found"
require "documentation expected-edge set existence guard" "documentation expected-edge set not found"
require "documentation DOCUMENTS MERGE operation_match anchor" 'documentation_edge_operation_match="MERGE (section)-[rel:DOCUMENTS]->(target)"'
require_driver "documentation drive in every cell" "ifa_documentation_drive"
require_cells "documentation exact assertion in baseline" "ifa_documentation_assert"
require_cells "documentation fault-free retry baseline" '"documentation_materialization"'
require "documentation kill-reclaim cell invocation" "cell_killworker_documentation"
require "documentation graph-write cell invocation" "cell_failgraphwrite_documentation"
require_documentation_lib "documentation drive command" 'eshu-ifa" drive -cassette "${cassette}" -workers "${workers}"'
require_documentation_lib "documentation exact assertion domain" "-domain documentation_edges"
require_documentation_lib "documentation non-vacuity framing" "three-edge exact set"
require_documentation_cells "claimed row targets documentation materialization" '"documentation_materialization"'
require_documentation_cells "kill cell proves a retry above baseline" "ifa_fault_assert_retried_above"
require_documentation_cells "graph-write cell selects queue-retry" '"queue-retry"'
require_documentation_cells "graph-write cell targets durable documentation marker" "ifa_fault_assert_once_fault_marker"
require_documentation_cells "graph-write cell probes documentation intents" "projection_domain = 'documentation_edges'"
require_documentation_cells "documentation precondition preserves query failure" "precondition query FAILED (exit"
require_documentation_cells "documentation precondition distinguishes empty output" "returned empty output"
require_documentation_cells "documentation precondition rejects non-numeric output" "returned non-numeric output"
require_documentation_cells "documentation precondition reports stale intents" "survived fresh_stack"
require_documentation_cells "both cells exact-assert three edges" "ifa_documentation_assert"
# deployable_unit_edges (#5993) cases live in a sourced case module so this
# structural verifier stays below 500 lines (mirroring the review-cases
# split just below).
# shellcheck source=scripts/lib/test-ifa-fault-injection-deployable-unit-cases.sh
source "${deployable_unit_cases_lib}"
run_ifa_fault_injection_deployable_unit_cases

# Behavioral regressions for the two review-discovered false-green seams live
# in a sourced case module so this structural verifier stays below 500 lines.
# shellcheck source=scripts/lib/test-ifa-fault-injection-review-cases.sh
source "${review_cases_lib}"
# documentation_edges (#5994) hermetic cases live in their own sibling module,
# same 500-line-cap reason as review_cases_lib above.
# shellcheck source=scripts/lib/test-ifa-fault-injection-documentation-cases.sh
source "${documentation_cases_lib}"
# codeowners_ownership_edges (#5992) hermetic cases, same split, and they own
# their own existence/syntax checks for the cells library they exercise.
# shellcheck source=scripts/lib/test-ifa-fault-injection-codeowners-cases.sh
source "${repo_root}/scripts/lib/test-ifa-fault-injection-codeowners-cases.sh"
run_ifa_fault_injection_review_cases
run_ifa_fault_injection_codeowners_cases

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
for f in "${script}" "${fault_lib}" "${driver_lib}" "${cells_lib}" "${sql_cells_lib}" "${delivery_cells_lib}" "${collateral_nodes_lib}" "${code_call_lib}" "${code_call_cells_lib}" "${deployable_unit_live_lib}" "${deployable_unit_diagnostics_lib}" "${deployable_unit_converge_lib}" "${deployable_unit_lock_lib}" "${deployable_unit_cells_lib}"; do
	if rg --pcre2 --quiet -- "${private_pattern}" "${f}"; then
		fail "$(basename "${f}") looks like it contains private data"
	fi
done

# Claimed-wait SQL-budget validation, domain-scoping (#5555), and the
# once-fired-marker three-way discrimination (#5974/#5555) are functional
# stub tests, not text greps -- they live in a sourced case module so this
# structural verifier stays under 500 lines (mirroring the deployable-unit
# and review-cases splits above).
# shellcheck source=scripts/lib/test-ifa-fault-injection-marker-cases.sh
source "${repo_root}/scripts/lib/test-ifa-fault-injection-marker-cases.sh"
run_ifa_fault_injection_marker_cases

printf 'test-verify-ifa-fault-injection: pass\n'
