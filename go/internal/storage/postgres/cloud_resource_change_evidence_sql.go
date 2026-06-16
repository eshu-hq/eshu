package postgres

// listCloudResourceChangeEvidenceForGenerationQuery returns Azure resource
// change source facts for one sealed collector generation. It projects only the
// changed target ARM id, stable fact key, and bounded payload. The scope_id +
// generation_id + fact_kind predicate is served by fact_records indexes and
// keeps the read bounded to one generation; stale generation rejection remains
// owned by the admission handler before any canonical write.
const listCloudResourceChangeEvidenceForGenerationQuery = `
WITH change_facts AS (
    SELECT
        fact.fact_kind AS fact_kind,
        NULLIF(btrim(fact.payload->>'target_arm_resource_id'), '') AS raw_identity,
        fact.stable_fact_key AS stable_fact_key,
        fact.payload AS payload,
        fact.fact_id AS fact_id
    FROM fact_records AS fact
    WHERE fact.scope_id = $1
      AND fact.generation_id = $2
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
