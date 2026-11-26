BEGIN;
SET TRANSACTION ISOLATION LEVEL REPEATABLE READ, READ ONLY;
SET LOCAL statement_timeout = '10s';
PREPARE h2 AS

WITH candidates AS MATERIALIZED (
    SELECT support.scope_id, support.generation_id
    FROM supply_chain_impact_finding_supports AS support
    WHERE support.cve_id <> ''
      AND support.cve_id = ANY($1::text[])
    UNION
    SELECT support.scope_id, support.generation_id
    FROM supply_chain_impact_finding_supports AS support
    WHERE support.advisory_id <> ''
      AND support.advisory_id = ANY($1::text[])
    UNION
    SELECT support.scope_id, support.generation_id
    FROM supply_chain_impact_finding_supports AS support
    WHERE support.payload #> '{provenance,advisory_group_members}' ?| $1::text[]
)
SELECT DISTINCT candidates.scope_id
FROM candidates
JOIN ingestion_scopes AS scope
  ON scope.scope_id = candidates.scope_id
 AND scope.active_generation_id = candidates.generation_id
WHERE candidates.scope_id <> $2
ORDER BY candidates.scope_id;
EXECUTE h2(ARRAY['GHSA-3ppc-4f35-3m26'], 'h2-shim-own-scope');
DEALLOCATE h2;
COMMIT;
