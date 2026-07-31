-- SPDX-License-Identifier: MIT
-- Copyright (c) 2025-2026 eshu-hq

WITH legacy_supports AS MATERIALIZED (
    SELECT
        fact.fact_id,
        fact.scope_id,
        fact.generation_id,
        COALESCE(fact.payload->>'digest', '') AS digest,
        COALESCE(fact.payload->>'image_ref', '') AS image_ref,
        COALESCE(fact.payload->>'repository_id', '') AS repository_id,
        COALESCE(fact.payload->>'outcome', '') AS outcome,
        COALESCE(fact.payload->>'identity_strength', '') AS identity_strength,
        COALESCE(fact.payload->>'source_revision', '') AS source_revision,
        COALESCE(fact.payload->>'source_revision_provenance', '') AS source_revision_provenance,
        COALESCE(fact.payload->>'reason', '') AS reason,
        GREATEST(COALESCE((fact.payload->>'canonical_writes')::INTEGER, 0), 0) AS canonical_writes,
        COALESCE(fact.payload->'source_repository_ids', '[]'::JSONB) AS source_repository_ids,
        COALESCE(fact.payload->'build_provenance_repository_ids', '[]'::JSONB) AS build_repository_ids,
        COALESCE(fact.payload->'base_image_for_repository_ids', '[]'::JSONB) AS base_repository_ids,
        COALESCE(fact.payload->'workload_ids', '[]'::JSONB) AS workload_ids,
        COALESCE(fact.payload->'service_ids', '[]'::JSONB) AS service_ids,
        COALESCE(fact.payload->'source_layers', '[]'::JSONB) AS source_layers,
        COALESCE(fact.payload->'evidence_fact_ids', '[]'::JSONB) AS evidence_fact_ids,
        COALESCE(fact.payload->'missing_evidence', '[]'::JSONB) AS missing_evidence,
        fact.source_system,
        fact.collector_kind,
        fact.source_confidence,
        fact.source_fact_key,
        COALESCE(fact.payload->>'cause', '') AS cause,
        fact.observed_at,
        fact.ingested_at,
        fact.fencing_token
    FROM fact_records AS fact
    JOIN ingestion_scopes AS scope
      ON scope.scope_id = fact.scope_id
     AND scope.active_generation_id = fact.generation_id
    WHERE fact.fact_kind = 'reducer_container_image_identity'
      AND NOT fact.is_tombstone
      AND COALESCE(fact.payload->>'digest', '') <> ''
),
legacy_documents AS MATERIALIZED (
    SELECT
        scope_id,
        generation_id,
        COUNT(*)::INTEGER AS support_count,
        jsonb_agg(
            jsonb_build_object(
                'digest', digest,
                'image_ref', image_ref,
                'repository_id', repository_id,
                'outcome', outcome,
                'identity_strength', identity_strength,
                'source_revision', source_revision,
                'source_revision_provenance', source_revision_provenance,
                'reason', reason,
                'canonical_writes', canonical_writes,
                'source_repository_ids', source_repository_ids,
                'build_provenance_repository_ids', build_repository_ids,
                'base_image_for_repository_ids', base_repository_ids,
                'workload_ids', workload_ids,
                'service_ids', service_ids,
                'source_layers', source_layers,
                'evidence_fact_ids', evidence_fact_ids,
                'missing_evidence', missing_evidence
            ) ORDER BY digest, image_ref, repository_id, fact_id
        ) AS document
    FROM legacy_supports
    GROUP BY scope_id, generation_id
),
legacy_sets AS MATERIALIZED (
    SELECT
        scope_id,
        generation_id,
        support_count,
        sha256(convert_to(document::TEXT, 'UTF8')) AS content_hash
    FROM legacy_documents
)
INSERT INTO container_image_identity_support_sets (
    set_id,
    scope_id,
    content_hash,
    support_count
)
SELECT
    sha256(convert_to(scope_id || ':' || encode(content_hash, 'hex'), 'UTF8')),
    scope_id,
    content_hash,
    support_count
FROM legacy_sets
ON CONFLICT (scope_id, content_hash) DO NOTHING;

