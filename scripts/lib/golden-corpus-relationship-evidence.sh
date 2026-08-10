#!/usr/bin/env bash
# SPDX-License-Identifier: MIT
# Copyright (c) 2025-2026 eshu-hq
#
# Leaf support for the golden relationship-evidence query proof. After the
# caller completes maintenance, this helper selects the single active
# DEPLOYS_FROM row for the public fixture pair and composes its runtime ID into
# an arbitrary input snapshot. It performs no database writes and orchestrates
# no maintenance or query phases.

golden_relationship_evidence_source_repo_id="repository:r_217415d9"
golden_relationship_evidence_target_repo_id="repository:r_1f68383d"
golden_relationship_evidence_sentinel="__runtime_group_c_relationship_resolved_id__"

golden_relationship_evidence_require_resolved_id() {
	local value="$1"
	[[ "${value}" =~ ^resolved_[0-9a-f]{16}$ ]] ||
		die "expected exactly one public-safe resolved relationship ID, got: ${value:-<empty>}"
}

golden_relationship_evidence_capture_resolved_id() {
	local resolved_id
	resolved_id="$(pg "
SELECT r.resolved_id
FROM resolved_relationships AS r
JOIN relationship_generations AS g
  ON g.generation_id = r.generation_id
WHERE g.status = 'active'
  AND r.relationship_type = 'DEPLOYS_FROM'
  AND r.source_repo_id = '${golden_relationship_evidence_source_repo_id}'
  AND r.target_repo_id = '${golden_relationship_evidence_target_repo_id}'
ORDER BY r.resolved_id;
")" || die "failed to read the golden relationship-evidence resolved ID"
	golden_relationship_evidence_require_resolved_id "${resolved_id}"
	golden_relationship_evidence_resolved_id="${resolved_id}"
}

golden_relationship_evidence_compose_snapshot() {
	local input_snapshot="$1" output_snapshot="$2" temporary sentinel_count
	[[ -f "${input_snapshot}" ]] || die "relationship-evidence input snapshot is missing"
	golden_relationship_evidence_require_resolved_id "${golden_relationship_evidence_resolved_id:-}"
	sentinel_count="$(jq -er \
		--arg sentinel "${golden_relationship_evidence_sentinel}" \
		'[.. | select(type == "string" and . == $sentinel)] | length' \
		"${input_snapshot}")" || die "relationship-evidence input snapshot is not valid JSON"
	[[ "${sentinel_count}" =~ ^[1-9][0-9]*$ ]] ||
		die "relationship-evidence runtime sentinel is missing"

	temporary="$(mktemp "${output_snapshot}.tmp.XXXXXX")" ||
		die "failed to create relationship-evidence snapshot temporary file"
	jq \
		--arg sentinel "${golden_relationship_evidence_sentinel}" \
		--arg resolved "${golden_relationship_evidence_resolved_id}" \
		'walk(if type == "string" and . == $sentinel then $resolved else . end)' \
		"${input_snapshot}" >"${temporary}" || {
		rm -f "${temporary}"
		die "failed to compose relationship-evidence runtime snapshot"
	}
	mv "${temporary}" "${output_snapshot}" || {
		rm -f "${temporary}"
		die "failed to install relationship-evidence runtime snapshot"
	}
}
