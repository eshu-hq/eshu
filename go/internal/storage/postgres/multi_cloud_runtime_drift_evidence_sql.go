// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

// listMultiCloudObservedResourcesForGenerationQuery returns the provider
// cloud-inventory source facts for one sealed collector generation, projecting
// each provider's native raw identity from the key its own fact kind preserves
// it under: AWS arn, GCP full_resource_name, Azure arm_resource_id. This mirrors
// listCloudInventorySourceFactsForGenerationQuery so the observed side of the
// multi-cloud drift join uses exactly the same raw-identity contract the shared
// cloud_resource_uid keyspace was admitted under. Selecting per fact kind (not a
// blind COALESCE across every provider key) prevents one provider's stray key
// from supplying a raw identity for a different provider.
//
// The scope_id + generation_id + fact_kind predicate is served by
// fact_records_scope_generation_idx (scope_id, generation_id, fact_kind,
// observed_at DESC), so the scan is index-bounded to one generation and never
// walks the whole table; a stale generation cannot leak rows into a newer
// evaluation. is_tombstone = FALSE keeps retired source facts out of the join.
const listMultiCloudObservedResourcesForGenerationQuery = `
SELECT
    fact.fact_kind AS fact_kind,
    NULLIF(btrim(
        CASE fact.fact_kind
            WHEN 'aws_resource'         THEN fact.payload->>'arn'
            WHEN 'gcp_cloud_resource'   THEN fact.payload->>'full_resource_name'
            WHEN 'azure_cloud_resource' THEN fact.payload->>'arm_resource_id'
        END
    ), '') AS raw_identity,
    fact.payload AS payload
FROM fact_records AS fact
WHERE fact.scope_id = $1
  AND fact.generation_id = $2
  AND fact.fact_kind IN ('aws_resource', 'gcp_cloud_resource', 'azure_cloud_resource')
  AND fact.is_tombstone = FALSE
  AND NULLIF(btrim(
        CASE fact.fact_kind
            WHEN 'aws_resource'         THEN fact.payload->>'arn'
            WHEN 'gcp_cloud_resource'   THEN fact.payload->>'full_resource_name'
            WHEN 'azure_cloud_resource' THEN fact.payload->>'arm_resource_id'
        END
      ), '') IS NOT NULL
ORDER BY fact.fact_kind ASC, raw_identity ASC, fact.fact_id ASC
`

// listActiveStateResourcesForMultiCloudIdentitiesQuery returns active
// terraform_state_resource facts whose provider-native identity matches one of
// the observed raw identities already loaded for the current generation. The
// JSON allowlist in $1 keeps the state scan tied to the observed identities in
// memory, while the requested_identities CTE rechecks at the database boundary
// so stale caller input cannot widen the join.
//
// Terraform stores the provider-native identity inside attributes under a
// provider-specific key, so the state side projects the union of the three
// identity keys (AWS arn, Azure id, GCP self_link or id) via COALESCE into one
// native_identity. Both sides reduce that identity to a match_key: AWS ARNs and
// GCP full resource names are case-significant and key verbatim, while Azure ARM
// ids (rooted at /subscriptions/) are case-insensitive per Azure and the shared
// cloud_resource_uid keyspace lower-cases them, so the match_key folds Azure-
// shaped identities to lower case. The equijoin on match_key therefore lets an
// Azure state row whose attributes.id differs only in casing from the observed
// arm_resource_id join, while AWS/GCP casing differences stay distinct. The
// loader re-resolves the returned native_identity into the canonical uid
// keyspace (exact, then Azure case-folded) so a coincidental match in the wrong
// provider keyspace is dropped rather than joined.
const listActiveStateResourcesForMultiCloudIdentitiesQuery = `
WITH requested_identities AS (
    SELECT DISTINCT
        btrim(value) AS identity,
        CASE
            WHEN lower(btrim(value)) LIKE '/subscriptions/%' THEN lower(btrim(value))
            ELSE btrim(value)
        END AS match_key
    FROM jsonb_array_elements_text($1::jsonb) AS value
    WHERE btrim(value) <> ''
),
state_resources AS (
    SELECT
        fact.scope_id      AS scope_id,
        fact.generation_id AS generation_id,
        fact.fact_id       AS fact_id,
        fact.payload       AS payload,
        COALESCE(
            fact.payload->'attributes'->>'arn',
            fact.payload->'attributes'->>'id',
            fact.payload->'attributes'->>'self_link'
        ) AS native_identity
    FROM fact_records AS fact
    JOIN ingestion_scopes AS scope
      ON scope.scope_id = fact.scope_id
     AND scope.active_generation_id = fact.generation_id
    WHERE fact.fact_kind = 'terraform_state_resource'
      AND fact.is_tombstone = FALSE
)
SELECT
    state.scope_id            AS state_scope_id,
    state.generation_id       AS state_generation_id,
    state.payload->>'address' AS address,
    state.native_identity     AS matched_identity,
    state.payload             AS payload
FROM state_resources AS state
JOIN requested_identities AS matched
  ON matched.match_key = CASE
        WHEN lower(state.native_identity) LIKE '/subscriptions/%' THEN lower(state.native_identity)
        ELSE state.native_identity
     END
ORDER BY state.native_identity ASC, state.scope_id ASC, state.payload->>'address' ASC, state.fact_id ASC
`
