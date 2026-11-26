BEGIN;
SET TRANSACTION ISOLATION LEVEL REPEATABLE READ, READ ONLY;
SET LOCAL statement_timeout = '300s';
\timing on
SELECT 'GHSA-7r86-cg39-jmmj' AS id, (SELECT COUNT(*) FROM supply_chain_impact_finding_supports WHERE cve_id = 'GHSA-7r86-cg39-jmmj') AS cve, (SELECT COUNT(*) FROM supply_chain_impact_finding_supports WHERE advisory_id = 'GHSA-7r86-cg39-jmmj') AS adv, (SELECT COUNT(*) FROM supply_chain_impact_finding_supports WHERE payload #> '{provenance,advisory_group_members}' ? 'GHSA-7r86-cg39-jmmj') AS m
UNION ALL
SELECT 'CVE-2026-42043' AS id, (SELECT COUNT(*) FROM supply_chain_impact_finding_supports WHERE cve_id = 'CVE-2026-42043') AS cve, (SELECT COUNT(*) FROM supply_chain_impact_finding_supports WHERE advisory_id = 'CVE-2026-42043') AS adv, (SELECT COUNT(*) FROM supply_chain_impact_finding_supports WHERE payload #> '{provenance,advisory_group_members}' ? 'CVE-2026-42043') AS m
UNION ALL
SELECT 'CVE-2026-73569' AS id, (SELECT COUNT(*) FROM supply_chain_impact_finding_supports WHERE cve_id = 'CVE-2026-73569') AS cve, (SELECT COUNT(*) FROM supply_chain_impact_finding_supports WHERE advisory_id = 'CVE-2026-73569') AS adv, (SELECT COUNT(*) FROM supply_chain_impact_finding_supports WHERE payload #> '{provenance,advisory_group_members}' ? 'CVE-2026-73569') AS m
UNION ALL
SELECT 'GHSA-4528-x5fx-h6hg' AS id, (SELECT COUNT(*) FROM supply_chain_impact_finding_supports WHERE cve_id = 'GHSA-4528-x5fx-h6hg') AS cve, (SELECT COUNT(*) FROM supply_chain_impact_finding_supports WHERE advisory_id = 'GHSA-4528-x5fx-h6hg') AS adv, (SELECT COUNT(*) FROM supply_chain_impact_finding_supports WHERE payload #> '{provenance,advisory_group_members}' ? 'GHSA-4528-x5fx-h6hg') AS m
UNION ALL
SELECT 'CVE-2026-112943' AS id, (SELECT COUNT(*) FROM supply_chain_impact_finding_supports WHERE cve_id = 'CVE-2026-112943') AS cve, (SELECT COUNT(*) FROM supply_chain_impact_finding_supports WHERE advisory_id = 'CVE-2026-112943') AS adv, (SELECT COUNT(*) FROM supply_chain_impact_finding_supports WHERE payload #> '{provenance,advisory_group_members}' ? 'CVE-2026-112943') AS m;
COMMIT;
