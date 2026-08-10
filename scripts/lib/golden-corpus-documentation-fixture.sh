#!/usr/bin/env bash
#
# golden-corpus-documentation-fixture.sh — deterministic documentation query facts.
#
# The pipeline's required drains must prove zero residual work before this helper
# runs. These public-safe facts are inserted into an active corpus generation
# after the final drain and before API/MCP query assertions. The three rows use
# only registered documentation fact kinds and their existing payload contracts.

seed_golden_documentation_fixture() {
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
), fixture AS (
  SELECT
    'golden-documentation-section'::text AS fact_id,
    'documentation_section'::text AS fact_kind,
    'golden-documentation-section'::text AS stable_fact_key,
    '1.1.0'::text AS schema_version,
    jsonb_build_object(
      'document_id', 'documentation-document:runtime-readiness',
      'revision_id', 'golden-v1',
      'section_id', 'documentation-section:runtime-readiness',
      'section_anchor', 'runtime-readiness',
      'heading_text', 'Runtime readiness',
      'ordinal_path', jsonb_build_array(1),
      'content', 'The synthetic orders-api fixture documents its runtime readiness contract.',
      'content_format', 'markdown',
      'contains_warnings', false
    ) AS payload
  UNION ALL
  SELECT
    'golden-documentation-finding',
    'documentation_finding',
    'golden-documentation-finding',
    '1.0.0',
    jsonb_build_object(
      'finding_id', 'documentation-finding:runtime-readiness',
      'finding_version', 'golden-v1',
      'finding_type', 'runtime_readiness',
      'status', 'active',
      'truth_level', 'derived',
      'freshness_state', 'fresh',
      'source_id', 'documentation-source:golden',
      'document_id', 'documentation-document:runtime-readiness',
      'section_id', 'documentation-section:runtime-readiness',
      'claim_id', 'documentation-claim:runtime-readiness',
      'claim_type', 'runtime_readiness',
      'claim_text', 'orders-api has a documented runtime readiness contract.',
      'normalized_claim', 'orders-api runtime readiness contract',
      'summary', 'Synthetic documentation finding for deployed query validation.',
      'evidence_packet_id', 'documentation-packet:runtime-readiness',
      'permissions', jsonb_build_object(
        'viewer_can_read_source', true,
        'source_acl_evaluated', true
      ),
      'states', jsonb_build_object(
        'permission_decision', 'allowed',
        'freshness_state', 'fresh'
      )
    )
  UNION ALL
  SELECT
    'golden-documentation-evidence-packet',
    'documentation_evidence_packet',
    'golden-documentation-evidence-packet',
    '1.0.0',
    jsonb_build_object(
      'packet_id', 'documentation-packet:runtime-readiness',
      'packet_version', 'golden-v1',
      'generated_at', NOW(),
      'finding_id', 'documentation-finding:runtime-readiness',
      'linked_entities', jsonb_build_array(jsonb_build_object(
        'entity_type', 'repository',
        'entity_id', 'orders-api'
      )),
      'finding', jsonb_build_object(
        'finding_id', 'documentation-finding:runtime-readiness',
        'finding_type', 'runtime_readiness',
        'status', 'active'
      ),
      'document', jsonb_build_object(
        'source_id', 'documentation-source:golden',
        'document_id', 'documentation-document:runtime-readiness',
        'title', 'Runtime readiness'
      ),
      'section', jsonb_build_object(
        'section_id', 'documentation-section:runtime-readiness'
      ),
      'bounded_excerpt', jsonb_build_object(
        'text', 'orders-api has a documented runtime readiness contract.'
      ),
      'current_truth', jsonb_build_object(
        'claim_key', 'runtime_readiness',
        'documented_value', 'orders-api runtime readiness contract',
        'truth_level', 'derived',
        'freshness_state', 'fresh'
      ),
      'permissions', jsonb_build_object(
        'viewer_can_read_source', true,
        'source_acl_evaluated', true
      ),
      'states', jsonb_build_object(
        'permission_decision', 'allowed',
        'freshness_state', 'fresh'
      )
    )
), seeded AS (
  INSERT INTO fact_records (
    fact_id, scope_id, generation_id, fact_kind, stable_fact_key,
    schema_version, collector_kind, fencing_token, source_confidence,
    source_system, source_fact_key, observed_at, ingested_at,
    is_tombstone, payload
  )
  SELECT
    fixture.fact_id, source_scope.scope_id, source_scope.generation_id,
    fixture.fact_kind, fixture.stable_fact_key, fixture.schema_version,
    'documentation', 0, 'derived', 'golden_fixture', fixture.fact_id,
    NOW(), NOW(), FALSE, fixture.payload
  FROM source_scope
  CROSS JOIN fixture
  ON CONFLICT (fact_id) DO UPDATE
  SET scope_id = EXCLUDED.scope_id,
      generation_id = EXCLUDED.generation_id,
      fact_kind = EXCLUDED.fact_kind,
      stable_fact_key = EXCLUDED.stable_fact_key,
      schema_version = EXCLUDED.schema_version,
      collector_kind = EXCLUDED.collector_kind,
      fencing_token = EXCLUDED.fencing_token,
      source_confidence = EXCLUDED.source_confidence,
      source_system = EXCLUDED.source_system,
      source_fact_key = EXCLUDED.source_fact_key,
      observed_at = EXCLUDED.observed_at,
      ingested_at = EXCLUDED.ingested_at,
      is_tombstone = FALSE,
      payload = EXCLUDED.payload
  RETURNING fact_id
)
SELECT string_agg(fact_id, ',' ORDER BY fact_id) || ' ' || count(*)
FROM seeded;
"
	)"
	local expected="golden-documentation-evidence-packet,golden-documentation-finding,golden-documentation-section 3"
	[[ "${seeded}" == "${expected}" ]] ||
		die "documentation fixture seed returned ${seeded:-no rows}, want ${expected}"
}
