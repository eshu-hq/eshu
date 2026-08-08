#!/usr/bin/env bash
# SPDX-License-Identifier: MIT
# Copyright (c) 2025-2026 eshu-hq
#
# Leaf fixture support for the repository changed-since golden proof. The caller
# captures the prior generation, mutates one staged source file, runs the
# existing maintenance cycle, validates durable lineage, and composes runtime
# generation IDs into an arbitrary input snapshot. This helper neither writes
# database rows nor orchestrates drains.
# shellcheck disable=SC2154

golden_changed_since_scope_id="git-repository-scope:repository:r_b11b6e25"
golden_changed_since_stable_fact_key="content:repository:r_b11b6e25:config/freshness.cfg"
golden_changed_since_prior_sentinel="__runtime_changed_since_prior_generation__"
golden_changed_since_current_sentinel="__runtime_changed_since_current_generation__"
golden_changed_since_old_marker='release_marker = "baseline"'
golden_changed_since_new_marker='release_marker = "current"'

golden_changed_since_require_generation_id() {
	local value="$1" context="$2"
	[[ -n "${value}" && "${value}" != *$'\n'* && "${value}" =~ ^[a-zA-Z0-9:._/-]+$ ]] ||
		die "${context} must be one public-safe generation ID, got: ${value:-<empty>}"
}

golden_changed_since_capture_prior() {
	local generation
	generation="$(pg "
SELECT active_generation_id
FROM ingestion_scopes
WHERE scope_id = '${golden_changed_since_scope_id}';
")" || die "failed to read the prior repository generation"
	golden_changed_since_require_generation_id "${generation}" "prior repository generation"
	golden_changed_since_prior_generation="${generation}"
}

golden_changed_since_mutate_fixture() {
	local fixture_path="${corpus_dir}/supply-chain-demo-db/config/freshness.cfg"
	local old_count new_count temporary
	[[ -f "${fixture_path}" ]] || die "repository changed-since fixture is missing"
	old_count="$(rg -Fxc "${golden_changed_since_old_marker}" "${fixture_path}" || true)"
	new_count="$(rg -Fxc "${golden_changed_since_new_marker}" "${fixture_path}" || true)"
	[[ "${old_count:-0}" == "1" && "${new_count:-0}" == "0" ]] ||
		die "repository changed-since marker precondition failed (old=${old_count:-0}, new=${new_count:-0})"
	temporary="$(mktemp "${fixture_path}.tmp.XXXXXX")" || die "failed to create fixture temporary file"
	sed "s|^${golden_changed_since_old_marker}$|${golden_changed_since_new_marker}|" \
		"${fixture_path}" >"${temporary}" || {
		rm -f "${temporary}"
		die "failed to rewrite staged freshness fixture"
	}
	mv "${temporary}" "${fixture_path}" || die "failed to install staged freshness fixture"
	[[ "$(rg -Fxc "${golden_changed_since_new_marker}" "${fixture_path}" || true)" == "1" ]] ||
		die "repository changed-since fixture mutation did not land exactly once"
}

