-- 049_drop_fact_records_stable_key_idx.sql
--
-- Drops the unused fact_records_stable_key_idx on
-- fact_records (stable_fact_key, generation_id). This index has no query reader:
-- the changed-since diff uses fact_records_scope_generation_idx, and the fact
-- upsert conflicts on fact_id (primary key). At 2132 MB on a live stack with
-- 22h uptime it is the largest idx_scan=0 general secondary index on
-- fact_records, and dropping it reclaims that space plus eliminates one B-tree
-- write per fact INSERT.
--
-- CONCURRENTLY avoids blocking writers while the index is removed on populated
-- deployments. New databases no longer create this index in the embedded Go
-- migration set's 003_fact_records.sql, so this migration is a no-op there.

DROP INDEX CONCURRENTLY IF EXISTS fact_records_stable_key_idx;
