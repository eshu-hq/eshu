#!/usr/bin/env bash
# Static structural test for verify-ifa-fault-injection.sh (issue #4580 P6
# slice S5, extended by #5555's SQL-targeted cells, #5991's code-call cells,
# and #5998's rationale cells). The gate
# itself needs Docker + a built toolchain and takes significantly longer
# than the sibling determinism matrix (43 fresh Postgres + NornicDB
# stacks, sixteen of them running a -tags ifafaultinjection
# reducer), so this mirror validates the contract that cannot silently
# drift: strict mode and the bash>=4.4 guard, an isolated Compose project and
# port triple distinct from every sibling verify-ifa-*.sh script, the
# 49-cell shape (baseline + 48 live cells; fail-terminal
# deliberately absent with its rationale documented), each cell's own
# recovery mechanism, the digest/dead_letter/non-vacuity assertions, the
# tagged-reducer + fault-script wiring this gate is the first thing to
# exercise live, and that each targeted cell provably selects its intended
# SQL, code-call, documentation, or rationale domain rather than whichever
# domain the driven cassettes happen to schedule first. The driver
# script itself was split into scripts/lib/ifa_fault_injection_driver.sh
# (shared per-cell plumbing), scripts/lib/ifa_fault_injection_cells.sh (the
# five original cells), scripts/lib/ifa_fault_injection_sql_cells.sh (two
# SQL-targeted cells), scripts/lib/ifa_fault_injection_code_call_cells.sh (two
# code-call cells), scripts/lib/ifa_fault_injection_rationale_cells.sh (two
# rationale cells), scripts/lib/ifa_fault_injection_delivery_cells.sh, and its
# full-node collateral helper ifa_fault_injection_collateral_nodes.sh to stay
# under the repo's 500-line cap; checks below point at whichever file now holds
# the content.
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
script="${repo_root}/scripts/verify-ifa-fault-injection.sh"
det_lib="${repo_root}/scripts/lib/ifa_determinism_common.sh"
fault_lib="${repo_root}/scripts/lib/ifa_fault_injection_common.sh"
driver_lib="${repo_root}/scripts/lib/ifa_fault_injection_driver.sh"
sources_lib="${repo_root}/scripts/lib/ifa_fault_injection_sources.sh"
delta_lib="${repo_root}/scripts/lib/ifa_sql_delta_live.sh"
cells_lib="${repo_root}/scripts/lib/ifa_fault_injection_cells.sh"
sql_cells_lib="${repo_root}/scripts/lib/ifa_fault_injection_sql_cells.sh"
delivery_cells_lib="${repo_root}/scripts/lib/ifa_fault_injection_delivery_cells.sh"
collateral_nodes_lib="${repo_root}/scripts/lib/ifa_fault_injection_collateral_nodes.sh"
code_call_lib="${repo_root}/scripts/lib/ifa_code_call_live.sh"
code_call_cells_lib="${repo_root}/scripts/lib/ifa_fault_injection_code_call_cells.sh"
code_call_cases_lib="${repo_root}/scripts/lib/test-ifa-fault-injection-code-call-cases.sh"
documentation_lib="${repo_root}/scripts/lib/ifa_documentation_live.sh"
documentation_cells_lib="${repo_root}/scripts/lib/ifa_fault_injection_documentation_cells.sh"
documentation_barrier_lib="${repo_root}/scripts/lib/ifa_fault_injection_documentation_ack_barrier.sh"
documentation_barrier_setup_lib="${repo_root}/scripts/lib/ifa_fault_injection_documentation_ack_setup.sh"
documentation_cases_lib="${repo_root}/scripts/lib/test-ifa-fault-injection-documentation-cases.sh"
deployable_unit_live_lib="${repo_root}/scripts/lib/ifa_deployable_unit_live.sh"
deployable_unit_diagnostics_lib="${repo_root}/scripts/lib/ifa_deployable_unit_live_diagnostics.sh"
deployable_unit_converge_lib="${repo_root}/scripts/lib/ifa_deployable_unit_live_converge.sh"
deployable_unit_lock_lib="${repo_root}/scripts/lib/ifa_fault_injection_deployable_unit_lock.sh"
deployable_unit_cells_lib="${repo_root}/scripts/lib/ifa_fault_injection_deployable_unit_cells.sh"
review_cases_lib="${repo_root}/scripts/lib/test-ifa-fault-injection-review-cases.sh"; codeowners_cases_lib="${repo_root}/scripts/lib/test-ifa-fault-injection-codeowners-cases.sh"  # packed for the 500-line cap
kubernetes_namespace_environment_cells_lib="${repo_root}/scripts/lib/ifa_fault_injection_kubernetes_namespace_environment_cells.sh"; iam_instance_profile_role_cells_lib="${repo_root}/scripts/lib/ifa_fault_injection_iam_instance_profile_role_cells.sh"; kubernetes_namespace_environment_cases_lib="${repo_root}/scripts/lib/test-ifa-fault-injection-kubernetes-namespace-environment-cases.sh"; iam_instance_profile_role_cases_lib="${repo_root}/scripts/lib/test-ifa-fault-injection-iam-instance-profile-role-cases.sh"  # direct families (#6309), packed for the 500-line cap
deployable_unit_cases_lib="${repo_root}/scripts/lib/test-ifa-fault-injection-deployable-unit-cases.sh"
documentation_barrier_cases_lib="${repo_root}/scripts/lib/test-ifa-fault-injection-documentation-ack-barrier-cases.sh"
documentation_barrier_cleanup_cases_lib="${repo_root}/scripts/lib/test-ifa-fault-injection-documentation-ack-cleanup-cases.sh"
rationale_lib="${repo_root}/scripts/lib/ifa_rationale_live.sh"
rationale_cells_lib="${repo_root}/scripts/lib/ifa_fault_injection_rationale_cells.sh"
rationale_cases_lib="${repo_root}/scripts/lib/test-ifa-fault-injection-rationale-cases.sh"; submodule_pin_cases_lib="${repo_root}/scripts/lib/test-ifa-fault-injection-submodule-pin-cases.sh"  # packed for the 500-line cap
entrypoint_cases_lib="${repo_root}/scripts/lib/test-ifa-fault-injection-entrypoint-cases.sh"; marker_cases_lib="${repo_root}/scripts/lib/test-ifa-fault-injection-marker-cases.sh"; cell_catalog_doc="${repo_root}/docs/internal/ifa-fault-cell-catalog.md"; cell_pins_cases_lib="${repo_root}/scripts/lib/test-ifa-fault-injection-cell-pins-cases.sh"  # packed for the 500-line cap
assertions_lib="${repo_root}/scripts/lib/test-ifa-fault-injection-assertions.sh"
# The pin-helper meta-gate, split out of the assertions lib under #6261 when
# that file had been sitting at exactly 499/500 for a whole review cycle.
pin_probe_lib="${repo_root}/scripts/lib/test-ifa-fault-injection-pin-probe.sh"
# shellcheck disable=SC2034  # read indirectly by the syntax-check loop below.
fixtures_lib="${repo_root}/scripts/lib/ifa_family_fixtures.sh"
shard_lib="${repo_root}/scripts/lib/ifa_fault_shard.sh"
shard_cases_lib="${repo_root}/scripts/lib/test-ifa-fault-injection-shard-cases.sh"; repo_dependency_lease_cases_lib="${repo_root}/scripts/lib/test-ifa-fault-injection-repo-dependency-lease-cases.sh"; repo_dependency_cases_lib="${repo_root}/scripts/lib/test-ifa-fault-injection-repo-dependency-cases.sh"  # packed for the 500-line cap
deployable_unit_ordering_cases_lib="${repo_root}/scripts/lib/test-ifa-fault-injection-deployable-unit-ordering-cases.sh"; workload_dependency_cases_lib="${repo_root}/scripts/lib/test-ifa-fault-injection-workload-dependency-cases.sh"  # packed for the 500-line cap
generic_cells_lib="${repo_root}/scripts/lib/ifa_fault_generic_cells.sh"
generic_baseline_lib="${repo_root}/scripts/lib/ifa_fault_generic_baseline_cell.sh"
table_lock_lib="${repo_root}/scripts/lib/ifa_fault_generic_table_lock.sh"
table_lock_cases_lib="${repo_root}/scripts/lib/test-ifa-fault-injection-generic-table-lock-cases.sh"
shared_intent_lock_cases_lib="${repo_root}/scripts/lib/test-ifa-fault-injection-generic-shared-intent-lock-cases.sh"
family_drive_cases_lib="${repo_root}/scripts/lib/test-ifa-fault-injection-generic-family-drive-cases.sh"; runner_lease_hold_cases_lib="${repo_root}/scripts/lib/test-ifa-fault-injection-generic-runner-lease-hold-cases.sh"; runner_lease_audit_cases_lib="${repo_root}/scripts/lib/test-ifa-fault-injection-generic-runner-lease-audit-cases.sh"  # packed for the 500-line cap
generic_modules_lib="${repo_root}/scripts/lib/test-ifa-fault-injection-generic-modules.sh"
# The dispatcher self-sources these two by variable path, so nothing else in
# this mirror named them and the derived syntax loop below could not see them.
# shared_intent_lock holds _ifa_generic_require_intent_writer, the mandatory
# non-vacuity precondition this change rests on.
generic_shared_intent_lock_lib="${repo_root}/scripts/lib/ifa_fault_generic_shared_intent_lock.sh"
generic_runner_wait_lib="${repo_root}/scripts/lib/ifa_fault_generic_runner_wait.sh"
private_data_pattern_lib="${repo_root}/scripts/lib/ifa_private_data_pattern.sh"
dead_command_lib="${repo_root}/scripts/lib/ifa_dead_command_line.sh"
fail() { printf 'test-verify-ifa-fault-injection: %s\n' "$*" >&2; exit 1; }
for f in "${script}" "${fault_lib}" "${det_lib}" "${driver_lib}" "${sources_lib}" "${delta_lib}" "${cells_lib}" "${sql_cells_lib}" "${delivery_cells_lib}" "${collateral_nodes_lib}" "${code_call_lib}" "${code_call_cells_lib}" "${code_call_cases_lib}" "${documentation_lib}" "${documentation_cells_lib}" "${documentation_barrier_lib}" "${documentation_barrier_setup_lib}" "${documentation_cases_lib}" "${documentation_barrier_cases_lib}" "${documentation_barrier_cleanup_cases_lib}" "${rationale_lib}" "${rationale_cells_lib}" "${rationale_cases_lib}" "${review_cases_lib}" "${entrypoint_cases_lib}" "${deployable_unit_cases_lib}" "${assertions_lib}" "${pin_probe_lib}" "${deployable_unit_live_lib}" "${deployable_unit_diagnostics_lib}" "${deployable_unit_converge_lib}" "${deployable_unit_lock_lib}" "${deployable_unit_cells_lib}" "${shard_lib}" "${shard_cases_lib}" "${repo_dependency_lease_cases_lib}" "${repo_dependency_cases_lib}" "${workload_dependency_cases_lib}" "${codeowners_cases_lib}" "${kubernetes_namespace_environment_cells_lib}" "${iam_instance_profile_role_cells_lib}" "${kubernetes_namespace_environment_cases_lib}" "${iam_instance_profile_role_cases_lib}" "${submodule_pin_cases_lib}" "${marker_cases_lib}" "${deployable_unit_ordering_cases_lib}" "${generic_cells_lib}" "${table_lock_lib}" "${table_lock_cases_lib}" "${shared_intent_lock_cases_lib}" "${family_drive_cases_lib}" "${runner_lease_hold_cases_lib}" "${runner_lease_audit_cases_lib}" "${generic_modules_lib}" "${generic_shared_intent_lock_lib}" "${generic_runner_wait_lib}" "${private_data_pattern_lib}"; do
	[[ -f "${f}" ]] || fail "missing ${f}"
