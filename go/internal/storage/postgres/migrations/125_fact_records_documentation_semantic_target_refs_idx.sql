-- SPDX-License-Identifier: MIT
-- Copyright (c) 2025-2026 eshu-hq

-- #7126: the documentation target-facts read
-- (go/internal/query/documentation_target_read_model.go,
-- buildDocumentationTargetFactsSQL) is a UNION ALL of two bounded branches. The
-- indexed branch (mention and claim kinds) is served by
-- fact_records_documentation_target_refs_idx. The semantic branch reads
-- semantic.documentation_observation, which that index deliberately does not
-- cover, so it had no usable index: a heap scan filtered by the payload
-- containment predicate, about 225 ms warm of the 301 ms statement on the QA
-- corpus (7,370 index probes, zero rows) and the residual fixed cost of both
-- stories.
--
-- This is the same partial GIN as the target-refs index (same jsonb_path_ops
-- opclass and payload expression, so the branch's `payload @>` containment
-- predicates use it identically), restricted to the semantic kind and
-- non-tombstoned facts. The branch's kind and tombstone predicates are literal,
-- so Postgres proves the index predicate in custom and generic plans alike
-- (TestDocumentationSemanticTargetRefsIndexMatchesQuery binds the kind to the
-- query). Only semantic observation facts pay index maintenance.
--
-- CONCURRENTLY so bootstrap never blocks writers; IF NOT EXISTS for rerun
-- safety, matching the 073/086/118/121/123/124 precedent. One statement per
-- file so the migration coordinator can run it outside a transaction
-- (coordination.IsSoleConcurrentIndexStatement). A NEW migration file per
-- issue #7002: never edit an applied migration.
CREATE INDEX CONCURRENTLY IF NOT EXISTS fact_records_documentation_semantic_target_refs_idx
    ON fact_records USING GIN (payload jsonb_path_ops)
    WHERE fact_kind = 'semantic.documentation_observation'
      AND is_tombstone = FALSE;
