-- Retire the legacy ACL-filtered index only after its corrected replacement
-- exists, preserving a usable findings index throughout an online upgrade.
DROP INDEX CONCURRENTLY IF EXISTS fact_records_documentation_findings_visible_idx;