done
[[ -x "${script}" ]] || fail "verify-ifa-fault-injection.sh must be executable"
# Syntax-check every declared library, derived from the *_lib variables above
# rather than a hand-typed list. The hand-typed form was 37 names and had the
# exact failure it was meant to prevent: ifa_fault_generic_shared_intent_lock.sh
# and ifa_fault_generic_runner_wait.sh -- the first of which holds the mandatory
# non-vacuity precondition this whole change rests on -- were introduced by this
# same branch and never added to it, so a syntax error or a truncating edit in
# either surfaced only in the ~22-minute live Docker shard. Deriving the list
# from the declarations makes the coverage total for real, instead of a comment
# claiming it is.
rg --fixed-strings --quiet -- 'ifa_fault_injection_documentation_ack_setup.sh' "${documentation_barrier_lib}" \
	|| fail "documentation ACK barrier must source its setup/holder helper"
[[ "$(wc -l <"${BASH_SOURCE[0]}" | tr -d '[:space:]')" -lt 500 ]] \
	|| fail "test-verify-ifa-fault-injection.sh must stay under 500 lines"
# The GATE script needs the same guard as this mirror. Its determinism sibling
# has always had one (test-verify-ifa-determinism.sh asserts on ${script}); the
# fault side asserted only on itself, so verify-ifa-fault-injection.sh could
# drift over the cap with nothing to catch it. `filecap-all` does not close the
# hole either -- it walks `git ls-files 'go/*.go'` and never sees shell.
[[ "$(wc -l <"${script}" | tr -d '[:space:]')" -lt 500 ]] \
	|| fail "verify-ifa-fault-injection.sh must stay under 500 lines"

