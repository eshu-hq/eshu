BEGIN;
SET TRANSACTION ISOLATION LEVEL REPEATABLE READ, READ ONLY;
SET LOCAL statement_timeout = '300s';
\timing on
SELECT 'GHSA-7r86-cg39-jmmj' AS id, (SELECT COUNT(*) FROM supply_chain_impact_finding_supports WHERE cve_id = 'GHSA-7r86-cg39-jmmj') AS cve, (SELECT COUNT(*) FROM supply_chain_impact_finding_supports WHERE advisory_id = 'GHSA-7r86-cg39-jmmj') AS adv, (SELECT COUNT(*) FROM supply_chain_impact_finding_supports WHERE payload #> '{provenance,advisory_group_members}' ? 'GHSA-7r86-cg39-jmmj') AS m
UNION ALL
SELECT 'GHSA-23c5-xmqv-rm74' AS id, (SELECT COUNT(*) FROM supply_chain_impact_finding_supports WHERE cve_id = 'GHSA-23c5-xmqv-rm74') AS cve, (SELECT COUNT(*) FROM supply_chain_impact_finding_supports WHERE advisory_id = 'GHSA-23c5-xmqv-rm74') AS adv, (SELECT COUNT(*) FROM supply_chain_impact_finding_supports WHERE payload #> '{provenance,advisory_group_members}' ? 'GHSA-23c5-xmqv-rm74') AS m
UNION ALL
SELECT 'GHSA-3ppc-4f35-3m26' AS id, (SELECT COUNT(*) FROM supply_chain_impact_finding_supports WHERE cve_id = 'GHSA-3ppc-4f35-3m26') AS cve, (SELECT COUNT(*) FROM supply_chain_impact_finding_supports WHERE advisory_id = 'GHSA-3ppc-4f35-3m26') AS adv, (SELECT COUNT(*) FROM supply_chain_impact_finding_supports WHERE payload #> '{provenance,advisory_group_members}' ? 'GHSA-3ppc-4f35-3m26') AS m
UNION ALL
SELECT 'GHSA-g3hv-75wg-m249' AS id, (SELECT COUNT(*) FROM supply_chain_impact_finding_supports WHERE cve_id = 'GHSA-g3hv-75wg-m249') AS cve, (SELECT COUNT(*) FROM supply_chain_impact_finding_supports WHERE advisory_id = 'GHSA-g3hv-75wg-m249') AS adv, (SELECT COUNT(*) FROM supply_chain_impact_finding_supports WHERE payload #> '{provenance,advisory_group_members}' ? 'GHSA-g3hv-75wg-m249') AS m
UNION ALL
SELECT 'GHSA-hxm2-r34f-qmc5' AS id, (SELECT COUNT(*) FROM supply_chain_impact_finding_supports WHERE cve_id = 'GHSA-hxm2-r34f-qmc5') AS cve, (SELECT COUNT(*) FROM supply_chain_impact_finding_supports WHERE advisory_id = 'GHSA-hxm2-r34f-qmc5') AS adv, (SELECT COUNT(*) FROM supply_chain_impact_finding_supports WHERE payload #> '{provenance,advisory_group_members}' ? 'GHSA-hxm2-r34f-qmc5') AS m;
COMMIT;
