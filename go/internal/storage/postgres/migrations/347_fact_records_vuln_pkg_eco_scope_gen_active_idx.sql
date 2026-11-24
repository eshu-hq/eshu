-- SPDX-License-Identifier: MIT
-- Copyright (c) 2025-2026 eshu-hq

-- #11376: the ecosystem arm of the package-keyed holder read
-- (FindPackageHolderCandidatesQuery in
-- storage/postgres/supply_chain_impact_package_holders.go). With many
-- scopes the planner bitmap-scanned the whole affected-package partial
-- index, superseded rows included, and filtered the folded ecosystem on
-- the heap, breaching the 10 s holder-read statement guard. This partial
-- index leads with the folded ecosystem expression byte-identical to the
-- query predicate, then the active-generation join columns, so the
-- requested spellings ride as an Index Cond. The bootstrap fragment lives
-- in schema_fact_records_supply_chain_impact_indexes.go; this migration
-- builds the index on existing deployments without blocking writers.
-- Single statement: a concurrent build cannot run inside a transaction
-- block. This is a new narrow partial index, not a covering rebuild, so
-- the H-3 no-build stand (#10646) is unaffected.
CREATE INDEX CONCURRENTLY IF NOT EXISTS fact_records_vuln_pkg_eco_scope_gen_active_idx
    ON fact_records (
        ((LOWER(BTRIM(payload->>'ecosystem') COLLATE "C")) COLLATE "default"),
        scope_id,
        generation_id
    )
    WHERE fact_kind = 'vulnerability.affected_package'
      AND is_tombstone = FALSE;