# The null-command rule the code-portion counters use to decide a line
# executes nothing. Sourced BEFORE the assertions lib, which calls it (#6194).
# shellcheck source=scripts/lib/ifa_dead_command_line.sh
source "${dead_command_lib}"
# shellcheck source=scripts/lib/ifa_private_data_pattern.sh
source "${private_data_pattern_lib}"
# shellcheck source=scripts/lib/test-ifa-fault-injection-assertions.sh
source "${assertions_lib}"
# shellcheck source=scripts/lib/test-ifa-fault-injection-pin-probe.sh
source "${pin_probe_lib}"
# Parses every declared *_lib and floors the count; defined in the assertions
# lib, so it must run after that source.
assert_libs_parse
# shellcheck source=scripts/lib/test-ifa-fault-injection-rationale-cases.sh
source "${rationale_cases_lib}"
# shellcheck source=scripts/lib/test-ifa-fault-injection-entrypoint-cases.sh
source "${entrypoint_cases_lib}"
run_ifa_fault_entrypoint_static_cases; run_ifa_fault_cell_catalog_cases; run_ifa_fault_lib_cap_coverage_cases  # packed for the 500-line cap

# Both GCP cassettes, generated synth-multiscope once, and the drive verb
# (now in the driver lib's drive_all_cassettes helper).
require_code "demo-org cassette" "testdata/cassettes/gcpcloud/supply-chain-demo.json"
require_code "synth-cassette verb invocation" '"${bin_dir}/eshu-ifa" synth-cassette'
require_driver "demo-org drive in every cell" 'eshu-ifa" drive -cassette "${cassette}" -workers "${drive_workers}"'; require_driver "synth drive in every cell" 'eshu-ifa" drive -cassette "${synth_cassette}" -workers "${drive_workers}"'  # packed for the 500-line cap; bind each drive, not the shared verb -- the SQL and deployable-unit drives below have their own pins, so the bare verb stayed green with either of these two replaced by `true` (#6161)
require_driver "vacuous-drive guard" "vacuous drain proof"

