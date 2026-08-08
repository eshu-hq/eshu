#!/usr/bin/env bash
# SPDX-License-Identifier: MIT
# Copyright (c) 2025-2026 eshu-hq
#
# Leaf fixture for the golden-corpus service changed-since proof. The caller
# captures the prior generation after the first drain, mutates the staged
# corpus, runs the existing maintenance drains, validates the resulting durable
# lineage, then composes the runtime generation IDs into its current snapshot.
# This file deliberately does not orchestrate drains or write database rows.
# shellcheck disable=SC2154

golden_service_changed_since_service_id="component:default/deployable-config"
golden_service_changed_since_old_owner="group:default/platform"
golden_service_changed_since_new_owner="group:default/runtime-platform"
golden_service_changed_since_prior_sentinel="__runtime_service_changed_since_prior_generation__"
golden_service_changed_since_current_sentinel="__runtime_service_changed_since_current_generation__"

golden_service_changed_since_capture_prior() {
	local generation
	generation="$(pg "
SELECT generation_id
FROM service_materialization_generations
WHERE service_id = '${golden_service_changed_since_service_id}'
  AND status = 'active';
")" || die "failed to read the prior service materialization generation"
	[[ -n "${generation}" && "${generation}" != *$'\n'* ]] ||
		die "expected exactly one active prior service generation, got: ${generation:-<empty>}"
	golden_service_changed_since_prior_generation="${generation}"
}

golden_service_changed_since_mutate_owner() {
	local fixture_repo="${corpus_dir}/deployable-config"
	local catalog_path="${corpus_dir}/deployable-config/catalog-info.yaml"
	local old_count new_count staged_state temporary
	[[ -f "${catalog_path}" ]] || die "service changed-since catalog fixture is missing"
	[[ -d "${fixture_repo}/.git" ]] || die "service changed-since fixture is not a staged Git repository"

	old_count="$(rg -Fxc "  owner: ${golden_service_changed_since_old_owner}" "${catalog_path}" || true)"
	new_count="$(rg -Fxc "  owner: ${golden_service_changed_since_new_owner}" "${catalog_path}" || true)"
	[[ "${old_count:-0}" == "1" && "${new_count:-0}" == "0" ]] ||
		die "service changed-since owner precondition failed (old=${old_count:-0}, new=${new_count:-0})"

	temporary="${catalog_path}.service-changed-since.tmp"
	sed "s|^  owner: ${golden_service_changed_since_old_owner}$|  owner: ${golden_service_changed_since_new_owner}|" \
		"${catalog_path}" >"${temporary}" || die "failed to rewrite staged catalog owner"
	mv "${temporary}" "${catalog_path}" || die "failed to install staged catalog owner"
	staged_state="$(git -C "${fixture_repo}" status --short --untracked-files=no)"
	[[ "${staged_state}" == " M catalog-info.yaml" ]] ||
		die "service changed-since mutation touched an unexpected path: ${staged_state:-<clean>}"
	git -C "${fixture_repo}" diff --check -- catalog-info.yaml >/dev/null ||
		die "service changed-since catalog mutation failed diff validation"
	git -C "${fixture_repo}" add -- catalog-info.yaml || die "failed to stage changed catalog owner"
	GIT_AUTHOR_DATE="2026-08-04T12:01:00Z" \
		GIT_COMMITTER_DATE="2026-08-04T12:01:00Z" \
		git -C "${fixture_repo}" commit -m "change deployable owner" >/dev/null ||
		die "failed to commit changed catalog owner in temporary corpus"
	[[ -z "$(git -C "${fixture_repo}" status --short)" ]] ||
		die "service changed-since temporary fixture is dirty after commit"
}

