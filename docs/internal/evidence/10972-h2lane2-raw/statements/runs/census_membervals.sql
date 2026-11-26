BEGIN;
SET TRANSACTION ISOLATION LEVEL REPEATABLE READ, READ ONLY;
SET LOCAL statement_timeout = '60s';
\timing on
SELECT DISTINCT m.v
FROM (SELECT payload FROM supply_chain_impact_finding_supports WHERE cve_id = 'CVE-2026-42043' LIMIT 2000) s,
     LATERAL jsonb_array_elements_text(s.payload #> '{provenance,advisory_group_members}') AS m(v)
LIMIT 60;
COMMIT;
