-- SPDX-License-Identifier: MIT
-- Copyright (c) 2025-2026 eshu-hq

-- #7126: the documentation story's source-only count
-- (go/internal/query/documentation_source_only.go,
-- buildDocumentationSourceOnlySQL) counts active documentation facts that carry
-- no structured target refs. With no index for that predicate it read every
-- documentation fact of every active generation and filtered payload refs out
-- of the heap: 1.3-4.5 s on the measured corpus.
--
-- Partial over exactly the statement's predicate: the four counted kinds as
-- SQL literals, not tombstoned, and none of candidate_refs / evidence_refs /
-- linked_entities is a non-empty array (a missing or JSON-null key means no
-- refs). Rows that carry refs, and every other fact kind, pay no index
-- maintenance. The predicate text is byte-identical to the one the query
-- renders over the bare payload column
-- (TestDocumentationSourceOnlyIndexMatchesQuery binds them), so Postgres proves
-- the query implies the index predicate in custom and generic plans alike. The
-- kinds must stay literals for that: `fact_kind = ANY($1)` cannot be proven
-- against this predicate until the parameter is known.
--
-- scope_id, generation_id, fact_kind lead so the active-generation join probes
-- the index directly and the per-kind counts are index-only.
--
-- CONCURRENTLY so bootstrap never blocks writers; IF NOT EXISTS for rerun
-- safety, matching the 073/086/118/121 precedent. One statement per file so the
-- migration coordinator can run it outside a transaction
-- (coordination.IsSoleConcurrentIndexStatement). A NEW migration file per
-- issue #7002: never edit an applied migration.
CREATE INDEX CONCURRENTLY IF NOT EXISTS fact_records_documentation_source_only_idx
    ON fact_records (scope_id, generation_id, fact_kind)
    WHERE fact_kind IN ('documentation_source', 'documentation_document', 'documentation_section', 'documentation_link')
      AND is_tombstone = FALSE
      AND NOT (
            COALESCE(jsonb_typeof(payload->'candidate_refs') = 'array' AND payload->'candidate_refs' <> '[]'::jsonb, FALSE)
         OR COALESCE(jsonb_typeof(payload->'evidence_refs') = 'array' AND payload->'evidence_refs' <> '[]'::jsonb, FALSE)
         OR COALESCE(jsonb_typeof(payload->'linked_entities') = 'array' AND payload->'linked_entities' <> '[]'::jsonb, FALSE)
      );
