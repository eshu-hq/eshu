package postgres

// listCloudTagEvidenceForGenerationQuery returns the tag-evidence source facts
// for one sealed collector generation. It loads the tag-evidence fact kinds
// (currently azure_tag_observation) and projects the provider raw identity (the
// tag subject) from arm_resource_id, the key the Azure collector preserves it
// under.
//
// The scope_id + generation_id + fact_kind predicate is served by
// fact_records_scope_generation_idx (scope_id, generation_id, fact_kind,
// observed_at DESC), so the scan is index-bounded to one generation and never
// walks the whole table; a stale generation cannot leak rows into a newer
// admission. is_tombstone = FALSE keeps retired source facts out. The
// raw-identity predicate drops blank-subject rows at the database boundary
// because the reducer cannot key an empty raw identity.
const listCloudTagEvidenceForGenerationQuery = `
SELECT
    fact.fact_kind AS fact_kind,
    NULLIF(btrim(fact.payload->>'arm_resource_id'), '') AS raw_identity,
    fact.payload AS payload
FROM fact_records AS fact
WHERE fact.scope_id = $1
  AND fact.generation_id = $2
  AND fact.fact_kind IN ('azure_tag_observation')
  AND fact.is_tombstone = FALSE
  AND NULLIF(btrim(fact.payload->>'arm_resource_id'), '') IS NOT NULL
ORDER BY raw_identity ASC, fact.fact_id ASC
`
