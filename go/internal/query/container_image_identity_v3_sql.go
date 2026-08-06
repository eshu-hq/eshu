// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

// containerImageIdentityStrengthOrderSQL must stay in lockstep with the
// deployed container_image_identity_current_facts_for function.
const containerImageIdentityStrengthOrderSQL = `CASE support.identity_strength
                    WHEN 'explicit_digest' THEN 50
                    WHEN 'oci_config_source_label_with_digest' THEN 40
                    WHEN 'artifact_digest_with_registry_observation' THEN 30
                    WHEN 'immutable_digest' THEN 20
                    WHEN 'tag_observation_with_digest' THEN 10
                    ELSE 0
                END DESC,
                support.identity_strength ASC,
                support.support_id`

var listContainerImageIdentitiesQuery = `
WITH filtered_supports AS MATERIALIZED (
    SELECT support.*
    FROM container_image_identity_current_supports AS support
    WHERE ($1 = '' OR support.digest = $1)
      AND ($2 = '' OR support.image_ref = $2)
      AND ($3 = '' OR $3 = ANY(support.source_repository_ids))
      AND ($4 = '' OR support.repository_id = $4)
      AND ($5 = '' OR support.outcome = $5)
      AND (
            COALESCE(cardinality($8::text[]), 0) = 0
            OR support.source_repository_ids && $8::text[]
          )
),
page_digests AS MATERIALIZED (
    SELECT support.identity_id, support.digest
    FROM filtered_supports AS support
    WHERE ($6 = '' OR support.identity_id > $6)
    GROUP BY support.identity_id, support.digest
    ORDER BY support.identity_id ASC
    LIMIT $7
),
ranked_supports AS MATERIALIZED (
    SELECT
        support.*,
        row_number() OVER (
            PARTITION BY digest
            ORDER BY
                CASE
                    WHEN cardinality(source_repository_ids) = 1 THEN 0
                    WHEN cardinality(build_provenance_repository_ids) = 1 THEN 1
                    ELSE 2
                END,
                CASE outcome WHEN 'exact_digest' THEN 0 ELSE 1 END,
                repository_id,
                image_ref,
                scope_id,
                support_id
        ) AS support_rank
    FROM filtered_supports AS support
    JOIN page_digests AS page USING (digest)
),
identity_strengths AS MATERIALIZED (
    SELECT support.digest, support.identity_strength AS value
    FROM (
        SELECT
            support.digest,
            support.identity_strength,
            support.support_id,
            row_number() OVER (
                PARTITION BY support.digest
                ORDER BY ` + containerImageIdentityStrengthOrderSQL + `
            ) AS strength_rank
        FROM ranked_supports AS support
    ) AS support
    WHERE support.strength_rank = 1
),
grouped_supports AS MATERIALIZED (
    SELECT
        support.digest,
        strength.value AS identity_strength,
        jsonb_agg(to_jsonb(source_repository_ids)) AS source_repository_sets,
        jsonb_agg(to_jsonb(build_provenance_repository_ids)) AS build_repository_sets,
        jsonb_agg(to_jsonb(workload_ids)) AS workload_sets,
        jsonb_agg(to_jsonb(service_ids)) AS service_sets,
        jsonb_agg(to_jsonb(source_layers)) AS source_layer_sets,
        jsonb_agg(to_jsonb(evidence_fact_ids)) AS evidence_fact_sets,
        jsonb_agg(to_jsonb(missing_evidence)) AS missing_evidence_sets,
        max(canonical_writes) AS canonical_writes
    FROM ranked_supports AS support
    JOIN identity_strengths AS strength USING (digest)
    GROUP BY support.digest, strength.value
)
SELECT
    winner.identity_id,
    winner.source_confidence,
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
        'source_repository_ids', ` + containerImageIdentityJSONTextArrayUnion("grouped.source_repository_sets") + `,
        'build_provenance_repository_ids', ` + containerImageIdentityJSONTextArrayUnion("grouped.build_repository_sets") + `,
        'workload_ids', ` + containerImageIdentityJSONTextArrayUnion("grouped.workload_sets") + `,
        'service_ids', ` + containerImageIdentityJSONTextArrayUnion("grouped.service_sets") + `,
        'outcome', winner.outcome,
        'reason', winner.reason,
        'canonical_id', winner.canonical_id,
        'canonical_writes', GREATEST(grouped.canonical_writes, 1),
        'evidence_fact_ids', ` + containerImageIdentityJSONTextArrayUnion("grouped.evidence_fact_sets") + `,
        'identity_strength', grouped.identity_strength,
        'publication_kind', 'reducer_container_image_identity',
        'source_layers', ` + containerImageIdentityJSONTextArrayUnion("grouped.source_layer_sets") + `,
        'missing_evidence', ` + containerImageIdentityJSONTextArrayUnion("grouped.missing_evidence_sets") + `
    ) AS payload
FROM ranked_supports AS winner
JOIN grouped_supports AS grouped USING (digest)
WHERE winner.support_rank = 1
ORDER BY winner.identity_id ASC
`

