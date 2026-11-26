BEGIN;
SET TRANSACTION ISOLATION LEVEL REPEATABLE READ, READ ONLY;
SET LOCAL statement_timeout = '120s';
\timing on
SELECT m.id, COUNT(*) AS n
FROM supply_chain_impact_finding_supports TABLESAMPLE SYSTEM (1),
     LATERAL jsonb_array_elements_text(payload #> '{provenance,advisory_group_members}') AS m(id)
GROUP BY 1 ORDER BY 2 DESC LIMIT 50;
COMMIT;
