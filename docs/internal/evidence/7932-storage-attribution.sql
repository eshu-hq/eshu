-- SPDX-License-Identifier: MIT
-- Copyright (c) 2025-2026 eshu-hq
--
-- #7932 bounded, read-only storage attribution for generation history and the
-- search index. Report and limits: docs/internal/evidence/7932-storage-audit.md.
--
-- Run it on a read replica or a quiet primary, as a role that can SELECT the
-- listed tables. It writes nothing: one READ ONLY transaction, a statement and a
-- lock timeout, no VACUUM, no DDL.
--
--   psql "$DSN" -X -v sample=40 -v soft_hours=168 -v min_newer=24 \
--        -v hard_hours=2160 -f 7932-storage-attribution.sql
--
-- Variables (defaults are the shipped retention defaults, go/cmd/reducer
-- config.go; pass the deployed values, for example hard_hours=87600):
--   sample      generations sampled per category for the row-count probe (40)
--   soft_hours  ESHU_GENERATION_RETENTION_MAX_SUPERSEDED_AGE in whole hours (168)
--   min_newer   ESHU_GENERATION_RETENTION_MIN_SUPERSEDED_GENERATIONS (24)
--   hard_hours  ESHU_GENERATION_RETENTION_HARD_MAX_SUPERSEDED_AGE in hours (2160)
--
-- Cost bound: section 1 reads the catalog; section 3 reads the retention
-- event ledger over 14 days. Section 2 reads scope_generations and counts rows
-- for at most (sample x categories) generations, each through its
-- (scope_id, generation_id) index prefix; that count is the only part whose
-- cost grows with generation size. Lower `sample` before running it on a
-- loaded primary. If the 60 s timeout aborts section 2, lower `sample` and rerun;
-- section 1 has already printed. Needs psql 13+ (`\if :{?var}`) and server 13+
-- (`pg_partition_root`).
--
-- The category predicate mirrors generationRetentionCandidateQuery
-- (go/internal/storage/postgres/generation_retention_sql.go, ranked_superseded_
-- generations / eligible CTEs) at origin/main 329b630a4. If that query changes,
-- re-derive this one. The category `superseded_eligible` is correct for that
-- commit only: open PR #8139 (delta-base retention guard, #7802) will make
-- retention keep delta bases, after which this script over-counts eligible
-- generations until it is re-derived.

\set ON_ERROR_STOP on
\if :{?sample} \else \set sample 40 \endif
\if :{?soft_hours} \else \set soft_hours 168 \endif
\if :{?min_newer} \else \set min_newer 24 \endif
\if :{?hard_hours} \else \set hard_hours 2160 \endif

BEGIN READ ONLY;
SET LOCAL statement_timeout = '60s';
SET LOCAL lock_timeout = '1s';
SET LOCAL idle_in_transaction_session_timeout = '120s';

-- 1. Relation bytes by family: heap, indexes and TOAST, partitions rolled up to
-- their root. Catalog and file-size reads only. reltuples is a planner
-- estimate; a negative value means "never analyzed" and is reported as NULL.
\echo '== 1. relation bytes by family (top 25)'
WITH rel AS (
    SELECT c.oid,
           COALESCE(pg_partition_root(c.oid), c.oid) AS root_oid,
           pg_relation_size(c.oid) AS heap_bytes,
           pg_indexes_size(c.oid) AS index_bytes,
           CASE WHEN c.reltoastrelid <> 0 THEN pg_total_relation_size(c.reltoastrelid) ELSE 0 END AS toast_bytes,
           GREATEST(c.reltuples, 0)::bigint AS est_rows
    FROM pg_class c
    JOIN pg_namespace n ON n.oid = c.relnamespace
    WHERE c.relkind IN ('r', 'm')
      AND n.nspname = current_schema()
)
SELECT root.relname AS family,
       count(*) AS relations,
       pg_size_pretty(sum(rel.heap_bytes + rel.index_bytes + rel.toast_bytes)) AS total,
       pg_size_pretty(sum(rel.heap_bytes)) AS heap,
       pg_size_pretty(sum(rel.index_bytes)) AS indexes,
       pg_size_pretty(sum(rel.toast_bytes)) AS toast,
       round(100.0 * sum(rel.index_bytes) / NULLIF(sum(rel.heap_bytes + rel.index_bytes + rel.toast_bytes), 0), 1) AS index_pct,
       sum(rel.est_rows) AS est_rows
