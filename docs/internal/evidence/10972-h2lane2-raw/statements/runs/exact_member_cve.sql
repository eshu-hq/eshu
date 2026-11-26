BEGIN;
SET TRANSACTION ISOLATION LEVEL REPEATABLE READ, READ ONLY;
SET LOCAL statement_timeout = '300s';
\timing on
SELECT 'CVE-2026-42043' AS id, (SELECT COUNT(*) FROM supply_chain_impact_finding_supports WHERE cve_id = 'CVE-2026-42043') AS cve, (SELECT COUNT(*) FROM supply_chain_impact_finding_supports WHERE advisory_id = 'CVE-2026-42043') AS adv, (SELECT COUNT(*) FROM supply_chain_impact_finding_supports WHERE payload #> '{provenance,advisory_group_members}' ? 'CVE-2026-42043') AS m;
COMMIT;
