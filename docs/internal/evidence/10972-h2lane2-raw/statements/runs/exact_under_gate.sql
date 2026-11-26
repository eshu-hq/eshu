BEGIN;
SET TRANSACTION ISOLATION LEVEL REPEATABLE READ, READ ONLY;
SET LOCAL statement_timeout = '300s';
\timing on
SELECT 'GHSA-4528-x5fx-h6hg' AS id, (SELECT COUNT(*) FROM supply_chain_impact_finding_supports WHERE cve_id = 'GHSA-4528-x5fx-h6hg') AS cve, (SELECT COUNT(*) FROM supply_chain_impact_finding_supports WHERE advisory_id = 'GHSA-4528-x5fx-h6hg') AS adv, (SELECT COUNT(*) FROM supply_chain_impact_finding_supports WHERE payload #> '{provenance,advisory_group_members}' ? 'GHSA-4528-x5fx-h6hg') AS m;
COMMIT;
