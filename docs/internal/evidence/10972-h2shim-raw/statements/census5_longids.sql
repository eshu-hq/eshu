-- H2 census step 5: long-id probe + small-id masses for the mixed chunk.
-- Diagnostic-only raised timeout; read-only transactions.
SET statement_timeout = '1800s';
BEGIN READ ONLY;
SELECT MAX(octet_length(cve_id)) AS max_cve_bytes,
       MAX(octet_length(advisory_id)) AS max_adv_bytes
  FROM supply_chain_impact_finding_supports;
COMMIT;
BEGIN READ ONLY;
SELECT m AS long_member, octet_length(m) AS bytes FROM supply_chain_impact_finding_supports,
  LATERAL (SELECT jsonb_array_elements_text(payload #> '{provenance,advisory_group_members}') AS m) t
  WHERE octet_length(m) > 1024 LIMIT 5;
COMMIT;
BEGIN READ ONLY;
SELECT 'direct:GHSA-f4gw-2p7v-4548', COUNT(*) FROM supply_chain_impact_finding_supports WHERE advisory_id = 'GHSA-f4gw-2p7v-4548';
SELECT 'direct:CVE-2026-112943', COUNT(*) FROM supply_chain_impact_finding_supports WHERE cve_id = 'CVE-2026-112943';
SELECT 'direct:GHSA-pmv8-rq9r-6j72', COUNT(*) FROM supply_chain_impact_finding_supports WHERE advisory_id = 'GHSA-pmv8-rq9r-6j72';
SELECT 'direct:CVE-2026-73569', COUNT(*) FROM supply_chain_impact_finding_supports WHERE cve_id = 'CVE-2026-73569';
SELECT 'direct:GHSA-4528-x5fx-h6hg', COUNT(*) FROM supply_chain_impact_finding_supports WHERE advisory_id = 'GHSA-4528-x5fx-h6hg';
SELECT 'member:CVE-2026-73569', COUNT(*) FROM supply_chain_impact_finding_supports WHERE (payload #> '{provenance,advisory_group_members}') ?| ARRAY['CVE-2026-73569'];
COMMIT;
