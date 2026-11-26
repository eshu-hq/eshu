BEGIN;
SET TRANSACTION ISOLATION LEVEL REPEATABLE READ, READ ONLY;
SET LOCAL statement_timeout = '10s';
SET LOCAL plan_cache_mode = 'force_custom_plan';
\timing on
PREPARE lane2_kr0(text[], text, text, text, text[]) AS WITH candidates AS MATERIALIZED (
    SELECT support.scope_id, support.generation_id
    FROM supply_chain_impact_finding_supports AS support
    WHERE support.cve_id <> ''
      AND support.cve_id = ANY($1::text[])
      AND support.fact_id >= $3
      AND support.fact_id < $4
    UNION
    SELECT support.scope_id, support.generation_id
    FROM supply_chain_impact_finding_supports AS support
    WHERE support.advisory_id <> ''
      AND support.advisory_id = ANY($1::text[])
      AND support.fact_id >= $3
      AND support.fact_id < $4
    UNION
    SELECT support.scope_id, support.generation_id
    FROM supply_chain_impact_finding_supports AS support
    WHERE eshu_sci_foreign_member_tokens_v4(support.fact_id, support.payload) && $5::text[]
)
SELECT DISTINCT candidates.scope_id
FROM candidates
JOIN ingestion_scopes AS scope
  ON scope.scope_id = candidates.scope_id
 AND scope.active_generation_id = candidates.generation_id
WHERE candidates.scope_id <> $2
ORDER BY candidates.scope_id;
EXECUTE lane2_kr0(ARRAY['GHSA-7r86-cg39-jmmj']::text[], 'scope:none', '', 'reducer_supply_chain_impact_finding:1', ARRAY['0:GHSA-7r86-cg39-jmmj','1:GHSA-7r86-cg39-jmmj']::text[]);
DEALLOCATE lane2_kr0;
COMMIT;
