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
golden_service_changed_since_deployment_added_sentinel="__runtime_service_changed_since_deployment_added_count__"
golden_service_changed_since_deployment_updated_sentinel="__runtime_service_changed_since_deployment_updated_count__"
golden_service_changed_since_deployment_unchanged_sentinel="__runtime_service_changed_since_deployment_unchanged_count__"
golden_service_changed_since_deployment_retired_sentinel="__runtime_service_changed_since_deployment_retired_count__"
golden_service_changed_since_deployment_superseded_sentinel="__runtime_service_changed_since_deployment_superseded_count__"

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
	local baseline_state catalog_change_count old_count new_count staged_state staged_without_catalog status_line temporary
	[[ -f "${catalog_path}" ]] || die "service changed-since catalog fixture is missing"
	[[ -d "${fixture_repo}/.git" ]] || die "service changed-since fixture is not a staged Git repository"
	baseline_state="$(git -C "${fixture_repo}" status --short --untracked-files=no)"

	old_count=0
	new_count=0
	while IFS= read -r status_line; do
		[[ "${status_line}" == "  owner: ${golden_service_changed_since_old_owner}" ]] && ((old_count += 1))
		[[ "${status_line}" == "  owner: ${golden_service_changed_since_new_owner}" ]] && ((new_count += 1))
	done <"${catalog_path}"
	[[ "${old_count:-0}" == "1" && "${new_count:-0}" == "0" ]] ||
		die "service changed-since owner precondition failed (old=${old_count:-0}, new=${new_count:-0})"

	temporary="${catalog_path}.service-changed-since.tmp"
	sed "s|^  owner: ${golden_service_changed_since_old_owner}$|  owner: ${golden_service_changed_since_new_owner}|" \
		"${catalog_path}" >"${temporary}" || die "failed to rewrite staged catalog owner"
	mv "${temporary}" "${catalog_path}" || die "failed to install staged catalog owner"
	staged_state="$(git -C "${fixture_repo}" status --short --untracked-files=no)"
	catalog_change_count=0
	staged_without_catalog=""
	while IFS= read -r status_line; do
		if [[ "${status_line}" == " M catalog-info.yaml" ]]; then
			((catalog_change_count += 1))
		elif [[ -n "${status_line}" ]]; then
			staged_without_catalog+="${staged_without_catalog:+$'\n'}${status_line}"
		fi
	done <<<"${staged_state}"
	[[ "${catalog_change_count:-0}" == "1" && "${staged_without_catalog}" == "${baseline_state}" ]] ||
		die "service changed-since mutation touched an unexpected path: ${staged_state:-<clean>}"
	git -C "${fixture_repo}" diff --check -- catalog-info.yaml >/dev/null ||
		die "service changed-since catalog mutation failed diff validation"
	git -C "${fixture_repo}" add -- catalog-info.yaml || die "failed to stage changed catalog owner"
	# Identity pinned inline, matching stage_deterministic_git_fixture
	# (scripts/lib/golden-corpus-stage.sh): git prefers GIT_AUTHOR_*/
	# GIT_COMMITTER_* from the environment over `git config user.*`, so an
	# inherited identity would otherwise move this commit's SHA without
	# touching its tree.
	GIT_AUTHOR_NAME="Golden Gate" \
		GIT_AUTHOR_EMAIL="gate@eshu.local" \
		GIT_COMMITTER_NAME="Golden Gate" \
		GIT_COMMITTER_EMAIL="gate@eshu.local" \
		GIT_AUTHOR_DATE="2026-08-04T12:01:00Z" \
		GIT_COMMITTER_DATE="2026-08-04T12:01:00Z" \
		git -C "${fixture_repo}" commit -m "change deployable owner" >/dev/null ||
		die "failed to commit changed catalog owner in temporary corpus"
	[[ "$(git -C "${fixture_repo}" status --short --untracked-files=no)" == "${baseline_state}" ]] ||
		die "service changed-since temporary fixture changed pre-existing status after commit"
}