# SQL relationship family cassette (#5351): driven into every cell so cells
# 2/3/6 (lease-expiry / kill-worker, including the SQL-targeted kill-worker)
# exercise the SQL relationship materialization handler's replay through the
# real durable fault path, plus a baseline absolute-set assertion
# (`ifa assert-edges`) proving the fault-free graph carries all nine SQL
# edges before the recovery cells compare against it. Backs the
# materialized_edges:sql_relationships manifest row's proof_gate:
# ifa-fault-injection claim.
require_fixture "SQL cassette path" "testdata/cassettes/sqlrelationships/ifa-sql-family.json"
require_fixture "SQL expected-edge set path" "go/internal/ifa/testdata/sqlrelationships/ifa-sql-family-expected-edges.json"
require_fixture "SQL cassette existence guard" 'SQL cassette not found'
require_fixture "SQL expected-edge set existence guard" 'SQL expected-edge set not found'
require_driver "SQL cassette driven into every cell" 'eshu-ifa" drive -cassette "${sql_cassette}" -workers "${drive_workers}"'
require_driver "drive helper defined" "drive_all_cassettes() {"
require_cells "assert-edges verb invocation on baseline" '"${bin_dir}/eshu-ifa" assert-edges'
require_cells "assert-edges domain flag" "-domain sql_relationships"
require_cells "assert-edges expected flag" '-expected "${sql_expected_edges}"'
require_cells "assert-edges non-vacuity framing" "non-vacuity"

