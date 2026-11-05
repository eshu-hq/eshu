-- SPDX-License-Identifier: MIT
-- Copyright (c) 2025-2026 eshu-hq

-- #10005 / #10003 row 3 (Crossplane SATISFIED_BY reads): the XRD read
-- (listActiveCrossplaneXRDFactsQuery) and its derived delta-base read join
-- every reached generation's fact rows through
-- fact_records_scope_generation_idx and filter each heap row for a
-- CrossplaneXRD content entity. No index served that predicate, so both
-- reads cost every fact row of every reached generation.
--
-- Shim (disposable PostgreSQL 18.6, 420 git scopes, 40 delta-active,
-- 472,600 fact rows): the active read went from 70.9-190.6 ms to 1.2 ms,
-- the derived base read from 167.7-312.7 ms (156,108 buffers) to 0.8 ms, the
-- all-full active read from 242.0 ms to 2.2 ms. The index is 32 kB there: it
-- holds one entry per XRD content entity only. Fixture timings, not a
-- deployed claim. Evidence:
-- docs/internal/evidence/10005-crossplane-delta-overlay.md.
--
-- One statement per file is required for a concurrent build; fact writers
-- continue during it (SHARE UPDATE EXCLUSIVE). The predicate is the reader
-- text's load-bearing filter modulo the table alias, so the planner proves
-- the implication for the active and the derived base text alike; keep the
-- reader text, the Go DDL entry (crossplaneXRDFactRecordReadIndexesSQL), and
-- this predicate in lockstep. Write cost is one predicate evaluation per
-- inserted fact, and a B-tree entry only for an XRD content entity.
CREATE INDEX CONCURRENTLY IF NOT EXISTS fact_records_crossplane_xrd_entity_idx
    ON fact_records (scope_id, generation_id, observed_at, fact_id)
    WHERE fact_kind = 'content_entity'
      AND source_system = 'git'
      AND is_tombstone = FALSE
      AND (
          payload->>'entity_type' = 'CrossplaneXRD'
          OR payload->>'entity_kind' = 'CrossplaneXRD'
      );