golden_service_changed_since_capture_deployment_counts() {
	local prior_generation="$1" current_generation="$2" state count
	local added updated unchanged retired superseded extra
	state="$(pg "
WITH prior_keys AS (
  SELECT evidence_family, service_evidence_key, payload_hash
  FROM service_evidence_snapshots
  WHERE generation_id = '${prior_generation}'
    AND is_tombstone = FALSE
),
current_active_keys AS (
  SELECT evidence_family, service_evidence_key, payload_hash
  FROM service_evidence_snapshots
  WHERE generation_id = '${current_generation}'
    AND is_tombstone = FALSE
),
current_tombstones AS (
  SELECT DISTINCT evidence_family, service_evidence_key
  FROM service_evidence_snapshots
  WHERE generation_id = '${current_generation}'
    AND is_tombstone = TRUE
),
classified AS (
  SELECT
    COALESCE(prior.evidence_family, current.evidence_family) AS evidence_family,
    CASE
      WHEN prior.service_evidence_key IS NULL THEN 'added'
      WHEN current.service_evidence_key IS NOT NULL
        AND prior.payload_hash IS DISTINCT FROM current.payload_hash THEN 'updated'
      WHEN current.service_evidence_key IS NOT NULL THEN 'unchanged'
      WHEN tombstone.service_evidence_key IS NOT NULL THEN 'retired'
      ELSE 'superseded'
    END AS classification
  FROM prior_keys AS prior
  FULL OUTER JOIN current_active_keys AS current
    ON current.evidence_family = prior.evidence_family
   AND current.service_evidence_key = prior.service_evidence_key
  LEFT JOIN current_tombstones AS tombstone
    ON tombstone.evidence_family = COALESCE(prior.evidence_family, current.evidence_family)
   AND tombstone.service_evidence_key = COALESCE(prior.service_evidence_key, current.service_evidence_key)
)
SELECT
  COUNT(*) FILTER (WHERE evidence_family = 'deployment' AND classification = 'added') || '|' ||
  COUNT(*) FILTER (WHERE evidence_family = 'deployment' AND classification = 'updated') || '|' ||
  COUNT(*) FILTER (WHERE evidence_family = 'deployment' AND classification = 'unchanged') || '|' ||
  COUNT(*) FILTER (WHERE evidence_family = 'deployment' AND classification = 'retired') || '|' ||
  COUNT(*) FILTER (WHERE evidence_family = 'deployment' AND classification = 'superseded')
FROM classified;
")" || die "failed to capture service changed-since deployment counts"
	IFS='|' read -r added updated unchanged retired superseded extra <<<"${state}"
	[[ -z "${extra:-}" ]] || die "service changed-since deployment counts returned too many fields: ${state}"
	for count in "${added}" "${updated}" "${unchanged}" "${retired}" "${superseded}"; do
		[[ "${count}" =~ ^[0-9]+$ ]] ||
			die "service changed-since deployment count is not a non-negative integer: ${state:-<empty>}"
	done
	golden_service_changed_since_deployment_added_count="${added}"
	golden_service_changed_since_deployment_updated_count="${updated}"
	golden_service_changed_since_deployment_unchanged_count="${unchanged}"
	golden_service_changed_since_deployment_retired_count="${retired}"
	golden_service_changed_since_deployment_superseded_count="${superseded}"
}

golden_service_changed_since_validate_current() {
	local current state total active superseded prior_status old_prior old_current
	local new_prior new_current
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

	# The staged Git mutation is already restricted to the one owner line. Later
	# maintenance may legitimately settle other derived evidence families between
	# the two materialization generations, so lineage proof must not require their
	# payloads to remain byte-identical.
	state="$(pg "
SELECT
  (SELECT COUNT(*) FROM service_materialization_generations WHERE service_id = '${golden_service_changed_since_service_id}') || '|' ||
  (SELECT COUNT(*) FROM service_materialization_generations WHERE service_id = '${golden_service_changed_since_service_id}' AND status = 'active') || '|' ||
  (SELECT COUNT(*) FROM service_materialization_generations WHERE service_id = '${golden_service_changed_since_service_id}' AND status = 'superseded') || '|' ||
  (SELECT status FROM service_materialization_generations WHERE generation_id = '${golden_service_changed_since_prior_generation}') || '|' ||
  (SELECT COUNT(*) FROM service_evidence_snapshots WHERE generation_id = '${golden_service_changed_since_prior_generation}' AND service_evidence_key = 'ownership:component:default/deployable-config:group:default/platform') || '|' ||
  (SELECT COUNT(*) FROM service_evidence_snapshots WHERE generation_id = '${current}' AND service_evidence_key = 'ownership:component:default/deployable-config:group:default/platform') || '|' ||
  (SELECT COUNT(*) FROM service_evidence_snapshots WHERE generation_id = '${golden_service_changed_since_prior_generation}' AND service_evidence_key = 'ownership:component:default/deployable-config:group:default/runtime-platform') || '|' ||
  (SELECT COUNT(*) FROM service_evidence_snapshots WHERE generation_id = '${current}' AND service_evidence_key = 'ownership:component:default/deployable-config:group:default/runtime-platform');
")" || die "failed to validate service changed-since durable lineage"
	IFS='|' read -r total active superseded prior_status old_prior old_current \
		new_prior new_current <<<"${state}"
	[[ "${total}|${active}|${superseded}|${prior_status}|${old_prior}|${old_current}|${new_prior}|${new_current}" == \
		"2|1|1|superseded|1|0|0|1" ]] ||
		die "service changed-since durable lineage mismatch: ${state}"
	golden_service_changed_since_capture_deployment_counts \
		"${golden_service_changed_since_prior_generation}" "${current}"
	golden_service_changed_since_current_generation="${current}"
}

