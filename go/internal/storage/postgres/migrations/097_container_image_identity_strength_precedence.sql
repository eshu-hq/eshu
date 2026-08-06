-- SPDX-License-Identifier: MIT
-- Copyright (c) 2025-2026 eshu-hq

CREATE OR REPLACE FUNCTION container_image_identity_current_facts_for(
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
WITH matched_digests AS MATERIALIZED (
    SELECT match.digest
    FROM (
        SELECT support.digest
        FROM container_image_identity_current_supports AS support
        WHERE COALESCE(cardinality($1), 0) > 0
          AND support.digest = ANY($1)
        UNION ALL
        SELECT support.digest
        FROM container_image_identity_current_supports AS support
        WHERE COALESCE(cardinality($2), 0) > 0
          AND support.image_ref = ANY($2)
        UNION ALL
        SELECT support.digest
        FROM container_image_identity_current_supports AS support
        WHERE COALESCE(cardinality($3), 0) > 0
          AND support.repository_id = ANY($3)
        UNION ALL
        SELECT support.digest
        FROM container_image_identity_current_supports AS support
        WHERE COALESCE(cardinality($4), 0) > 0
          AND support.source_repository_ids && $4
        UNION ALL
        SELECT support.digest
        FROM container_image_identity_current_supports AS support
        WHERE COALESCE(cardinality($5), 0) > 0
          AND support.scope_id = ANY($5)
    ) AS match
),
identified_digests AS MATERIALIZED (
    SELECT DISTINCT
        match.digest,
        'reducer_container_image_identity:' ||
            encode(sha256(convert_to(
                '{"fact_type":"reducer_container_image_identity","identity":{"digest":' ||
                to_json(match.digest)::TEXT || '}}', 'UTF8'
            )), 'hex') AS identity_id
    FROM matched_digests AS match
),
selected_digests AS MATERIALIZED (
    SELECT identified.digest
    FROM identified_digests AS identified
    WHERE $6 = '' OR identified.identity_id > $6
    ORDER BY identified.identity_id
    LIMIT GREATEST($7, 0)
),
selected_supports AS MATERIALIZED (
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
    FROM selected_digests AS selected
    CROSS JOIN container_image_identity_scope_state AS state
    JOIN ingestion_scopes AS scope
      ON scope.scope_id = state.scope_id
     AND scope.active_generation_id = state.active_generation_id
    JOIN scope_generations AS generation
      ON generation.scope_id = state.scope_id
     AND generation.generation_id = state.active_generation_id
     AND generation.status = 'active'
    JOIN container_image_identity_supports AS support
      ON support.set_id = state.active_set_id
     AND support.digest = selected.digest
    UNION ALL
    SELECT
        'reducer_container_image_identity:' ||
            encode(sha256(convert_to(
                '{"fact_type":"reducer_container_image_identity","identity":{"digest":' ||
                to_json(fact.payload->>'digest')::TEXT || '}}', 'UTF8'
            )), 'hex'),
        'canonical:container_image_identity:' || (fact.payload->>'digest'),
        NULL::BYTEA,
        fact.payload->>'digest',
        sha256(convert_to(fact.fact_id, 'UTF8')),
        COALESCE(fact.payload->>'image_ref', ''),
        COALESCE(fact.payload->>'repository_id', ''),
        COALESCE(fact.payload->>'outcome', ''),
        COALESCE(fact.payload->>'identity_strength', ''),
        COALESCE(fact.payload->>'source_revision', ''),
        COALESCE(fact.payload->>'source_revision_provenance', ''),
        COALESCE(fact.payload->>'reason', ''),
        GREATEST(COALESCE((fact.payload->>'canonical_writes')::INTEGER, 0), 0),
        CASE WHEN jsonb_typeof(fact.payload->'source_repository_ids') = 'array' THEN ARRAY(SELECT jsonb_array_elements_text(fact.payload->'source_repository_ids')) ELSE '{}'::TEXT[] END,
        CASE WHEN jsonb_typeof(fact.payload->'build_provenance_repository_ids') = 'array' THEN ARRAY(SELECT jsonb_array_elements_text(fact.payload->'build_provenance_repository_ids')) ELSE '{}'::TEXT[] END,
        CASE WHEN jsonb_typeof(fact.payload->'base_image_for_repository_ids') = 'array' THEN ARRAY(SELECT jsonb_array_elements_text(fact.payload->'base_image_for_repository_ids')) ELSE '{}'::TEXT[] END,
        CASE WHEN jsonb_typeof(fact.payload->'workload_ids') = 'array' THEN ARRAY(SELECT jsonb_array_elements_text(fact.payload->'workload_ids')) ELSE '{}'::TEXT[] END,
        CASE WHEN jsonb_typeof(fact.payload->'service_ids') = 'array' THEN ARRAY(SELECT jsonb_array_elements_text(fact.payload->'service_ids')) ELSE '{}'::TEXT[] END,
        CASE WHEN jsonb_typeof(fact.payload->'source_layers') = 'array' THEN ARRAY(SELECT jsonb_array_elements_text(fact.payload->'source_layers')) ELSE '{}'::TEXT[] END,
        CASE WHEN jsonb_typeof(fact.payload->'evidence_fact_ids') = 'array' THEN ARRAY(SELECT jsonb_array_elements_text(fact.payload->'evidence_fact_ids')) ELSE '{}'::TEXT[] END,
        CASE WHEN jsonb_typeof(fact.payload->'missing_evidence') = 'array' THEN ARRAY(SELECT jsonb_array_elements_text(fact.payload->'missing_evidence')) ELSE '{}'::TEXT[] END,
        state.scope_id,
        state.active_generation_id,
        state.activation_epoch,
        state.published_claim_epoch,
        fact.source_system,
        fact.collector_kind,
        fact.source_confidence,
        fact.source_fact_key,
        COALESCE(fact.payload->>'cause', ''),
        fact.observed_at,
        fact.ingested_at,
        fact.fencing_token
    FROM selected_digests AS selected
    CROSS JOIN container_image_identity_scope_state AS state
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
     AND fact.payload->>'digest' = selected.digest
    WHERE state.active_set_id IS NULL
),
ranked AS MATERIALIZED (
    SELECT
        support.*,
        row_number() OVER (
            PARTITION BY support.digest
            ORDER BY
                CASE
                    WHEN cardinality(support.source_repository_ids) = 1 THEN 0
                    WHEN cardinality(support.build_provenance_repository_ids) = 1 THEN 1
                    ELSE 2
                END,
                CASE support.outcome WHEN 'exact_digest' THEN 0 ELSE 1 END,
                support.repository_id,
                support.image_ref,
                support.scope_id,
                support.support_id
        ) AS support_rank
    FROM selected_supports AS support
),
-- Strength is a digest-level evidence conclusion, not a side effect of which
-- support supplies the canonical image/repository metadata. Keep those winner
-- rules unchanged and fold strength independently with an explicit total order.
identity_strengths AS MATERIALIZED (
    SELECT support.digest, support.identity_strength AS value
    FROM (
        SELECT
            support.digest,
            support.identity_strength,
            support.support_id,
            row_number() OVER (
                PARTITION BY support.digest
                ORDER BY
                    CASE support.identity_strength
                        WHEN 'explicit_digest' THEN 50
                        WHEN 'oci_config_source_label_with_digest' THEN 40
                        WHEN 'artifact_digest_with_registry_observation' THEN 30
                        WHEN 'immutable_digest' THEN 20
                        WHEN 'tag_observation_with_digest' THEN 10
                        ELSE 0
                    END DESC,
                    support.identity_strength ASC,
                    support.support_id
            ) AS strength_rank
        FROM ranked AS support
    ) AS support
    WHERE support.strength_rank = 1
),
source_repositories AS MATERIALIZED (
    SELECT row.digest, array_agg(DISTINCT value ORDER BY value) AS values
    FROM ranked AS row
    CROSS JOIN LATERAL unnest(row.source_repository_ids) AS value
    GROUP BY row.digest
),
build_repositories AS MATERIALIZED (
    SELECT row.digest, array_agg(DISTINCT value ORDER BY value) AS values
    FROM ranked AS row
    CROSS JOIN LATERAL unnest(row.build_provenance_repository_ids) AS value
    GROUP BY row.digest
),
base_repositories AS MATERIALIZED (
    SELECT row.digest, array_agg(DISTINCT value ORDER BY value) AS values
    FROM ranked AS row
    CROSS JOIN LATERAL unnest(row.base_image_for_repository_ids) AS value
    GROUP BY row.digest
),
workloads AS MATERIALIZED (
    SELECT row.digest, array_agg(DISTINCT value ORDER BY value) AS values
    FROM ranked AS row
    CROSS JOIN LATERAL unnest(row.workload_ids) AS value
    GROUP BY row.digest
),
services AS MATERIALIZED (
    SELECT row.digest, array_agg(DISTINCT value ORDER BY value) AS values
    FROM ranked AS row
    CROSS JOIN LATERAL unnest(row.service_ids) AS value
    GROUP BY row.digest
),
source_layers AS MATERIALIZED (
    SELECT row.digest, array_agg(DISTINCT value ORDER BY value) AS values
    FROM ranked AS row
    CROSS JOIN LATERAL unnest(row.source_layers) AS value
    GROUP BY row.digest
),
evidence_facts AS MATERIALIZED (
    SELECT row.digest, array_agg(DISTINCT value ORDER BY value) AS values
    FROM ranked AS row
    CROSS JOIN LATERAL unnest(row.evidence_fact_ids) AS value
    GROUP BY row.digest
),
missing_evidence AS MATERIALIZED (
    SELECT row.digest, array_agg(DISTINCT value ORDER BY value) AS values
    FROM ranked AS row
    CROSS JOIN LATERAL unnest(row.missing_evidence) AS value
    GROUP BY row.digest
),
canonical_write_counts AS MATERIALIZED (
    SELECT row.digest, GREATEST(max(row.canonical_writes), 1) AS value
    FROM ranked AS row
    GROUP BY row.digest
),
folded AS MATERIALIZED (
    SELECT
        digest.digest,
        identity_strength.value AS identity_strength,
        COALESCE(source_repositories.values, '{}'::TEXT[]) AS source_repository_ids,
        COALESCE(build_repositories.values, '{}'::TEXT[]) AS build_repository_ids,
        COALESCE(base_repositories.values, '{}'::TEXT[]) AS base_repository_ids,
        COALESCE(workloads.values, '{}'::TEXT[]) AS workload_ids,
        COALESCE(services.values, '{}'::TEXT[]) AS service_ids,
        COALESCE(source_layers.values, '{}'::TEXT[]) AS source_layers,
        COALESCE(evidence_facts.values, '{}'::TEXT[]) AS evidence_fact_ids,
        COALESCE(missing_evidence.values, '{}'::TEXT[]) AS missing_evidence,
        canonical_write_counts.value AS canonical_writes
    FROM selected_digests AS digest
    JOIN identity_strengths AS identity_strength USING (digest)
    JOIN canonical_write_counts USING (digest)
    LEFT JOIN source_repositories USING (digest)
    LEFT JOIN build_repositories USING (digest)
    LEFT JOIN base_repositories USING (digest)
    LEFT JOIN workloads USING (digest)
    LEFT JOIN services USING (digest)
    LEFT JOIN source_layers USING (digest)
    LEFT JOIN evidence_facts USING (digest)
    LEFT JOIN missing_evidence USING (digest)
)
SELECT
    winner.identity_id,
    winner.scope_id,
    winner.generation_id,
    'reducer_container_image_identity'::TEXT,
    'container_image_identity:' || winner.digest,
    '1.0.0'::TEXT,
    winner.collector_kind,
    winner.fencing_token,
    winner.source_confidence,
    winner.source_system,
    winner.source_fact_key,
    ''::TEXT,
    ''::TEXT,
    winner.observed_at,
    FALSE,
    jsonb_build_object(
        'identity_format', 'digest_v3',
        'reducer_domain', 'container_image_identity',
        'intent_id', winner.source_fact_key,
        'scope_id', winner.scope_id,
        'generation_id', winner.generation_id,
        'source_system', winner.source_system,
        'cause', winner.cause,
        'image_ref', winner.image_ref,
        'digest', winner.digest,
        'repository_id', winner.repository_id,
        'source_revision', winner.source_revision,
        'source_revision_provenance', winner.source_revision_provenance,
        'source_repository_ids', to_jsonb(folded.source_repository_ids),
        'build_provenance_repository_ids', to_jsonb(folded.build_repository_ids),
        'base_image_for_repository_ids', to_jsonb(folded.base_repository_ids),
        'workload_ids', to_jsonb(folded.workload_ids),
        'service_ids', to_jsonb(folded.service_ids),
        'outcome', winner.outcome,
        'reason', winner.reason,
        'canonical_id', winner.canonical_id,
        'canonical_writes', folded.canonical_writes,
        'evidence_fact_ids', to_jsonb(folded.evidence_fact_ids),
        'identity_strength', folded.identity_strength,
        'publication_kind', 'reducer_container_image_identity',
        'source_layers', to_jsonb(folded.source_layers),
        'missing_evidence', to_jsonb(folded.missing_evidence)
    )
FROM ranked AS winner
JOIN folded USING (digest)
WHERE winner.support_rank = 1;
$function$;
