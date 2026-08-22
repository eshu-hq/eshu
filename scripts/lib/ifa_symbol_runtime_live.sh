#!/usr/bin/env bash
# shellcheck shell=bash
# Shared symbol-runtime live-gate helpers for the handles_route (#5995),
# runs_in (#6000), and invokes_cloud_action (#5997) trio, mirroring
# scripts/lib/ifa_shell_exec_live.sh's shape. Callers own strict mode and
# cleanup.
#
# SOURCED by both scripts/verify-ifa-determinism.sh and
# scripts/verify-ifa-fault-injection.sh. All three families are registered
# through scripts/lib/ifa_family_registry/rows/{09_handles_route,10_runs_in,
# 11_invokes_cloud_action}.sh, which is what decides which cells drive them;
# this file only supplies the callbacks those rows name.
#
# ONE shared drive function for all three rows (they name the SAME
# IFA_FAMILY_DRIVE_FN/IFA_FAMILY_CASSETTE_VAR -- one cassette, one builder
# pass): the trio's rows
# all come from reducer.ExtractSymbolRuntimeIntentRows
# (go/internal/reducer/symbol_runtime_refresh_intents.go), which is called
# inside CodeCallMaterializationHandler.Handle, so driving the cassette once
# produces all three families' edges together.
#
# CELL-SCOPED IDEMPOTENT, guarded on "label" (the caller's cell identifier),
# NOT a bare global flag: scripts/verify-ifa-determinism.sh's shared N-loop
# calls ifa_family_registry_drive once per shared_cell=1 family per N, and
# all three of this trio's rows share this same drive_fn -- so within ONE
# N-cell (label="n${n}") it is called three times back to back. A plain
# global "already driven" boolean would correctly suppress calls 2 and 3
# there, but would ALSO wrongly suppress the drive when the NEXT N-cell's
# fresh stack (a genuinely different label) starts, since the whole gate
# runs every cell sequentially in one shell process. Keying on label instead
# means "already driven for this SAME fresh stack" is exactly what gets
# skipped: a repeat call with the identical label is a deliberate no-op --
# shared-projection intent IDs are deterministic and completed rows are
# never reopened, the same property
# ifa_fault_injection_sql_cells.sh's fresh-stack precondition comment relies
# on -- while a new label always drives. The fault-injection gate's own cells
# (scripts/lib/ifa_fault_injection_symbol_runtime_cells.sh) call this exactly
# once per cell with that cell's own name as label, so the guard is inert
# there by construction -- it exists for the determinism gate's repeat-label
# pattern.
declare -gA _ifa_symbol_runtime_driven_labels=()
ifa_symbol_runtime_drive() {
	local label="$1" bin_dir="$2" cassette="$3" workers="$4" log_dir="$5"
	if [[ -n "${_ifa_symbol_runtime_driven_labels[${label}]:-}" ]]; then
		printf '%s: symbol-runtime family cassette already driven for this cell (handles_route/runs_in/invokes_cloud_action share one cassette) -- skipping redundant drive\n' "${label}"
		return 0
	fi
	printf '\n=== %s: drive symbol-runtime family cassette (handles_route/runs_in/invokes_cloud_action, -workers %s) ===\n' "${label}" "${workers}"
	if ! "${bin_dir}/eshu-ifa" drive -cassette "${cassette}" -workers "${workers}" \
		>"${log_dir}/ifa-drive-symbol-runtime-${label}.log" 2>&1; then
		tail -40 "${log_dir}/ifa-drive-symbol-runtime-${label}.log" >&2 || true
		return 1
	fi
	cat "${log_dir}/ifa-drive-symbol-runtime-${label}.log"
	_ifa_symbol_runtime_driven_labels[${label}]=1
}

# ifa_handles_route_assert rejects an empty, incomplete, duplicated, or
# spurious live materialization. The committed expectation is the two-edge
# exact set: HandleWidgets' GET+POST /widgets collapses to ONE edge (the
# HANDLES_ROUTE MERGE identity has no method dimension -- see
# go/internal/ifa/testdata/handlesroute/ifa-handles-route-family-expected-edges.json's
# own note), plus HandleHealth's distinct GET /healthz edge. The exact-set
# assertion is what makes that collapse provable rather than assumed: a
# regression that stopped deduping the method pair would produce an EXTRA
# edge here, not merely "some edges".
ifa_handles_route_assert() {
	local label="$1" bin_dir="$2" expected_edges="$3"
	printf '\n=== %s: assert handles_route materialized edges (two-edge exact set) ===\n' "${label}"
	"${bin_dir}/eshu-ifa" assert-edges \
		-domain handles_route \
		-expected "${expected_edges}"
}

# ifa_runs_in_assert asserts the two-edge exact set: HandleWidgets and
# HandleHealth each bind to the repository's one Workload. The general
# N-Workload fan-out (no LIMIT in the live Cypher's
# (Repository)-[:DEFINES]->(Workload) MATCH) is proven by a synthetic offline
# unit test instead (materialized_edges_runs_in_test.go); this live fixture
# deliberately stays single-Workload, per the expected-edges fixture's own
# note.
ifa_runs_in_assert() {
	local label="$1" bin_dir="$2" expected_edges="$3"
	printf '\n=== %s: assert runs_in materialized edges (two-edge exact set) ===\n' "${label}"
	"${bin_dir}/eshu-ifa" assert-edges \
		-domain runs_in \
		-expected "${expected_edges}"
}

# ifa_invokes_cloud_action_assert asserts the one-edge exact set:
# InvokeAWS' (s3, PutObject) call resolves through the closed
# cloudActionByServiceMethod table; its second call, (widget, Frobnicate), is
# NOT in that table and must yield nothing. The exact-set assertion is what
# proves the non-catalog exclusion live rather than only in a synthetic test:
# a regression that stopped filtering on the closed table would produce an
# EXTRA edge here.
ifa_invokes_cloud_action_assert() {
	local label="$1" bin_dir="$2" expected_edges="$3"
	printf '\n=== %s: assert invokes_cloud_action materialized edges (one-edge exact set) ===\n' "${label}"
	"${bin_dir}/eshu-ifa" assert-edges \
		-domain invokes_cloud_action \
		-expected "${expected_edges}"
}