golden_service_changed_since_compose_snapshot() {
	local input_snapshot="$1" output_snapshot="$2" temporary count_name count_variable
	[[ -f "${input_snapshot}" ]] || die "service changed-since input snapshot is missing"
	[[ -n "${golden_service_changed_since_prior_generation:-}" ]] || die "prior generation was not captured"
	[[ -n "${golden_service_changed_since_current_generation:-}" ]] || die "current generation was not validated"
	for count_name in added updated unchanged retired superseded; do
		count_variable="golden_service_changed_since_deployment_${count_name}_count"
		[[ -n "${!count_variable:-}" ]] || die "deployment ${count_name} count was not captured"
	done
	jq -e \
		--arg prior "${golden_service_changed_since_prior_sentinel}" \
		--arg current "${golden_service_changed_since_current_sentinel}" \
		--arg deployment_added "${golden_service_changed_since_deployment_added_sentinel}" \
		--arg deployment_updated "${golden_service_changed_since_deployment_updated_sentinel}" \
		--arg deployment_unchanged "${golden_service_changed_since_deployment_unchanged_sentinel}" \
		--arg deployment_retired "${golden_service_changed_since_deployment_retired_sentinel}" \
		--arg deployment_superseded "${golden_service_changed_since_deployment_superseded_sentinel}" \
		'.query_shapes.mcp.get_service_changed_since.arguments.since_generation_id == $prior
		 and .query_shapes.mcp.get_service_changed_since.required_json_values.since_generation_id == $prior
		 and .query_shapes.mcp.get_service_changed_since.required_json_values.current_active_generation_id == $current
		 and ([.query_shapes.mcp.get_service_changed_since.required_json_object_matches["categories[]"][]
		       | select(.category == "deployment")] | length) == 1
		 and (.query_shapes.mcp.get_service_changed_since.required_json_object_matches["categories[]"][]
		      | select(.category == "deployment").counts) == {
		        added: $deployment_added,
		        updated: $deployment_updated,
		        unchanged: $deployment_unchanged,
		        retired: $deployment_retired,
		        superseded: $deployment_superseded
		      }' \
		"${input_snapshot}" >/dev/null || die "service changed-since runtime sentinels are missing"
	temporary="$(mktemp "${output_snapshot}.tmp.XXXXXX")" || die "failed to create runtime snapshot temporary file"
	jq \
		--arg prior "${golden_service_changed_since_prior_generation}" \
		--arg current "${golden_service_changed_since_current_generation}" \
		--argjson deployment_added "${golden_service_changed_since_deployment_added_count}" \
		--argjson deployment_updated "${golden_service_changed_since_deployment_updated_count}" \
		--argjson deployment_unchanged "${golden_service_changed_since_deployment_unchanged_count}" \
		--argjson deployment_retired "${golden_service_changed_since_deployment_retired_count}" \
		--argjson deployment_superseded "${golden_service_changed_since_deployment_superseded_count}" \
		'.query_shapes.mcp.get_service_changed_since.arguments.since_generation_id = $prior
		 | .query_shapes.mcp.get_service_changed_since.required_json_values.since_generation_id = $prior
		 | .query_shapes.mcp.get_service_changed_since.required_json_values.current_active_generation_id = $current
		 | (.query_shapes.mcp.get_service_changed_since.required_json_object_matches["categories[]"][]
		    | select(.category == "deployment").counts) = {
		      added: $deployment_added,
		      updated: $deployment_updated,
		      unchanged: $deployment_unchanged,
		      retired: $deployment_retired,
		      superseded: $deployment_superseded
		    }' \
		"${input_snapshot}" >"${temporary}" || {
		rm -f "${temporary}"
		die "failed to compose service changed-since runtime snapshot"
	}
	mv "${temporary}" "${output_snapshot}" || die "failed to install service changed-since runtime snapshot"
}