func containerImageIdentityJSONTextArrayUnion(expression string) string {
	return `COALESCE((
            SELECT jsonb_agg(value ORDER BY value)
            FROM (
                SELECT DISTINCT element.value
                FROM jsonb_array_elements(` + expression + `) AS array_value(value)
                CROSS JOIN LATERAL jsonb_array_elements_text(array_value.value) AS element(value)
            ) AS values(value)
        ), '[]'::jsonb)`
}

const containerImageIdentityAggregateFilteredSupportsCTE = `
WITH filtered_supports AS MATERIALIZED (
    SELECT support.*
    FROM container_image_identity_current_supports AS support
    WHERE ($1 = '' OR support.digest = $1)
      AND ($2 = '' OR support.image_ref = $2)
      AND ($3 = '' OR $3 = ANY(support.source_repository_ids))
      AND ($4 = '' OR support.repository_id = $4)
      AND ($5 = '' OR support.outcome = $5)
      AND (
            COALESCE(cardinality($6::text[]), 0) = 0
            OR support.source_repository_ids && $6::text[]
          )
)
`

const containerImageIdentityCanonicalSupportsCTE = `,
ranked_supports AS MATERIALIZED (
    SELECT
        support.*,
        row_number() OVER (
            PARTITION BY digest
            ORDER BY
                CASE
                    WHEN cardinality(source_repository_ids) = 1 THEN 0
                    WHEN cardinality(build_provenance_repository_ids) = 1 THEN 1
                    ELSE 2
                END,
                CASE outcome WHEN 'exact_digest' THEN 0 ELSE 1 END,
                repository_id,
                image_ref,
                scope_id,
                support_id
        ) AS support_rank
    FROM filtered_supports AS support
),
identity_strengths AS MATERIALIZED (
    SELECT support.digest, support.identity_strength AS value
    FROM (
        SELECT
            support.digest,
            support.identity_strength,
            support.support_id,
            row_number() OVER (
                PARTITION BY support.digest
                ORDER BY ` + containerImageIdentityStrengthOrderSQL + `
            ) AS strength_rank
        FROM filtered_supports AS support
    ) AS support
    WHERE support.strength_rank = 1
),
canonical_supports AS MATERIALIZED (
    SELECT support.*, strength.value AS canonical_identity_strength
    FROM ranked_supports AS support
    JOIN identity_strengths AS strength USING (digest)
    WHERE support.support_rank = 1
)
`

var containerImageIdentityAggregateTotalQuery = containerImageIdentityAggregateFilteredSupportsCTE + `
SELECT COUNT(DISTINCT digest) AS total
FROM filtered_supports;
`

var containerImageIdentityAggregateGroupQueryTemplate = containerImageIdentityAggregateFilteredSupportsCTE +
	containerImageIdentityCanonicalSupportsCTE + `
SELECT %s AS bucket, COUNT(*) AS bucket_count
FROM canonical_supports AS canonical
GROUP BY bucket;
`

var containerImageIdentityCanonicalInventoryQueryTemplate = containerImageIdentityAggregateFilteredSupportsCTE +
	containerImageIdentityCanonicalSupportsCTE + `
SELECT %s AS bucket, COUNT(*) AS bucket_count
FROM canonical_supports AS canonical
GROUP BY bucket
ORDER BY bucket_count DESC, bucket
LIMIT $7 OFFSET $8;
`

var containerImageIdentityInventoryQueryTemplate = containerImageIdentityAggregateFilteredSupportsCTE + `
SELECT
    COALESCE(NULLIF(support.repository_id, ''), 'unknown') AS bucket,
    COUNT(DISTINCT digest) AS bucket_count
FROM filtered_supports AS support
GROUP BY bucket
ORDER BY bucket_count DESC, bucket
LIMIT $7 OFFSET $8;
`