FROM rel
JOIN pg_class root ON root.oid = rel.root_oid
GROUP BY root.relname
ORDER BY sum(rel.heap_bytes + rel.index_bytes + rel.toast_bytes) DESC
LIMIT 25;

-- 2. Generation lifecycle census with a sampled per-generation footprint, one
-- row per category. The categories are mutually exclusive and ordered; the
-- order is the order of the CASE below.
--   active                       the scope's active pointer
--   pending / failed / other     non-superseded, non-active statuses
--   superseded_no_timestamp      superseded with NULL superseded_at (retention cannot select it)
--   superseded_pinned_live_work  claimed/running/retrying work item names it
--   superseded_uncovered_writer  wrote, never activated, newer than the last full activation
--   superseded_eligible          meets the soft age + rank/same-SHA arm, or the hard ceiling
--   superseded_retained          superseded, none of the above: kept by policy
--
-- The footprint columns come from a deterministic sample (ordered by
-- md5(generation_id)) of at most :sample generations per category, counted in
-- the three generation-keyed tables that dominate storage. Counts are exact for
-- the sampled generations; a category total is sample mean x category size and
-- carries sampling error (sd_* shows the spread). A category with fewer
-- generations than :sample is counted in full. One statement, because a temp
-- table cannot be created inside a READ ONLY transaction.
\echo '== 2. generation lifecycle census and sampled footprint'
WITH ranked AS (
    SELECT g.scope_id, g.generation_id, g.status, g.superseded_at, g.is_delta,
           g.activated_at, g.projection_write_started_at,
           COALESCE(g.source_commit_sha, '') AS source_commit_sha,
           COALESCE(g.freshness_hint, '') AS freshness_hint,
           -- Partitioning by the "rankable" flag ranks only the superseded rows
           -- that carry a timestamp, as retention's WHERE does; the rank is read
           -- only for those rows.
           ROW_NUMBER() OVER (PARTITION BY g.scope_id,
                                           (g.status = 'superseded' AND g.superseded_at IS NOT NULL)
                              ORDER BY g.superseded_at DESC, g.generation_id DESC) AS superseded_rank
    FROM scope_generations g
),
last_full AS (
    SELECT DISTINCT ON (g.scope_id) g.scope_id, g.projection_write_started_at AS full_write_started_at
    FROM scope_generations g
    WHERE g.is_delta = false AND g.activated_at IS NOT NULL
    ORDER BY g.scope_id, g.ingested_at DESC, g.generation_id DESC
),
live_work AS (
    SELECT DISTINCT w.generation_id
    FROM fact_work_items w
    WHERE w.status IN ('claimed', 'running', 'retrying')
),
categorized AS (
    SELECT r.scope_id, r.generation_id, r.superseded_at,
           CASE
             WHEN r.generation_id = s.active_generation_id THEN 'active'
             WHEN r.status = 'pending' THEN 'pending'
             WHEN r.status = 'failed' THEN 'failed'
             WHEN r.status <> 'superseded' THEN 'other_status'
             WHEN r.superseded_at IS NULL THEN 'superseded_no_timestamp'
             WHEN lw.generation_id IS NOT NULL THEN 'superseded_pinned_live_work'
             WHEN r.projection_write_started_at IS NOT NULL AND r.activated_at IS NULL
                  AND r.projection_write_started_at > COALESCE(lf.full_write_started_at, '-infinity'::timestamptz)
                  THEN 'superseded_uncovered_writer'
             WHEN (r.superseded_at < now() - make_interval(hours => :soft_hours)
                   AND (r.superseded_rank > :min_newer
                        OR (r.source_commit_sha <> '' AND EXISTS (
                              SELECT 1 FROM scope_generations n
                              WHERE n.scope_id = r.scope_id
                                AND n.generation_id <> r.generation_id
                                AND COALESCE(n.source_commit_sha, '') = r.source_commit_sha
                                AND n.is_delta = r.is_delta
                                AND COALESCE(n.freshness_hint, '') = r.freshness_hint
                                AND (n.generation_id = s.active_generation_id
                                     OR (n.status = 'superseded' AND n.superseded_at IS NOT NULL
                                         AND (n.superseded_at, n.generation_id) > (r.superseded_at, r.generation_id)))))))
                  OR r.superseded_at < now() - make_interval(hours => :hard_hours)
                  THEN 'superseded_eligible'
             ELSE 'superseded_retained'
           END AS category
    FROM ranked r
    JOIN ingestion_scopes s ON s.scope_id = r.scope_id
    LEFT JOIN last_full lf ON lf.scope_id = r.scope_id
    LEFT JOIN live_work lw ON lw.generation_id = r.generation_id
),
census AS (
    SELECT category,
           count(*) AS generations,
           count(DISTINCT scope_id) AS scopes,
           min(superseded_at) AS oldest_superseded_at,
           max(superseded_at) AS newest_superseded_at
    FROM categorized
    GROUP BY category
),
picked AS (
    SELECT category, scope_id, generation_id
    FROM (
        SELECT c.category, c.scope_id, c.generation_id,
               ROW_NUMBER() OVER (PARTITION BY c.category ORDER BY md5(c.generation_id)) AS n
        FROM categorized c
    ) x
    WHERE n <= :sample
),
counted AS (
    SELECT p.category,
           (SELECT count(*) FROM fact_records f
             WHERE f.scope_id = p.scope_id AND f.generation_id = p.generation_id) AS facts,
           (SELECT count(*) FROM eshu_search_index_documents d
             WHERE d.scope_id = p.scope_id AND d.generation_id = p.generation_id) AS documents,
           (SELECT count(*) FROM eshu_search_index_terms t
             WHERE t.scope_id = p.scope_id AND t.generation_id = p.generation_id) AS terms
    FROM picked p
),
footprint AS (
    SELECT category,
           count(*) AS sampled,
           round(avg(facts), 1) AS mean_facts,
           round(stddev_samp(facts), 1) AS sd_facts,
           round(avg(documents), 1) AS mean_documents,
           round(avg(terms), 1) AS mean_terms,
           round(stddev_samp(terms), 1) AS sd_terms,
           max(terms) AS max_terms
    FROM counted
    GROUP BY category
)
SELECT c.category, c.generations, c.scopes, c.oldest_superseded_at, c.newest_superseded_at,
       f.sampled, f.mean_facts, f.sd_facts, f.mean_documents, f.mean_terms, f.sd_terms, f.max_terms
FROM census c
JOIN footprint f USING (category)
ORDER BY c.generations DESC;

-- 3. Retention history already recorded: pruned generations and cascaded rows
-- per day and reason, from the event ledger (hashed ids only). The ledger has no
-- index that leads with pruned_at (reason_idx is (reason, pruned_at)), so this may
-- scan it; it stays cheap while retention is held and the statement timeout
-- bounds it.
\echo '== 3. retention events, last 14 days'
SELECT date_trunc('day', pruned_at) AS day,
       reason,
       count(*) AS generations_pruned,
       sum((row_counts ->> 'fact_records')::bigint) AS fact_rows,
       sum((row_counts ->> 'eshu_search_index_terms')::bigint) AS term_rows
FROM generation_retention_events
WHERE pruned_at >= now() - interval '14 days'
GROUP BY 1, 2
ORDER BY 1 DESC, 2;

ROLLBACK;
