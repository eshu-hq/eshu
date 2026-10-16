-- SPDX-License-Identifier: MIT
-- Copyright (c) 2025-2026 eshu-hq
--
-- #7932 seeded fixture for 7932-storage-attribution.sql: every lifecycle
-- category at a known count, so the census output can be checked by eye and by
-- the expected table below. Apply to an empty database that already has the
-- production schema (go/cmd/bootstrap-data-plane), then run the attribution
-- script with the shipped defaults (sample=40 soft_hours=168 min_newer=24
-- hard_hours=2160).
--
-- One-shot: rerunning it on the same database fails on duplicate keys.
--
-- Expected census (generations per category):
--   active 7 (6 seeded + the schema's own eshu:global genesis scope) | superseded_eligible 13 | superseded_retained 29
--   superseded_pinned_live_work 2 | superseded_uncovered_writer 2
--   superseded_no_timestamp 1 | pending 1 | failed 1
--
--   S1  30 superseded, 8..37 days old: ranks 1..24 retained, ranks 25..30 eligible (6)
--   S2  5 superseded, 1..5 days old: younger than the soft age, retained (5);
--       plus one pending and one failed generation
--   S3  3 superseded, 100 days old: eligible by the hard ceiling (3)
--   S4  2 superseded, 100 days old, each named by a running work item: pinned (2)
--   S5  2 superseded, 100 days old, wrote but never activated: uncovered writers (2)
--   S6  4 superseded, 10..13 days old, same commit SHA as the active generation:
--       eligible by the same-SHA arm (4); plus one superseded row with NULL superseded_at
-- Per generation: 10 facts, 5 documents, 50 terms (active generations: 20/10/100).

\set ON_ERROR_STOP on

CREATE OR REPLACE FUNCTION pg_temp.seed_generation(
    p_scope text, p_gen text, p_status text, p_age_days int, p_sha text,
    p_activated boolean, p_write_started boolean, p_facts int, p_docs int, p_terms int)
RETURNS void LANGUAGE plpgsql AS $$
BEGIN
    INSERT INTO scope_generations (generation_id, scope_id, trigger_kind, observed_at, ingested_at, status,
                                   activated_at, superseded_at, source_commit_sha, projection_write_started_at)
    VALUES (p_gen, p_scope, 'snapshot', now() - make_interval(days => p_age_days + 1),
            now() - make_interval(days => p_age_days + 1), p_status,
            CASE WHEN p_activated THEN now() - make_interval(days => p_age_days + 1) END,
            CASE WHEN p_status = 'superseded' AND p_age_days >= 0 THEN now() - make_interval(days => p_age_days) END,
            p_sha,
            CASE WHEN p_write_started THEN now() - make_interval(days => p_age_days + 1) END);
    INSERT INTO fact_records (fact_id, scope_id, generation_id, fact_kind, stable_fact_key, source_system,
                              source_fact_key, observed_at, ingested_at, payload)
    SELECT p_gen || '-f' || i, p_scope, p_gen, 'content_entity', 'k-' || p_gen || '-' || i, 'git',
           'sf-' || i, now(), now(), '{}'::jsonb
    FROM generate_series(1, p_facts) i;
    INSERT INTO eshu_search_index_documents (scope_id, generation_id, document_id, fact_id, repo_id,
                                             source_kind, document, document_length, updated_at)
    SELECT p_scope, p_gen, 'd' || i, p_gen || '-f' || i, 'repo-' || p_scope, 'content_entity',
           '{}'::jsonb, 10, now()
    FROM generate_series(1, p_docs) i;
    INSERT INTO eshu_search_index_terms (scope_id, generation_id, document_id, term_key, term, term_frequency)
    SELECT p_scope, p_gen, 'd' || (1 + i % p_docs), 't' || i, 't' || i, 1
    FROM generate_series(1, p_terms) i;
END $$;

INSERT INTO ingestion_scopes (scope_id, scope_kind, source_system, source_key, collector_kind, partition_key,
                              observed_at, ingested_at, status, active_generation_id)
SELECT 'S' || s, 'repository', 'git', 'src-S' || s, 'git', 'p' || s, now(), now(), 'active', NULL
FROM generate_series(1, 6) s;

DO $$
DECLARE s int; i int;
BEGIN
    -- active generation per scope (S6's carries the SHA the duplicates share)
    FOR s IN 1..6 LOOP
        PERFORM pg_temp.seed_generation('S' || s, 'S' || s || '-active', 'active', 0,
                                        CASE WHEN s = 6 THEN 'sha-dup' END, true, false, 20, 10, 100);
        UPDATE ingestion_scopes SET active_generation_id = 'S' || s || '-active' WHERE scope_id = 'S' || s;
    END LOOP;
    FOR i IN 1..30 LOOP   -- S1: 8..37 days
        PERFORM pg_temp.seed_generation('S1', 'S1-sup-' || i, 'superseded', 7 + i, NULL, true, false, 10, 5, 50);
    END LOOP;
    FOR i IN 1..5 LOOP    -- S2: 1..5 days
        PERFORM pg_temp.seed_generation('S2', 'S2-sup-' || i, 'superseded', i, NULL, true, false, 10, 5, 50);
    END LOOP;
    PERFORM pg_temp.seed_generation('S2', 'S2-pending', 'pending', 0, NULL, false, false, 10, 5, 50);
    PERFORM pg_temp.seed_generation('S2', 'S2-failed', 'failed', 0, NULL, false, false, 10, 5, 50);
    FOR i IN 1..3 LOOP    -- S3: past the hard ceiling
        PERFORM pg_temp.seed_generation('S3', 'S3-sup-' || i, 'superseded', 100, NULL, true, false, 10, 5, 50);
    END LOOP;
    FOR i IN 1..2 LOOP    -- S4: pinned by running work
        PERFORM pg_temp.seed_generation('S4', 'S4-sup-' || i, 'superseded', 100, NULL, true, false, 10, 5, 50);
        INSERT INTO fact_work_items (work_item_id, scope_id, generation_id, stage, domain, conflict_key, status, created_at, updated_at)
        VALUES ('wi-S4-' || i, 'S4', 'S4-sup-' || i, 'reducer', 'eshu_search_document', 'S4-sup-' || i, 'running', now(), now());
    END LOOP;
    FOR i IN 1..2 LOOP    -- S5: wrote, never activated
        PERFORM pg_temp.seed_generation('S5', 'S5-sup-' || i, 'superseded', 100, NULL, false, true, 10, 5, 50);
    END LOOP;
    FOR i IN 1..4 LOOP    -- S6: same SHA as the active generation
        PERFORM pg_temp.seed_generation('S6', 'S6-dup-' || i, 'superseded', 9 + i, 'sha-dup', true, false, 10, 5, 50);
    END LOOP;
    PERFORM pg_temp.seed_generation('S6', 'S6-nots', 'superseded', -1, NULL, true, false, 10, 5, 50);
END $$;

ANALYZE;