golden_changed_since_validate_current() {
	local current state total active superseded prior_status target_updated unexpected_changed
	[[ -n "${golden_changed_since_prior_generation:-}" ]] || die "prior repository generation was not captured"
	current="$(pg "
SELECT active_generation_id
FROM ingestion_scopes
WHERE scope_id = '${golden_changed_since_scope_id}';
")" || die "failed to read the current repository generation"
	golden_changed_since_require_generation_id "${current}" "current repository generation"
	[[ "${current}" != "${golden_changed_since_prior_generation}" ]] ||
		die "repository changed-since current generation did not advance"

	state="$(pg "
WITH prior_keys AS (
  SELECT CASE WHEN fact_kind = 'file' THEN 'files'
              WHEN fact_kind = 'content_entity' THEN 'content_entities'
              ELSE 'facts' END AS category,
         stable_fact_key,
         MIN(md5(payload::text)) AS payload_hash
  FROM fact_records
  WHERE scope_id = '${golden_changed_since_scope_id}'
    AND generation_id = '${golden_changed_since_prior_generation}'
    AND is_tombstone = FALSE
  GROUP BY category, stable_fact_key
), current_keys AS (
  SELECT CASE WHEN fact_kind = 'file' THEN 'files'
              WHEN fact_kind = 'content_entity' THEN 'content_entities'
              ELSE 'facts' END AS category,
         stable_fact_key,
         MIN(md5(payload::text)) AS payload_hash
  FROM fact_records
  WHERE scope_id = '${golden_changed_since_scope_id}'
    AND generation_id = '${current}'
    AND is_tombstone = FALSE
  GROUP BY category, stable_fact_key
), current_tombstones AS (
  SELECT DISTINCT CASE WHEN fact_kind = 'file' THEN 'files'
                       WHEN fact_kind = 'content_entity' THEN 'content_entities'
                       ELSE 'facts' END AS category,
                  stable_fact_key
  FROM fact_records
  WHERE scope_id = '${golden_changed_since_scope_id}'
    AND generation_id = '${current}'
    AND is_tombstone = TRUE
), classified AS (
  SELECT COALESCE(prior.category, current.category) AS category,
         COALESCE(prior.stable_fact_key, current.stable_fact_key) AS stable_fact_key,
         CASE WHEN prior.stable_fact_key IS NULL THEN 'added'
              WHEN current.stable_fact_key IS NOT NULL
                   AND prior.payload_hash IS DISTINCT FROM current.payload_hash THEN 'updated'
              WHEN current.stable_fact_key IS NOT NULL THEN 'unchanged'
              WHEN tombstone.stable_fact_key IS NOT NULL THEN 'retired'
              ELSE 'superseded' END AS classification
  FROM prior_keys AS prior
  FULL OUTER JOIN current_keys AS current
    ON current.category = prior.category
   AND current.stable_fact_key = prior.stable_fact_key
  LEFT JOIN current_tombstones AS tombstone
    ON tombstone.category = COALESCE(prior.category, current.category)
   AND tombstone.stable_fact_key = COALESCE(prior.stable_fact_key, current.stable_fact_key)
)
SELECT
  (SELECT COUNT(*) FROM scope_generations WHERE scope_id = '${golden_changed_since_scope_id}') || '|' ||
  (SELECT COUNT(*) FROM scope_generations WHERE scope_id = '${golden_changed_since_scope_id}' AND status = 'active') || '|' ||
  (SELECT COUNT(*) FROM scope_generations WHERE scope_id = '${golden_changed_since_scope_id}' AND status = 'superseded') || '|' ||
  (SELECT status FROM scope_generations WHERE generation_id = '${golden_changed_since_prior_generation}') || '|' ||
  (SELECT COUNT(*) FROM classified WHERE category = 'facts' AND classification = 'updated' AND stable_fact_key = '${golden_changed_since_stable_fact_key}') || '|' ||
  (SELECT COUNT(*) FROM classified WHERE classification <> 'unchanged' AND NOT (
      category = 'facts' AND classification = 'updated' AND stable_fact_key = '${golden_changed_since_stable_fact_key}'
  ));
")" || die "failed to validate repository changed-since durable lineage"
	IFS='|' read -r total active superseded prior_status target_updated unexpected_changed <<<"${state}"
	[[ "${total}|${active}|${superseded}|${prior_status}|${target_updated}|${unexpected_changed}" == \
		"2|1|1|superseded|1|0" ]] ||
		die "repository changed-since durable lineage mismatch: ${state}"
	golden_changed_since_current_generation="${current}"
}

golden_changed_since_compose_snapshot() {
	local input_snapshot="$1" output_snapshot="$2" temporary
	[[ -f "${input_snapshot}" ]] || die "repository changed-since input snapshot is missing"
	golden_changed_since_require_generation_id "${golden_changed_since_prior_generation:-}" "prior repository generation"
	golden_changed_since_require_generation_id "${golden_changed_since_current_generation:-}" "current repository generation"
	jq -e \
		--arg prior "${golden_changed_since_prior_sentinel}" \
		--arg current "${golden_changed_since_current_sentinel}" \
		'.query_shapes.mcp.get_changed_since.arguments.since_generation_id == $prior
		 and .query_shapes.mcp.get_changed_since.required_json_values.since_generation_id == $prior
		 and .query_shapes.mcp.get_changed_since.required_json_values.current_active_generation_id == $current' \
		"${input_snapshot}" >/dev/null || die "repository changed-since runtime sentinels are missing"
	temporary="$(mktemp "${output_snapshot}.tmp.XXXXXX")" || die "failed to create runtime snapshot temporary file"
	jq \
		--arg prior "${golden_changed_since_prior_generation}" \
		--arg current "${golden_changed_since_current_generation}" \
		'.query_shapes.mcp.get_changed_since.arguments.since_generation_id = $prior
		 | .query_shapes.mcp.get_changed_since.required_json_values.since_generation_id = $prior
		 | .query_shapes.mcp.get_changed_since.required_json_values.current_active_generation_id = $current' \
		"${input_snapshot}" >"${temporary}" || {
		rm -f "${temporary}"
		die "failed to compose repository changed-since runtime snapshot"
	}
	mv "${temporary}" "${output_snapshot}" || die "failed to install repository changed-since runtime snapshot"
}
