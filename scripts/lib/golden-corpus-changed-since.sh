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
golden_changed_since_added_sentinel="__runtime_changed_since_facts_added_count__"
golden_changed_since_updated_sentinel="__runtime_changed_since_facts_updated_count__"
golden_changed_since_unchanged_sentinel="__runtime_changed_since_facts_unchanged_count__"
golden_changed_since_retired_sentinel="__runtime_changed_since_facts_retired_count__"
golden_changed_since_superseded_sentinel="__runtime_changed_since_facts_superseded_count__"
golden_changed_since_old_marker='release_marker = "baseline"'
golden_changed_since_new_marker='release_marker = "current"'

golden_changed_since_require_generation_id() {
	local value="$1" context="$2"
	[[ -n "${value}" && "${value}" != *$'\n'* && "${value}" =~ ^[a-zA-Z0-9:._/-]+$ ]] ||
		die "${context} must be one public-safe generation ID, got: ${value:-<empty>}"
}

golden_changed_since_require_count() {
	local value="$1" context="$2"
	[[ "${value}" =~ ^[0-9]+$ ]] || die "${context} must be one non-negative integer, got: ${value:-<empty>}"
}

# golden_changed_since_count_marker prints how many lines of file match
# marker via `rg -Fxc` and dies (via die) on any rg failure OTHER than rg's
# own genuine "no match" exit code (1) -- notably exit 127 ("command not
# found" when rg is missing from PATH). Before this, `rg -Fxc ... || true`
# folded EVERY rg failure into an empty/zero count indistinguishably from a
# real zero match, so a missing rg silently produced a misleading
# "(old=0, new=0)" precondition failure instead of naming the actual missing
# tool (#6401).
golden_changed_since_count_marker() {
	local marker="$1" file="$2" count status
	count="$(rg -Fxc -- "${marker}" "${file}")" && status=0 || status=$?
	case "${status}" in
		0) printf '%s\n' "${count}" ;;
		1) printf '0\n' ;;
		*) die "changed-since marker count failed (rg exited ${status} on ${file}); rg is a required tool -- confirm it is installed and on PATH" ;;
	esac
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
	old_count="$(golden_changed_since_count_marker "${golden_changed_since_old_marker}" "${fixture_path}")"
	new_count="$(golden_changed_since_count_marker "${golden_changed_since_new_marker}" "${fixture_path}")"
	[[ "${old_count}" == "1" && "${new_count}" == "0" ]] ||
		die "repository changed-since marker precondition failed (old=${old_count}, new=${new_count})"
	temporary="$(mktemp "${fixture_path}.tmp.XXXXXX")" || die "failed to create fixture temporary file"
	sed "s|^${golden_changed_since_old_marker}$|${golden_changed_since_new_marker}|" \
		"${fixture_path}" >"${temporary}" || {
		rm -f "${temporary}"
		die "failed to rewrite staged freshness fixture"
	}
	mv "${temporary}" "${fixture_path}" || die "failed to install staged freshness fixture"
	[[ "$(golden_changed_since_count_marker "${golden_changed_since_new_marker}" "${fixture_path}")" == "1" ]] ||
		die "repository changed-since fixture mutation did not land exactly once"
}

golden_changed_since_validate_current() {
	local current state total active superseded prior_status target_updated
	local facts_added facts_updated facts_unchanged facts_retired facts_superseded
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
  (SELECT COUNT(*) FROM classified WHERE category = 'facts' AND classification = 'added') || '|' ||
  (SELECT COUNT(*) FROM classified WHERE category = 'facts' AND classification = 'updated') || '|' ||
  (SELECT COUNT(*) FROM classified WHERE category = 'facts' AND classification = 'unchanged') || '|' ||
  (SELECT COUNT(*) FROM classified WHERE category = 'facts' AND classification = 'retired') || '|' ||
  (SELECT COUNT(*) FROM classified WHERE category = 'facts' AND classification = 'superseded');
")" || die "failed to validate repository changed-since durable lineage"
	IFS='|' read -r total active superseded prior_status target_updated \
		facts_added facts_updated facts_unchanged facts_retired facts_superseded <<<"${state}"
	[[ "${total}|${active}|${superseded}|${prior_status}|${target_updated}" == "2|1|1|superseded|1" ]] ||
		die "repository changed-since durable lineage mismatch: ${state}"
	golden_changed_since_require_count "${facts_added}" "repository facts added count"
	golden_changed_since_require_count "${facts_updated}" "repository facts updated count"
	golden_changed_since_require_count "${facts_unchanged}" "repository facts unchanged count"
	golden_changed_since_require_count "${facts_retired}" "repository facts retired count"
	golden_changed_since_require_count "${facts_superseded}" "repository facts superseded count"
	((facts_updated > 0 && facts_updated <= 200)) ||
		die "repository facts updated count must fit the bounded sample, got: ${facts_updated}"
	golden_changed_since_current_generation="${current}"
	golden_changed_since_facts_added_count="${facts_added}"
	golden_changed_since_facts_updated_count="${facts_updated}"
	golden_changed_since_facts_unchanged_count="${facts_unchanged}"
	golden_changed_since_facts_retired_count="${facts_retired}"
	golden_changed_since_facts_superseded_count="${facts_superseded}"
}

