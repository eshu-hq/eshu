// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

// listCloudIdentityPolicyEvidenceForGenerationQuery returns Azure
// identity-policy source facts for one sealed collector generation. It projects
// the raw ARM identity used only for reducer attachment; raw principal GUIDs and
// raw assignment scopes remain inside the source payload and are not selected as
// first-class row fields.
//
// The scope_id + generation_id + fact_kind predicate is served by
// fact_records_scope_generation_idx (scope_id, generation_id, fact_kind,
// observed_at DESC), so the scan is index-bounded to one generation and never
// walks the whole table. is_tombstone = FALSE keeps retired source facts out.
const listCloudIdentityPolicyEvidenceForGenerationQuery = `
WITH identity_facts AS (
    SELECT
        fact.fact_kind AS fact_kind,
        NULLIF(btrim(fact.payload->>'arm_resource_id'), '') AS raw_identity,
        fact.stable_fact_key AS stable_fact_key,
        fact.payload AS payload,
        fact.fact_id AS fact_id
    FROM fact_records AS fact
    WHERE fact.scope_id = $1
      AND fact.generation_id = $2
      AND fact.fact_kind = 'azure_identity_observation'
      AND fact.is_tombstone = FALSE
)
SELECT
    fact_kind,
    raw_identity,
    stable_fact_key,
    payload
FROM identity_facts
WHERE raw_identity IS NOT NULL
ORDER BY raw_identity ASC, stable_fact_key ASC, fact_id ASC
`
