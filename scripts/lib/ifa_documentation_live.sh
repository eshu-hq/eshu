#!/usr/bin/env bash
# Shared documentation_edges live-gate helpers (#5994). This file is sourced by
# the determinism and fault-injection drivers; callers own strict mode and
# cleanup. Mirrors scripts/lib/ifa_code_call_live.sh's shape exactly, one
# family later.

# ifa_documentation_drive replays the committed family cassette into one
# matrix cell. The caller performs the aggregate fact_work_items non-vacuity
# check.
ifa_documentation_drive() {
	local label="$1" bin_dir="$2" cassette="$3" workers="$4" log_dir="$5"
	printf '\n=== %s: drive documentation family cassette (-workers %s) ===\n' "${label}" "${workers}"
	if ! "${bin_dir}/eshu-ifa" drive -cassette "${cassette}" -workers "${workers}" \
		>"${log_dir}/ifa-drive-documentation-${label}.log" 2>&1; then
		tail -40 "${log_dir}/ifa-drive-documentation-${label}.log" >&2 || true
		return 1
	fi
	cat "${log_dir}/ifa-drive-documentation-${label}.log"
}

# ifa_documentation_assert rejects an empty, incomplete, duplicated, or
# spurious live materialization. The committed expectation is a two-edge exact
# set: one Function target and one Class target from the same
# DocumentationSection (proving the section->target dedup identity the
# extractor's `seen` map enforces), plus five mentions that must derive no
# edge at all (duplicate pair, ambiguous resolution, multi-candidate,
# service-kind target, blank section_id).
#
# A table-kind (SqlTable) target is deliberately absent from this set:
# batchCanonicalDocumentationEntityEdgeCypher's MATCH label alternation
# (Function|Class|Struct|Interface|TypeAlias|Enum|File) excludes SqlTable, so
# that edge cannot materialize against a live backend today. Pinned RED by
# TestBuildDocumentationRowMapTableTargetMatchesSqlTableLabel (commit
# a3347e898); the fix (widen the writer's MATCH, or drop the offline fixture's
# table-kind case) is a production-truth decision still under owner review
# (#5994) and is independent of this live lane.
ifa_documentation_assert() {
	local label="$1" bin_dir="$2" expected_edges="$3"
	printf '\n=== %s: assert documentation materialized edges (two-edge exact set) ===\n' "${label}"
	"${bin_dir}/eshu-ifa" assert-edges \
		-domain documentation_edges \
		-expected "${expected_edges}"
}
