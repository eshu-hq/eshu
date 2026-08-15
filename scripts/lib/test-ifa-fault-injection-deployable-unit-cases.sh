#!/usr/bin/env bash
# shellcheck disable=SC2154  # Sourced test helper reads parent-owned paths.
# deployable_unit_edges (#5993) structural cases for
# scripts/test-verify-ifa-fault-injection.sh, split out (mirroring
# test-ifa-fault-injection-review-cases.sh) to keep that file under the
# repository's 500-line cap. The parent verifier owns strict mode, fail(),
# and every path variable referenced below (script, driver_lib,
# deployable_unit_live_lib, deployable_unit_cells_lib).

require_deployable_unit_live_lib() {
	local label="$1" needle="$2"
	rg --fixed-strings --quiet -- "${needle}" "${deployable_unit_live_lib}" || fail "missing ${label} (deployable-unit live lib): ${needle}"
}
require_deployable_unit_cells() {
	local label="$1" needle="$2"
	rg --fixed-strings --quiet -- "${needle}" "${deployable_unit_cells_lib}" || fail "missing ${label} (deployable-unit cells lib): ${needle}"
}

# run_ifa_fault_injection_deployable_unit_cases asserts the family-scoped
# baseline cell, its two fault cells, and their dispatch ordering. Wrapped in
# a function (mirroring run_ifa_fault_injection_review_cases) so ${script},
# ${driver_lib}, and the deployable-unit path vars resolve at CALL time from
# the parent's scope, not at source time -- a bare top-level block here would
# silently assert against empty variables.
run_ifa_fault_injection_deployable_unit_cases() {
	# deployable_unit_edges (#5993): a family-scoped baseline cell plus two
	# fault cells, run after a bootstrap-index maintenance pass
	# (ifa_deployable_unit_live.sh's header explains why); fault cells compare
	# against their OWN baseline.
	require "deployable-unit cassette path" "testdata/cassettes/deployableunit/ifa-deployable-unit-family.json"
	require "deployable-unit expected-edge set path" "go/internal/ifa/testdata/deployableunit/ifa-deployable-unit-family-expected-edges.json"
	require "deployable-unit cassette existence guard" "deployable-unit cassette not found"
	require "deployable-unit expected-edge set existence guard" "deployable-unit expected-edge set not found"
	require "deployable-unit MERGE operation_match anchor" 'deployable_unit_edge_operation_match="MERGE (source_repo)-[rel:CORRELATES_DEPLOYABLE_UNIT]->(deployment_repo)"'
	require "sixth binary: bootstrap-index build" "ifa_det_build_bin \"\${bin_dir}\" bootstrap-index"
	require_driver "deployable-unit drive in every cell" 'eshu-ifa" drive -cassette "${deployable_unit_cassette}" -workers "${drive_workers}"'
	# Four checks against the LIVE LIB itself (ifa_deployable_unit_live.sh),
	# mirroring the documentation family's require_documentation_lib set.
	# Without these, require_deployable_unit_live_lib existed with zero call
	# sites: the mirror asserted the CELLS file's shape (it calls the wrapper
	# function names) but proved nothing about the file that does the actual
	# drive/assert-edges/maintenance-pass work -- someone could gut this
	# family's live proof entirely and the mirror would still pass.
	require_deployable_unit_live_lib "live lib drive command" '"${bin_dir}/eshu-ifa" drive -cassette "${cassette}" -workers 1'
	require_deployable_unit_live_lib "live lib exact assertion domain" "-domain deployable_unit_edges"
	require_deployable_unit_live_lib "live lib non-vacuity framing" "one-edge exact set"
	require_deployable_unit_live_lib "live lib maintenance-pass invocation" '"${bin_dir}/eshu-bootstrap-index"'
	# Pinned to the CODE SHAPE, not the bare phrase: the phrase
	# "cross-repo relationship resolution started" also appears in this
	# function's own header comment and in its echo/printf diagnostics, so a
	# plain phrase needle would still match after the functional comparison
	# below was deleted -- exactly the "cannot fail" trap this whole item
	# exists to avoid. Each needle is the literal `if [[ ... ]]` condition, so
	# deleting either comparison (not just the phrase) turns this red.
	require_deployable_unit_live_lib "live lib readiness-opened resolution-started check" '"${contents}" != *"cross-repo relationship resolution started"* ]]'
	require_deployable_unit_live_lib "live lib readiness-opened not-gated check" '"${contents}" == *"cross-repo resolution gated"* ]]'
	# Post-maintenance intents diagnostic (#5993 follow-up): separates the two
	# indistinguishable causes of the live gate's "expected 1, got 0" failure --
	# a writer endpoint-MATCH miss (intents > 0, 0 edges) vs. correlation never
	# admitting a row (intents == 0). Six needles against the live lib: the
	# function exists, it states BOTH interpretations (not just a count), and
	# it degrades to an explicit "unavailable" reading instead of killing the
	# cell -- the call site has no `|| return 1` on purpose (this is a
	# diagnostic, not a gate), which means the caller's `set -e` is NOT
	# suppressed at that call site; an unguarded query failure inside the
	# function would abort the whole cell before the real edge assertion runs,
	# the exact false-red this probe exists to avoid -- and that tolerance is
	# caller-independent, not merely pipefail-shaped: ifa_det_pg is the first
	# stage of a pipeline ending in `tr`, so without pipefail its failure alone
	# leaves admitted empty rather than tripping the `if !`, which is why the
	# `[[ -z "${admitted}" ]]` needle is pinned separately from the `if !`
	# needle. Wiring existence alone would still pass if the call were wired
	# below the final edge assertion it is supposed to precede, so ordering is
	# pinned as line-number checks below rather than as a text needle.
	require_deployable_unit_live_lib "live lib post-maintenance intents diagnostic definition" "ifa_deployable_unit_live_report_intents_after_maintenance() {"
	require_deployable_unit_live_lib "live lib post-maintenance intents diagnostic writer-cause framing" "intents > 0 with 0 edges implicates the writer"
	require_deployable_unit_live_lib "live lib post-maintenance intents diagnostic correlation-cause framing" "intents == 0 implicates correlation"
	require_deployable_unit_live_lib "live lib post-maintenance intents diagnostic set -e suppression guard" 'if ! admitted="$(ifa_det_pg'
	require_deployable_unit_live_lib "live lib post-maintenance intents diagnostic failure-tolerant reading" 'admitted="unavailable (query failed)"'
	require_deployable_unit_live_lib "live lib post-maintenance intents diagnostic pipefail-independent emptiness check" '[[ -z "${admitted}" ]]; then'
	# Ordering: the standalone determinism cell's post-maintenance intents
	# call must precede ITS OWN final edge assertion (ifa_deployable_unit_live_assert
	# ... || return 1 -- a call moved below it never executes on the only path
	# where the diagnostic matters, the failing path).
	local du_standalone_report_line du_standalone_assert_line
	du_standalone_report_line="$(rg -n --fixed-strings -- 'ifa_deployable_unit_live_report_intents_after_maintenance "${compose_project}"' "${deployable_unit_live_lib}" | cut -d: -f1 || true)"
	du_standalone_assert_line="$(rg -n --fixed-strings -- 'ifa_deployable_unit_live_assert "${bin_dir}" "${expected_edges}"' "${deployable_unit_live_lib}" | cut -d: -f1 || true)"
	[[ "${du_standalone_report_line}" =~ ^[0-9]+$ && "${du_standalone_assert_line}" =~ ^[0-9]+$ \
		&& "${du_standalone_report_line}" -lt "${du_standalone_assert_line}" ]] \
		|| fail "deployable-unit standalone cell: post-maintenance intents diagnostic (line ${du_standalone_report_line}) must run before the cell's final edge assertion (line ${du_standalone_assert_line}) in ${deployable_unit_live_lib}"
	# Fault cells need this diagnostic MORE than the standalone cell, not
	# less: a fault-cell failure has four candidate causes (writer drop,
	# correlation produced nothing, the injected fault did not recover, or
	# recovery raced the assertion) where the standalone cell only has two,
	# and the probe collapses the two hardest to tell apart. All three fault
	# cells (baseline, killworker, failgraphwrite) already call
	# ifa_deployable_unit_live_assert_empty_before_maintenance and run a
	# maintenance pass -- the after-probe was the only missing piece. Checks
	# the call appears exactly once PER CELL (not just once anywhere in the
	# file, which a single cell wiring it would also satisfy) and that each
	# cell's call precedes that SAME cell's own final edge assertion; the two
	# line-number lists come from one top-to-bottom scan of the file, so
	# pairing by index is safe only because the three cells' bodies do not
	# interleave.
	local du_report_lines du_assert_lines du_report_count du_assert_count fault_i
	du_report_lines=($(rg -n --fixed-strings -- 'ifa_deployable_unit_live_report_intents_after_maintenance "${FAULT_COMPOSE_PROJECT}"' "${deployable_unit_cells_lib}" | cut -d: -f1 || true))
	du_assert_lines=($(rg -n --fixed-strings -- 'ifa_deployable_unit_live_assert "${bin_dir}" "${deployable_unit_expected_edges}"' "${deployable_unit_cells_lib}" | cut -d: -f1 || true))
	du_report_count="${#du_report_lines[@]}"
	du_assert_count="${#du_assert_lines[@]}"
	[[ "${du_report_count}" -eq 3 ]] \
		|| fail "post-maintenance intents diagnostic must be wired into all three deployable-unit fault cells (baseline, killworker, failgraphwrite); found ${du_report_count} call site(s) in ${deployable_unit_cells_lib}"
	[[ "${du_assert_count}" -eq 3 ]] \
		|| fail "expected exactly 3 final deployable_unit_edges assert-edges call sites in the fault cells (one per cell); found ${du_assert_count} in ${deployable_unit_cells_lib}"
	for ((fault_i = 0; fault_i < du_report_count; fault_i++)); do
		[[ "${du_report_lines[fault_i]}" -lt "${du_assert_lines[fault_i]}" ]] \
			|| fail "deployable-unit fault cell: post-maintenance intents diagnostic (line ${du_report_lines[fault_i]}) must run before that cell's final edge assertion (line ${du_assert_lines[fault_i]}) in ${deployable_unit_cells_lib}"
	done
	for cell in cell_baseline_deployable_unit cell_killworker_deployable_unit cell_failgraphwrite_deployable_unit; do
		rg --quiet -- "^${cell}\$" "${script}" || fail "verifier does not INVOKE ${cell} on its own line"
	done
	# The baseline cell must dispatch before both fault cells.
	local baseline_du_line killworker_du_line failgraphwrite_du_line
	baseline_du_line="$(rg -n --line-regexp -- 'cell_baseline_deployable_unit' "${script}" | cut -d: -f1 || true)"
	killworker_du_line="$(rg -n --line-regexp -- 'cell_killworker_deployable_unit' "${script}" | cut -d: -f1 || true)"
	failgraphwrite_du_line="$(rg -n --line-regexp -- 'cell_failgraphwrite_deployable_unit' "${script}" | cut -d: -f1 || true)"
	[[ "${baseline_du_line}" =~ ^[0-9]+$ && "${killworker_du_line}" =~ ^[0-9]+$ && "${failgraphwrite_du_line}" =~ ^[0-9]+$ \
		&& "${baseline_du_line}" -lt "${killworker_du_line}" && "${baseline_du_line}" -lt "${failgraphwrite_du_line}" ]] \
		|| fail "cell_baseline_deployable_unit must be dispatched before both deployable-unit fault cells"
	require_deployable_unit_cells "baseline cell captures digests[baseline_deployable_unit]" "capture_digest baseline_deployable_unit"
	require_deployable_unit_cells "baseline cell captures the retry baseline" "baseline_deployable_unit_retried="
	require_deployable_unit_cells "pre-maintenance drain before the maintenance pass" "ifa_deployable_unit_live_assert_empty_before_maintenance"
	require_deployable_unit_cells "maintenance pass invocation" "ifa_deployable_unit_live_run_maintenance_pass"
	require_deployable_unit_cells "kill cell scopes the claimed-row wait to deployable_unit_correlation" '"deployable_unit_correlation")"'
	require_deployable_unit_cells "kill cell proves a retry above the family-scoped baseline" '"${baseline_deployable_unit_retried}"'
	require_deployable_unit_cells "graph-write cell selects queue-retry" '"queue-retry"'
	require_deployable_unit_cells "graph-write cell reads the durable marker, not a log" "ifa_fault_assert_once_fault_marker"
	require_deployable_unit_cells "fault cells compare against the family-scoped baseline, not the shared one" "assert_matches_baseline killworkerdeployableunit baseline_deployable_unit"
}
