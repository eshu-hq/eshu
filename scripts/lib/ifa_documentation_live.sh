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
# spurious live materialization. The committed expectation is a three-edge
# exact set: a Function target, a Class target, and a SqlTable target, all
# from the same DocumentationSection (proving the section->target dedup
# identity the extractor's `seen` map enforces), plus five mentions that must
# derive no edge at all (duplicate pair, ambiguous resolution, multi-candidate,
# service-kind target, blank section_id).
#
# The SqlTable edge closes issue #5994's production-truth gap:
# batchCanonicalDocumentationEntityEdgeCypher's MATCH label alternation
# (Function|Class|Struct|Interface|TypeAlias|Enum|File) used to exclude
# SqlTable, so a table-kind mention's DOCUMENTS edge silently no-opped against
# a live backend. Pinned RED by
# TestBuildDocumentationRowMapTableTargetMatchesSqlTableLabel (commit
# a3347e898), fixed by adding SqlTable to the label alternation
# (go/internal/storage/cypher/canonical_documentation_edges.go). TypeAlias and
# File documentation targets remain unproven for a different reason — their
# uid formats would have to be independently correct upstream, and no
# producer of documentation_entity_mention facts exists in-repo to verify
# against; this fix does not close that class.
ifa_documentation_assert() {
	local label="$1" bin_dir="$2" expected_edges="$3"
	printf '\n=== %s: assert documentation materialized edges (three-edge exact set) ===\n' "${label}"
	"${bin_dir}/eshu-ifa" assert-edges \
		-domain documentation_edges \
		-expected "${expected_edges}"
}
