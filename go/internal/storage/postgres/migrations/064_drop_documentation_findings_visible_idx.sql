-- Replace the stale documentation-finding index whose partial predicate still
-- encoded the pre-2164 source ACL filter.
DROP INDEX CONCURRENTLY IF EXISTS fact_records_documentation_findings_visible_idx;