golden_service_changed_since_validate_current() {
	local current state total active superseded prior_status old_prior old_current
	local new_prior new_current adjacent_differences
	current="$(pg "
SELECT generation_id
FROM service_materialization_generations
WHERE service_id = '${golden_service_changed_since_service_id}'
  AND status = 'active';
")" || die "failed to read the current service materialization generation"
	[[ -n "${current}" && "${current}" != *$'\n'* ]] ||
		die "expected exactly one active current service generation, got: ${current:-<empty>}"
	[[ "${current}" != "${golden_service_changed_since_prior_generation}" ]] ||
		die "service changed-since current generation did not advance"

	state="$(pg "
WITH adjacent_differences AS (
  (SELECT evidence_family, service_evidence_key, payload_hash, is_tombstone
   FROM service_evidence_snapshots
   WHERE generation_id = '${golden_service_changed_since_prior_generation}'
     AND evidence_family <> 'ownership'
   EXCEPT
   SELECT evidence_family, service_evidence_key, payload_hash, is_tombstone
   FROM service_evidence_snapshots
   WHERE generation_id = '${current}'
     AND evidence_family <> 'ownership')
  UNION ALL
  (SELECT evidence_family, service_evidence_key, payload_hash, is_tombstone
   FROM service_evidence_snapshots
   WHERE generation_id = '${current}'
     AND evidence_family <> 'ownership'
   EXCEPT
   SELECT evidence_family, service_evidence_key, payload_hash, is_tombstone
   FROM service_evidence_snapshots
   WHERE generation_id = '${golden_service_changed_since_prior_generation}'
     AND evidence_family <> 'ownership')
)
SELECT
  (SELECT COUNT(*) FROM service_materialization_generations WHERE service_id = '${golden_service_changed_since_service_id}') || '|' ||
  (SELECT COUNT(*) FROM service_materialization_generations WHERE service_id = '${golden_service_changed_since_service_id}' AND status = 'active') || '|' ||
  (SELECT COUNT(*) FROM service_materialization_generations WHERE service_id = '${golden_service_changed_since_service_id}' AND status = 'superseded') || '|' ||
  (SELECT status FROM service_materialization_generations WHERE generation_id = '${golden_service_changed_since_prior_generation}') || '|' ||
  (SELECT COUNT(*) FROM service_evidence_snapshots WHERE generation_id = '${golden_service_changed_since_prior_generation}' AND service_evidence_key = 'ownership:component:default/deployable-config:group:default/platform') || '|' ||
  (SELECT COUNT(*) FROM service_evidence_snapshots WHERE generation_id = '${current}' AND service_evidence_key = 'ownership:component:default/deployable-config:group:default/platform') || '|' ||
  (SELECT COUNT(*) FROM service_evidence_snapshots WHERE generation_id = '${golden_service_changed_since_prior_generation}' AND service_evidence_key = 'ownership:component:default/deployable-config:group:default/runtime-platform') || '|' ||
  (SELECT COUNT(*) FROM service_evidence_snapshots WHERE generation_id = '${current}' AND service_evidence_key = 'ownership:component:default/deployable-config:group:default/runtime-platform') || '|' ||
  (SELECT COUNT(*) FROM adjacent_differences);
")" || die "failed to validate service changed-since durable lineage"
	IFS='|' read -r total active superseded prior_status old_prior old_current \
		new_prior new_current adjacent_differences <<<"${state}"
	[[ "${total}|${active}|${superseded}|${prior_status}|${old_prior}|${old_current}|${new_prior}|${new_current}|${adjacent_differences}" == \
		"2|1|1|superseded|1|0|0|1|0" ]] ||
		die "service changed-since durable lineage mismatch: ${state}"
	golden_service_changed_since_current_generation="${current}"
}

golden_service_changed_since_compose_snapshot() {
	local input_snapshot="$1" output_snapshot="$2" temporary
	[[ -f "${input_snapshot}" ]] || die "service changed-since input snapshot is missing"
	[[ -n "${golden_service_changed_since_prior_generation:-}" ]] || die "prior generation was not captured"
	[[ -n "${golden_service_changed_since_current_generation:-}" ]] || die "current generation was not validated"
	jq -e \
		--arg prior "${golden_service_changed_since_prior_sentinel}" \
		--arg current "${golden_service_changed_since_current_sentinel}" \
		'.query_shapes.mcp.get_service_changed_since.arguments.since_generation_id == $prior
		 and .query_shapes.mcp.get_service_changed_since.required_json_values.since_generation_id == $prior
		 and .query_shapes.mcp.get_service_changed_since.required_json_values.current_active_generation_id == $current' \
		"${input_snapshot}" >/dev/null || die "service changed-since runtime sentinels are missing"
	temporary="$(mktemp "${output_snapshot}.tmp.XXXXXX")" || die "failed to create runtime snapshot temporary file"
	jq \
		--arg prior "${golden_service_changed_since_prior_generation}" \
		--arg current "${golden_service_changed_since_current_generation}" \
		'.query_shapes.mcp.get_service_changed_since.arguments.since_generation_id = $prior
		 | .query_shapes.mcp.get_service_changed_since.required_json_values.since_generation_id = $prior
		 | .query_shapes.mcp.get_service_changed_since.required_json_values.current_active_generation_id = $current' \
		"${input_snapshot}" >"${temporary}" || {
		rm -f "${temporary}"
		die "failed to compose service changed-since runtime snapshot"
	}
	mv "${temporary}" "${output_snapshot}" || die "failed to install service changed-since runtime snapshot"
}
