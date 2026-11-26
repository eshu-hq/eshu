-- H2 census step 5b: small-id masses for the mixed chunk (indexed, fast).
-- Diagnostic-only 60s per-statement timeout; read-only transaction.
SET statement_timeout = '60s';
BEGIN READ ONLY;
SELECT 'direct:GHSA-f4gw-2p7v-4548', COUNT(*) FROM supply_chain_impact_finding_supports WHERE advisory_id = 'GHSA-f4gw-2p7v-4548';
SELECT 'direct:CVE-2026-112943', COUNT(*) FROM supply_chain_impact_finding_supports WHERE cve_id = 'CVE-2026-112943';
SELECT 'direct:GHSA-pmv8-rq9r-6j72', COUNT(*) FROM supply_chain_impact_finding_supports WHERE advisory_id = 'GHSA-pmv8-rq9r-6j72';
SELECT 'direct:CVE-2026-73569', COUNT(*) FROM supply_chain_impact_finding_supports WHERE cve_id = 'CVE-2026-73569';
SELECT 'direct:GHSA-4528-x5fx-h6hg', COUNT(*) FROM supply_chain_impact_finding_supports WHERE advisory_id = 'GHSA-4528-x5fx-h6hg';
SELECT 'member:CVE-2026-73569', COUNT(*) FROM supply_chain_impact_finding_supports WHERE (payload #> '{provenance,advisory_group_members}') ?| ARRAY['CVE-2026-73569'];
SELECT 'direct:GHSA-c29m-xwm3-cm6r', COUNT(*) FROM supply_chain_impact_finding_supports WHERE advisory_id = 'GHSA-c29m-xwm3-cm6r';
COMMIT;
