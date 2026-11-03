-- QA read-only readings for eshu-hq/eshu#9774 (winners resweep write-on-change).
-- Run against the QA environment read replica (`eshu-postgresql-read-0`, `eshu`
-- db); every statement here is a SELECT and changes no cluster state.

-- 1. per-tick change rate: two hashed dumps of the winners-table content
--    (everything except canonical_key and materialized_at) taken ~5 minutes
--    apart. Diff the two dumps: inserted = keys in the second dump only,
--    deleted = keys in the first only, changed = same key, different hash,
--    touched = inserted + deleted + changed.
SELECT canonical_key,
       md5((to_jsonb(w) - 'materialized_at' - 'canonical_key')::text) AS content_hash
FROM supply_chain_impact_canonical_winners AS w;

-- 2. pg_stat_database.stats_reset for the eshu database, plus the primary's
--    per-table row-version counters for the winners table. stats_reset is NULL
--    when the statistics have never been reset since the server started.
SELECT stats_reset
FROM pg_stat_database
WHERE datname = 'eshu';

SELECT relname, n_tup_ins, n_tup_upd, n_tup_del, n_live_tup, n_dead_tup, last_autovacuum
FROM pg_stat_user_tables
WHERE relname IN ('supply_chain_impact_canonical_winners', 'supply_chain_impact_winners_materialization');
