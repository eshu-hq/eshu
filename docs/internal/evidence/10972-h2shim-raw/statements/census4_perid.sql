-- H2 census step 4: exact per-id direct + member-arm counts (indexed, fast).
-- Diagnostic-only 60s per-statement timeout; read-only transactions.
SET statement_timeout = '60s';
BEGIN READ ONLY;
SELECT 'direct:GHSA-7r86-cg39-jmmj', COUNT(*) FROM supply_chain_impact_finding_supports WHERE advisory_id = 'GHSA-7r86-cg39-jmmj';
SELECT 'member:GHSA-7r86-cg39-jmmj', COUNT(*) FROM supply_chain_impact_finding_supports WHERE (payload #> '{provenance,advisory_group_members}') ?| ARRAY['GHSA-7r86-cg39-jmmj'];
SELECT 'direct:CVE-2026-26996', COUNT(*) FROM supply_chain_impact_finding_supports WHERE cve_id = 'CVE-2026-26996';
SELECT 'member:CVE-2026-26996', COUNT(*) FROM supply_chain_impact_finding_supports WHERE (payload #> '{provenance,advisory_group_members}') ?| ARRAY['CVE-2026-26996'];
SELECT 'direct:CVE-2025-27789', COUNT(*) FROM supply_chain_impact_finding_supports WHERE cve_id = 'CVE-2025-27789';
SELECT 'member:CVE-2025-27789', COUNT(*) FROM supply_chain_impact_finding_supports WHERE (payload #> '{provenance,advisory_group_members}') ?| ARRAY['CVE-2025-27789'];
SELECT 'direct:GHSA-968p-4wvh-cqc8', COUNT(*) FROM supply_chain_impact_finding_supports WHERE advisory_id = 'GHSA-968p-4wvh-cqc8';
SELECT 'member:GHSA-968p-4wvh-cqc8', COUNT(*) FROM supply_chain_impact_finding_supports WHERE (payload #> '{provenance,advisory_group_members}') ?| ARRAY['GHSA-968p-4wvh-cqc8'];
SELECT 'direct:CVE-2026-42043', COUNT(*) FROM supply_chain_impact_finding_supports WHERE cve_id = 'CVE-2026-42043';
SELECT 'member:CVE-2026-42043', COUNT(*) FROM supply_chain_impact_finding_supports WHERE (payload #> '{provenance,advisory_group_members}') ?| ARRAY['CVE-2026-42043'];
SELECT 'direct:GHSA-c29m-xwm3-cm6r', COUNT(*) FROM supply_chain_impact_finding_supports WHERE advisory_id = 'GHSA-c29m-xwm3-cm6r';
SELECT 'member:GHSA-c29m-xwm3-cm6r', COUNT(*) FROM supply_chain_impact_finding_supports WHERE (payload #> '{provenance,advisory_group_members}') ?| ARRAY['GHSA-c29m-xwm3-cm6r'];
SELECT 'direct:GHSA-fvcv-3m26-pcqx', COUNT(*) FROM supply_chain_impact_finding_supports WHERE advisory_id = 'GHSA-fvcv-3m26-pcqx';
SELECT 'direct:GHSA-m8m8-qj5v-23w3', COUNT(*) FROM supply_chain_impact_finding_supports WHERE advisory_id = 'GHSA-m8m8-qj5v-23w3';
SELECT 'direct:CVE-2026-40175', COUNT(*) FROM supply_chain_impact_finding_supports WHERE cve_id = 'CVE-2026-40175';
SELECT 'member:GHSA-23c5-xmqv-rm74', COUNT(*) FROM supply_chain_impact_finding_supports WHERE (payload #> '{provenance,advisory_group_members}') ?| ARRAY['GHSA-23c5-xmqv-rm74'];
COMMIT;
