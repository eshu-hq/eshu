// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

// crossplaneXRDFactRecordReadIndexesSQL serves the Crossplane SATISFIED_BY
// XRD read (listActiveCrossplaneXRDFactsQuery) and its derived delta-base
// read (#10005): the (scope_id, generation_id) prefix both join on, then the
// (observed_at, fact_id) keyset order, over XRD content entities only. The
// predicate is the reader text's load-bearing filter modulo the table alias,
// so the planner can prove the implication for both texts; keep the reader
// text, migration 280, and this DDL in lockstep
// (TestCrossplaneXRDIndexPredicateMatchesTheReader).
const crossplaneXRDFactRecordReadIndexesSQL = `
CREATE INDEX IF NOT EXISTS fact_records_crossplane_xrd_entity_idx
    ON fact_records (scope_id, generation_id, observed_at, fact_id)
    WHERE fact_kind = 'content_entity'
      AND source_system = 'git'
      AND is_tombstone = FALSE
      AND (
          payload->>'entity_type' = 'CrossplaneXRD'
          OR payload->>'entity_kind' = 'CrossplaneXRD'
      );
`
