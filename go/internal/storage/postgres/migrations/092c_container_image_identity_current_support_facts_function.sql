-- SPDX-License-Identifier: MIT
-- Copyright (c) 2025-2026 eshu-hq

-- Reducer consumers need one envelope per evidence support so correlated
-- repository, image, provenance, and runtime fields are never flattened before
-- their domain-specific winner selection runs. Public query surfaces continue
-- to use container_image_identity_current_facts_for and its one-row-per-digest
-- aggregate.
CREATE OR REPLACE FUNCTION container_image_identity_try_decode_utf8_hex(value TEXT)
RETURNS TEXT
LANGUAGE plpgsql
IMMUTABLE
STRICT
PARALLEL SAFE
AS $function$
BEGIN
    IF value !~ '^([0-9a-f]{2})+$' THEN
        RETURN NULL;
    END IF;
    RETURN convert_from(decode(value, 'hex'), 'UTF8');
EXCEPTION
    WHEN SQLSTATE '22021' OR SQLSTATE '22023' THEN
        RETURN NULL;
END;
$function$;

CREATE OR REPLACE FUNCTION container_image_identity_current_support_facts_for(
    digests TEXT[],
    image_refs TEXT[],
    repository_ids TEXT[],
    source_repository_ids TEXT[],
    scope_ids TEXT[],
    after_fact_id TEXT,
    result_limit INTEGER
)
RETURNS TABLE (
    fact_id TEXT,
    scope_id TEXT,
    generation_id TEXT,
    fact_kind TEXT,
    stable_fact_key TEXT,
    schema_version TEXT,
    collector_kind TEXT,
    fencing_token BIGINT,
    source_confidence TEXT,
    source_system TEXT,
    source_fact_key TEXT,
    source_uri TEXT,
    source_record_id TEXT,
    observed_at TIMESTAMPTZ,
    is_tombstone BOOLEAN,
    payload JSONB
)
LANGUAGE sql
STABLE
SECURITY INVOKER
PARALLEL SAFE
AS $function$
WITH cursor_parts AS MATERIALIZED (
    SELECT regexp_split_to_array($6, ':') AS parts
),
cursor_boundary AS MATERIALIZED (
    SELECT
        decoded.scope_id,
        decoded.digest,
        decoded.support_id,
        cardinality(parts.parts) = 4
            AND parts.parts[1] = 'reducer_container_image_identity_support'
            AND decoded.scope_id IS NOT NULL
            AND decoded.digest IS NOT NULL
            AND decoded.support_id IS NOT NULL
            AND octet_length(decoded.support_id) = 32 AS is_canonical
    FROM cursor_parts AS parts
    CROSS JOIN LATERAL (
        SELECT
            container_image_identity_try_decode_utf8_hex(parts.parts[2]) AS scope_id,
            container_image_identity_try_decode_utf8_hex(parts.parts[3]) AS digest,
            CASE WHEN parts.parts[4] ~ '^([0-9a-f]{2})+$'
                THEN decode(parts.parts[4], 'hex')
            END AS support_id
    ) AS decoded
),
v3_candidates AS MATERIALIZED (
    SELECT
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
        state.source_system,
        state.collector_kind,
        state.source_confidence,
        state.source_fact_key,
        state.cause,
        state.observed_at,
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
    CROSS JOIN cursor_boundary AS cursor
    WHERE state.active_set_id IS NOT NULL
      AND (
          (COALESCE(cardinality($1), 0) > 0 AND support.digest = ANY($1))
          OR (COALESCE(cardinality($2), 0) > 0 AND support.image_ref = ANY($2))
          OR (COALESCE(cardinality($3), 0) > 0 AND support.repository_id = ANY($3))
          OR (COALESCE(cardinality($4), 0) > 0 AND support.source_repository_ids && $4)
          OR (COALESCE(cardinality($5), 0) > 0 AND state.scope_id = ANY($5))
      )
      AND (
          $6 = ''
          OR (
              NOT cursor.is_canonical
              AND convert_to(
                  'reducer_container_image_identity_support:' ||
                      encode(convert_to(state.scope_id, 'UTF8'), 'hex') || ':' ||
                      encode(convert_to(support.digest, 'UTF8'), 'hex') || ':' ||
                      encode(support.support_id, 'hex'),
                  'UTF8'
              ) > convert_to($6, 'UTF8')
          )
          OR (
              cursor.is_canonical
              AND (
                  convert_to(state.scope_id, 'UTF8'),
                  convert_to(support.digest, 'UTF8'),
                  support.support_id
              ) > (
                  convert_to(cursor.scope_id, 'UTF8'),
                  convert_to(cursor.digest, 'UTF8'),
                  cursor.support_id
              )
          )
      )
    ORDER BY
        convert_to(state.scope_id, 'UTF8'),
        convert_to(support.digest, 'UTF8'),
        support.support_id
    LIMIT GREATEST($7, 0)
),
legacy_candidates AS MATERIALIZED (
    SELECT
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
        CASE WHEN jsonb_typeof(fact.payload->'source_repository_ids') = 'array' THEN ARRAY(SELECT jsonb_array_elements_text(fact.payload->'source_repository_ids')) ELSE '{}'::TEXT[] END AS source_repository_ids,
        CASE WHEN jsonb_typeof(fact.payload->'build_provenance_repository_ids') = 'array' THEN ARRAY(SELECT jsonb_array_elements_text(fact.payload->'build_provenance_repository_ids')) ELSE '{}'::TEXT[] END AS build_provenance_repository_ids,
        CASE WHEN jsonb_typeof(fact.payload->'base_image_for_repository_ids') = 'array' THEN ARRAY(SELECT jsonb_array_elements_text(fact.payload->'base_image_for_repository_ids')) ELSE '{}'::TEXT[] END AS base_image_for_repository_ids,
        CASE WHEN jsonb_typeof(fact.payload->'workload_ids') = 'array' THEN ARRAY(SELECT jsonb_array_elements_text(fact.payload->'workload_ids')) ELSE '{}'::TEXT[] END AS workload_ids,
        CASE WHEN jsonb_typeof(fact.payload->'service_ids') = 'array' THEN ARRAY(SELECT jsonb_array_elements_text(fact.payload->'service_ids')) ELSE '{}'::TEXT[] END AS service_ids,
        CASE WHEN jsonb_typeof(fact.payload->'source_layers') = 'array' THEN ARRAY(SELECT jsonb_array_elements_text(fact.payload->'source_layers')) ELSE '{}'::TEXT[] END AS source_layers,
        CASE WHEN jsonb_typeof(fact.payload->'evidence_fact_ids') = 'array' THEN ARRAY(SELECT jsonb_array_elements_text(fact.payload->'evidence_fact_ids')) ELSE '{}'::TEXT[] END AS evidence_fact_ids,
        CASE WHEN jsonb_typeof(fact.payload->'missing_evidence') = 'array' THEN ARRAY(SELECT jsonb_array_elements_text(fact.payload->'missing_evidence')) ELSE '{}'::TEXT[] END AS missing_evidence,
        state.scope_id,
        state.active_generation_id AS generation_id,
        fact.source_system,
        fact.collector_kind,
        fact.source_confidence,
        fact.source_fact_key,
        COALESCE(fact.payload->>'cause', '') AS cause,
        fact.observed_at,
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
     AND fact.fact_kind = 'reducer_container_image_identity'
     AND NOT fact.is_tombstone
    CROSS JOIN cursor_boundary AS cursor
    WHERE state.active_set_id IS NULL
      AND COALESCE(fact.payload->>'digest', '') <> ''
      AND (
          (COALESCE(cardinality($1), 0) > 0 AND fact.payload->>'digest' = ANY($1))
          OR (COALESCE(cardinality($2), 0) > 0 AND fact.payload->>'image_ref' = ANY($2))
          OR (COALESCE(cardinality($3), 0) > 0 AND fact.payload->>'repository_id' = ANY($3))
          OR (COALESCE(cardinality($4), 0) > 0 AND CASE WHEN jsonb_typeof(fact.payload->'source_repository_ids') = 'array' THEN ARRAY(SELECT jsonb_array_elements_text(fact.payload->'source_repository_ids')) ELSE '{}'::TEXT[] END && $4)
          OR (COALESCE(cardinality($5), 0) > 0 AND state.scope_id = ANY($5))
      )
      AND (
          $6 = ''
          OR (
              NOT cursor.is_canonical
              AND convert_to(
                  'reducer_container_image_identity_support:' ||
                      encode(convert_to(state.scope_id, 'UTF8'), 'hex') || ':' ||
                      encode(convert_to(fact.payload->>'digest', 'UTF8'), 'hex') || ':' ||
                      encode(sha256(convert_to(fact.fact_id, 'UTF8')), 'hex'),
                  'UTF8'
              ) > convert_to($6, 'UTF8')
          )
          OR (
              cursor.is_canonical
              AND (
                  convert_to(state.scope_id, 'UTF8'),
                  convert_to(fact.payload->>'digest', 'UTF8'),
                  sha256(convert_to(fact.fact_id, 'UTF8'))
              ) > (
                  convert_to(cursor.scope_id, 'UTF8'),
                  convert_to(cursor.digest, 'UTF8'),
                  cursor.support_id
              )
          )
      )
    ORDER BY
        convert_to(state.scope_id, 'UTF8'),
        convert_to(fact.payload->>'digest', 'UTF8'),
        sha256(convert_to(fact.fact_id, 'UTF8'))
    LIMIT GREATEST($7, 0)
),
selected_support_candidates AS MATERIALIZED (
    SELECT * FROM v3_candidates
    UNION ALL
    SELECT * FROM legacy_candidates
),
selected_supports AS MATERIALIZED (
    SELECT *
    FROM selected_support_candidates
    ORDER BY convert_to(scope_id, 'UTF8'), convert_to(digest, 'UTF8'), support_id
    LIMIT GREATEST($7, 0)
)
SELECT
    'reducer_container_image_identity_support:' ||
        encode(convert_to(support.scope_id, 'UTF8'), 'hex') || ':' ||
        encode(convert_to(support.digest, 'UTF8'), 'hex') || ':' ||
        encode(support.support_id, 'hex'),
    support.scope_id,
    support.generation_id,
    'reducer_container_image_identity'::TEXT,
    'container_image_identity_support:' || support.scope_id || ':' ||
        support.digest || ':' || encode(support.support_id, 'hex'),
    '1.0.0'::TEXT,
    support.collector_kind,
    support.fencing_token,
    support.source_confidence,
    support.source_system,
    support.source_fact_key,
    ''::TEXT,
    ''::TEXT,
    support.observed_at,
    FALSE,
    jsonb_build_object(
        'identity_format', CASE WHEN support.set_id IS NULL THEN 'digest_v2' ELSE 'digest_v3' END,
        'reducer_domain', 'container_image_identity',
        'intent_id', support.source_fact_key,
        'scope_id', support.scope_id,
        'generation_id', support.generation_id,
        'source_system', support.source_system,
        'cause', support.cause,
        'image_ref', support.image_ref,
        'digest', support.digest,
        'repository_id', support.repository_id,
        'source_revision', support.source_revision,
        'source_revision_provenance', support.source_revision_provenance,
        'source_repository_ids', to_jsonb(support.source_repository_ids),
        'build_provenance_repository_ids', to_jsonb(support.build_provenance_repository_ids),
        'base_image_for_repository_ids', to_jsonb(support.base_image_for_repository_ids),
        'workload_ids', to_jsonb(support.workload_ids),
        'service_ids', to_jsonb(support.service_ids),
        'outcome', support.outcome,
        'reason', support.reason,
        'canonical_id', 'canonical:container_image_identity:' || support.digest,
        'canonical_writes', GREATEST(support.canonical_writes, 1),
        'evidence_fact_ids', to_jsonb(support.evidence_fact_ids),
        'identity_strength', support.identity_strength,
        'publication_kind', 'reducer_container_image_identity',
        'source_layers', to_jsonb(support.source_layers),
        'missing_evidence', to_jsonb(support.missing_evidence)
    )
FROM selected_supports AS support
ORDER BY
    convert_to(support.scope_id, 'UTF8'),
    convert_to(support.digest, 'UTF8'),
    support.support_id;
$function$;