golden_changed_since_compose_snapshot() {
	local input_snapshot="$1" output_snapshot="$2" temporary
	[[ -f "${input_snapshot}" ]] || die "repository changed-since input snapshot is missing"
	golden_changed_since_require_generation_id "${golden_changed_since_prior_generation:-}" "prior repository generation"
	golden_changed_since_require_generation_id "${golden_changed_since_current_generation:-}" "current repository generation"
	golden_changed_since_require_count "${golden_changed_since_facts_added_count:-}" "repository facts added count"
	golden_changed_since_require_count "${golden_changed_since_facts_updated_count:-}" "repository facts updated count"
	golden_changed_since_require_count "${golden_changed_since_facts_unchanged_count:-}" "repository facts unchanged count"
	golden_changed_since_require_count "${golden_changed_since_facts_retired_count:-}" "repository facts retired count"
	golden_changed_since_require_count "${golden_changed_since_facts_superseded_count:-}" "repository facts superseded count"
	jq -e \
		--arg prior "${golden_changed_since_prior_sentinel}" \
		--arg current "${golden_changed_since_current_sentinel}" \
		--arg added "${golden_changed_since_added_sentinel}" \
		--arg updated "${golden_changed_since_updated_sentinel}" \
		--arg unchanged "${golden_changed_since_unchanged_sentinel}" \
		--arg retired "${golden_changed_since_retired_sentinel}" \
		--arg superseded "${golden_changed_since_superseded_sentinel}" \
		'.query_shapes.mcp.get_changed_since.arguments.since_generation_id == $prior
		 and .query_shapes.mcp.get_changed_since.required_json_values.since_generation_id == $prior
		 and .query_shapes.mcp.get_changed_since.required_json_values.current_active_generation_id == $current
		 and .query_shapes.mcp.get_changed_since.required_json_object_matches["categories[]"][0].counts == {
		   added: $added, updated: $updated, unchanged: $unchanged,
		   retired: $retired, superseded: $superseded
		 }' \
		"${input_snapshot}" >/dev/null || die "repository changed-since runtime sentinels are missing"
	temporary="$(mktemp "${output_snapshot}.tmp.XXXXXX")" || die "failed to create runtime snapshot temporary file"
	jq \
		--arg prior "${golden_changed_since_prior_generation}" \
		--arg current "${golden_changed_since_current_generation}" \
		--argjson added "${golden_changed_since_facts_added_count}" \
		--argjson updated "${golden_changed_since_facts_updated_count}" \
		--argjson unchanged "${golden_changed_since_facts_unchanged_count}" \
		--argjson retired "${golden_changed_since_facts_retired_count}" \
		--argjson superseded "${golden_changed_since_facts_superseded_count}" \
		'.query_shapes.mcp.get_changed_since.arguments.since_generation_id = $prior
		 | .query_shapes.mcp.get_changed_since.required_json_values.since_generation_id = $prior
		 | .query_shapes.mcp.get_changed_since.required_json_values.current_active_generation_id = $current
		 | .query_shapes.mcp.get_changed_since.required_json_object_matches["categories[]"][0].counts = {
		     added: $added, updated: $updated, unchanged: $unchanged,
		     retired: $retired, superseded: $superseded
		   }' \
		"${input_snapshot}" >"${temporary}" || {
		rm -f "${temporary}"
		die "failed to compose repository changed-since runtime snapshot"
	}
	mv "${temporary}" "${output_snapshot}" || die "failed to install repository changed-since runtime snapshot"
}