# Untagged binaries plus a SEPARATE tagged reducer build for the queue-retry /
# graph-fault and restart cells.
require_code "untagged reducer build" "ifa_det_build_bin \"\${bin_dir}\" reducer"
require_code "tagged reducer build" "ifa_det_build_bin \"\${tagged_bin_dir}\" reducer \"ifafaultinjection\""
require_code "gate binary build" "ifa_det_build_bin \"\${bin_dir}\" golden-corpus-gate"
require_driver "gate binary invocation" "eshu-golden-corpus-gate"
require_driver "drains phase" "-phase=drains"
require_driver "snapshot contract" "testdata/golden/e2e-20repo-snapshot.json"

# Drain-must-be-polled-not-slept, mirroring the determinism gate's own check.
if rg --quiet --pcre2 'sleep\s+\$\{?GATE_DRAIN' "${driver_lib}"; then
	fail "drain must be polled by the gate, not slept"
fi

# The full-cell-shape anchored-invocation check and the SQL permanent-
# member pin (both touch the ifa_fault_shard_run dispatch wrapper) live in
# the sourced shard-cases module below, extracted to buy this file real
# line-count headroom rather than trimming their comments in place.
# #5974 probes. A missing marker had two explanations and the gate had to guess
# between them, which is how this issue stayed open on a wrong root cause. These
# three close the gap: the stack is provably fresh, the edge provably exists,
# and a failed marker write is no longer silent.
require_driver "fresh_stack fails loudly when teardown fails" "the stack is NOT fresh"
# The redirect and the fail-closed die, named separately: the bare log-path
# needle also matched the tail and the die message, so the capture the label
# names could be dropped back to >/dev/null with this pin green (#6161).
require_driver "fresh_stack captures teardown output instead of discarding it" '>"${log_dir}/compose-down-${cell}.log" 2>&1; then'
require_driver "fresh_stack fails closed when teardown fails" 'die "${cell}: docker compose down -v failed'
require_sql_cells "probe 1: fresh-stack intent precondition" "survived fresh_stack"
require_sql_cells "probe 2: SQL edges asserted after the drain" "assert-edges is set-exact"
require_sql_cells "probe 2: this cell's intent window is reported" "projection_domain = 'sql_relationships'"
# IFA_ONCE_MARKER_WRITE_FAILED_PREFIX has TWO code occurrences in the SQL cells
# lib and both do work: the branch condition that tells a failed marker WRITE
# apart from a fault that never fired, and the diagnostic that prints the
# matching reducer-log lines. A -ge 1 pin on the bare variable name is satisfied
# by either, so mangling the branch condition left this gate green while the cell
# stopped distinguishing the two failure modes -- the exact confusion #5974 was
# reopened over. Re-measured on this branch: the note that used to sit here
# claimed mangling the condition reds, and it does not. One pin per occurrence,
# each carrying enough of its own line to name it (#6161).
require_sql_cells "the write-failure branch condition uses the single-source prefix variable" '== *"${IFA_ONCE_MARKER_WRITE_FAILED_PREFIX}"*'
require_sql_cells "the write-failure diagnostic prints the matching log lines" 'sed -n "/${IFA_ONCE_MARKER_WRITE_FAILED_PREFIX}/p"'
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
require_framing "the marker assertion matches in bash, not via an external binary" "Bash substring match, NOT an external tool" "${fault_lib}"
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

