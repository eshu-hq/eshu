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
