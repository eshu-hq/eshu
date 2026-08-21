#!/usr/bin/env bash
# Shared submodule_pin_edges live-gate helpers (#6002), mirroring
# scripts/lib/ifa_codeowners_live.sh's shape exactly. Callers own strict mode
# and cleanup.
#
# SOURCED by both scripts/verify-ifa-determinism.sh and
# scripts/verify-ifa-fault-injection.sh. On the determinism gate, the shared
# N={1,2,4} loop dispatches these two functions by name through
# scripts/lib/ifa_family_registry.sh's IFA_FAMILY_DRIVE_FN/ASSERT_FN
# indirection (row: scripts/lib/ifa_family_registry/rows/07_submodule_pin_edges.sh).
# On the fault-injection gate, scripts/lib/ifa_fault_injection_submodule_pin_cells.sh
# calls ifa_submodule_pin_drive/ifa_submodule_pin_assert directly from its
# three cells (baseline, kill-worker-after-claim, fail-graph-write-once-then-
# succeed) -- see that file's own header for why those cells are hand-written
# rather than routed through the generic table_lock path.

# ifa_submodule_pin_drive replays the committed family cassette into one
# matrix cell. The caller performs the aggregate fact_work_items non-vacuity
# check.
ifa_submodule_pin_drive() {
	local label="$1" bin_dir="$2" cassette="$3" workers="$4" log_dir="$5"
	printf '\n=== %s: drive submodule-pin family cassette (-workers %s) ===\n' "${label}" "${workers}"
	if ! "${bin_dir}/eshu-ifa" drive -cassette "${cassette}" -workers "${workers}" \
		>"${log_dir}/ifa-drive-submodule-pin-${label}.log" 2>&1; then
		tail -40 "${log_dir}/ifa-drive-submodule-pin-${label}.log" >&2 || true
		return 1
	fi
	cat "${log_dir}/ifa-drive-submodule-pin-${label}.log"
}

# ifa_submodule_pin_assert rejects an empty, incomplete, duplicated, or
# spurious live materialization. The committed expectation is the three-edge
# exact set (go/internal/ifa/testdata/submodulepin/ifa-submodule-pin-family-expected-edges.json):
# two PINS_SUBMODULE edges from the family repo to one target at two distinct
# paths, plus one edge to a second target -- proving the {path} identity
# property keeps the two same-target edges distinct rather than collapsing
# onto one relationship. Each expected edge also asserts its SET-only
# pinned_sha. PIN A therefore requires its explicit current SHA, proving stale
# SET-only graph truth cannot pass behind the correct edge count and identity.
#
# This assertion is property-aware. assert-edges reads
# cypher.MaterializedEdgeIdentityProperties for the domain, so a live
# PINS_SUBMODULE edge is keyed on (relationship_type, source, target, path) --
# batchCanonicalSubmodulePinEdgeCypher's MERGE key
# (go/internal/storage/cypher/canonical_submodule_edges.go), mirroring
# codeowners_ownership_edges' identical (pattern, source_path) reasoning for
# DECLARES_CODEOWNER. It proves WHICH gitmodules entry produced which edge and
# which pinned commit survived duplicate reduction, so a dropped path or stale
# pinned_sha is caught rather than netting out in the count.
ifa_submodule_pin_assert() {
	local label="$1" bin_dir="$2" expected_edges="$3"
	printf '\n=== %s: assert submodule-pin materialized edges (three-edge exact set) ===\n' "${label}"
	"${bin_dir}/eshu-ifa" assert-edges \
		-domain submodule_pin_edges \
		-expected "${expected_edges}"
}