# The SQL permanent-member invocation pin also moved to the shard-cases
# module (see the note above the full-cell-shape comment).
require "failgraphwrite_sql is documented as permanent, not an experiment" "permanent member of the matrix as of #5974"
# The library must DEFINE both cells. The needles below check implementation
# details that could still match if the function wrapper were renamed away.
require_delivery_cells "delivery lib defines cell_duplicatedelivery" "cell_duplicatedelivery() {"
require_delivery_cells "delivery lib defines cell_deltaretract" "cell_deltaretract() {"

# The numbered per-cell pins (cells 10, 11 and the candidate-adjacent
# kill/reclaim/graph-write cells) live in a sourced case module so this
# verifier stays below 500 lines -- it had reached exactly 499. Same split, and
# the same reason, as the case modules around it.
# shellcheck source=scripts/lib/test-ifa-fault-injection-cell-pins-cases.sh
source "${cell_pins_cases_lib}"
run_ifa_fault_injection_cell_pins_cases
# Pin this gate's OWN call site the way its siblings are pinned. EXACTLY THREE
# (the call, this needle, and the name in the message) because an in-file pin is
# always satisfied by its own line.
[[ "$(_ifa_count_code_matches 'run_ifa_fault_injection_cell_pins_cases' "${BASH_SOURCE[0]}")" -eq 3 ]] \
	|| fail "this mirror no longer calls run_ifa_fault_injection_cell_pins_cases (expected the call, this pin, and the name in this message) -- the numbered per-cell pins would all stop running"

# documentation_edges (#5994) cases live in a sourced case module so this
# structural verifier stays below 500 lines (mirroring the deployable-unit
# split just below); run_ifa_fault_injection_documentation_registry_cases
# holds the static require()/rg pins, alongside the hermetic behavioral
# cases the same sourced file already carried.
# shellcheck source=scripts/lib/test-ifa-fault-injection-documentation-cases.sh
source "${documentation_cases_lib}"
run_ifa_fault_injection_documentation_registry_cases

# Sourced ahead of deployable_unit_cases_lib below: that module calls
# run_ifa_fault_injection_deployable_unit_ordering_cases and
# run_ifa_fault_injection_atomic_group_ordering_cases (defined across these
# two files), so both must exist before that call runs.
# shellcheck source=scripts/lib/test-ifa-fault-injection-shard-cases.sh
source "${shard_cases_lib}"
# shellcheck source=scripts/lib/test-ifa-fault-injection-deployable-unit-ordering-cases.sh
source "${deployable_unit_ordering_cases_lib}"

# deployable_unit_edges (#5993) cases live in a sourced case module so this
# structural verifier stays below 500 lines (mirroring the review-cases
# split just below).
# shellcheck source=scripts/lib/test-ifa-fault-injection-deployable-unit-cases.sh
source "${deployable_unit_cases_lib}"
run_ifa_fault_injection_deployable_unit_cases
run_ifa_rationale_live_static_cases

