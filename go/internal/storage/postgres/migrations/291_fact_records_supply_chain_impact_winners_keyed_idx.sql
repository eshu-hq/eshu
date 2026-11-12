-- SPDX-License-Identifier: MIT
-- Copyright (c) 2025-2026 eshu-hq

-- #9975: the keyed winners resweep probe index. The probe
-- (buildWinnersKeyedActiveFactsSQL) matches, per dirty identifier, the
-- package triple plus the coalesced c0 (cve_id, else advisory_id) and
-- advisory_id payload expressions. With only the triple-keyed
-- package-lookup index, a sparse tick heap-fetched every candidate of each
-- touched triple as wide rows and discarded most after the fetch (QA:
-- 15,888 fetched rows / 160 MB of payload for 2,720 kept on 200
-- identifiers; build_active_facts p95 17.1s). Putting the two expressions
-- in the index makes all five equalities index conditions, so the heap
-- fetches only kept facts. Cost: one more partial index over the finding
-- rows only (about 1.13M rows / 0.6 GB on QA, against 98 GB of existing
-- fact_records indexes); finding writes maintain it, autovacuum cleans it
-- with its siblings. The bootstrap fragment lives in
-- schema_fact_records_supply_chain_impact_indexes.go; the expressions must
-- stay byte-identical to the probe or the planner cannot use the index.
-- This migration builds it on existing deployments without blocking
-- writers. Single statement: a concurrent build cannot run inside a
-- transaction block.
CREATE INDEX CONCURRENTLY IF NOT EXISTS fact_records_supply_chain_impact_winners_keyed_idx
    ON fact_records (
        (payload->>'package_id'),
        (payload->>'repository_id'),
        (payload->>'subject_digest'),
        (COALESCE(NULLIF(payload->>'cve_id', ''), NULLIF(payload->>'advisory_id', ''), '')),
        (COALESCE(payload->>'advisory_id', '')),
        fact_id ASC,
        generation_id
    )
    WHERE fact_kind = 'reducer_supply_chain_impact_finding'
      AND is_tombstone = FALSE;
