-- SPDX-License-Identifier: MIT
-- Copyright (c) 2025-2026 eshu-hq

-- #10408: the advisory arm of the holder fan-out lookup. A re-key retires
-- group and member ids no replacement row names; a holder keyed by such an
-- id (an advisory-keyed GHSA-only row, a holder whose deciding record names
-- the fragment) matches payload->>'advisory_id' when payload->>'cve_id'
-- misses. The bootstrap fragment lives in
-- schema_fact_records_supply_chain_impact_indexes.go; this migration builds
-- the index on existing deployments without blocking writers. Single
-- statement: a concurrent build cannot run inside a transaction block.
CREATE INDEX CONCURRENTLY IF NOT EXISTS fact_records_supply_chain_impact_advisory_lookup_idx
    ON fact_records (
        (payload->>'advisory_id'),
        (payload->>'impact_status'),
        fact_id ASC,
        generation_id
    )
    WHERE fact_kind = 'reducer_supply_chain_impact_finding'
      AND is_tombstone = FALSE;