# Behavioral regressions live in sourced modules to stay below 500 lines.
# shellcheck source=scripts/lib/test-ifa-fault-injection-review-cases.sh
source "${review_cases_lib}"
# shellcheck source=scripts/lib/test-ifa-fault-injection-code-call-cases.sh
source "${code_call_cases_lib}"
# documentation_edges (#5994) hermetic behavioral cases were already sourced
# (and their registry-pin sibling function called) above, alongside this
# family's static require()/rg pins -- not re-sourced here.
# codeowners_ownership_edges (#5992) hermetic cases, same split, and they own
# their own existence/syntax checks for the cells library they exercise.
# shellcheck source=scripts/lib/test-ifa-fault-injection-codeowners-cases.sh
source "${codeowners_cases_lib}"
# kubernetes_namespace_environment + iam_instance_profile_role (#6309) hermetic
# cases, same split; each module owns its cells library's existence/syntax
# checks (vars declared with the other lib paths above).
# shellcheck source=scripts/lib/test-ifa-fault-injection-kubernetes-namespace-environment-cases.sh
source "${kubernetes_namespace_environment_cases_lib}"
# shellcheck source=scripts/lib/test-ifa-fault-injection-iam-instance-profile-role-cases.sh
source "${iam_instance_profile_role_cases_lib}"
source "${repo_dependency_cases_lib}"; source "${repo_dependency_lease_cases_lib}"; source "${workload_dependency_cases_lib}"  # family case modules packed for the 500-line cap
# shellcheck source=scripts/lib/test-ifa-fault-injection-documentation-ack-barrier-cases.sh
source "${documentation_barrier_cases_lib}"
# shellcheck source=scripts/lib/test-ifa-fault-injection-documentation-ack-cleanup-cases.sh
source "${documentation_barrier_cleanup_cases_lib}"
run_ifa_documentation_live_static_cases
run_ifa_fault_injection_review_cases
run_ifa_fault_injection_codeowners_cases
run_ifa_fault_injection_kubernetes_namespace_environment_cases; run_ifa_fault_injection_iam_instance_profile_role_cases  # direct families (#6309), packed for the 500-line cap
run_ifa_fault_injection_repo_dependency_cases; run_ifa_fault_injection_workload_dependency_cases  # family case modules packed for the 500-line cap
source "${submodule_pin_cases_lib}"; run_ifa_fault_injection_submodule_pin_cases  # submodule_pin_edges (#6002) hermetic cases, same split; packed for the 500-line cap

# The unchanged Layer 4 acceptance: digest equality against baseline plus a
# hard failure (never a retry) on divergence.
require_framing "baseline digest capture" "digests[baseline]" "${driver_lib}"
require_driver "digest comparison helper" "assert_matches_baseline"
require_driver "mismatch framing" "MISMATCH:"
require_driver "full-bytes diff on divergence" "diff -u"
require_driver "no-normalize-away directive" "do NOT retry, lower workers, or otherwise normalize this away"
require_driver "dead-letter zero assertion" "assert_no_dead_letters() {"
require_lib "dead-letter count query" "SELECT count(*) FROM fact_work_items WHERE status = 'dead_letter';"

# Per-cell wall time is captured by every cell and reported in the driver's
# final summary.
# Fifteen occurrences across five cells (declare, assign, subtract). Left as
# -ge 1: wall time is reporting, not proof, and an exact count here would churn
# with every added cell for no assertion gained (#6161 audit).
require_cells "per-cell wall time capture" "cell_start"
# Same reasoning as the cells-lib wall-time pin above (#6161 audit).
require_sql_cells "per-cell wall time capture (sql cells)" "cell_start"
require_code "wall time in summary" "wall=%ss"

