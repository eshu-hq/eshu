#!/usr/bin/env bash
# SPDX-License-Identifier: MIT
# Copyright (c) 2025-2026 eshu-hq

set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
# shellcheck source=scripts/lib/golden-corpus-relationship-evidence.sh
source "${repo_root}/scripts/lib/golden-corpus-relationship-evidence.sh"

fail() { printf 'test-golden-corpus-relationship-evidence: %s\n' "$*" >&2; exit 1; }
die() { fail "$@"; }

case_dir="$(mktemp -d -t golden-relationship-evidence.XXXXXX)"
trap 'rm -rf "${case_dir}"' EXIT
sql_log="${case_dir}/sql.log"
mock_resolved_id="resolved_0123456789abcdef"

pg() {
	local sql="$1"
	printf '%s\n-- statement boundary --\n' "${sql}" >>"${sql_log}"
	printf '%s\n' "${mock_resolved_id}"
}

golden_relationship_evidence_capture_resolved_id
[[ "${golden_relationship_evidence_resolved_id}" == "${mock_resolved_id}" ]] ||
	fail "resolved relationship ID was not captured"
for predicate in \
	"g.status = 'active'" \
	"r.relationship_type = 'DEPLOYS_FROM'" \
	"r.source_repo_id = 'repository:r_217415d9'" \
	"r.target_repo_id = 'repository:r_1f68383d'"; do
	rg -Fq "${predicate}" "${sql_log}" || fail "selection SQL missing ${predicate}"
done
if rg -ni '\b(insert|update|delete|merge|truncate|create|alter|drop)\b' "${sql_log}" >/dev/null; then
	fail "leaf helper issued mutating SQL"
fi

input_snapshot="${case_dir}/prior transforms [input].json"
output_snapshot="${case_dir}/relationship evidence [output].json"
second_output="${case_dir}/relationship evidence [second].json"
jq -n \
	--arg sentinel "${golden_relationship_evidence_sentinel}" \
	'{
	  composition_marker: "preserved",
	  prior_runtime_sentinel: "__runtime_changed_since_prior_generation__",
	  query_shapes: {mcp: {get_relationship_evidence: {
	    arguments: {resolved_id: $sentinel},
	    required_json_values: {resolved_id: $sentinel},
	    nested_copy: [$sentinel, {again: $sentinel}]
	  }}}
	}' >"${input_snapshot}"

golden_relationship_evidence_compose_snapshot "${input_snapshot}" "${output_snapshot}"
golden_relationship_evidence_compose_snapshot "${input_snapshot}" "${second_output}"
cmp -s "${output_snapshot}" "${second_output}" || fail "runtime composition is not deterministic"
jq -e \
	--arg resolved "${mock_resolved_id}" \
	--arg sentinel "${golden_relationship_evidence_sentinel}" \
	'  .composition_marker == "preserved"
	and .prior_runtime_sentinel == "__runtime_changed_since_prior_generation__"
	and .query_shapes.mcp.get_relationship_evidence.arguments.resolved_id == $resolved
	and .query_shapes.mcp.get_relationship_evidence.required_json_values.resolved_id == $resolved
	and .query_shapes.mcp.get_relationship_evidence.nested_copy[0] == $resolved
	and .query_shapes.mcp.get_relationship_evidence.nested_copy[1].again == $resolved
	and ([.. | select(type == "string" and . == $sentinel)] | length) == 0' \
	"${output_snapshot}" >/dev/null || fail "runtime composition lost prior edits or left a sentinel"

in_place_snapshot="${case_dir}/in place.json"
cp "${input_snapshot}" "${in_place_snapshot}"
golden_relationship_evidence_compose_snapshot "${in_place_snapshot}" "${in_place_snapshot}"
cmp -s "${output_snapshot}" "${in_place_snapshot}" || fail "in-place composition changed output"

assert_subprocess_fails() {
	local name="$1" case_function="$2"
	if ("${case_function}" >/dev/null 2>&1); then
		fail "hostile case unexpectedly passed: ${name}"
	fi
}

hostile_zero_rows() {
	pg() { :; }
	golden_relationship_evidence_capture_resolved_id
}

hostile_multiple_rows() {
	pg() { printf 'resolved_0123456789abcdef\nresolved_fedcba9876543210\n'; }
	golden_relationship_evidence_capture_resolved_id
}

hostile_unsafe_id() {
	pg() { printf '%s\n' "resolved_0123456789abcde'"; }
	golden_relationship_evidence_capture_resolved_id
}

hostile_missing_sentinel() {
	local missing_snapshot="${case_dir}/missing sentinel.json"
	printf '{}\n' >"${missing_snapshot}"
	golden_relationship_evidence_resolved_id="${mock_resolved_id}"
	golden_relationship_evidence_compose_snapshot "${missing_snapshot}" "${case_dir}/unused.json"
}

hostile_malformed_snapshot() {
	local malformed_snapshot="${case_dir}/malformed.json"
	printf '{not-json\n' >"${malformed_snapshot}"
	golden_relationship_evidence_resolved_id="${mock_resolved_id}"
	golden_relationship_evidence_compose_snapshot "${malformed_snapshot}" "${case_dir}/unused-malformed.json"
}

assert_subprocess_fails "zero rows" hostile_zero_rows
assert_subprocess_fails "multiple rows" hostile_multiple_rows
assert_subprocess_fails "unsafe ID" hostile_unsafe_id
assert_subprocess_fails "missing sentinel" hostile_missing_sentinel
assert_subprocess_fails "malformed snapshot" hostile_malformed_snapshot

printf 'PASS: golden relationship-evidence leaf helper\n'
