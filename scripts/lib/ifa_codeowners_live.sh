#!/usr/bin/env bash
# Shared codeowners_ownership_edges live-gate helpers (#5992), mirroring
# scripts/lib/ifa_code_call_live.sh's shape exactly. This file is sourced by
# the determinism and fault-injection drivers; callers own strict mode and
# cleanup.

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
# KNOWN LIMIT (see materialized_edges_codeowners.go and
# materialized_edges_codeowners_property_gap_test.go, reported to the #5543
# coordinator): this assertion, like every other family's, is keyed on
# (relationship_type, source_uid, target_uid) only. It proves the live graph
# carries the right COUNT of DECLARES_CODEOWNER edges between each (repo,
# team) pair; it cannot by itself prove which rule's pattern/source_path
# produced which edge.
ifa_codeowners_assert() {
	local label="$1" bin_dir="$2" expected_edges="$3"
	printf '\n=== %s: assert codeowners-ownership materialized edges (five-edge exact set) ===\n' "${label}"
	"${bin_dir}/eshu-ifa" assert-edges \
		-domain codeowners_ownership_edges \
		-expected "${expected_edges}"
}