WITH legacy_supports AS MATERIALIZED (
    SELECT
        fact.fact_id,
        fact.scope_id,
        COALESCE(fact.payload->>'digest', '') AS digest,
        COALESCE(fact.payload->>'image_ref', '') AS image_ref,
        COALESCE(fact.payload->>'repository_id', '') AS repository_id,
        COALESCE(fact.payload->>'outcome', '') AS outcome,
        COALESCE(fact.payload->>'identity_strength', '') AS identity_strength,
        COALESCE(fact.payload->>'source_revision', '') AS source_revision,
        COALESCE(fact.payload->>'source_revision_provenance', '') AS source_revision_provenance,
        COALESCE(fact.payload->>'reason', '') AS reason,
        GREATEST(COALESCE((fact.payload->>'canonical_writes')::INTEGER, 0), 0) AS canonical_writes,
        COALESCE(fact.payload->'source_repository_ids', '[]'::JSONB) AS source_repository_ids,
        COALESCE(fact.payload->'build_provenance_repository_ids', '[]'::JSONB) AS build_repository_ids,
        COALESCE(fact.payload->'base_image_for_repository_ids', '[]'::JSONB) AS base_repository_ids,
        COALESCE(fact.payload->'workload_ids', '[]'::JSONB) AS workload_ids,
        COALESCE(fact.payload->'service_ids', '[]'::JSONB) AS service_ids,
        COALESCE(fact.payload->'source_layers', '[]'::JSONB) AS source_layers,
        COALESCE(fact.payload->'evidence_fact_ids', '[]'::JSONB) AS evidence_fact_ids,
        COALESCE(fact.payload->'missing_evidence', '[]'::JSONB) AS missing_evidence
    FROM fact_records AS fact
    JOIN ingestion_scopes AS scope
      ON scope.scope_id = fact.scope_id
     AND scope.active_generation_id = fact.generation_id
    WHERE fact.fact_kind = 'reducer_container_image_identity'
      AND NOT fact.is_tombstone
      AND COALESCE(fact.payload->>'digest', '') <> ''
),
legacy_documents AS MATERIALIZED (
    SELECT
        scope_id,
        sha256(convert_to(jsonb_agg(
            jsonb_build_object(
                'digest', digest, 'image_ref', image_ref, 'repository_id', repository_id,
                'outcome', outcome, 'identity_strength', identity_strength,
                'source_revision', source_revision,
                'source_revision_provenance', source_revision_provenance,
                'reason', reason, 'canonical_writes', canonical_writes,
                'source_repository_ids', source_repository_ids,
                'build_provenance_repository_ids', build_repository_ids,
                'base_image_for_repository_ids', base_repository_ids,
                'workload_ids', workload_ids, 'service_ids', service_ids,
                'source_layers', source_layers, 'evidence_fact_ids', evidence_fact_ids,
                'missing_evidence', missing_evidence
            ) ORDER BY digest, image_ref, repository_id, fact_id
        )::TEXT, 'UTF8')) AS content_hash
    FROM legacy_supports
    GROUP BY scope_id
)
INSERT INTO container_image_identity_supports (
    set_id, digest, support_id, image_ref, repository_id, outcome,
    identity_strength, source_revision, source_revision_provenance, reason,
    canonical_writes, source_repository_ids, build_provenance_repository_ids,
    base_image_for_repository_ids, workload_ids, service_ids, source_layers,
    evidence_fact_ids, missing_evidence
)
SELECT
    support_set.set_id,
    legacy.digest,
    sha256(convert_to(legacy.fact_id, 'UTF8')),
    legacy.image_ref,
    legacy.repository_id,
    legacy.outcome,
    legacy.identity_strength,
    legacy.source_revision,
    legacy.source_revision_provenance,
    legacy.reason,
    legacy.canonical_writes,
    ARRAY(SELECT jsonb_array_elements_text(legacy.source_repository_ids)),
    ARRAY(SELECT jsonb_array_elements_text(legacy.build_repository_ids)),
    ARRAY(SELECT jsonb_array_elements_text(legacy.base_repository_ids)),
    ARRAY(SELECT jsonb_array_elements_text(legacy.workload_ids)),
    ARRAY(SELECT jsonb_array_elements_text(legacy.service_ids)),
    ARRAY(SELECT jsonb_array_elements_text(legacy.source_layers)),
    ARRAY(SELECT jsonb_array_elements_text(legacy.evidence_fact_ids)),
    ARRAY(SELECT jsonb_array_elements_text(legacy.missing_evidence))
