-- SPDX-License-Identifier: MIT
-- Copyright (c) 2025-2026 eshu-hq

-- #10874 (epic #10860): make autovacuum keep up with the search index terms
-- leaves and the documents table by scaling its triggers to each leaf's
-- churn instead of its row count. On live QA the p29 leaf holds 193 GB at
-- about 500 KB per live row and p07 37 GB at about 40 KB per row, while a
-- healthy leaf (p03) holds 32 GB at about 1.5 KB per row: the files sit at
-- the high-water mark of the historical ~99%-superseded backlog (#7932) and
-- neither heap nor btree files ever shrink under plain VACUUM. The writer
-- DELETE-all + COPY-fresh cycle per generation build and the batched
-- retention drains (predrain, orphan sweep, #8231 superseded sweep) churn
-- dead rows and dead index entries continuously; the 0.2 defaults leave the
-- dead-row trigger at 20% of each leaf, so vacuum arrives days late on the
-- hot leaves.
--
-- 0.02 puts the dead-row trigger at about 2% of live rows and 0.05 puts the
-- insert trigger at about 5%: the same starting point as the fact_records
-- reloptions (migration 279, #9975). The final scale factors are set from
-- the primary-side pg_stat read (n_dead_tup, n_ins_since_vacuum per leaf),
-- which the read-only QA reader grant cannot see; recalibrate against the
-- all-64-leaf distribution measured on the copy before locking constants.
--
-- Deliberately not set, because each is an unmeasured theory on a primary
-- that is already write-loaded: autovacuum_vacuum_cost_delay and
-- autovacuum_vacuum_cost_limit (the first pass is sized by the server
-- defaults), any toast.* value (the TOAST relation inherits these when
-- unset), and any analyze knob (autoanalyze is not the defect). No
-- fillfactor change: the workload is insert-mostly with per-generation
-- bulk loads, and lowering it inflates the heap. No VACUUM calls from
-- writer code.
--
-- SET on autovacuum storage parameters takes SHARE UPDATE EXCLUSIVE. That
-- lock does not conflict with INSERT, UPDATE, DELETE, or SELECT, and the
-- statement changes only the catalog: no rewrite, no new files. Parent
-- reloptions do not govern existing leaves' autovacuum, so the loop sets
-- each leaf explicitly; a replay leaves the options unchanged but cancels
-- and restarts a vacuum that is still running, so do not re-issue this
-- statement while the first vacuum runs. Fresh installs whose terms table
-- is absent (deferred search-index variant) set nothing and change nothing.
-- Keep this the only statement in the file. There is no reverse migration;
-- a later migration would RESET either parameter if ever needed.
DO $$
DECLARE
    leaf TEXT;
BEGIN
    FOR leaf IN
        SELECT child.oid::regclass::text
        FROM pg_inherits AS inheritance
        JOIN pg_class AS child ON child.oid = inheritance.inhrelid
        WHERE inheritance.inhparent = to_regclass('eshu_search_index_terms')
    LOOP
        EXECUTE format(
            'ALTER TABLE %s SET (autovacuum_vacuum_scale_factor = 0.02, autovacuum_vacuum_insert_scale_factor = 0.05)',
            leaf
        );
    END LOOP;

    IF to_regclass('eshu_search_index_documents') IS NOT NULL THEN
        ALTER TABLE eshu_search_index_documents SET (
            autovacuum_vacuum_scale_factor = 0.02,
            autovacuum_vacuum_insert_scale_factor = 0.05
        );
    END IF;
END
$$;
