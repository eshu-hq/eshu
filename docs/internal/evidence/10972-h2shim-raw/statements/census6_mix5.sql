-- H2 diagnostics: member-arm mass for mix5 + plan shapes of failing ids.
-- Read-only; 60s diagnostic timeout (no ANALYZE here, planning only).
SET statement_timeout = '60s';
BEGIN READ ONLY;
SELECT 'member:CVE-2026-112943', COUNT(*) FROM supply_chain_impact_finding_supports WHERE (payload #> '{provenance,advisory_group_members}') ?| ARRAY['CVE-2026-112943'];
SELECT 'direct:CVE-2026-112943-cveonly', COUNT(*) FROM supply_chain_impact_finding_supports WHERE cve_id = 'CVE-2026-112943';
COMMIT;
