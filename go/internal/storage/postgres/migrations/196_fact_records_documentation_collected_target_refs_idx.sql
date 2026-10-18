-- SPDX-License-Identifier: MIT
-- Copyright (c) 2025-2026 eshu-hq

-- #7098 slice B: the documentation-facts read
-- (go/internal/query/documentation_read_model.go, buildDocumentationFactsSQL)
-- anchors repository and target reads with `payload @>` containment ORs. The
-- pre-change partial GIN fact_records_documentation_target_refs_idx covers
-- only documentation_entity_mention, documentation_claim_candidate, and
-- documentation_finding, so the dominant collected kinds --
-- documentation_document, documentation_section, and documentation_link --
-- have no usable index: a heap scan filtered by the containment predicate
-- behind an active_join over every ingestion scope (1.5-2.5 s warm on the QA
-- corpus for GET /api/v0/documentation/facts).
--
-- This is the same partial GIN shape as the target-refs index (same
-- jsonb_path_ops opclass and payload expression, so a branch's `payload @>`
-- containment predicates use it identically), restricted to the three
-- collected kinds and non-tombstoned facts. Single-kind document, section,
-- and link reads bind their kind as a literal, so Postgres proves the index
-- predicate in custom and generic plans alike
-- (TestBuildDocumentationFactsSQLBindsCollectedKindsLiterally). Only
-- document, section, and link facts pay index maintenance.
--
-- CONCURRENTLY so bootstrap never blocks writers; IF NOT EXISTS for rerun
-- safety, matching the 073/086/118/121/122/123/124 precedent. One statement
-- per file so the migration coordinator can run it outside a transaction
-- (coordination.IsSoleConcurrentIndexStatement). A NEW migration file per
-- issue #7002: never edit an applied migration.
CREATE INDEX CONCURRENTLY IF NOT EXISTS fact_records_documentation_collected_target_refs_idx
    ON fact_records USING GIN (payload jsonb_path_ops)
    WHERE fact_kind IN ('documentation_document', 'documentation_section', 'documentation_link')
      AND is_tombstone = FALSE;
