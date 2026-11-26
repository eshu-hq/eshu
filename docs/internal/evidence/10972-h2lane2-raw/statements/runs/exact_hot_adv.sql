BEGIN;
SET TRANSACTION ISOLATION LEVEL REPEATABLE READ, READ ONLY;
SET LOCAL statement_timeout = '300s';
\timing on
SELECT 'GHSA-7r86-cg39-jmmj' AS id, (SELECT COUNT(*) FROM supply_chain_impact_finding_supports WHERE cve_id = 'GHSA-7r86-cg39-jmmj') AS cve, (SELECT COUNT(*) FROM supply_chain_impact_finding_supports WHERE advisory_id = 'GHSA-7r86-cg39-jmmj') AS adv, (SELECT COUNT(*) FROM supply_chain_impact_finding_supports WHERE payload #> '{provenance,advisory_group_members}' ? 'GHSA-7r86-cg39-jmmj') AS m;
COMMIT;
