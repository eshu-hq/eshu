// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

// listCloudTagEvidenceForGenerationQuery returns the tag-evidence source facts
// for one sealed collector generation. It loads the tag-evidence fact kinds
// and projects the provider raw identity (the tag subject) from the provider's
// source identity field: arm_resource_id for Azure, full_resource_name for GCP.
//
// The scope_id + generation_id + fact_kind predicate is served by
// fact_records_scope_generation_idx (scope_id, generation_id, fact_kind,
// observed_at DESC), so the scan is index-bounded to one generation and never
// walks the whole table; a stale generation cannot leak rows into a newer
// admission. is_tombstone = FALSE keeps retired source facts out. The
// raw-identity predicate drops blank-subject rows at the database boundary
// because the reducer cannot key an empty raw identity.
const listCloudTagEvidenceForGenerationQuery = `
WITH tag_facts AS (
    SELECT
        fact.fact_kind AS fact_kind,
        CASE fact.fact_kind
            WHEN 'azure_tag_observation' THEN NULLIF(btrim(fact.payload->>'arm_resource_id'), '')
            WHEN 'gcp_tag_observation' THEN NULLIF(btrim(fact.payload->>'full_resource_name'), '')
        END AS raw_identity,
        fact.payload AS payload,
        fact.fact_id AS fact_id
    FROM fact_records AS fact
    WHERE fact.scope_id = $1
      AND fact.generation_id = $2
      AND fact.fact_kind IN ('azure_tag_observation', 'gcp_tag_observation')
      AND fact.is_tombstone = FALSE
)
SELECT
    fact_kind,
    raw_identity,
    payload
FROM tag_facts
WHERE raw_identity IS NOT NULL
ORDER BY raw_identity ASC, fact_id ASC
`
