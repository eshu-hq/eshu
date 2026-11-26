-- SPDX-License-Identifier: MIT
-- Copyright (c) 2025-2026 eshu-hq

-- #7584: partial trigram GIN over lower(payload::text) for the targeted
-- anchor-scoped relationship fact load. scoped_facts.go tests
-- lower(fact.payload::text) LIKE ANY($1) over the onboarding delta's alias
-- anchors, limited to the content/file/gcp_cloud_relationship fact kinds.
-- Without a serving index every keyset page runs a full parallel sequential
-- scan with a whole-payload detoast per row, dying at the 300s page guard
-- (bounded_pages.go), and every cursor page re-scans the whole corpus, so
-- the total work is pages x corpus scan. The GIN turns each page into a
-- bitmap index search over only the matching rows; the predicate is
-- unchanged, so the match set is identical by construction.
--
-- The partial predicate mirrors the query's fact_kind filter so kinds the
-- anchor load never reads carry no index entries. pg_trgm ships with the
-- schema since migration 004, and the concurrent-build convention follows
-- migrations 134 and 177.
--
-- This file must remain one concurrent statement: PostgreSQL rejects CREATE
-- INDEX CONCURRENTLY in a transaction block, and the migration coordinator
-- executes sole concurrent-index definitions in autocommit mode. The schema
-- executor clears an invalid same-name index before retrying a failed build.
--
-- Migration proof (local Postgres 18.6, 600k-row / 964MB-total scratch
-- corpus): 10-term anchor page 4890ms sequential scan vs 190ms bitmap index
-- search (~25x, same ratio at 300k rows); 100-term scan 17280ms vs 97ms;
-- keyed-sidecar alternative 505ms. Index build 19.6s, 75MB at that mass.
-- QA is ~103M rows / 579GB total (reltuples estimate 2026-11-26), so expect
-- a proportionally larger one-time CONCURRENTLY build; per-page search cost
-- scales with matches, not corpus size. Exactness is by construction
-- (same predicate); a row-count differential on the scratch corpus matched
-- exactly. Until the index is valid the loader keeps working, only slower:
-- unlike the search tail there is no readiness gate, so no 503 window.
CREATE INDEX CONCURRENTLY IF NOT EXISTS fact_records_anchor_payload_trgm_idx
    ON fact_records USING gin (lower((payload::text)) gin_trgm_ops)
    WHERE fact_kind IN ('content', 'file', 'gcp_cloud_relationship');
