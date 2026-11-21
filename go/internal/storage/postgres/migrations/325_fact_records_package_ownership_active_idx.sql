-- Drive the code-import ownership load from fact_records instead of from the
-- active generations (#11082).
--
-- ListActivePackageOwnershipFacts (facts_active_package_ownership.go) loads
-- the active, non-tombstoned package-registry identity plus reducer ownership
-- and publication correlation facts across package-registry scopes: three
-- fact kinds, no scope or generation equality prefix. No existing index can
-- serve that cross-scope (observed_at, fact_id) scan: the keyset and
-- scope-generation indexes both require a scope+generation equality prefix,
-- and the active-repository index is partial to fact_kind = 'repository'.
-- The folded single statement therefore planned, under the generic plan the
-- services get through database/sql, as a nested loop over every active
-- (scope, generation) pair probing fact_records with the kind, tombstone,
-- and cursor predicates left as a Filter -- per page, one full-corpus probe
-- set. On the QA-shaped copy that is ~11.5k probes over a 93M-row/193GB heap
-- with IO:DataFileRead waits and a 3423s statement age against a 1h handler
-- bound, while 33 concurrent retries restampede the same scan.
--
-- This partial index matches the rewritten drive-from-fact statements
-- exactly: the WHERE kinds plus NOT is_tombstone are the index predicate,
-- and the key (observed_at, fact_id) is the ORDER BY and the cursor row
-- comparison, so a page is one index seek plus a short forward scan.
--
-- Deciding experiment (#11082 ruling decision 2), disposable Postgres 18,
-- 1.2M fact_records rows, 1200 generations sharing one observed_at per
-- generation, 1200 active + 1500 inactive interleaved target-kind rows, tombstones
-- present, plan_cache_mode = force_generic_plan (the mode the services run):
--
--   arm 0 (folded statement, no new index): 3 pages, full drain 3573ms;
--     page 1 EXPLAIN (ANALYZE, BUFFERS) 528ms, 145647 buffers, nested loop
--     over 600 generation probes, kind/tombstone/cursor-OR as Filter with
--     Rows Removed by Filter: 1798 per probe set
--   arm A (split statements, no new index): 3 pages, 2135ms; planner keeps
--     the generation-driven nested loop (85k-145k buffers/page) -- the split
--     alone has no access path to use. REJECTED.
--   arm B (split join form + this index): partial index never chosen;
--     planner keeps the probe loop. REJECTED.
--   arm C (MATERIALIZED CTE with inner LIMIT + this index): 1.5ms/page but
--     the drain stops after 204 of 1200 rows -- a pre-join LIMIT with the
--     short-post-join-page stop silently drops rows. REJECTED as incorrect.
--   arm C2 (MATERIALIZED CTE, no inner LIMIT + this index): 3 pages,
--     md5-identical to arm 0 including inactive-generation exclusion;
--     page 1 7.5ms / 4726 buffers, page 2 4.1ms / 2721 buffers,
--     page 3 1.3ms, row comparison as Index Cond, no Filter rows-removed
--     scaling with corpus size. SHIPPED.
--
-- Dropping MATERIALIZED flattens arm C2 back into the probe loop (60.7ms vs
-- 7.5ms per page on the same fixture), so the keyword is load-bearing and is
-- pinned by the hermetic shape test.
--
-- Index size on the fixture is 128kB for 2700 matching rows (vs 205MB for the
-- scope+generation keyset index over the same table); write churn is bounded
-- by the three listed kinds, which are a small fraction of ingest.
--
-- Deliberately no ANALYZE. This is a plain-column btree, so the planner does
-- not need fresh statistics to use it -- same reasoning as 099. Verified
-- rather than assumed: the C2 plans above were captured immediately after
-- CREATE INDEX with no ANALYZE, under a forced generic plan.
--
-- CONCURRENTLY, so building it does not block writes to a table under
-- continuous ingest. Same pattern as 099_fact_records_keyset_index.sql and
-- 105_fact_records_identity_epoch_idx_v2.sql on this table. The usual
-- objection to CONCURRENTLY -- that a failed build leaves an INVALID index
-- which IF NOT EXISTS then skips forever -- does not apply here: the schema
-- apply path drops invalid concurrent indexes by name before executing each
-- definition (SQLDB.dropInvalidConcurrentIndexes, db.go), and it runs each
-- statement outside any transaction on a dedicated bootstrap connection,
-- which CONCURRENTLY requires.
CREATE INDEX CONCURRENTLY IF NOT EXISTS fact_records_package_ownership_active_idx
    ON fact_records (observed_at, fact_id)
    WHERE fact_kind IN (
        'package_registry.package',
        'reducer_package_ownership_correlation',
        'reducer_package_publication_correlation'
    )
    AND is_tombstone = FALSE;
