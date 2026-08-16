#!/usr/bin/env bash
# Shared codeowners_ownership_edges live-gate helpers (#5992), mirroring
# scripts/lib/ifa_code_call_live.sh's shape exactly. Callers own strict mode
# and cleanup.
#
# NOT SOURCED YET. The sibling this mirrors carries "this file is sourced by
# the determinism and fault-injection drivers", which is true there and not
# here: neither scripts/verify-ifa-determinism.sh nor
# scripts/verify-ifa-fault-injection.sh references this file or the codeowners
# cassette, so nothing below has ever executed. Wiring it is the live-proof
# phase, tracked under #5543.

# ifa_codeowners_drive replays the committed family cassette into one matrix
# cell. The caller performs the aggregate fact_work_items non-vacuity check.
ifa_codeowners_drive() {
	local label="$1" bin_dir="$2" cassette="$3" workers="$4" log_dir="$5"
	printf '\n=== %s: drive codeowners-ownership family cassette (-workers %s) ===\n' "${label}" "${workers}"
	if ! "${bin_dir}/eshu-ifa" drive -cassette "${cassette}" -workers "${workers}" \
		>"${log_dir}/ifa-drive-codeowners-${label}.log" 2>&1; then
		tail -40 "${log_dir}/ifa-drive-codeowners-${label}.log" >&2 || true
		return 1
	fi
	cat "${log_dir}/ifa-drive-codeowners-${label}.log"
}

# ifa_codeowners_assert rejects an empty, incomplete, duplicated, or spurious
# live materialization. The committed expectation is the five-edge exact set
# (three to @org/docs, one to @org/backend, one to @org/infra) --
# codeowners_family_catalog.go's doc comment names which rule contributes
# which edge.
#
# This assertion is property-aware. assert-edges reads
# cypher.MaterializedEdgeIdentityProperties for the domain, so a live
# DECLARES_CODEOWNER edge is keyed on (relationship_type, source, target,
# pattern, source_path) -- not the bare triple this comment claimed before
# #6137 widened the mechanism. It proves WHICH rule produced which edge, so a
# dropped rule masked by an unrelated duplicate is caught rather than netting
# out in the count.
#
# KNOWN LIMIT: a permutation of identity properties among edges sharing one
# (source, target) pair preserves the multiset and is invisible. Reaching that
# class means widening the relationship MERGE key, which is a different
# change. The full statement of the boundary lives in
# materialized_edges_codeowners.go's guard doc comment and
# TestCodeownersOwnershipIdentityExcludesOrderIndex; it is deliberately not
# restated here, because the copy that drifts is the one nobody runs.
ifa_codeowners_assert() {
	local label="$1" bin_dir="$2" expected_edges="$3"
	printf '\n=== %s: assert codeowners-ownership materialized edges (five-edge exact set) ===\n' "${label}"
	"${bin_dir}/eshu-ifa" assert-edges \
		-domain codeowners_ownership_edges \
		-expected "${expected_edges}"
}