# The lib functions this script depends on all exist with the expected shape.
require_lib "once-script function signature" 'ifa_fault_write_once_script() {'
require_lib "restart-script function signature" 'ifa_fault_write_restart_script() {'
require_lib "claimed-wait function signature" 'ifa_fault_wait_for_claimed() {'
# Bind the SELECT that RUNS it, not the name: the CREATE OR REPLACE FUNCTION
# above carries the same name, so replacing the call with `SELECT 1;` left the
# pin green with the claimed-wait never executing -- the cell then races the
# fault against the claim it is supposed to wait for (#6161).
require_lib "claimed-wait uses one server-side polling connection" 'SELECT pg_temp.ifa_wait_for_claimed(${budget});'
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
# ${det_lib} is passed EXPLICITLY even though the *_lib derivation inside
# already reaches it: that coverage is an accident of how the variable is
# spelled. Renaming det_lib -> det_shared, a pure rename with no behaviour
# change, drops the shared lib from the scan in silence -- the floor of 40 is
# nowhere near binding, and the mirror still prints `87 file(s) scanned` with a
# planted AWS key sitting in that file (#6161). Named here, a rename breaks
# loudly under `set -u`. The cost is one duplicate scan of one file, which is
# why the printed number counts targets rather than distinct files.
assert_no_private_data "${script}" "${det_lib}"
# ...and the NUMBER in each derivation floor, not just the message that reports
# it. test-ifa-fault-injection-documentation-cases.sh pins that both floors are
# still asserted, by counting a string only each can produce -- but a floor
# lowered rather than deleted keeps that string and stops catching anything:
# `-ge 40` -> `-ge 1` and `-ge 45` -> `-ge 1` both left this mirror at exit 0
# (#6195). Its determinism sibling pins the number and this side did not, which
# is the same asymmetry #6173 had to close twice.
#
# EXACTLY ONE each, counted in the assertions lib rather than here, so these two
# lines cannot satisfy themselves. RAISING a floor is meant to cost an edit in
# both places: the number here is the claim, and a claim that tracks whatever
# the subject happens to say is not a pin.
[[ "$(_ifa_count_code_matches '"${#targets[@]}" -ge 40' "${assertions_lib}")" -eq 1 ]] \
	|| fail "assert_no_private_data's scanned-file floor is no longer exactly 40 -- a lowered floor still reports a count and still passes on a collapsed derivation"
[[ "$(_ifa_count_code_matches '"${syntax_checked}" -ge 45' "${assertions_lib}")" -eq 1 ]] \
	|| fail "assert_libs_parse's parsed-lib floor is no longer exactly 45 -- a lowered floor still reports a count and still passes on a collapsed derivation"

# Claimed-wait SQL-budget validation, domain-scoping (#5555), and the
# once-fired-marker three-way discrimination (#5974/#5555) are functional
# stub tests, not text greps -- they live in a sourced case module so this
# structural verifier stays under 500 lines (mirroring the deployable-unit
# and review-cases splits above).
# shellcheck source=scripts/lib/test-ifa-fault-injection-marker-cases.sh
source "${marker_cases_lib}"
run_ifa_fault_injection_marker_cases
# shellcheck source=scripts/lib/test-ifa-fault-injection-generic-modules.sh
source "${generic_modules_lib}"
run_ifa_fault_injection_generic_modules

# Shard selector: exact-cover proof, invalid-input rejection, and the
# CI-wiring/matrix-cardinality cross-checks against the workflow -- module
# already sourced above (deployable_unit_cases_lib needs it earlier).
run_ifa_fault_injection_shard_cases

# META-GATE, run LAST so every case module has been sourced and bash can see the
# helpers they define. Discovery is `compgen -A function`, so it sees exactly
# what is loaded -- running it earlier silently exercised 18 helpers instead of
# all of them, which the floor caught.
assert_pin_helpers_bind_code

printf 'test-verify-ifa-fault-injection: pass\n'
