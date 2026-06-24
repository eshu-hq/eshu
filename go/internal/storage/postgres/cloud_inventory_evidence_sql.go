// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

// listCloudInventorySourceFactsForGenerationQuery returns the provider
// cloud-inventory source facts for one sealed collector generation. It loads the
// three shared inventory source fact kinds (aws_resource, gcp_cloud_resource,
// azure_cloud_resource) and projects the provider-native raw identity strictly
// from the key that fact kind's own provider preserves it under: AWS arn, GCP
// full_resource_name, and Azure arm_resource_id. Selecting per fact kind (not a
// blind COALESCE across every provider key) prevents one provider's stray key
// from supplying a raw identity for a different provider, which would resolve
// into the wrong provider keyspace.
//
// The scope_id + generation_id + fact_kind predicate is served by
// fact_records_scope_generation_idx (scope_id, generation_id, fact_kind,
// observed_at DESC), so the scan is index-bounded to one generation and never
// walks the whole table; a stale generation cannot leak rows into a newer
// admission. is_tombstone = FALSE keeps retired source facts out of canonical
// admission. The raw-identity predicate drops blank-identity rows at the
// database boundary because the reducer cannot key an empty raw identity.
const listCloudInventorySourceFactsForGenerationQuery = `
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
