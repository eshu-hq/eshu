// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

// listCloudResourceChangeEvidenceForGenerationQuery returns Azure resource
// change source facts for the active resource_changes lane that corresponds to
// the supplied inventory generation. Azure Resource Graph inventory and
// resource-changes facts are emitted from sibling source lanes, so the
// inventory generation identifies the tenant/subscription/family/location shard
// and this query resolves the active sibling resource_changes generation before
// reading facts. The final fact read remains bound to one scope generation and
// one fact kind; stale inventory generations are rejected by the admission
// handler before this query runs.
const listCloudResourceChangeEvidenceForGenerationQuery = `
WITH requested_inventory_generation AS (
    SELECT
        generation.scope_id AS scope_id,
        generation.generation_id AS generation_id
    FROM scope_generations AS generation
    WHERE generation.scope_id = $1
      AND generation.generation_id = $2
),
resource_change_scope AS (
    SELECT
        CASE
            WHEN source.scope_id LIKE 'azure:%:%:%:%:%:resource_graph' THEN
                left(source.scope_id, length(source.scope_id) - length(':resource_graph')) || ':resource_changes'
            WHEN source.scope_id LIKE 'azure:%:%:%:%:%:arm_fallback' THEN
                left(source.scope_id, length(source.scope_id) - length(':arm_fallback')) || ':resource_changes'
            ELSE source.scope_id
        END AS scope_id
    FROM requested_inventory_generation AS source
),
active_resource_change_generation AS (
    SELECT
        generation.scope_id AS scope_id,
        generation.generation_id AS generation_id
    FROM scope_generations AS generation
    JOIN resource_change_scope AS resolved
      ON resolved.scope_id = generation.scope_id
    WHERE generation.status = 'active'
    ORDER BY generation.observed_at DESC,
             generation.ingested_at DESC,
             generation.generation_id DESC
    LIMIT 1
),
change_facts AS (
    SELECT
        fact.fact_kind AS fact_kind,
        NULLIF(btrim(fact.payload->>'target_arm_resource_id'), '') AS raw_identity,
        fact.stable_fact_key AS stable_fact_key,
        fact.payload AS payload,
        fact.fact_id AS fact_id
    FROM fact_records AS fact
    JOIN active_resource_change_generation AS generation
      ON fact.scope_id = generation.scope_id
     AND fact.generation_id = generation.generation_id
    WHERE fact.scope_id = generation.scope_id
      AND fact.generation_id = generation.generation_id
      AND fact.fact_kind = 'azure_resource_change'
      AND fact.is_tombstone = FALSE
)
SELECT
    fact_kind,
    raw_identity,
    stable_fact_key,
    payload
FROM change_facts
WHERE raw_identity IS NOT NULL
ORDER BY raw_identity ASC, stable_fact_key ASC, fact_id ASC
`
