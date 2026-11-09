-- SPDX-License-Identifier: MIT
-- Copyright (c) 2025-2026 eshu-hq

-- #10408: the member-set arm of the holder fan-out lookup. A holder whose
-- group id the committing pass never saw (a private advisory id under a
-- divergent alias closure) still carries the shared member in its stored
-- advisory_group_members, so the lookup matches the stored set against the
-- fanned-out ids with ?|. GIN like the sibling
-- fact_records_security_alert_cve_ids_idx array arm. The bootstrap fragment
-- lives in schema_fact_records_supply_chain_impact_indexes.go; this migration
-- builds the index on existing deployments without blocking writers. Single
-- statement: a concurrent build cannot run inside a transaction block.
CREATE INDEX CONCURRENTLY IF NOT EXISTS fact_records_supply_chain_impact_group_members_idx
    ON fact_records USING GIN ((payload #> '{provenance,advisory_group_members}'))
    WHERE fact_kind = 'reducer_supply_chain_impact_finding'
      AND is_tombstone = FALSE;
