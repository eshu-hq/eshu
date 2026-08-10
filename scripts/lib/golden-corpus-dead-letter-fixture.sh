#!/usr/bin/env bash
#
# golden-corpus-dead-letter-fixture.sh — one deterministic operator-query row.
#
# The pipeline's required drains must prove zero residual work before this helper
# runs. The fixture is therefore inserted only after the final maintenance drain
# and before API/MCP query assertions. It validates the read surface without
# weakening the pipeline-health invariant or depending on a real failure.

seed_golden_dead_letter_fixture() {
	local seeded
	seeded="$(
		pg "
WITH source_scope AS (
  SELECT scope.scope_id, scope.active_generation_id AS generation_id
  FROM ingestion_scopes AS scope
  JOIN scope_generations AS generation
    ON generation.generation_id = scope.active_generation_id
  WHERE scope.scope_kind = 'repository'
    AND COALESCE(scope.payload->>'name', scope.payload->>'repo_name', scope.payload->>'repo_slug') = 'orders-api'
    AND generation.status = 'active'
  LIMIT 1
), seeded AS (
  INSERT INTO fact_work_items (
    work_item_id, scope_id, generation_id, stage, domain,
    conflict_domain, conflict_key, status, attempt_count,
    visible_at, failure_class, failure_message, failure_details,
    payload, created_at, updated_at
  )
  SELECT
    'golden-dead-letter-fixture', scope_id, generation_id, 'reducer',
    'golden_fixture', 'scope', scope_id, 'dead_letter', 1,
    NOW(), 'golden_fixture', 'Synthetic golden-corpus operator-query fixture.',
    'Public-safe fixture inserted after required drains.',
    jsonb_build_object('fixture', 'golden_dead_letter'), NOW(), NOW()
  FROM source_scope
  ON CONFLICT (work_item_id) DO UPDATE
  SET scope_id = EXCLUDED.scope_id,
      generation_id = EXCLUDED.generation_id,
      stage = EXCLUDED.stage,
      domain = EXCLUDED.domain,
      conflict_domain = EXCLUDED.conflict_domain,
      conflict_key = EXCLUDED.conflict_key,
      status = EXCLUDED.status,
      attempt_count = EXCLUDED.attempt_count,
      lease_owner = NULL,
      claim_until = NULL,
      visible_at = EXCLUDED.visible_at,
      last_attempt_at = NULL,
      next_attempt_at = NULL,
      failure_class = EXCLUDED.failure_class,
      failure_message = EXCLUDED.failure_message,
      failure_details = EXCLUDED.failure_details,
      payload = EXCLUDED.payload,
      updated_at = EXCLUDED.updated_at
  RETURNING work_item_id
)
SELECT work_item_id FROM seeded;
"
	)"
	[[ "${seeded}" == "golden-dead-letter-fixture" ]] ||
		die "dead-letter fixture seed returned ${seeded:-no row}, want golden-dead-letter-fixture"
}