FROM legacy_supports AS legacy
JOIN legacy_documents AS document USING (scope_id)
JOIN container_image_identity_support_sets AS support_set
  ON support_set.scope_id = legacy.scope_id
 AND support_set.content_hash = document.content_hash
ON CONFLICT (set_id, digest, support_id) DO NOTHING;

WITH ranked_legacy AS MATERIALIZED (
    SELECT
        fact.*,
        row_number() OVER (
            PARTITION BY fact.scope_id
            ORDER BY fact.fact_id
        ) AS row_rank
    FROM fact_records AS fact
    JOIN ingestion_scopes AS scope
      ON scope.scope_id = fact.scope_id
     AND scope.active_generation_id = fact.generation_id
    WHERE fact.fact_kind = 'reducer_container_image_identity'
      AND NOT fact.is_tombstone
      AND COALESCE(fact.payload->>'digest', '') <> ''
),
active_sets AS MATERIALIZED (
    SELECT DISTINCT ON (support_set.scope_id)
        support_set.scope_id,
        support_set.set_id,
        support_set.content_hash
    FROM container_image_identity_support_sets AS support_set
    JOIN container_image_identity_scope_state AS state USING (scope_id)
    ORDER BY support_set.scope_id, support_set.created_at DESC, support_set.set_id
)
UPDATE container_image_identity_scope_state AS state
SET last_set_id = active_set.set_id,
    last_set_hash = active_set.content_hash,
    published_claim_epoch = COALESCE(work_item.container_image_identity_claim_epoch, 0),
    source_system = legacy.source_system,
    collector_kind = legacy.collector_kind,
    source_confidence = legacy.source_confidence,
    source_fact_key = legacy.source_fact_key,
    cause = COALESCE(legacy.payload->>'cause', ''),
    observed_at = legacy.observed_at,
    ingested_at = legacy.ingested_at,
    fencing_token = legacy.fencing_token,
    updated_at = clock_timestamp()
FROM active_sets AS active_set
JOIN ranked_legacy AS legacy
  ON legacy.scope_id = active_set.scope_id
 AND legacy.row_rank = 1
LEFT JOIN fact_work_items AS work_item
  ON work_item.work_item_id = legacy.source_fact_key
 AND work_item.scope_id = legacy.scope_id
 AND work_item.generation_id = legacy.generation_id
WHERE state.scope_id = active_set.scope_id;

CREATE OR REPLACE FUNCTION reset_container_image_identity_scope_state()
RETURNS trigger
LANGUAGE plpgsql
AS $function$
BEGIN
    IF TG_OP = 'INSERT' AND NEW.active_generation_id IS NULL THEN
        RETURN NEW;
    END IF;
    IF TG_OP = 'UPDATE' AND NEW.active_generation_id IS NOT DISTINCT FROM OLD.active_generation_id THEN
        RETURN NEW;
    END IF;
    INSERT INTO container_image_identity_scope_state (
        scope_id, active_generation_id, activation_epoch
    ) VALUES (
        NEW.scope_id,
        NEW.active_generation_id,
        nextval('container_image_identity_activation_epoch_seq')
    )
    ON CONFLICT (scope_id) DO UPDATE
    SET active_generation_id = EXCLUDED.active_generation_id,
        activation_epoch = EXCLUDED.activation_epoch,
        active_set_id = NULL,
        published_claim_epoch = 0,
        source_fact_key = '',
        fencing_token = 0,
        updated_at = clock_timestamp();
    RETURN NEW;
END;
$function$;

DROP TRIGGER IF EXISTS ingestion_scopes_container_image_identity_state_reset
    ON ingestion_scopes;
CREATE TRIGGER ingestion_scopes_container_image_identity_state_reset
AFTER INSERT OR UPDATE OF active_generation_id ON ingestion_scopes
FOR EACH ROW
EXECUTE FUNCTION reset_container_image_identity_scope_state();

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
