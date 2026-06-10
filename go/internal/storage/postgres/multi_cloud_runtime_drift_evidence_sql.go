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
// provider-specific key, so the join checks the union of the three identity
// keys (AWS arn, Azure id, GCP self_link or id). COALESCE over those keys keys
// the row by whichever identity is present; the loader re-resolves the matched
// identity into the canonical uid keyspace so a coincidental match in the wrong
// provider keyspace is dropped rather than joined.
const listActiveStateResourcesForMultiCloudIdentitiesQuery = `
WITH requested_identities AS (
    SELECT DISTINCT value AS identity
    FROM jsonb_array_elements_text($1::jsonb) AS value
    WHERE btrim(value) <> ''
)
SELECT
    fact.scope_id            AS state_scope_id,
    fact.generation_id       AS state_generation_id,
    fact.payload->>'address' AS address,
    matched.identity         AS matched_identity,
    fact.payload             AS payload
FROM fact_records AS fact
JOIN ingestion_scopes AS scope
  ON scope.scope_id = fact.scope_id
 AND scope.active_generation_id = fact.generation_id
JOIN requested_identities AS matched
  ON matched.identity = COALESCE(
        fact.payload->'attributes'->>'arn',
        fact.payload->'attributes'->>'id',
        fact.payload->'attributes'->>'self_link'
     )
WHERE fact.fact_kind = 'terraform_state_resource'
  AND fact.is_tombstone = FALSE
ORDER BY matched.identity ASC, fact.scope_id ASC, fact.payload->>'address' ASC, fact.fact_id ASC
`
