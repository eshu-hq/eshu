-- SPDX-License-Identifier: MIT
-- Copyright (c) 2025-2026 eshu-hq

-- Legacy facts remain the pre-pointer authority until the first fenced v3
-- publication converts only the exact scope/generation it owns. Schema
-- bootstrap deliberately performs no eager fact scan or typed-set backfill.
CREATE OR REPLACE VIEW container_image_identity_current_supports
WITH (security_invoker = true)
AS
SELECT
    'reducer_container_image_identity:' ||
        encode(sha256(convert_to(
            '{"fact_type":"reducer_container_image_identity","identity":{"digest":' ||
            to_json(support.digest)::TEXT || '}}', 'UTF8'
        )), 'hex') AS identity_id,
    'canonical:container_image_identity:' || support.digest AS canonical_id,
    support.set_id,
    support.digest,
    support.support_id,
    support.image_ref,
    support.repository_id,
    support.outcome,
    support.identity_strength,
    support.source_revision,
    support.source_revision_provenance,
    support.reason,
    support.canonical_writes,
    support.source_repository_ids,
    support.build_provenance_repository_ids,
    support.base_image_for_repository_ids,
    support.workload_ids,
    support.service_ids,
    support.source_layers,
    support.evidence_fact_ids,
    support.missing_evidence,
    state.scope_id,
    state.active_generation_id AS generation_id,
    state.activation_epoch,
    state.published_claim_epoch,
    state.source_system,
    state.collector_kind,
    state.source_confidence,
    state.source_fact_key,
    state.cause,
    state.observed_at,
    state.ingested_at,
    state.fencing_token
FROM container_image_identity_scope_state AS state
JOIN ingestion_scopes AS scope
  ON scope.scope_id = state.scope_id
 AND scope.active_generation_id = state.active_generation_id
JOIN scope_generations AS generation
  ON generation.scope_id = state.scope_id
 AND generation.generation_id = state.active_generation_id
 AND generation.status = 'active'
JOIN container_image_identity_supports AS support
  ON support.set_id = state.active_set_id
UNION ALL
SELECT
    'reducer_container_image_identity:' ||
        encode(sha256(convert_to(
            '{"fact_type":"reducer_container_image_identity","identity":{"digest":' ||
            to_json(fact.payload->>'digest')::TEXT || '}}', 'UTF8'
        )), 'hex') AS identity_id,
    'canonical:container_image_identity:' || (fact.payload->>'digest') AS canonical_id,
    NULL::BYTEA AS set_id,
    fact.payload->>'digest' AS digest,
    sha256(convert_to(fact.fact_id, 'UTF8')) AS support_id,
    COALESCE(fact.payload->>'image_ref', '') AS image_ref,
    COALESCE(fact.payload->>'repository_id', '') AS repository_id,
    COALESCE(fact.payload->>'outcome', '') AS outcome,
    COALESCE(fact.payload->>'identity_strength', '') AS identity_strength,
    COALESCE(fact.payload->>'source_revision', '') AS source_revision,
    COALESCE(fact.payload->>'source_revision_provenance', '') AS source_revision_provenance,
    COALESCE(fact.payload->>'reason', '') AS reason,
    GREATEST(COALESCE((fact.payload->>'canonical_writes')::INTEGER, 0), 0) AS canonical_writes,
    CASE WHEN jsonb_typeof(fact.payload->'source_repository_ids') = 'array' THEN ARRAY(SELECT jsonb_array_elements_text(fact.payload->'source_repository_ids')) ELSE '{}'::TEXT[] END,
    CASE WHEN jsonb_typeof(fact.payload->'build_provenance_repository_ids') = 'array' THEN ARRAY(SELECT jsonb_array_elements_text(fact.payload->'build_provenance_repository_ids')) ELSE '{}'::TEXT[] END,
    CASE WHEN jsonb_typeof(fact.payload->'base_image_for_repository_ids') = 'array' THEN ARRAY(SELECT jsonb_array_elements_text(fact.payload->'base_image_for_repository_ids')) ELSE '{}'::TEXT[] END,
    CASE WHEN jsonb_typeof(fact.payload->'workload_ids') = 'array' THEN ARRAY(SELECT jsonb_array_elements_text(fact.payload->'workload_ids')) ELSE '{}'::TEXT[] END,
    CASE WHEN jsonb_typeof(fact.payload->'service_ids') = 'array' THEN ARRAY(SELECT jsonb_array_elements_text(fact.payload->'service_ids')) ELSE '{}'::TEXT[] END,
    CASE WHEN jsonb_typeof(fact.payload->'source_layers') = 'array' THEN ARRAY(SELECT jsonb_array_elements_text(fact.payload->'source_layers')) ELSE '{}'::TEXT[] END,
    CASE WHEN jsonb_typeof(fact.payload->'evidence_fact_ids') = 'array' THEN ARRAY(SELECT jsonb_array_elements_text(fact.payload->'evidence_fact_ids')) ELSE '{}'::TEXT[] END,
    CASE WHEN jsonb_typeof(fact.payload->'missing_evidence') = 'array' THEN ARRAY(SELECT jsonb_array_elements_text(fact.payload->'missing_evidence')) ELSE '{}'::TEXT[] END,
    state.scope_id,
    state.active_generation_id AS generation_id,
    state.activation_epoch,
    state.published_claim_epoch,
    fact.source_system,
    fact.collector_kind,
    fact.source_confidence,
    fact.source_fact_key,
    COALESCE(fact.payload->>'cause', '') AS cause,
    fact.observed_at,
    fact.ingested_at,
    fact.fencing_token
FROM container_image_identity_scope_state AS state
JOIN ingestion_scopes AS scope
  ON scope.scope_id = state.scope_id
 AND scope.active_generation_id = state.active_generation_id
JOIN scope_generations AS generation
  ON generation.scope_id = state.scope_id
 AND generation.generation_id = state.active_generation_id
 AND generation.status = 'active'
JOIN fact_records AS fact
  ON fact.scope_id = state.scope_id
 AND fact.generation_id = state.active_generation_id
WHERE state.active_set_id IS NULL
  AND fact.fact_kind = 'reducer_container_image_identity'
  AND NOT fact.is_tombstone
  AND COALESCE(fact.payload->>'digest', '') <> '';
